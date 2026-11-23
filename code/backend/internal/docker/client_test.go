package docker

import (
	"context"
	"encoding/json"
	"io"
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

// ContainerLogs 应携带 stdout/stderr/follow/timestamps/tail 参数并返回可读流。
func TestContainerLogsQueryAndStream(t *testing.T) {
	var gotURI string
	client := newFakeEngine(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/containers/demo/logs"):
			gotURI = r.URL.RequestURI()
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("log-body"))
		case strings.HasPrefix(r.URL.Path, "/containers/gone/logs"):
			http.Error(w, `{"message":"No such container: gone"}`, http.StatusNotFound)
		default:
			http.NotFound(w, r)
		}
	}))

	body, err := client.ContainerLogs(context.Background(), "demo", 150)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	content, _ := io.ReadAll(body)
	if string(content) != "log-body" {
		t.Fatalf("unexpected log body: %q", content)
	}
	for _, want := range []string{"stdout=1", "stderr=1", "follow=1", "timestamps=1", "tail=150"} {
		if !strings.Contains(gotURI, want) {
			t.Fatalf("logs 请求缺少参数 %s，实际 %s", want, gotURI)
		}
	}

	if _, err := client.ContainerLogs(context.Background(), "gone", 10); !IsNotFound(err) {
		t.Fatalf("容器不存在应映射为 NotFound，得到 %v", err)
	}
}

// ContainerTTY 应如实反映 inspect 的 Config.Tty。
func TestContainerTTY(t *testing.T) {
	client := newFakeEngine(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/containers/tty/json"):
			_ = json.NewEncoder(w).Encode(map[string]any{"Id": "tty", "Config": map[string]any{"Tty": true}})
		case strings.HasPrefix(r.URL.Path, "/containers/pipe/json"):
			_ = json.NewEncoder(w).Encode(map[string]any{"Id": "pipe", "Config": map[string]any{"Tty": false}})
		default:
			http.NotFound(w, r)
		}
	}))

	tty, err := client.ContainerTTY(context.Background(), "tty")
	if err != nil || !tty {
		t.Fatalf("tty 容器应返回 true，得到 %v, %v", tty, err)
	}
	tty, err = client.ContainerTTY(context.Background(), "pipe")
	if err != nil || tty {
		t.Fatalf("非 tty 容器应返回 false，得到 %v, %v", tty, err)
	}
	if _, err := client.ContainerTTY(context.Background(), "gone"); !IsNotFound(err) {
		t.Fatalf("容器不存在应映射为 NotFound，得到 %v", err)
	}
}
