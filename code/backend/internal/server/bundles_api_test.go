package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ghoul02015/flowpilot/internal/bundle"
	"github.com/ghoul02015/flowpilot/internal/database"
)

// stubDownloader 立即把任务标记为完成，用于 API 层测试。
type stubDownloader struct {
	mu   sync.Mutex
	seq  int
	done map[string]string
}

func (s *stubDownloader) Create(_ context.Context, _ bundle.DownloadRequest) (bundle.DownloadState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	id := "stub-" + string(rune('a'+s.seq%26))
	if s.done == nil {
		s.done = map[string]string{}
	}
	s.done[id] = "completed"
	return bundle.DownloadState{ID: id, Status: "completed", DownloadedBytes: 1, TotalBytes: 1}, nil
}

func (s *stubDownloader) Get(_ context.Context, id string) (bundle.DownloadState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := s.done[id]
	if status == "" {
		status = "completed"
	}
	return bundle.DownloadState{ID: id, Status: status}, nil
}

func (s *stubDownloader) Action(_ context.Context, _, _ string) error { return nil }

func newBundleTestServer(t *testing.T) (http.Handler, *bundle.Service) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "bundles-api.db"))
	if err != nil {
		t.Fatalf("打开数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := database.Migrate(db, database.Migrations); err != nil {
		t.Fatalf("迁移失败：%v", err)
	}
	manifest, err := bundle.Default()
	if err != nil {
		t.Fatalf("加载清单失败：%v", err)
	}
	store := bundle.NewStore(db)
	scan := func() (bundle.RepoSnapshot, int64, error) {
		return bundle.NewRepoSnapshot("models", nil), 1 << 40, nil
	}
	installer := bundle.NewInstaller(store, manifest, &stubDownloader{}, scan)
	installer.SetPollInterval(time.Millisecond)
	service := &bundle.Service{
		Manifest: manifest, Store: store, Installer: installer, ScanRepo: scan,
		Hardware: func() bundle.HardwareSummary {
			return bundle.HardwareSummary{GPUs: []bundle.GPUInfo{{Index: 0, Name: "NVIDIA GB10"}}, UnifiedMemory: true}
		},
		ImageMeta: func() (string, string) { return "0.45.0", "0.11.200" },
	}
	handler := &Handler{bundles: service, authDisabled: true}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/model-bundles", handler.listModelBundles)
	mux.HandleFunc("GET /api/v1/model-bundles/{id}", handler.getModelBundle)
	mux.HandleFunc("POST /api/v1/model-bundles/{id}/preflight", handler.preflightModelBundle)
	mux.HandleFunc("POST /api/v1/model-bundles/{id}/installs", handler.createModelBundleInstall)
	mux.HandleFunc("GET /api/v1/model-bundles/{id}/installs/{installId}", handler.getModelBundleInstall)
	mux.HandleFunc("POST /api/v1/model-bundles/{id}/license", handler.confirmModelBundleLicense)
	mux.HandleFunc("POST /api/v1/model-bundles/{id}/acceptances", handler.createModelBundleAcceptance)
	mux.HandleFunc("GET /api/v1/model-bundles/{id}/acceptances", handler.listModelBundleAcceptances)
	return handler.requireAuth(mux), service
}

func doJSON(t *testing.T, handler http.Handler, method, target string, body any) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(payload)
	} else {
		reader = bytes.NewReader(nil)
	}
	request := httptest.NewRequest(method, target, reader)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	decoded := map[string]any{}
	_ = json.Unmarshal(response.Body.Bytes(), &decoded)
	return response, decoded
}

func TestModelBundlesListAndDetail(t *testing.T) {
	handler, _ := newBundleTestServer(t)

	response, body := doJSON(t, handler, http.MethodGet, "/api/v1/model-bundles", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("列表应 200，实际 %d：%s", response.Code, response.Body.String())
	}
	list, _ := body["bundles"].([]any)
	if len(list) != 5 {
		t.Fatalf("应返回 5 个套装，实际 %d", len(list))
	}
	if body["checksum"] == "" {
		t.Fatal("列表应暴露清单校验值")
	}

	response, detail := doJSON(t, handler, http.MethodGet, "/api/v1/model-bundles/h3-image-base", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("详情应 200，实际 %d", response.Code)
	}
	manifest, _ := detail["manifest"].(map[string]any)
	if manifest["checksum"] == "" {
		t.Fatal("详情应暴露清单校验值")
	}
	items, _ := detail["items"].([]any)
	if len(items) != 3 {
		t.Fatalf("S1 应有 3 个文件项，实际 %d", len(items))
	}

	if response, _ := doJSON(t, handler, http.MethodGet, "/api/v1/model-bundles/not-exist", nil); response.Code != http.StatusBadRequest {
		t.Fatalf("不存在的套装应 400，实际 %d", response.Code)
	}
}

