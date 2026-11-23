package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/ghoul02015/flowpilot/internal/auth"
	"github.com/ghoul02015/flowpilot/internal/backup"
	"github.com/ghoul02015/flowpilot/internal/bundle"
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
	"github.com/ghoul02015/flowpilot/internal/storage"
	"github.com/ghoul02015/flowpilot/internal/tasks"
)

const Version = "0.1.0.17"

type Handler struct {
	docker        *docker.Client
	staticDir     string
	hardware      *hardware.Collector
	instances     *instance.Manager
	models        *model.Manager
	images        *image.Manager
	downloads     *download.Manager
	catalog       *catalog.Manager
	storage       *storage.Manager
	database      *database.Store
	backupDir     string
	auth          *auth.Manager
	authDisabled  bool
	migrations    *migration.Manager
	workspaceRoot string
	healthChecker *health.Checker
	taskStore     *tasks.Store
	backupRunner  *backup.Runner
	comfyGateway  string
	comfyClient   *http.Client
	bundles       *bundle.Service
}

func New(staticDir string, dockerClient *docker.Client, hardwareCollector *hardware.Collector, instanceManager *instance.Manager, modelManager *model.Manager, imageManager *image.Manager, downloadManager *download.Manager, comfyGateway string, catalogManager *catalog.Manager, storageManager *storage.Manager, dataStore *database.Store, backupDir string, authManager *auth.Manager, authDisabled bool, migrationManager *migration.Manager, workspaceRoot string, healthChecker *health.Checker, unifiedTasks *tasks.Store, backups *backup.Runner, bundleService *bundle.Service) http.Handler {
	if comfyGateway == "" {
		comfyGateway = "host.docker.internal"
	}
	handler := &Handler{
		bundles: bundleService,
		docker:  dockerClient, staticDir: staticDir, hardware: hardwareCollector, instances: instanceManager,
		models: modelManager, images: imageManager, downloads: downloadManager, catalog: catalogManager,
		storage:       storageManager,
		database:      dataStore,
		backupDir:     backupDir,
		auth:          authManager,
		authDisabled:  authDisabled,
		migrations:    migrationManager,
		workspaceRoot: workspaceRoot,
		healthChecker: healthChecker,
		taskStore:     unifiedTasks,
		backupRunner:  backups,
		comfyGateway:  comfyGateway,
		// ComfyUI 状态探测是秒级接口，必须有整体超时，避免拖住轮询。
		comfyClient: &http.Client{Timeout: 4 * time.Second},
	}
	if dataStore != nil {
		// 实例创建／删除即时写库；启动同步见 syncAtStartup。
		if instanceManager != nil {
			instanceManager.SetRecorder(handler.recordCreate, handler.recordDelete)
		}
		go handler.syncAtStartup(context.Background())
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", handler.health)
	// ComfyUI 兼容探测端点（0.1.0.0）：flowpilotd 兼作回归健康镜像时供实例健康检查器使用。
	mux.HandleFunc("GET /queue", handler.compatQueue)
	mux.HandleFunc("GET /system_stats", handler.compatSystemStats)
	// 认证与初始化（0.0.9.6）。
	mux.HandleFunc("GET /api/v1/auth/setup/status", handler.setupStatus)
	mux.HandleFunc("POST /api/v1/auth/setup", handler.setupSubmit)
	mux.HandleFunc("POST /api/v1/auth/login", handler.loginSubmit)
	mux.HandleFunc("POST /api/v1/auth/logout", handler.logoutSubmit)
	mux.HandleFunc("GET /api/v1/auth/session", handler.currentSession)
	mux.HandleFunc("PUT /api/v1/auth/password", handler.changePassword)
	mux.HandleFunc("GET /api/v1/auth/sessions", handler.listSessions)
	mux.HandleFunc("DELETE /api/v1/auth/sessions/{id}", handler.revokeSession)
	mux.HandleFunc("GET /api/v1/audit/logs", handler.auditLogsPage)
	mux.HandleFunc("GET /api/v1/system/info", handler.systemInfo)
	mux.HandleFunc("GET /api/v1/hardware", handler.hardwareReport)
	mux.HandleFunc("GET /api/v1/hardware/compat", handler.hardwareCompat)
	mux.HandleFunc("GET /api/v1/docker/info", handler.dockerInfo)
	mux.HandleFunc("GET /api/v1/docker/containers", handler.dockerContainers)
	mux.HandleFunc("GET /api/v1/instances", handler.listInstances)
	mux.HandleFunc("POST /api/v1/instances", handler.createInstance)
	mux.HandleFunc("POST /api/v1/instances/{name}/start", handler.instanceAction("start"))
	mux.HandleFunc("POST /api/v1/instances/{name}/stop", handler.instanceAction("stop"))
	mux.HandleFunc("POST /api/v1/instances/{name}/restart", handler.instanceAction("restart"))
	mux.HandleFunc("DELETE /api/v1/instances/{name}", handler.deleteInstance)
	mux.HandleFunc("GET /api/v1/instances/{name}/comfy/status", handler.comfyStatus)
	// 实例日志（SSE 实时流）与模型关联（符号链接快捷层）。
	mux.HandleFunc("GET /api/v1/instances/{name}/logs", handler.instanceLogs)
	mux.HandleFunc("GET /api/v1/instances/{name}/model-links", handler.getModelLinks)
	mux.HandleFunc("PUT /api/v1/instances/{name}/model-links", handler.putModelLinks)
	mux.HandleFunc("GET /api/v1/models", handler.listModels)
	mux.HandleFunc("DELETE /api/v1/models", handler.deleteModel)
	mux.HandleFunc("GET /api/v1/models/catalog", handler.modelCatalog)
	mux.HandleFunc("POST /api/v1/models/catalog/refresh", handler.refreshModelCatalog)
	mux.HandleFunc("GET /api/v1/sources/status", handler.sourcesStatus)
	mux.HandleFunc("GET /api/v1/models/online/search", handler.onlineSearch)
	mux.HandleFunc("GET /api/v1/models/online/files", handler.onlineFiles)
	mux.HandleFunc("GET /api/v1/storage/network", handler.getNetworkStorage)
	mux.HandleFunc("PUT /api/v1/storage/network", handler.putNetworkStorage)
	mux.HandleFunc("GET /api/v1/storage/network/mount-script", handler.networkMountScript)
	mux.HandleFunc("GET /api/v1/images/check", handler.checkImage)
	mux.HandleFunc("GET /api/v1/images/local", handler.localImages)
	mux.HandleFunc("DELETE /api/v1/images/local", handler.deleteLocalImage)
	mux.HandleFunc("POST /api/v1/images/tag", handler.tagLocalImage)
	mux.HandleFunc("GET /api/v1/images/export", handler.exportLocalImage)
	mux.HandleFunc("POST /api/v1/images/import", handler.importLocalImage)
	mux.HandleFunc("GET /api/v1/images/catalog", handler.imageCatalog)
	mux.HandleFunc("GET /api/v1/images/comfyui-refs", handler.comfyUIRefs)
	mux.HandleFunc("GET /api/v1/images/comfyui-versions", handler.comfyVersions)
	mux.HandleFunc("POST /api/v1/images/comfyui-resolve", handler.comfyVersionResolve)
	mux.HandleFunc("GET /api/v1/images/build-script", handler.buildScript)
	mux.HandleFunc("GET /api/v1/images/install-script", handler.installScript)
	mux.HandleFunc("POST /api/v1/images/pull", handler.startPull)
	mux.HandleFunc("POST /api/v1/images/build", handler.startBuild)
	mux.HandleFunc("GET /api/v1/images/tasks", handler.imageTasks)
	mux.HandleFunc("GET /api/v1/images/tasks/{id}", handler.imageTask)
	mux.HandleFunc("POST /api/v1/images/tasks/{id}/cancel", handler.cancelImageTask)
	mux.HandleFunc("DELETE /api/v1/images/tasks/{id}", handler.deleteImageTask)
	mux.HandleFunc("GET /api/v1/images/disk-usage", handler.imageDiskUsage)
	mux.HandleFunc("POST /api/v1/images/cache/prune", handler.pruneBuildCache)
	// 备份（0.0.9.12）。
	mux.HandleFunc("GET /api/v1/backups", handler.listBackups)
	mux.HandleFunc("POST /api/v1/backups", handler.createBackup)
	// 模型组合套装（0.1.0.4 引入，0.1.0.5 许可按模型确认）。
	mux.HandleFunc("GET /api/v1/model-bundles", handler.listModelBundles)
	mux.HandleFunc("GET /api/v1/model-bundles/{id}", handler.getModelBundle)
	mux.HandleFunc("POST /api/v1/model-bundles/{id}/preflight", handler.preflightModelBundle)
	mux.HandleFunc("POST /api/v1/model-bundles/{id}/installs", handler.createModelBundleInstall)
	mux.HandleFunc("GET /api/v1/model-bundles/{id}/installs/{installId}", handler.getModelBundleInstall)
	mux.HandleFunc("POST /api/v1/model-bundles/{id}/installs/{installId}/{action}", handler.modelBundleInstallAction)
	mux.HandleFunc("POST /api/v1/model-bundles/{id}/license", handler.confirmModelBundleLicense)
	mux.HandleFunc("GET /api/v1/model-bundles/{id}/license/{file}", handler.downloadModelBundleLicenseFile)
	mux.HandleFunc("POST /api/v1/model-bundles/{id}/link", handler.linkModelBundle)
	mux.HandleFunc("GET /api/v1/model-bundles/{id}/acceptances", handler.listModelBundleAcceptances)
	mux.HandleFunc("POST /api/v1/model-bundles/{id}/acceptances", handler.createModelBundleAcceptance)
	mux.HandleFunc("GET /api/v1/model-bundles/{id}/acceptances/{acceptanceId}/export", handler.exportModelBundleAcceptance)
	// 统一任务（0.0.9.9）。
	mux.HandleFunc("GET /api/v1/tasks/summary", handler.taskSummary)
	mux.HandleFunc("GET /api/v1/tasks", handler.listUnifiedTasks)
	mux.HandleFunc("DELETE /api/v1/tasks", handler.cleanupFinishedTasks)
	mux.HandleFunc("GET /api/v1/tasks/{id}", handler.getUnifiedTask)
	mux.HandleFunc("DELETE /api/v1/tasks/{id}", handler.deleteUnifiedTask)
	mux.HandleFunc("GET /api/v1/downloads", handler.listDownloads)
	mux.HandleFunc("POST /api/v1/downloads", handler.createDownload)
	mux.HandleFunc("POST /api/v1/imports", handler.createImport)
	mux.HandleFunc("POST /api/v1/downloads/{id}/{action}", handler.downloadAction)
	mux.HandleFunc("DELETE /api/v1/downloads/{id}", handler.deleteDownload)
	// 应用健康与生命周期（0.0.9.8）。
	mux.HandleFunc("GET /api/v1/instances/{name}/health", handler.instanceHealth)
	mux.HandleFunc("GET /api/v1/instances/{name}/events", handler.instanceEvents)
	mux.HandleFunc("POST /api/v1/instances/{name}/rebuild/plan", handler.rebuildPlan)
	// 实例数据隔离与历史迁移（0.0.9.7）。
	mux.HandleFunc("GET /api/v1/instances/workspaces", handler.listWorkspaces)
	mux.HandleFunc("GET /api/v1/migration/scan", handler.migrationScan)
	mux.HandleFunc("GET /api/v1/migration/preview", handler.migrationPreview)
	mux.HandleFunc("POST /api/v1/migration/run", handler.runMigrationAsync)
	// 数据底座（0.0.9.5）：状态、一致性检查与备份。
	mux.HandleFunc("GET /api/v1/database", handler.databaseStatus)
	mux.HandleFunc("POST /api/v1/database/check", handler.databaseCheck)
	mux.HandleFunc("POST /api/v1/database/backup", handler.databaseBackup)
	mux.HandleFunc("/api/v1/", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "API endpoint not found"})
	})
	mux.Handle("/", handler.spa())
	return securityHeaders(handler.requireAuth(mux))
}

