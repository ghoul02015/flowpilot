package server

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ghoul02015/flowpilot/internal/catalog"
	"github.com/ghoul02015/flowpilot/internal/database"
	"github.com/ghoul02015/flowpilot/internal/docker"
	"github.com/ghoul02015/flowpilot/internal/hardware"
	"github.com/ghoul02015/flowpilot/internal/instance"
	"github.com/ghoul02015/flowpilot/internal/model"
)

func newTestHandler(t *testing.T, instances *instance.Manager) http.Handler {
	t.Helper()
	return New(t.TempDir(), docker.NewClient("/unused/docker.sock"),
		hardware.NewCollector(hardware.Config{ProcRoot: t.TempDir(), HostRoot: t.TempDir(), DataDir: t.TempDir(), NvidiaSMIBin: ""}),
		instances, nil, nil, nil, "", catalog.NewManager(""), nil, nil, "", nil, true, nil, "", nil, nil)
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
	handler := New(t.TempDir(), docker.NewClient("/unused/docker.sock"), nil, nil, nil, nil, nil, "", catalog.NewManager(""), nil, nil, "", nil, true, nil, "", nil, nil)
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
	handler := New(t.TempDir(), docker.NewClient("/unused/docker.sock"), nil, nil, model.NewManager(root), nil, nil, "", catalog.NewManager(""), nil, nil, "", nil, true, nil, "", nil, nil)

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
func (f *fakeInstanceDocker) InspectImageLabels(_ context.Context, _ string) (map[string]string, error) {
	return map[string]string{"flowpilot.kind": "comfyui"}, nil
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
	handler := New(t.TempDir(), docker.NewClient("/unused/docker.sock"), nil, manager, nil, nil, nil, "127.0.0.1", catalog.NewManager(""), nil, nil, "", nil, true, nil, "", nil, nil)

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
	handler := New(t.TempDir(), docker.NewClient("/unused/docker.sock"), nil, nil, nil, nil, nil, "", catalog.NewManager(filepath.Join(t.TempDir(), "catalog.json")), nil, nil, "", nil, true, nil, "", nil, nil)

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
	handler := New(t.TempDir(), docker.NewClient("/unused/docker.sock"), nil, nil, nil, nil, nil, "", nil, nil, nil, "", nil, true, nil, "", nil, nil)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/models/catalog", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", response.Code)
	}
}

// newFakeDockerEngine 在临时 unix socket 上启动假 Docker 引擎并返回客户端。
func newFakeDockerEngine(t *testing.T, handler http.Handler) *docker.Client {
	t.Helper()
	socketPath := filepath.Join(t.TempDir(), "docker.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		_ = server.Close()
		_ = os.Remove(socketPath)
	})
	return docker.NewClient(socketPath)
}

// multiplexFrame 构造一个 Docker multiplex 日志帧。
func multiplexFrame(stream byte, payload string) []byte {
	header := make([]byte, 8)
	header[0] = stream
	binary.BigEndian.PutUint32(header[4:8], uint32(len(payload)))
	return append(header, []byte(payload)...)
}

func runningInstanceLabels(name string, withLinks bool) map[string]string {
	labels := map[string]string{
		"flowpilot.instance":               "true",
		"flowpilot.instance.name":          name,
		"flowpilot.instance.gpu":           "0",
		"flowpilot.instance.port":          "18188",
		"flowpilot.instance.models-target": "/data/models",
	}
	if withLinks {
		labels["flowpilot.instance.model-links"] = "true"
	}
	return labels
}

