package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ghoul02015/flowpilot/internal/docker"
	"github.com/ghoul02015/flowpilot/internal/hardware"
)

func TestHealth(t *testing.T) {
	handler := New(t.TempDir(), docker.NewClient("/unused/docker.sock"), hardware.NewCollector(hardware.Config{ProcRoot: t.TempDir(), HostRoot: t.TempDir(), DataDir: t.TempDir(), NvidiaSMIBin: ""}))
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
	handler := New(t.TempDir(), docker.NewClient("/unused/docker.sock"), hardware.NewCollector(hardware.Config{ProcRoot: t.TempDir(), HostRoot: t.TempDir(), DataDir: t.TempDir(), NvidiaSMIBin: ""}))
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
	handler := New(t.TempDir(), docker.NewClient("/unused/docker.sock"), hardware.NewCollector(hardware.Config{ProcRoot: t.TempDir(), HostRoot: t.TempDir(), DataDir: t.TempDir(), NvidiaSMIBin: ""}))
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
	handler := New(t.TempDir(), docker.NewClient("/unused/docker.sock"), nil)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/hardware", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d", response.Code)
	}
}