// compatQueue ComfyUI /queue 兼容端点（回归健康镜像语义：空闲队列）。
func (h *Handler) compatQueue(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"queue_running": []any{}, "queue_pending": []any{}})
}

// compatSystemStats ComfyUI /system_stats 兼容端点（回归健康镜像语义）。
func (h *Handler) compatSystemStats(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"system":  map[string]any{"comfyui_version": "flowpilot-regress", "python_version": "3", "os": "flowpilot"},
		"devices": []any{},
	})
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

func (h *Handler) listInstances(w http.ResponseWriter, r *http.Request) {
	if h.instances == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "实例管理未启用"})
		return
	}
	instances, err := h.instances.List(r.Context())
	if err != nil {
		slog.Warn("list instances failed", "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "无法读取实例列表", "detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"instances": instances, "defaults": h.instances.Defaults()})
}

func (h *Handler) createInstance(w http.ResponseWriter, r *http.Request) {
	if h.instances == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "实例管理未启用"})
		return
	}
	var spec instance.Spec
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&spec); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误", "detail": err.Error()})
		return
	}
	created, err := h.instances.Create(r.Context(), spec)
	if err != nil {
		h.writeInstanceError(w, err, "创建实例失败")
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (h *Handler) instanceAction(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.instances == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "实例管理未启用"})
			return
		}
		name := r.PathValue("name")
		// 停止/重启前读取队列：有任务时附带警告（阶段 1 不阻断）。
		warning := ""
		if action == "stop" || action == "restart" {
			if target, err := h.instances.Get(r.Context(), name); err == nil {
				warning = h.queueSummary(r, target)
			}
		}
		updated, err := h.instances.Action(r.Context(), name, action)
		if err != nil {
			h.writeInstanceError(w, err, "实例操作失败")
			return
		}
		h.recordActionEvent(r, updated, action)
		body := map[string]any{"instance": updated}
		if warning != "" {
			body["queueWarning"] = warning
		}
		writeJSON(w, http.StatusOK, body)
	}
}