// 实例日志 SSE：首帧 meta、stdout/stderr 行、结束 end 事件均应按序输出。
func TestInstanceLogsSSE(t *testing.T) {
	client := newFakeDockerEngine(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/json"):
			_ = json.NewEncoder(w).Encode(map[string]any{"Id": "abc", "Config": map[string]any{"Tty": false}})
		case strings.HasSuffix(r.URL.Path, "/logs"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(multiplexFrame(1, "2026-11-10T08:00:00Z server started\n"))
			_, _ = w.Write(multiplexFrame(2, "2026-11-10T08:00:01Z warning line\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	fake := &fakeInstanceDocker{containers: []docker.Container{{
		ID: "abc", Names: []string{"/flowpilot-comfyui-demo"}, State: "running", Labels: runningInstanceLabels("demo", true),
	}}}
	manager := instance.NewManager(fake, func() int { return 1 }, "/host/data", "comfyui:test", 8188, "", "", "")
	handler := New(t.TempDir(), client, nil, manager, nil, nil, nil, "127.0.0.1", catalog.NewManager(""), nil, nil, "", nil, true, nil, "", nil, nil)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/instances/demo/logs?tail=50", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "text/event-stream" {
		t.Fatalf("unexpected content type %q", contentType)
	}
	body := response.Body.String()
	for _, want := range []string{"event: meta", `"stream":"stdout"`, "server started", `"stream":"stderr"`, "warning line", "event: end"} {
		if !strings.Contains(body, want) {
			t.Fatalf("SSE 响应缺少 %q：%s", want, body)
		}
	}
}

// 实例不存在时应返回普通 JSON 404，而不是 SSE 流。
func TestInstanceLogsNotFound(t *testing.T) {
	fake := &fakeInstanceDocker{}
	manager := instance.NewManager(fake, func() int { return 0 }, "/host/data", "comfyui:test", 8188, "", "", "")
	handler := New(t.TempDir(), docker.NewClient("/unused/docker.sock"), nil, manager, nil, nil, nil, "", catalog.NewManager(""), nil, nil, "", nil, true, nil, "", nil, nil)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/instances/missing/logs", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", response.Code)
	}
	if contentType := response.Header().Get("Content-Type"); strings.Contains(contentType, "text/event-stream") {
		t.Fatalf("404 不应以 SSE 响应，Content-Type=%q", contentType)
	}
}

