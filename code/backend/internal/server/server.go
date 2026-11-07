package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ghoul02015/flowpilot/internal/docker"
	"github.com/ghoul02015/flowpilot/internal/hardware"
)

const Version = "0.0.0.6"

type Handler struct {
	docker    *docker.Client
	staticDir string
	hardware  *hardware.Collector
}

func New(staticDir string, dockerClient *docker.Client, hardwareCollector *hardware.Collector) http.Handler {
	handler := &Handler{docker: dockerClient, staticDir: staticDir, hardware: hardwareCollector}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", handler.health)
	mux.HandleFunc("GET /api/v1/system/info", handler.systemInfo)
	mux.HandleFunc("GET /api/v1/hardware", handler.hardwareReport)
	mux.HandleFunc("GET /api/v1/docker/info", handler.dockerInfo)
	mux.HandleFunc("GET /api/v1/docker/containers", handler.dockerContainers)
	mux.HandleFunc("/api/v1/", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "API endpoint not found"})
	})
	mux.Handle("/", handler.spa())
	return securityHeaders(mux)
}

func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"service": "flowpilotd",
		"version": Version,
	})
}

func (h *Handler) systemInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"service":     "flowpilotd",
		"version":     Version,
		"goVersion":   runtime.Version(),
		"os":          runtime.GOOS,
		"arch":        runtime.GOARCH,
		"cpuCount":    runtime.NumCPU(),
		"staticDir":   h.staticDir,
		"requestHost": r.Host,
	})
}

func (h *Handler) hardwareReport(w http.ResponseWriter, r *http.Request) {
	if h.hardware == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "硬件采集未启用"})
		return
	}
	writeJSON(w, http.StatusOK, h.hardware.Collect(r.Context()))
}

func (h *Handler) dockerInfo(w http.ResponseWriter, r *http.Request) {
	engine, err := h.docker.Engine(r.Context())
	if err != nil {
		slog.Warn("Docker info request failed", "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "无法读取 Docker Engine 信息", "detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, engine)
}

func (h *Handler) dockerContainers(w http.ResponseWriter, r *http.Request) {
	containers, err := h.docker.Containers(r.Context())
	if err != nil {
		slog.Warn("Docker containers request failed", "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "无法读取 Docker 容器列表", "detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, containers)
}

func (h *Handler) spa() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		name := strings.TrimPrefix(filepath.Clean(r.URL.Path), string(filepath.Separator))
		candidate := filepath.Join(h.staticDir, name)
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			http.ServeFile(w, r, candidate)
			return
		}
		http.ServeFile(w, r, filepath.Join(h.staticDir, "index.html"))
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