// comfyStatusResponse 是实例 ComfyUI 制图队列的摘要视图。
type comfyStatusResponse struct {
	Reachable      bool   `json:"reachable"`
	InstanceStatus string `json:"instanceStatus"`
	QueueRunning   int    `json:"queueRunning"`
	QueuePending   int    `json:"queuePending"`
	ComfyVersion   string `json:"comfyuiVersion,omitempty"`
}

// comfyStatus 通过实例映射的宿主机端口探测其 ComfyUI 服务，
// 返回制图队列摘要（运行中／排队数）与版本；实例未运行或
// 服务未就绪时 reachable=false，界面据此刻意降级展示。
func (h *Handler) comfyStatus(w http.ResponseWriter, r *http.Request) {
	if h.instances == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "实例管理未启用"})
		return
	}
	target, err := h.instances.Get(r.Context(), r.PathValue("name"))
	if err != nil {
		h.writeInstanceError(w, err, "读取实例失败")
		return
	}
	result := comfyStatusResponse{InstanceStatus: target.Status}
	if target.Status != "running" {
		writeJSON(w, http.StatusOK, result)
		return
	}
	base := "http://" + h.comfyGateway + ":" + strconv.Itoa(target.Port)
	if body, err := h.comfyGet(base + "/queue"); err == nil {
		var queue struct {
			QueueRunning []json.RawMessage `json:"queue_running"`
			QueuePending []json.RawMessage `json:"queue_pending"`
		}
		if json.Unmarshal(body, &queue) == nil {
			result.Reachable = true
			result.QueueRunning = len(queue.QueueRunning)
			result.QueuePending = len(queue.QueuePending)
		}
	}
	if result.Reachable {
		if body, err := h.comfyGet(base + "/system_stats"); err == nil {
			var stats struct {
				System struct {
					ComfyuiVersion string `json:"comfyui_version"`
				} `json:"system"`
			}
			if json.Unmarshal(body, &stats) == nil {
				result.ComfyVersion = stats.System.ComfyuiVersion
			}
		}
	}
	writeJSON(w, http.StatusOK, result)
}