// 模型关联端点：PUT 全量设置、GET 回读一致；非法模型 400；旧实例 409。
func TestModelLinksEndpoints(t *testing.T) {
	base := t.TempDir()
	modelRoot := filepath.Join(base, "models")
	if err := os.MkdirAll(filepath.Join(modelRoot, "loras"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modelRoot, "loras", "a.safetensors"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := &fakeInstanceDocker{containers: []docker.Container{
		{ID: "abc", Names: []string{"/flowpilot-comfyui-demo"}, State: "running", Labels: runningInstanceLabels("demo", true)},
		{ID: "def", Names: []string{"/flowpilot-comfyui-old"}, State: "running", Labels: runningInstanceLabels("old", false)},
	}}
	manager := instance.NewManager(fake, func() int { return 1 }, "/host/data", "comfyui:test", 8188, "", "", "")
	manager.SetModelLinks(filepath.Join(base, "hostlinks"), filepath.Join(base, "instance-links"), modelRoot)
	handler := New(t.TempDir(), docker.NewClient("/unused/docker.sock"), nil, manager, nil, nil, nil, "", catalog.NewManager(""), nil, nil, "", nil, true, nil, "", nil, nil)

	putBody := `{"links":[{"category":"loras","name":"a.safetensors"}]}`
	putRequest := httptest.NewRequest(http.MethodPut, "/api/v1/instances/demo/model-links", strings.NewReader(putBody))
	putResponse := httptest.NewRecorder()
	handler.ServeHTTP(putResponse, putRequest)
	if putResponse.Code != http.StatusOK {
		t.Fatalf("PUT 应成功，得到 %d：%s", putResponse.Code, putResponse.Body.String())
	}
	var putResult struct {
		Added   int `json:"added"`
		Removed int `json:"removed"`
	}
	if err := json.NewDecoder(putResponse.Body).Decode(&putResult); err != nil {
		t.Fatal(err)
	}
	if putResult.Added != 1 || putResult.Removed != 0 {
		t.Fatalf("首次设置应新增 1 个，得到 %+v", putResult)
	}

	getRequest := httptest.NewRequest(http.MethodGet, "/api/v1/instances/demo/model-links", nil)
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, getRequest)
	var getResult struct {
		Supported bool                 `json:"supported"`
		Links     []instance.ModelLink `json:"links"`
	}
	if err := json.NewDecoder(getResponse.Body).Decode(&getResult); err != nil {
		t.Fatal(err)
	}
	if !getResult.Supported || len(getResult.Links) != 1 || getResult.Links[0].Name != "a.safetensors" {
		t.Fatalf("GET 回读不一致：%+v", getResult)
	}

	badBody := `{"links":[{"category":"loras","name":"missing.safetensors"}]}`
	badRequest := httptest.NewRequest(http.MethodPut, "/api/v1/instances/demo/model-links", strings.NewReader(badBody))
	badResponse := httptest.NewRecorder()
	handler.ServeHTTP(badResponse, badRequest)
	if badResponse.Code != http.StatusBadRequest {
		t.Fatalf("不存在的模型应返回 400，得到 %d", badResponse.Code)
	}

	oldRequest := httptest.NewRequest(http.MethodPut, "/api/v1/instances/old/model-links", strings.NewReader(`{"links":[]}`))
	oldResponse := httptest.NewRecorder()
	handler.ServeHTTP(oldResponse, oldRequest)
	if oldResponse.Code != http.StatusConflict {
		t.Fatalf("旧实例应返回 409，得到 %d", oldResponse.Code)
	}
}

func TestDatabaseEndpointsDisabled(t *testing.T) {
	handler := newTestHandler(t, nil)
	for _, path := range []string{"/api/v1/database", "/api/v1/database/check", "/api/v1/database/backup"} {
		method := http.MethodGet
		if path != "/api/v1/database" {
			method = http.MethodPost
		}
		request := httptest.NewRequest(method, path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s 未启用数据底座时应返回 503，实际 %d", path, response.Code)
		}
	}
}

func TestDatabaseEndpointsWithStore(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "flowpilot-test.db")
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("打开测试数据库失败：%v", err)
	}
	defer db.Close()
	if _, err := database.Migrate(db, database.Migrations); err != nil {
		t.Fatalf("迁移失败：%v", err)
	}
	store := database.NewStore(db)
	backupDir := filepath.Join(t.TempDir(), "backups")
	handler := New(t.TempDir(), docker.NewClient("/unused/docker.sock"), nil, nil, nil, nil, nil, "", catalog.NewManager(""), nil, store, backupDir, nil, true, nil, "", nil, nil)

	// 状态：schema 版本 1、完整性通过。
	request := httptest.NewRequest(http.MethodGet, "/api/v1/database", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("database status 应为 200，实际 %d", response.Code)
	}
	var status struct {
		SchemaVersion    int `json:"schemaVersion"`
		InstanceMappings int `json:"instanceMappings"`
		Integrity        struct {
			OK bool `json:"ok"`
		} `json:"integrity"`
	}
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if status.SchemaVersion != 4 || !status.Integrity.OK {
		t.Fatalf("schema 版本应为 4 且完整性通过，实际 %+v", status)
	}

	// 检查：无实例时空报告。
	request = httptest.NewRequest(http.MethodPost, "/api/v1/database/check", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("database check 应为 200，实际 %d", response.Code)
	}

	// 备份：产物含 name 与 size。
	request = httptest.NewRequest(http.MethodPost, "/api/v1/database/backup", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("database backup 应为 200，实际 %d", response.Code)
	}
	var backup struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
	}
	if err := json.NewDecoder(response.Body).Decode(&backup); err != nil {
		t.Fatal(err)
	}
	if backup.Name == "" || backup.Size <= 0 {
		t.Fatalf("备份产物异常：%+v", backup)
	}
}
