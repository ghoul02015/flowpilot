package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ghoul02015/flowpilot/internal/catalog"
	"github.com/ghoul02015/flowpilot/internal/docker"
	"github.com/ghoul02015/flowpilot/internal/download"
	"github.com/ghoul02015/flowpilot/internal/hardware"
	"github.com/ghoul02015/flowpilot/internal/image"
	"github.com/ghoul02015/flowpilot/internal/instance"
	"github.com/ghoul02015/flowpilot/internal/model"
	"github.com/ghoul02015/flowpilot/internal/server"
)

func main() {
	address := environment("FLOWPILOT_ADDR", ":18080")
	staticDir := environment("FLOWPILOT_STATIC_DIR", "./web")
	socketPath := environment("FLOWPILOT_DOCKER_SOCKET", "/var/run/docker.sock")
	dataDir := environment("FLOWPILOT_DATA_DIR", "/data/flowpilot")
	hardwareCollector := hardware.NewCollector(hardware.Config{
		ProcRoot:     os.Getenv("FLOWPILOT_PROC_ROOT"),
		HostRoot:     os.Getenv("FLOWPILOT_HOST_ROOT"),
		DataDir:      os.Getenv("FLOWPILOT_DATA_DIR"),
		NvidiaSMIBin: os.Getenv("FLOWPILOT_NVIDIA_SMI"),
	})
	dockerClient := docker.NewClient(socketPath)
	instanceManager := instance.NewManager(
		dockerClient,
		func() int { return len(hardwareCollector.Collect(context.Background()).GPUs) },
		environment("FLOWPILOT_HOST_DATA_DIR", dataDir),
		environment("FLOWPILOT_COMFYUI_IMAGE", "comfyui:latest"),
		envInt("FLOWPILOT_COMFYUI_CONTAINER_PORT", 8188),
		environment("FLOWPILOT_COMFYUI_MODELS_TARGET", "/data/models"),
		environment("FLOWPILOT_COMFYUI_OUTPUTS_TARGET", "/data/outputs"),
		environment("FLOWPILOT_COMFYUI_WORKFLOWS_TARGET", "/data/workflows"),
	)
	modelManager := model.NewManager(filepath.Join(dataDir, "models"))
	imagePuller := image.NewPuller(dockerClient)
	importDirs := strings.Split(environment("FLOWPILOT_IMPORT_DIRS", "/data/import"), ":")
	downloadManager := download.NewManager(filepath.Join(dataDir, "models"), importDirs, filepath.Join(dataDir, "tasks.json"))
	catalogManager := catalog.NewManager(filepath.Join(dataDir, "catalog.json"))

	httpServer := &http.Server{
		Addr:              address,
		Handler:           server.New(staticDir, dockerClient, hardwareCollector, instanceManager, modelManager, imagePuller, downloadManager, environment("FLOWPILOT_HOST_GATEWAY", "host.docker.internal"), catalogManager),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	shutdownContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-shutdownContext.Done()
		context, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(context); err != nil {
			slog.Error("HTTP server shutdown failed", "error", err)
		}
	}()

	slog.Info("FlowPilot backend started", "version", server.Version, "address", address, "staticDir", staticDir)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("HTTP server failed", "error", err)
		os.Exit(1)
	}
}

func environment(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
	}
	return fallback
}