func TestModelBundleLicenseGatesInstall(t *testing.T) {
	handler, _ := newBundleTestServer(t)

	// 未确认许可时预检应阻断。
	response, preflight := doJSON(t, handler, http.MethodPost, "/api/v1/model-bundles/h3-image-base/preflight", map[string]any{"variant": "default"})
	if response.Code != http.StatusOK {
		t.Fatalf("预检应 200，实际 %d", response.Code)
	}
	if preflight["canInstall"] != false {
		t.Fatal("许可未确认时不应允许安装")
	}

	// 未确认许可直接创建安装必须被拒绝。
	response, _ = doJSON(t, handler, http.MethodPost, "/api/v1/model-bundles/h3-image-base/installs", map[string]any{"variant": "default"})
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("缺少许可确认应 422，实际 %d：%s", response.Code, response.Body.String())
	}

	// 确认许可后允许安装。
	if response, _ := doJSON(t, handler, http.MethodPost, "/api/v1/model-bundles/h3-image-base/license", map[string]any{"deliveryMethod": "online"}); response.Code != http.StatusOK {
		t.Fatalf("许可确认应 200，实际 %d", response.Code)
	}
	response, preflight = doJSON(t, handler, http.MethodPost, "/api/v1/model-bundles/h3-image-base/preflight", map[string]any{"variant": "default"})
	if preflight["canInstall"] != true {
		t.Fatalf("确认后应允许安装：%+v", preflight)
	}
}

func TestModelBundleInstallIdempotent(t *testing.T) {
	handler, service := newBundleTestServer(t)
	doJSON(t, handler, http.MethodPost, "/api/v1/model-bundles/h3-image-base/license", map[string]any{"deliveryMethod": "online"})

	response, created := doJSON(t, handler, http.MethodPost, "/api/v1/model-bundles/h3-image-base/installs", map[string]any{
		"variant": "default", "requestKey": "req-001",
	})
	if response.Code != http.StatusAccepted {
		t.Fatalf("创建安装应 202，实际 %d：%s", response.Code, response.Body.String())
	}
	installID, _ := created["id"].(string)
	if installID == "" {
		t.Fatal("应返回安装计划 ID")
	}

	// 相同 requestKey 重复提交返回同一计划。
	_, again := doJSON(t, handler, http.MethodPost, "/api/v1/model-bundles/h3-image-base/installs", map[string]any{
		"variant": "default", "requestKey": "req-001",
	})
	if again["id"] != installID {
		t.Fatalf("幂等提交应返回同一计划：%v vs %v", again["id"], installID)
	}

	// 等待串行安装完成后状态应为文件齐备。
	service.Installer.Wait(installID)
	response, detail := doJSON(t, handler, http.MethodGet, "/api/v1/model-bundles/h3-image-base/installs/"+installID, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("读取计划应 200，实际 %d", response.Code)
	}
	if detail["status"] != bundle.StateFilesReady {
		t.Fatalf("安装应完成，实际 %v", detail["status"])
	}
	items, _ := detail["items"].([]any)
	if len(items) == 0 {
		t.Fatal("应返回逐项进度")
	}
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item["status"] != bundle.ItemPresent {
			t.Fatalf("文件项应 present：%+v", item)
		}
	}
}

func TestModelBundleAcceptance(t *testing.T) {
	handler, _ := newBundleTestServer(t)
	// 非法结果必须被拒绝。
	response, _ := doJSON(t, handler, http.MethodPost, "/api/v1/model-bundles/h3-image-base/acceptances", map[string]any{
		"instance": "comfy-01", "result": "certified",
	})
	if response.Code != http.StatusBadRequest {
		t.Fatalf("非人工结果应 400，实际 %d", response.Code)
	}
	response, created := doJSON(t, handler, http.MethodPost, "/api/v1/model-bundles/h3-image-base/acceptances", map[string]any{
		"instance": "comfy-01", "result": "pass", "notes": "人工跑通 4MP 文生图",
		"metrics": map[string]any{"note": "未自动采集"},
	})
	if response.Code != http.StatusCreated {
		t.Fatalf("验收记录应 201，实际 %d：%s", response.Code, response.Body.String())
	}
	if created["result"] != "pass" {
		t.Fatalf("验收结果不符：%v", created)
	}
	response, list := doJSON(t, handler, http.MethodGet, "/api/v1/model-bundles/h3-image-base/acceptances", nil)
	if response.Code != http.StatusOK || list["total"].(float64) != 1 {
		t.Fatalf("验收列表异常：%v", list)
	}
}
