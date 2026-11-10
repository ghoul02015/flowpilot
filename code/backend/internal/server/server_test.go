package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ghoul02015/flowpilot/internal/catalog"
	"github.com/ghoul02015/flowpilot/internal/docker"
	"github.com/ghoul02015/flowpilot/internal/hardware"
	"github.com/ghoul02015/flowpilot/internal/instance"
	"github.com/ghoul02015/flowpilot/internal/model"
)

func newTestHandler(t *testing.T, instances *instance.Manager) http.Handler {
	t.Helper()
	return New(t.TempDir(), docker.NewClient("/unused/docker.sock"),
		hardware.NewCollector(hardware.Config{ProcRoot: t.TempDir(), HostRoot: t.TempDir(), DataDir: t.TempDir(), NvidiaSMIBin: ""}),
		instances, nil, nil, nil, "", catalog.NewManager(""), nil)
}

func TestHealth(t *testing.T) {
	handler := newTestHandler(t, nil)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" || body["version"] != Version {
		t.Fatalf("unexpected health response: %+v", body)
	}
	if response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("expected security headers")
	}
}

func TestUnknownAPIReturnsJSON(t *testing.T) {
	handler := newTestHandler(t, nil)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/unknown", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", response.Code)
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "application/json; charset=utf-8" {
		t.Fatalf("unexpected content type %q", contentType)
	}
}

