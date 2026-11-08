package download

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var payload = []byte(strings.Repeat("flowpilot-model-weights;", 4096)) // 96 KB

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// rangeServer 支持 Range 与限速的测试源。
func rangeServer(t *testing.T, throttleBytes int64) *httptest.Server {
	t.Helper()
	var served atomic.Int64
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := 0
		if rangeHeader := r.Header.Get("Range"); rangeHeader != "" {
			if _, err := fmt.Sscanf(rangeHeader, "bytes=%d-", &start); err != nil || start < 0 || start >= len(payload) {
				w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
				return
			}
			w.WriteHeader(http.StatusPartialContent)
		}
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(payload)-start))
		for i := start; i < len(payload); i++ {
			w.Write(payload[i : i+1])
			if throttleBytes > 0 && served.Add(1)%throttleBytes == 0 {
				time.Sleep(2 * time.Millisecond)
			}
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		}
	}))
}

func waitStatus(t *testing.T, manager *Manager, id, want string) Task {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		task, err := manager.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if task.Status == want {
			return task
		}
		if task.Status == "failed" && want != "failed" {
			t.Fatalf("任务失败：%s", task.Message)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("状态未变为 %s：%+v", want, manager.tasks[id].task)
	return Task{}
}

func TestDownloadCompletesIntoRepository(t *testing.T) {
	server := rangeServer(t, 0)
	defer server.Close()
	root := t.TempDir()
	manager := NewManager(root)

	task, err := manager.Create(Spec{URL: server.URL + "/models/test.safetensors", Category: "checkpoints", SHA256: sha256Hex(payload)})
	if err != nil {
		t.Fatal(err)
	}
	done := waitStatus(t, manager, task.ID, "completed")
	if done.TotalBytes != int64(len(payload)) || done.DownloadedBytes != int64(len(payload)) {
		t.Fatalf("进度错误：%+v", done)
	}
	content, err := os.ReadFile(filepath.Join(root, "checkpoints", "test.safetensors"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != string(payload) {
		t.Fatal("仓库文件内容不一致")
	}
	if _, err := os.Stat(filepath.Join(root, stagingDirName, task.ID+".part")); !os.IsNotExist(err) {
		t.Fatal("临时分片应已归档")
	}
}

func TestPauseAndResume(t *testing.T) {
	server := rangeServer(t, 256) // 限速，确保能在中途暂停
	defer server.Close()
	root := t.TempDir()
	manager := NewManager(root)

	task, err := manager.Create(Spec{URL: server.URL + "/m.bin", Category: "loras"})
	if err != nil {
		t.Fatal(err)
	}
	// 等到有进度后暂停。
	deadline := time.Now().Add(5 * time.Second)
	for {
		current, _ := manager.Get(task.ID)
		if current.DownloadedBytes > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("下载无进度")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := manager.Action(task.ID, "pause"); err != nil {
		t.Fatal(err)
	}
	paused := waitStatus(t, manager, task.ID, "paused")
	if paused.DownloadedBytes == 0 || paused.DownloadedBytes >= int64(len(payload)) {
		t.Fatalf("暂停时进度异常：%d", paused.DownloadedBytes)
	}

	if _, err := manager.Action(task.ID, "resume"); err != nil {
		t.Fatal(err)
	}
	done := waitStatus(t, manager, task.ID, "completed")
	if done.DownloadedBytes != int64(len(payload)) {
		t.Fatalf("续传后大小错误：%d", done.DownloadedBytes)
	}
	content, _ := os.ReadFile(filepath.Join(root, "loras", "m.bin"))
	if string(content) != string(payload) {
		t.Fatal("续传文件内容不一致")
	}
}

func TestSHA256MismatchFails(t *testing.T) {
	server := rangeServer(t, 0)
	defer server.Close()
	manager := NewManager(t.TempDir())

	task, err := manager.Create(Spec{URL: server.URL + "/m.bin", Category: "vae", SHA256: strings.Repeat("0", 64)})
	if err != nil {
		t.Fatal(err)
	}
	failed := waitStatus(t, manager, task.ID, "failed")
	if !strings.Contains(failed.Message, "SHA256 校验失败") {
		t.Fatalf("应报告校验失败：%s", failed.Message)
	}
	if _, err := os.Stat(filepath.Join(manager.root, "vae", "m.bin")); !os.IsNotExist(err) {
		t.Fatal("校验失败不应写入仓库")
	}
}

func TestCreateValidationAndConflict(t *testing.T) {
	server := rangeServer(t, 0)
	defer server.Close()
	root := t.TempDir()
	manager := NewManager(root)

	var validation *ValidationError
	if _, err := manager.Create(Spec{URL: "ftp://x/y.bin"}); !errors.As(err, &validation) {
		t.Fatalf("非 http URL 应被拒绝：%v", err)
	}
	if _, err := manager.Create(Spec{URL: server.URL + "/x.bin", Category: "Bad Name"}); !errors.As(err, &validation) {
		t.Fatalf("非法类别应被拒绝：%v", err)
	}
	if _, err := manager.Create(Spec{URL: server.URL + "/x.bin", SHA256: "abc"}); !errors.As(err, &validation) {
		t.Fatalf("非法 SHA256 应被拒绝：%v", err)
	}

	// 仓库已有同名文件时冲突。
	if err := os.MkdirAll(filepath.Join(root, "vae"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "vae", "exist.bin"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	var conflict *ConflictError
	if _, err := manager.Create(Spec{URL: server.URL + "/exist.bin", Category: "vae"}); !errors.As(err, &conflict) {
		t.Fatalf("已存在文件应返回冲突：%v", err)
	}
}

func TestCancelDeletesPartFile(t *testing.T) {
	server := rangeServer(t, 256)
	defer server.Close()
	manager := NewManager(t.TempDir())

	task, err := manager.Create(Spec{URL: server.URL + "/m.bin", Category: "vae"})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if _, err := manager.Action(task.ID, "cancel"); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, manager, task.ID, "cancelled")
	if _, err := os.Stat(manager.partPath(task.ID)); !os.IsNotExist(err) {
		t.Fatal("取消后分片应被删除")
	}
	if err := manager.Delete(task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Get(task.ID); err == nil {
		t.Fatal("删除后任务不应存在")
	}
}
