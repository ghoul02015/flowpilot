package docker

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newFakeEngine(t *testing.T, handler http.Handler) *Client {
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
	return NewClient(socketPath)
}

func TestEngineAndContainers(t *testing.T) {
	client := newFakeEngine(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.RequestURI() {
		case "/info":
			_ = json.NewEncoder(w).Encode(Info{Name: "test-engine", ServerVersion: "29.4.2", Containers: 3})
		case "/version":
			_ = json.NewEncoder(w).Encode(Version{Version: "29.4.2", APIVersion: "1.52"})
		case "/containers/json?all=1":
			_ = json.NewEncoder(w).Encode([]Container{{ID: "abc", Names: []string{"/flowpilot"}, State: "running"}})
		default:
			http.NotFound(w, r)
		}
	}))

	engine, err := client.Engine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if engine.Info.Name != "test-engine" || engine.Version.APIVersion != "1.52" {
		t.Fatalf("unexpected engine response: %+v", engine)
	}

	containers, err := client.Containers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(containers) != 1 || containers[0].Names[0] != "/flowpilot" {
		t.Fatalf("unexpected containers response: %+v", containers)
	}
}

// 停止请求失败但容器实际已退出时应视为成功（大容器优雅停止
// 可能拖垮响应，但 Docker 已完成 SIGKILL），仍在运行时才报错。
func TestStopContainerVerifiesStateOnError(t *testing.T) {
	client := newFakeEngine(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/containers/stopped/stop"):
			http.Error(w, "timeout", http.StatusInternalServerError)
		case strings.HasPrefix(r.URL.Path, "/containers/stopped/json"):
			_ = json.NewEncoder(w).Encode(map[string]any{"Id": "stopped", "State": map[string]any{"Status": "exited", "Running": false}})
		case strings.HasPrefix(r.URL.Path, "/containers/busy/stop"):
			http.Error(w, "timeout", http.StatusInternalServerError)
		case strings.HasPrefix(r.URL.Path, "/containers/busy/json"):
			_ = json.NewEncoder(w).Encode(map[string]any{"Id": "busy", "State": map[string]any{"Status": "running", "Running": true}})
		case strings.HasPrefix(r.URL.Path, "/containers/gone/stop"):
			w.WriteHeader(http.StatusNotModified)
		default:
			http.NotFound(w, r)
		}
	}))

	if err := client.StopContainer(context.Background(), "stopped", 1); err != nil {
		t.Fatalf("容器已退出时停止应视为成功，得到 %v", err)
	}
	if err := client.StopContainer(context.Background(), "busy", 1); err == nil {
		t.Fatal("容器仍在运行且停止失败时应返回错误")
	}
	if err := client.StopContainer(context.Background(), "gone", 1); err != nil {
		t.Fatalf("已停止（304）应视为成功，得到 %v", err)
	}
}

func TestDockerErrorIncludesStatus(t *testing.T) {
	client := newFakeEngine(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "permission denied", http.StatusUnauthorized)
	}))
	_, err := client.Engine(context.Background())
	if err == nil || !strings.Contains(err.Error(), "401 Unauthorized") || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("expected Docker status error, got %v", err)
	}
}