func TestHardwareEndpoint(t *testing.T) {
	handler := newTestHandler(t, nil)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/hardware", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.Code)
	}
	var body struct {
		CollectedAt   string `json:"collectedAt"`
		HostName      string `json:"hostName"`
		GPUAvailable  bool   `json:"gpuAvailable"`
		GPUMemoryMode string `json:"gpuMemoryMode"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.CollectedAt == "" || body.HostName == "" || body.GPUAvailable || body.GPUMemoryMode != "none" {
		t.Fatalf("unexpected hardware response: %+v", body)
	}
}

func TestHardwareEndpointWithoutCollector(t *testing.T) {
	handler := New(t.TempDir(), docker.NewClient("/unused/docker.sock"), nil, nil, nil, nil, nil, "", catalog.NewManager(""), nil)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/hardware", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d", response.Code)
	}
}

func TestInstancesEndpointWithoutManager(t *testing.T) {
	handler := newTestHandler(t, nil)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/instances", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d", response.Code)
	}
}

func TestModelsEndpointLifecycle(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "checkpoints", "unit-test.safetensors")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("weights"), 0o644); err != nil {
		t.Fatal(err)
	}
	handler := New(t.TempDir(), docker.NewClient("/unused/docker.sock"), nil, nil, model.NewManager(root), nil, nil, "", catalog.NewManager(""), nil)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/models", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.Code)
	}
	var report model.Report
	if err := json.NewDecoder(response.Body).Decode(&report); err != nil {
		t.Fatal(err)
	}
	if len(report.Files) != 1 || report.Files[0].Type != "Checkpoints" || report.TotalBytes != 7 {
		t.Fatalf("unexpected models report: %+v", report)
	}

	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/models?path=checkpoints/unit-test.safetensors", nil)
	deleteResponse := httptest.NewRecorder()
	handler.ServeHTTP(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", deleteResponse.Code)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("文件应已被删除")
	}

	traversalRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/models?path=../escape", nil)
	traversalResponse := httptest.NewRecorder()
	handler.ServeHTTP(traversalResponse, traversalRequest)
	if traversalResponse.Code != http.StatusBadRequest {
		t.Fatalf("路径穿越应返回 400，实际 %d", traversalResponse.Code)
	}
}

func TestModelsEndpointWithoutManager(t *testing.T) {
	handler := newTestHandler(t, nil)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/models", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d", response.Code)
	}
}

// 实例 ComfyUI 状态代理：运行中的实例返回队列摘要与版本，
// 未运行实例不发起探测、reachable=false。
type fakeInstanceDocker struct {
	containers []docker.Container
}

func (f *fakeInstanceDocker) Containers(context.Context) ([]docker.Container, error) {
	return f.containers, nil
}
func (f *fakeInstanceDocker) ImageExists(context.Context, string) (bool, error) { return true, nil }
func (f *fakeInstanceDocker) CreateContainer(context.Context, string, any) (string, error) {
	return "", nil
}
func (f *fakeInstanceDocker) StartContainer(context.Context, string) error        { return nil }
func (f *fakeInstanceDocker) StopContainer(context.Context, string, int) error    { return nil }
func (f *fakeInstanceDocker) RemoveContainer(context.Context, string, bool) error { return nil }

func TestComfyStatusEndpoint(t *testing.T) {
	comfy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/queue":
			_, _ = w.Write([]byte(`{"queue_running":[["0","p1"],["1","p2"]],"queue_pending":[["2","p3"]]}`))
		case "/system_stats":
			_, _ = w.Write([]byte(`{"system":{"comfyui_version":"0.44.0"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer comfy.Close()
	comfyPort, _ := strconv.Atoi(strings.TrimPrefix(comfy.URL, "http://127.0.0.1:"))

	fake := &fakeInstanceDocker{containers: []docker.Container{
		{ID: "abc", Names: []string{"/flowpilot-comfyui-draw"}, State: "running",
			Labels: map[string]string{
				"flowpilot.instance":      "true",
				"flowpilot.instance.name": "draw",
				"flowpilot.instance.gpu":  "0",
				"flowpilot.instance.port": strconv.Itoa(comfyPort),
			}},
		{ID: "def", Names: []string{"/flowpilot-comfyui-rest"}, State: "exited",
			Labels: map[string]string{
				"flowpilot.instance":      "true",
				"flowpilot.instance.name": "rest",
				"flowpilot.instance.gpu":  "0",
				"flowpilot.instance.port": "19999",
			}},
	}}
	manager := instance.NewManager(fake, func() int { return 1 }, "/host/data", "comfyui:test", 8188, "", "", "")
	handler := New(t.TempDir(), docker.NewClient("/unused/docker.sock"), nil, manager, nil, nil, nil, "127.0.0.1", catalog.NewManager(""), nil)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/instances/draw/comfy/status", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.Code)
	}
	var status comfyStatusResponse
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if !status.Reachable || status.QueueRunning != 2 || status.QueuePending != 1 || status.ComfyVersion != "0.44.0" {
		t.Fatalf("unexpected comfy status: %+v", status)
	}

	restRequest := httptest.NewRequest(http.MethodGet, "/api/v1/instances/rest/comfy/status", nil)
	restResponse := httptest.NewRecorder()
	handler.ServeHTTP(restResponse, restRequest)
	var restStatus comfyStatusResponse
	_ = json.NewDecoder(restResponse.Body).Decode(&restStatus)
	if restStatus.Reachable || restStatus.InstanceStatus != "stopped" {
		t.Fatalf("stopped instance must not be probed: %+v", restStatus)
	}
}

func TestModelCatalogEndpoint(t *testing.T) {
	handler := New(t.TempDir(), docker.NewClient("/unused/docker.sock"), nil, nil, nil, nil, nil, "", catalog.NewManager(filepath.Join(t.TempDir(), "catalog.json")), nil)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/models/catalog", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.Code)
	}
	var result catalog.Catalog
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) == 0 || result.Source != "builtin" {
		t.Fatalf("unexpected catalog: source=%s entries=%d", result.Source, len(result.Entries))
	}
	found := false
	for _, entry := range result.Entries {
		if !strings.HasPrefix(entry.URL, "https://modelscope.cn/models/") {
			t.Fatalf("条目主地址必须是 ModelScope：%s", entry.URL)
		}
		if entry.ID == "sdxl-offset-lora" {
			found = true
		}
	}
	if !found {
		t.Fatal("内置目录缺少 sdxl-offset-lora 条目")
	}
}

func TestModelCatalogEndpointDisabled(t *testing.T) {
	handler := New(t.TempDir(), docker.NewClient("/unused/docker.sock"), nil, nil, nil, nil, nil, "", nil, nil)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/models/catalog", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", response.Code)
	}
}