// comfyGet 发起一次带长度上限的 GET，返回响应体。
func (h *Handler) comfyGet(rawURL string) ([]byte, error) {
	response, err := h.comfyClient.Get(rawURL)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ComfyUI 返回 HTTP %d", response.StatusCode)
	}
	return io.ReadAll(io.LimitReader(response.Body, 1<<20))
}

// writeSSE 输出一条 server-sent event；event 为空时省略 event 行。
func writeSSE(w io.Writer, event string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	if event != "" {
		_, _ = fmt.Fprintf(w, "event: %s\n", event)
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
}

// instanceLogs 以 SSE 推送实例容器日志：先回放最近 tail 行，再实时 follow。
// 事件序列：meta（首帧）→ 若干日志 data 行 → end（结束原因 eof／container_gone）；
// 每 15 秒发送注释心跳，避免反向代理断开空闲连接。
// 实例停止时 Docker 仍会回放历史日志后结束流，便于排查启动失败与挂起。
func (h *Handler) instanceLogs(w http.ResponseWriter, r *http.Request) {
	if h.instances == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "实例管理未启用"})
		return
	}
	target, err := h.instances.Get(r.Context(), r.PathValue("name"))
	if err != nil {
		h.writeInstanceError(w, err, "读取实例失败")
		return
	}
	tail := 200
	if raw := r.URL.Query().Get("tail"); raw != "" {
		if parsed, convErr := strconv.Atoi(raw); convErr == nil && parsed >= 0 {
			if parsed > 1000 {
				parsed = 1000
			}
			tail = parsed
		}
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "当前环境不支持流式响应"})
		return
	}
	// 非 TTY 容器的日志流带 multiplex header，需按 inspect 结果决定是否剥离。
	tty, ttyErr := h.docker.ContainerTTY(r.Context(), target.ContainerID)
	if ttyErr != nil {
		slog.Warn("inspect instance tty failed", "name", target.Name, "error", ttyErr)
	}
	stream, err := h.docker.ContainerLogs(r.Context(), target.ContainerID, tail)
	if err != nil {
		h.writeInstanceError(w, err, "读取实例日志失败")
		return
	}
	defer stream.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	writeSSE(w, "meta", map[string]any{"instance": target.Name, "tail": tail, "tty": tty, "status": target.Status})
	flusher.Flush()

	reader := docker.NewDemuxReader(stream, tty)
	lineCh := make(chan docker.LogLine, 64)
	errCh := make(chan error, 1)
	go func() {
		defer close(lineCh)
		for {
			line, readErr := reader.ReadLine()
			if readErr != nil {
				if readErr != io.EOF {
					select {
					case errCh <- readErr:
					default:
					}
				}
				return
			}
			select {
			case lineCh <- line:
			case <-r.Context().Done():
				return
			}
		}
	}()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if _, writeErr := io.WriteString(w, ": ping\n\n"); writeErr != nil {
				return
			}
			flusher.Flush()
		case readErr := <-errCh:
			writeSSE(w, "end", map[string]string{"reason": "error", "detail": readErr.Error()})
			flusher.Flush()
			return
		case line, open := <-lineCh:
			if !open {
				reason := "eof"
				if _, getErr := h.instances.Get(r.Context(), target.Name); getErr != nil {
					reason = "container_gone" // 流因容器删除而结束
				}
				writeSSE(w, "end", map[string]string{"reason": reason})
				flusher.Flush()
				return
			}
			writeSSE(w, "", line)
			flusher.Flush()
		}
	}
}

// getModelLinks 返回实例是否支持模型关联（旧版本实例不支持）及当前关联列表。
func (h *Handler) getModelLinks(w http.ResponseWriter, r *http.Request) {
	if h.instances == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "实例管理未启用"})
		return
	}
	name := r.PathValue("name")
	supported, links, err := h.instances.ListModelLinks(r.Context(), name)
	if err != nil {
		h.writeInstanceError(w, err, "读取模型关联失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"instance": name, "supported": supported, "links": links})
}

// putModelLinks 以全量集合语义更新实例的模型关联：
// 新增/移除即时生效，ComfyUI 页面刷新即可见，无需重启实例。
func (h *Handler) putModelLinks(w http.ResponseWriter, r *http.Request) {
	if h.instances == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "实例管理未启用"})
		return
	}
	name := r.PathValue("name")
	var body struct {
		Links []instance.ModelLink `json:"links"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误", "detail": err.Error()})
		return
	}
	added, removed, err := h.instances.SetModelLinksFor(r.Context(), name, body.Links)
	if err != nil {
		h.writeInstanceError(w, err, "更新模型关联失败")
		return
	}
	supported, links, err := h.instances.ListModelLinks(r.Context(), name)
	if err != nil {
		h.writeInstanceError(w, err, "读取模型关联失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"instance":  name,
		"supported": supported,
		"links":     links,
		"added":     added,
		"removed":   removed,
		"hint":      "ComfyUI 页面刷新后即可在对应类别看到关联模型（无需重启实例）",
	})
}

func (h *Handler) deleteInstance(w http.ResponseWriter, r *http.Request) {
	if h.instances == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "实例管理未启用"})
		return
	}
	force := r.URL.Query().Get("force") == "1" || r.URL.Query().Get("force") == "true"
	purge := r.URL.Query().Get("purge") == "1" || r.URL.Query().Get("purge") == "true"
	if err := h.instances.Delete(r.Context(), r.PathValue("name"), force, purge); err != nil {
		h.writeInstanceError(w, err, "删除实例失败")
		return
	}
	if purge {
		writeJSON(w, http.StatusOK, map[string]string{"status": "purged"})
	} else {
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}

func (h *Handler) listModels(w http.ResponseWriter, _ *http.Request) {
	if h.models == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "模型仓库未启用"})
		return
	}
	report, err := h.models.Scan()
	if err != nil {
		slog.Warn("scan models failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "扫描模型仓库失败", "detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// getNetworkStorage 返回网络存储配置与挂载探测状态。
func (h *Handler) getNetworkStorage(w http.ResponseWriter, _ *http.Request) {
	if h.storage == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "网络存储未启用"})
		return
	}
	writeJSON(w, http.StatusOK, h.storage.View())
}

// putNetworkStorage 校验并保存网络存储配置。
func (h *Handler) putNetworkStorage(w http.ResponseWriter, r *http.Request) {
	if h.storage == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "网络存储未启用"})
		return
	}
	var config storage.Config
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体不是合法 JSON"})
		return
	}
	if err := h.storage.Save(config); err != nil {
		var validationError *storage.ValidationError
		if errors.As(err, &validationError) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": validationError.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "保存网络存储配置失败", "detail": err.Error()})
		return
	}
	// 配置变化只失效网络仓库缓存，本地仓库不受影响。
	if h.models != nil {
		h.models.InvalidateNetwork()
	}
	writeJSON(w, http.StatusOK, h.storage.View())
}

// networkMountScript 生成宿主机一键挂载脚本（附件下载）。
func (h *Handler) networkMountScript(w http.ResponseWriter, _ *http.Request) {
	if h.storage == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "网络存储未启用"})
		return
	}
	script, err := h.storage.MountScript()
	if err != nil {
		var validationError *storage.ValidationError
		if errors.As(err, &validationError) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": validationError.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "生成挂载脚本失败", "detail": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="mount-network-models.sh"`)
	_, _ = w.Write([]byte(script))
}

