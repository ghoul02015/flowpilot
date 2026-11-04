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

func TestDockerErrorIncludesStatus(t *testing.T) {
	client := newFakeEngine(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "permission denied", http.StatusUnauthorized)
	}))
	_, err := client.Engine(context.Background())
	if err == nil || !strings.Contains(err.Error(), "401 Unauthorized") || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("expected Docker status error, got %v", err)
	}
}
