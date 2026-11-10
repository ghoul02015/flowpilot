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
	"github.com/ghoul02015/flowpilot/internal/storage"
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
	hostDataDir := environment("FLOWPILOT_HOST_DATA_DIR", dataDir)
	dockerClient := docker.NewClient(socketPath)
	instanceManager := instance.NewManager(
		dockerClient,
		func() int { return len(hardwareCollector.Collect(context.Background()).GPUs) },
		hostDataDir,
		environment("FLOWPILOT_COMFYUI_IMAGE", "comfyui:latest"),
		envInt("FLOWPILOT_COMFYUI_CONTAINER_PORT", 8188),
		environment("FLOWPILOT_COMFYUI_MODELS_TARGET", "/data/models"),
		environment("FLOWPILOT_COMFYUI_OUTPUTS_TARGET", "/data/outputs"),
		environment("FLOWPILOT_COMFYUI_WORKFLOWS_TARGET", "/data/workflows"),
	)
	// 网络存储（NAS 只读模型仓库）：配置持久化在数据目录，宿主机挂载点为共享目录下的 network。
	storageManager := storage.NewManager(
		filepath.Join(dataDir, "network-storage.json"),
		filepath.Join(dataDir, "network"),
		filepath.Join(hostDataDir, "network"),
		filepath.Join(dataDir, "network-extra"),
	)
	modelManager := model.NewManager(filepath.Join(dataDir, "models"))
	modelManager.SetNetworkSource(func() (string, bool) { return storageManager.NetworkContainerPath(), storageManager.Detected() })
	instanceManager.SetNetworkStorage(storageManager.NetworkHostPath(), storageManager.Detected, storageManager.WriteExtraModelPaths, storageManager.RemoveExtraModelPaths)
	imageManager := image.NewManager(dockerClient, filepath.Join(dataDir, "image-tasks.json"))
	// 网络仓库路径同时作为导入白名单：「本地化」任务即把 NAS 模型复制进本地仓库。
	importDirs := strings.Split(environment("FLOWPILOT_IMPORT_DIRS", "/data/import"), ":")
	importDirs = append(importDirs, filepath.Join(dataDir, "network"))
	downloadManager := download.NewManager(filepath.Join(dataDir, "models"), importDirs, filepath.Join(dataDir, "tasks.json"))
	catalogManager := catalog.NewManager(filepath.Join(dataDir, "catalog.json"))

	httpServer := &http.Server{
		Addr:    address,
		Handler: server.New(staticDir, dockerClient, hardwareCollector, instanceManager, modelManager, imageManager, downloadManager, environment("FLOWPILOT_HOST_GATEWAY", "host.docker.internal"), catalogManager, storageManager),
		// 镜像导入/导出可能是 GB 级长连接；保留请求头超时，正文与响应不设置固定上限。
		ReadHeaderTimeout: 5 * time.Second,
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