func (h *Handler) modelCatalog(w http.ResponseWriter, _ *http.Request) {
	if h.catalog == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "模型目录未启用"})
		return
	}
	writeJSON(w, http.StatusOK, h.catalog.List())
}

func (h *Handler) refreshModelCatalog(w http.ResponseWriter, r *http.Request) {
	if h.catalog == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "模型目录未启用"})
		return
	}
	// 刷新需要逐个查询 ModelScope，必须在响应超时前收口。
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	result, err := h.catalog.Refresh(ctx)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "刷新模型目录失败", "detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// sourcesStatus 返回 ModelScope 与 HF 镜像的实时可达性（60 秒缓存）。
func (h *Handler) sourcesStatus(w http.ResponseWriter, r *http.Request) {
	if h.catalog == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "模型目录未启用"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	writeJSON(w, http.StatusOK, map[string]any{"sources": h.catalog.SourcesStatus(ctx)})
}

// onlineSearch 代理源站模型搜索，返回归一化仓库列表。
func (h *Handler) onlineSearch(w http.ResponseWriter, r *http.Request) {
	if h.catalog == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "模型目录未启用"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	result, err := h.catalog.SearchOnline(ctx, r.URL.Query().Get("source"), r.URL.Query().Get("q"))
	if err != nil {
		writeOnlineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// onlineFiles 代理源站仓库文件列表，仅返回权重文件（含大小与推断类别）。
func (h *Handler) onlineFiles(w http.ResponseWriter, r *http.Request) {
	if h.catalog == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "模型目录未启用"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	files, err := h.catalog.OnlineFiles(ctx, r.URL.Query().Get("source"), r.URL.Query().Get("repo"))
	if err != nil {
		writeOnlineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files})
}

// writeOnlineError 区分参数错误（400）与源站上游错误（502）。
func writeOnlineError(w http.ResponseWriter, err error) {
	if errors.Is(err, catalog.ErrInvalidSource) || errors.Is(err, catalog.ErrInvalidQuery) || errors.Is(err, catalog.ErrInvalidRepo) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusBadGateway, map[string]string{"error": "源站请求失败", "detail": err.Error()})
}

func (h *Handler) deleteModel(w http.ResponseWriter, r *http.Request) {
	if h.models == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "模型仓库未启用"})
		return
	}
	// 服务端删除保护（0.0.9.11）：存在运行中实例时禁止删除共享仓库文件
	// （前端禁用仅为体验优化，安全约束必须在服务端强制）。
	if h.instances != nil {
		if instances, err := h.instances.List(r.Context()); err == nil {
			running := []string{}
			for _, item := range instances {
				if item.Status == "running" {
					running = append(running, item.Name)
				}
			}
			if len(running) > 0 {
				writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "存在运行中的实例（" + strings.Join(running, "、") + "），请先停止后再删除模型"})
				return
			}
		}
	}
	if err := h.models.Delete(r.URL.Query().Get("path")); err != nil {
		var validationError *model.ValidationError
		var notFoundError *model.NotFoundError
		switch {
		case errors.As(err, &validationError):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": validationError.Message})
		case errors.As(err, &notFoundError):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": notFoundError.Error()})
		default:
			slog.Warn("delete model failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "删除模型文件失败", "detail": err.Error()})
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *Handler) checkImage(w http.ResponseWriter, r *http.Request) {
	if h.images == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "镜像管理未启用"})
		return
	}
	name := r.URL.Query().Get("image")
	exists, err := h.images.Exists(r.Context(), name)
	if err != nil {
		var validationError *image.ValidationError
		if errors.As(err, &validationError) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": validationError.Message})
			return
		}
		slog.Warn("check image failed", "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "检查镜像失败", "detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"image": name, "exists": exists})
}

