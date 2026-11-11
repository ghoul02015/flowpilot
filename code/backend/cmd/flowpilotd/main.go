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

	"github.com/ghoul02015/flowpilot/internal/auth"
	"github.com/ghoul02015/flowpilot/internal/catalog"
	"github.com/ghoul02015/flowpilot/internal/database"
	"github.com/ghoul02015/flowpilot/internal/docker"
	"github.com/ghoul02015/flowpilot/internal/download"
	"github.com/ghoul02015/flowpilot/internal/hardware"
	"github.com/ghoul02015/flowpilot/internal/health"
	"github.com/ghoul02015/flowpilot/internal/image"
	"github.com/ghoul02015/flowpilot/internal/instance"
	"github.com/ghoul02015/flowpilot/internal/migration"
	"github.com/ghoul02015/flowpilot/internal/model"
	"github.com/ghoul02015/flowpilot/internal/server"
	"github.com/ghoul02015/flowpilot/internal/storage"
	"github.com/ghoul02015/flowpilot/internal/tasks"
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
	// 镜像可信校验逃生阀（0.0.9.10）：受控环境放行无 flowpilot.kind 标签镜像；
	// 生产环境建议关闭（默认关闭），镜像应经镜像中心构建获得认证标签。
	if os.Getenv("FLOWPILOT_IMAGE_ALLOW_UNLABELED") == "1" {
		instanceManager.SetAllowUnlabeledImages(true)
		slog.Warn("已放行无标签镜像（FLOWPILOT_IMAGE_ALLOW_UNLABELED=1），仅限受控环境")
	}
	// 模型关联快捷层：宿主机侧目录作 bind 源，容器内同目录作读写路径（二者为同一物理目录）。
	instanceLinksContainerDir := filepath.Join(dataDir, "instance-links")
	instanceManager.SetModelLinks(filepath.Join(hostDataDir, "instance-links"), instanceLinksContainerDir, filepath.Join(dataDir, "models"))
	modelManager.SetLinksRoot(instanceLinksContainerDir)
	// 实例独立数据目录（0.0.9.7）：shared/instances/<uuid>/{outputs,workflows,user,config}。
	workspacesContainerRoot := filepath.Join(dataDir, "instances")
	instanceManager.SetWorkspaceRoot(filepath.Join(hostDataDir, "instances"), workspacesContainerRoot)
	// 历史数据迁移：旧共享 outputs/workflows → 实例 workspace。
	migrationManager := migration.NewManager(dataDir, filepath.Join(dataDir, "backups"), func(instanceName string) (string, bool) {
		target, err := instanceManager.Get(context.Background(), instanceName)
		if err != nil || target.UUID == "" {
			return "", false
		}
		return filepath.Join(workspacesContainerRoot, target.UUID), true
	})
	imageManager := image.NewManager(dockerClient, filepath.Join(dataDir, "image-tasks.json"))
	// 网络仓库路径同时作为导入白名单：「本地化」任务即把 NAS 模型复制进本地仓库。
	importDirs := strings.Split(environment("FLOWPILOT_IMPORT_DIRS", "/data/import"), ":")
	importDirs = append(importDirs, filepath.Join(dataDir, "network"))
	downloadManager := download.NewManager(filepath.Join(dataDir, "models"), importDirs, filepath.Join(dataDir, "tasks.json"))
	catalogManager := catalog.NewManager(filepath.Join(dataDir, "catalog.json"))

	// 数据底座（0.0.9.5）：嵌入式 SQLite，迁移后由 server 在启动时同步实例映射。
	// 初始化失败降级运行（数据底座接口返回 503），不让整个管理进程崩溃——
	// flowpilotd 同时被回归镜像用作实例内假 ComfyUI，其容器无 /data/flowpilot 挂载。
	dbPath := filepath.Join(dataDir, "flowpilot.db")
	var dataStore *database.Store
	var authManager *auth.Manager
	var healthChecker *health.Checker
	var taskStore *tasks.Store
	backupDir := filepath.Join(dataDir, "backups")
	if err := os.MkdirAll(dataDir, 0o755); err == nil {
		if db, openErr := database.Open(dbPath); openErr == nil {
			defer db.Close()
			if applied, migrateErr := database.Migrate(db, database.Migrations); migrateErr != nil {
				slog.Error("数据底座迁移失败，降级运行", "error", migrateErr)
			} else {
				if applied > 0 {
					slog.Info("数据底座迁移完成", "applied", applied)
				}
				dataStore = database.NewStore(db)
				taskStore = tasks.NewStore(db)
				// 实例重建沿用历史 workspace（孤儿记录复用 UUID）。
				instanceManager.SetOrphanMarker(func(name string) { dataStore.MarkOrphanByName(name) })
				instanceManager.SetWorkspaceReuse(func(name string) string {
					if uuidValue, ok := dataStore.FindOrphanByName(name); ok {
						slog.Info("实例重建沿用历史 workspace", "name", name, "uuid", uuidValue)
						return uuidValue
					}
					return ""
				})
				// 应用健康检查器（0.0.9.8）：独立周期探测，事件写入数据底座。
				startTimeout := 180 * time.Second
				if raw := os.Getenv("FLOWPILOT_START_TIMEOUT"); raw != "" {
					if parsed, err := time.ParseDuration(raw); err == nil && parsed > 0 {
						startTimeout = parsed
					}
				}
				healthChecker = health.NewChecker(func(ctx context.Context) ([]instance.Instance, error) {
					return instanceManager.List(ctx)
				}, environment("FLOWPILOT_HOST_GATEWAY", "host.docker.internal"), dataStore, startTimeout)
				if dataStore != nil {
					go healthChecker.Run(context.Background())
				}
				// 认证（0.0.9.6）：会话与审计存于数据底座；数据库不可用则认证不可用。
				authManager = auth.NewManager(db, dataDir, os.Getenv("FLOWPILOT_INIT_TOKEN"))
				authManager.CleanupStartup()
			}
		} else {
			slog.Error("数据底座初始化失败，降级运行", "error", openErr)
		}
	} else {
		slog.Error("数据目录不可创建，数据底座降级运行", "dir", dataDir, "error", err)
	}
	// 认证默认必须开启；仅显式环境变量可关闭（受控调试/回归场景）。
	// 数据底座不可用时认证管理器为 nil——中间件按失败关闭处理（全部 401），绝不静默放行。
	authDisabledFlag := os.Getenv("FLOWPILOT_AUTH_DISABLED") == "1" || os.Getenv("FLOWPILOT_REGRESS_MODE") == "1"
	if authDisabledFlag {
		slog.Warn("认证已显式关闭（FLOWPILOT_AUTH_DISABLED/REGRESS_MODE），仅限受控调试环境")
	}

	httpServer := &http.Server{
		Addr:    address,
		Handler: server.New(staticDir, dockerClient, hardwareCollector, instanceManager, modelManager, imageManager, downloadManager, environment("FLOWPILOT_HOST_GATEWAY", "host.docker.internal"), catalogManager, storageManager, dataStore, backupDir, authManager, authDisabledFlag, migrationManager, workspacesContainerRoot, healthChecker, taskStore),
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
