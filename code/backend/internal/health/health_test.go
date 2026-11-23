package health

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ghoul02015/flowpilot/internal/database"
	"github.com/ghoul02015/flowpilot/internal/instance"
)

// fmtSscanf 从 http://127.0.0.1:<port> 提取端口。
func fmtSscanf(address string, port *int) (int, error) {
	return fmt.Sscanf(strings.TrimPrefix(address, "http://127.0.0.1:"), "%d", port)
}

func openStore(t *testing.T) *database.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "health-test.db")
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("打开数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := database.Migrate(db, database.Migrations); err != nil {
		t.Fatalf("迁移失败：%v", err)
	}
	return database.NewStore(db)
}

func TestProbeReadyAndEvents(t *testing.T) {
	store := openStore(t)
	target := instance.Instance{Name: "demo", UUID: "uuid-demo", Status: "running", Port: 0}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"system": map[string]string{"comfyui_version": "v1.2.3"}})
	}))
	defer server.Close()
	var port int
	if _, err := fmtSscanf(server.URL, &port); err != nil {
		t.Fatal(err)
	}
	target.Port = port
	instances := []instance.Instance{target}
	checker := NewChecker(func(context.Context) ([]instance.Instance, error) { return instances, nil }, "127.0.0.1", store, time.Minute)

	checker.probe(target)
	if health := checker.HealthOf(target); health.App != StateReady {
		t.Fatalf("探测成功应为 ready：%+v", health)
	}
	events := store.ListEvents("uuid-demo", 10)
	if len(events) != 1 || events[0].Kind != "ready" {
		t.Fatalf("应记录 ready 事件：%+v", events)
	}
	// 稳定 ready 不重复记事件。
	checker.probe(target)
	checker.probe(target)
	if events := store.ListEvents("uuid-demo", 10); len(events) != 1 {
		t.Fatalf("稳定期不应重复记录：%+v", events)
	}
}

func TestDegradedFailedAndRecovery(t *testing.T) {
	store := openStore(t)
	// 失败服务器（连接拒绝用不存在端口）。
	target := instance.Instance{Name: "demo", UUID: "uuid-demo", Status: "running", Port: 59999}
	instances := []instance.Instance{target}
	checker := NewChecker(func(context.Context) ([]instance.Instance, error) { return instances, nil }, "127.0.0.1", store, time.Minute)

	// 首次失败：starting（未超时）。
	checker.probe(target)
	if h := checker.HealthOf(target); h.App != StateStarting {
		t.Fatalf("启动期失败应保持 starting：%+v", h)
	}
	// 手动置为 ready 模拟曾就绪。
	checker.mu.Lock()
	if state := checker.states["demo"]; state != nil {
		state.app = StateReady
	}
	checker.mu.Unlock()
	checker.probe(target)
	if h := checker.HealthOf(target); h.App != StateDegraded {
		t.Fatalf("就绪后失败应为 degraded：%+v", h)
	}
	// 连续失败至 failed。
	checker.mu.Lock()
	if state := checker.states["demo"]; state != nil {
		state.failures = failedThreshold - 1
	}
	checker.mu.Unlock()
	checker.probe(target)
	if h := checker.HealthOf(target); h.App != StateFailed {
		t.Fatalf("达到阈值应为 failed：%+v", h)
	}
	events := store.ListEvents("uuid-demo", 10)
	kinds := map[string]bool{}
	for _, item := range events {
		kinds[item.Kind] = true
	}
	if !kinds["degraded"] || !kinds["failed"] {
		t.Fatalf("应记录 degraded 与 failed：%+v", events)
	}

	// 恢复：接入健康服务器。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"system": map[string]string{"comfyui_version": "v1"}})
	}))
	defer server.Close()
	var port int
	if _, err := fmtSscanf(server.URL, &port); err != nil {
		t.Fatal(err)
	}
	target.Port = port
	checker.probe(target)
	if h := checker.HealthOf(target); h.App != StateReady {
		t.Fatalf("恢复探测成功应为 ready：%+v", h)
	}
	recovered := store.ListEvents("uuid-demo", 10)[0]
	if recovered.Kind != "recovered" {
		t.Fatalf("恢复应记录 recovered 事件：%+v", recovered)
	}
}

func TestStartTimeout(t *testing.T) {
	store := openStore(t)
	target := instance.Instance{Name: "demo", UUID: "uuid-demo", Status: "running", Port: 59999}
	instances := []instance.Instance{target}
	// 1ms 超时。
	checker := NewChecker(func(context.Context) ([]instance.Instance, error) { return instances, nil }, "127.0.0.1", store, time.Millisecond)
	checker.probe(target)
	// 超时需 starting 起点 + timeout 已过：首 probe 可能未超，等待后重试。
	checker.mu.Lock()
	if state := checker.states["demo"]; state != nil {
		state.firstRunning = time.Now().Add(-time.Hour)
	}
	checker.mu.Unlock()
	checker.probe(target)
	if h := checker.HealthOf(target); h.App != StateFailed {
		t.Fatalf("启动超时应 failed：%+v", h)
	}
	events := store.ListEvents("uuid-demo", 10)
	if len(events) == 0 || events[0].Kind != "failed" {
		t.Fatalf("超时应记录 failed 事件：%+v", events)
	}
}

func TestNotRunningTransition(t *testing.T) {
	store := openStore(t)
	target := instance.Instance{Name: "demo", UUID: "uuid-demo", Status: "stopped", Port: 0}
	instances := []instance.Instance{target}
	checker := NewChecker(func(context.Context) ([]instance.Instance, error) { return instances, nil }, "127.0.0.1", store, time.Minute)
	// 先建立 ready 状态再停止。
	checker.states["demo"] = &trackState{app: StateReady}
	checker.probe(target)
	if h := checker.HealthOf(target); h.App != StateNotRunning {
		t.Fatalf("容器停止应 not_running：%+v", h)
	}
	if events := store.ListEvents("uuid-demo", 10); len(events) != 1 || events[0].Kind != "stopped" {
		t.Fatalf("应记录 stopped 事件：%+v", events)
	}
}

func TestOperationMutex(t *testing.T) {
	store := openStore(t)
	checker := NewChecker(func(context.Context) ([]instance.Instance, error) { return nil, nil }, "127.0.0.1", store, time.Minute)
	checker.MarkStarted("demo")
	if h := checker.HealthOf(instance.Instance{Name: "demo", Status: "running"}); h.App != StateStarting {
		t.Fatalf("MarkStarted 应置 starting：%+v", h)
	}
	checker.MarkStopped("demo")
	if h := checker.HealthOf(instance.Instance{Name: "demo", Status: "stopped"}); h.App != StateNotRunning {
		t.Fatalf("MarkStopped 应置 not_running：%+v", h)
	}
}