// localImages 返回本机 Docker 镜像列表（仓库标签、大小、创建时间）。
func (h *Handler) localImages(w http.ResponseWriter, r *http.Request) {
	images, err := h.docker.Images(r.Context())
	if err != nil {
		slog.Warn("list local images failed", "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "无法读取本机镜像列表", "detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"images": images})
}

func (h *Handler) deleteLocalImage(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("image")
	if _, err := h.images.Exists(r.Context(), name); err != nil {
		var validationError *image.ValidationError
		if errors.As(err, &validationError) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": validationError.Message})
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "检查镜像失败", "detail": err.Error()})
		return
	}
	// 引用保护（0.0.9.10）：被任何实例使用的镜像不可删除。
	if h.instances != nil {
		if instances, err := h.instances.List(r.Context()); err == nil {
			references := []string{}
			for _, item := range instances {
				if item.Image == name {
					references = append(references, item.Name+"（"+item.Status+"）")
				}
			}
			if len(references) > 0 {
				writeJSON(w, http.StatusConflict, map[string]string{"error": "镜像正被实例使用，无法删除：" + strings.Join(references, "、")})
				return
			}
		}
	}
	force := r.URL.Query().Get("force") == "1"
	if err := h.docker.RemoveImage(r.Context(), name, force); err != nil {
		slog.Warn("delete local image failed", "image", name, "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "删除镜像失败", "detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// normalizeImageReference 规范化镜像标签；省略标签时按 Docker 约定补 latest。
func normalizeImageReference(reference string) string {
	trimmed := strings.TrimSpace(reference)
	if trimmed == "" {
		return ""
	}
	lastSlash := strings.LastIndex(trimmed, "/")
	if !strings.Contains(trimmed[lastSlash+1:], ":") {
		return trimmed + ":latest"
	}
	return trimmed
}

// imageReferencingInstances 返回使用指定镜像标签的实例（含已停止容器）。
func (h *Handler) imageReferencingInstances(ctx context.Context, image string) ([]string, error) {
	if h.instances == nil {
		return nil, nil
	}
	instances, err := h.instances.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取实例引用失败：%w", err)
	}
	references := make([]string, 0)
	for _, item := range instances {
		if item.Image == image {
			references = append(references, fmt.Sprintf("%s（%s）", item.Name, item.Status))
		}
	}
	return references, nil
}

func (h *Handler) tagLocalImage(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Source string `json:"source"`
		Target string `json:"target"`
		Mode   string `json:"mode"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误", "detail": err.Error()})
		return
	}
	source := normalizeImageReference(request.Source)
	target := normalizeImageReference(request.Target)
	mode := request.Mode
	if mode == "" {
		mode = "alias"
	}
	if mode != "alias" && mode != "rename" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "不支持的标签操作模式"})
		return
	}
	if target == "" || strings.Contains(target, "@") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请输入包含标签的合法镜像名，例如 flowpilot/comfyui:cu130"})
		return
	}
	if target == source {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "新标签与源标签相同"})
		return
	}
	if exists, err := h.images.Exists(r.Context(), source); err != nil {
		var validationError *image.ValidationError
		if errors.As(err, &validationError) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": validationError.Message})
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "检查源镜像失败", "detail": err.Error()})
		return
	} else if !exists {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "源镜像不存在"})
		return
	}
	if targetExists, err := h.images.Exists(r.Context(), target); err != nil {
		var validationError *image.ValidationError
		if errors.As(err, &validationError) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": validationError.Message})
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "检查目标标签失败", "detail": err.Error()})
		return
	} else if targetExists {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "目标标签已存在：" + target})
		return
	}
	if mode == "rename" {
		references, err := h.imageReferencingInstances(r.Context(), source)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "暂时无法确认实例引用，已取消重命名", "detail": err.Error()})
			return
		}
		if len(references) > 0 {
			writeJSON(w, http.StatusConflict, map[string]string{
				"error": "源标签正被实例使用，无法重命名：" + strings.Join(references, "、") + "。可改为添加别名，或先调整实例镜像。",
			})
			return
		}
	}
	if err := h.docker.TagImage(r.Context(), source, target); err != nil {
		slog.Warn("tag local image failed", "source", source, "target", target, "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "添加镜像标签失败", "detail": err.Error()})
		return
	}
	if mode == "rename" {
		if err := h.docker.RemoveImage(r.Context(), source, false); err != nil {
			// 尽力回滚新标签，避免留下半完成的重命名。
			rollbackErr := h.docker.RemoveImage(r.Context(), target, false)
			detail := err.Error()
			if rollbackErr != nil {
				detail += "；回滚新标签失败：" + rollbackErr.Error()
			}
			slog.Warn("rename local image failed", "source", source, "target", target, "error", err, "rollbackError", rollbackErr)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "重命名镜像标签失败，已尽量回滚", "detail": detail})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status": mode,
		"action": mode,
		"source": source,
		"target": target,
	})
}

func (h *Handler) exportLocalImage(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("image")
	if exists, err := h.images.Exists(r.Context(), name); err != nil {
		var validationError *image.ValidationError
		if errors.As(err, &validationError) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": validationError.Message})
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "检查镜像失败", "detail": err.Error()})
		return
	} else if !exists {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "镜像不存在"})
		return
	}
	fileName := strings.NewReplacer("/", "_", ":", "_", "@", "_").Replace(name) + ".tar"
	w.Header().Set("Content-Type", "application/x-tar")
	w.Header().Set("Content-Disposition", "attachment; filename="+fileName)
	if err := h.docker.SaveImage(r.Context(), name, w); err != nil {
		slog.Warn("export local image failed", "image", name, "error", err)
	}
}

func (h *Handler) importLocalImage(w http.ResponseWriter, r *http.Request) {
	file, _, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请上传 Docker 镜像 tar 文件", "detail": err.Error()})
		return
	}
	defer file.Close()
	if err := h.docker.LoadImage(r.Context(), file); err != nil {
		slog.Warn("import local image failed", "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "导入镜像失败", "detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "imported"})
}

// imageCatalog 返回预置镜像构建配方（按显卡类型区分）。
func (h *Handler) imageCatalog(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"recipes": image.Recipes()})
}

// comfyUIRefs 保留旧客户端兼容视图；新前端使用 comfyui-versions。
func (h *Handler) comfyUIRefs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"refs": image.ComfyUIRefs(r.Context())})
}

// buildSourceFromQuery 用构建脚本查询参数构造待核验源码坐标。
func buildSourceFromQuery(kind, version, commit string) *image.ComfyVersion {
	if commit == "" {
		return nil
	}
	return &image.ComfyVersion{Kind: kind, Version: version, Commit: commit}
}

// buildScript 按构建弹窗参数生成仅构建镜像的一键脚本。
func (h *Handler) buildScript(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	params := image.BuildParams{
		Variant:     query.Get("variant"),
		ImageName:   query.Get("image"),
		ComfyUIRef:  query.Get("ref"),
		Source:      buildSourceFromQuery(query.Get("kind"), query.Get("version"), query.Get("commit")),
		PipIndexURL: query.Get("pip"),
		WithManager: query.Get("manager"),
		Platform:    query.Get("platform"),
	}
	if err := image.PrepareBuildSource(r.Context(), &params); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	script, err := image.BuildScript(params)
	if err != nil {
		var validationError *image.ValidationError
		if errors.As(err, &validationError) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": validationError.Message})
			return
		}
		slog.Warn("render build script failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "生成构建脚本失败", "detail": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=build-comfyui-image.sh")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, script)
}

// installScript 按向导参数渲染一键安装脚本并以下载形式返回。
// 查询参数：variant（auto 或配方 ID）、image、ref、pip、manager。
func (h *Handler) installScript(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	params := image.InstallParams{
		Variant:     query.Get("variant"),
		ImageName:   query.Get("image"),
		ComfyUIRef:  query.Get("ref"),
		PipIndexURL: query.Get("pip"),
		WithManager: query.Get("manager"),
	}
	resolved, err := image.ResolveComfyVersion(r.Context(), params.ComfyUIRef, query.Get("kind"), query.Get("commit"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "无法解析 ComfyUI 版本：" + err.Error()})
		return
	}
	params.ComfyUIRef = resolved.Ref
	params.Source = &resolved
	script, err := image.InstallScript(params)
	if err != nil {
		slog.Warn("render install script failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "生成安装脚本失败", "detail": err.Error()})
		return
	}
	params.Validate()
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=install-comfyui-image.sh")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, script)
}

// imageTasks 返回全部镜像构建／拉取任务（最新在前）。
func (h *Handler) imageTasks(w http.ResponseWriter, _ *http.Request) {
	if h.images == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "镜像管理未启用"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": h.images.List()})
}

// imageTask 返回单个任务详情（含日志）。
func (h *Handler) imageTask(w http.ResponseWriter, r *http.Request) {
	if h.images == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "镜像管理未启用"})
		return
	}
	task, err := h.images.Get(r.PathValue("id"))
	if err != nil {
		var notFoundError *image.TaskNotFoundError
		if errors.As(err, &notFoundError) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": notFoundError.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "读取任务失败", "detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, task)
}

// cancelImageTask 停止排队中／进行中的任务。
func (h *Handler) cancelImageTask(w http.ResponseWriter, r *http.Request) {
	if h.images == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "镜像管理未启用"})
		return
	}
	task, err := h.images.Cancel(r.PathValue("id"))
	if err != nil {
		h.writeImageTaskError(w, err, "停止任务失败")
		return
	}
	writeJSON(w, http.StatusOK, task)
}

// deleteImageTask 删除已结束的任务记录。
func (h *Handler) deleteImageTask(w http.ResponseWriter, r *http.Request) {
	if h.images == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "镜像管理未启用"})
		return
	}
	if err := h.images.Delete(r.PathValue("id")); err != nil {
		h.writeImageTaskError(w, err, "删除任务记录失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// imageDiskUsage 返回镜像与构建缓存的磁盘占用。
func (h *Handler) imageDiskUsage(w http.ResponseWriter, r *http.Request) {
	usage, err := h.docker.DiskUsage(r.Context())
	if err != nil {
		slog.Warn("read docker disk usage failed", "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "无法读取 Docker 磁盘占用", "detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, usage)
}

// pruneBuildCache 手动清理 BuildKit 构建缓存，返回回收空间。
func (h *Handler) pruneBuildCache(w http.ResponseWriter, r *http.Request) {
	reclaimed, err := h.docker.PruneBuildCache(r.Context())
	if err != nil {
		slog.Warn("prune build cache failed", "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "清理构建缓存失败", "detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "pruned", "reclaimedBytes": reclaimed})
}

// writeImageTaskError 将任务层错误映射为合适的 HTTP 状态码。
func (h *Handler) writeImageTaskError(w http.ResponseWriter, err error, prefix string) {
	var validationError *image.ValidationError
	var notFoundError *image.TaskNotFoundError
	switch {
	case errors.As(err, &validationError):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": validationError.Message})
	case errors.As(err, &notFoundError):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": notFoundError.Error()})
	default:
		slog.Warn(prefix, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": prefix, "detail": err.Error()})
	}
}

func (h *Handler) startPull(w http.ResponseWriter, r *http.Request) {
	if h.images == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "镜像管理未启用"})
		return
	}
	var request struct {
		Image string `json:"image"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误", "detail": err.Error()})
		return
	}
	task, err := h.images.SubmitPull(request.Image)
	if err != nil {
		var validationError *image.ValidationError
		var conflictError *image.ConflictError
		switch {
		case errors.As(err, &validationError):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": validationError.Message})
		case errors.As(err, &conflictError):
			writeJSON(w, http.StatusConflict, map[string]string{"error": conflictError.Error()})
		default:
			slog.Warn("submit image pull failed", "error", err)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "启动镜像拉取失败", "detail": err.Error()})
		}
		return
	}
	writeJSON(w, http.StatusAccepted, task)
}

func (h *Handler) startBuild(w http.ResponseWriter, r *http.Request) {
	if h.images == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "镜像管理未启用"})
		return
	}
	var request image.BuildParams
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误", "detail": err.Error()})
		return
	}
	if err := image.PrepareBuildSource(r.Context(), &request); err != nil {
		var validationError *image.ValidationError
		if errors.As(err, &validationError) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": validationError.Message})
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "无法解析 ComfyUI 版本", "detail": err.Error()})
		return
	}
	task, err := h.images.SubmitBuild(request)
	if err != nil {
		var validationError *image.ValidationError
		var conflictError *image.ConflictError
		switch {
		case errors.As(err, &validationError):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": validationError.Message})
		case errors.As(err, &conflictError):
			writeJSON(w, http.StatusConflict, map[string]string{"error": conflictError.Error()})
		default:
			slog.Warn("submit image build failed", "error", err)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "启动镜像构建失败", "detail": err.Error()})
		}
		return
	}
	writeJSON(w, http.StatusAccepted, task)
}

func (h *Handler) listDownloads(w http.ResponseWriter, _ *http.Request) {
	if h.downloads == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "下载任务未启用"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": h.downloads.List()})
}

func (h *Handler) createDownload(w http.ResponseWriter, r *http.Request) {
	if h.downloads == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "下载任务未启用"})
		return
	}
	var spec download.Spec
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&spec); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误", "detail": err.Error()})
		return
	}
	task, err := h.downloads.Create(spec)
	if err != nil {
		h.writeDownloadError(w, err, "创建下载任务失败")
		return
	}
	writeJSON(w, http.StatusCreated, task)
}

func (h *Handler) createImport(w http.ResponseWriter, r *http.Request) {
	if h.downloads == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "下载任务未启用"})
		return
	}
	var spec download.ImportSpec
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&spec); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误", "detail": err.Error()})
		return
	}
	task, err := h.downloads.CreateImport(spec)
	if err != nil {
		h.writeDownloadError(w, err, "创建导入任务失败")
		return
	}
	writeJSON(w, http.StatusCreated, task)
}

func (h *Handler) downloadAction(w http.ResponseWriter, r *http.Request) {
	if h.downloads == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "下载任务未启用"})
		return
	}
	task, err := h.downloads.Action(r.PathValue("id"), r.PathValue("action"))
	if err != nil {
		h.writeDownloadError(w, err, "下载任务操作失败")
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (h *Handler) deleteDownload(w http.ResponseWriter, r *http.Request) {
	if h.downloads == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "下载任务未启用"})
		return
	}
	if err := h.downloads.Delete(r.PathValue("id")); err != nil {
		h.writeDownloadError(w, err, "删除下载任务失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// writeDownloadError 将下载层错误映射为合适的 HTTP 状态码。
func (h *Handler) writeDownloadError(w http.ResponseWriter, err error, prefix string) {
	var validationError *download.ValidationError
	var notFoundError *download.NotFoundError
	var conflictError *download.ConflictError
	switch {
	case errors.As(err, &validationError):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": validationError.Message})
	case errors.As(err, &notFoundError):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": notFoundError.Error()})
	case errors.As(err, &conflictError):
		writeJSON(w, http.StatusConflict, map[string]string{"error": conflictError.Error()})
	default:
		slog.Warn(prefix, "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": prefix, "detail": err.Error()})
	}
}

// writeInstanceError 将实例层错误映射为合适的 HTTP 状态码。
func (h *Handler) writeInstanceError(w http.ResponseWriter, err error, prefix string) {
	var validationError *instance.ValidationError
	var notFoundError *instance.NotFoundError
	var notSupportedError *instance.NotSupportedError
	var busyError *instance.BusyError
	switch {
	case errors.As(err, &busyError):
		writeJSON(w, http.StatusConflict, map[string]string{"error": busyError.Error(), "code": "busy"})
	case errors.As(err, &validationError):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": validationError.Message})
	case errors.As(err, &notFoundError):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": notFoundError.Error()})
	case errors.As(err, &notSupportedError):
		writeJSON(w, http.StatusConflict, map[string]string{"error": notSupportedError.Error()})
	default:
		slog.Warn(prefix, "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": prefix, "detail": err.Error()})
	}
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
