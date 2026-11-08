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

	"github.com/ghoul02015/flowpilot/internal/catalog"
	"github.com/ghoul02015/flowpilot/internal/docker"
	"github.com/ghoul02015/flowpilot/internal/download"
	"github.com/ghoul02015/flowpilot/internal/hardware"
	"github.com/ghoul02015/flowpilot/internal/image"
	"github.com/ghoul02015/flowpilot/internal/instance"
	"github.com/ghoul02015/flowpilot/internal/model"
)

const Version = "0.0.6.2"

type Handler struct {
	docker       *docker.Client
	staticDir    string
	hardware     *hardware.Collector
	instances    *instance.Manager
	models       *model.Manager
	images       *image.Puller
	downloads    *download.Manager
	catalog      *catalog.Manager
	comfyGateway string
	comfyClient  *http.Client
}

func New(staticDir string, dockerClient *docker.Client, hardwareCollector *hardware.Collector, instanceManager *instance.Manager, modelManager *model.Manager, imagePuller *image.Puller, downloadManager *download.Manager, comfyGateway string, catalogManager *catalog.Manager) http.Handler {
	if comfyGateway == "" {
		comfyGateway = "host.docker.internal"
	}
	handler := &Handler{
		docker: dockerClient, staticDir: staticDir, hardware: hardwareCollector, instances: instanceManager,
		models: modelManager, images: imagePuller, downloads: downloadManager, catalog: catalogManager,
		comfyGateway: comfyGateway,
		// ComfyUI 状态探测是秒级接口，必须有整体超时，避免拖住轮询。
		comfyClient: &http.Client{Timeout: 4 * time.Second},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", handler.health)
	mux.HandleFunc("GET /api/v1/system/info", handler.systemInfo)
	mux.HandleFunc("GET /api/v1/hardware", handler.hardwareReport)
	mux.HandleFunc("GET /api/v1/docker/info", handler.dockerInfo)
	mux.HandleFunc("GET /api/v1/docker/containers", handler.dockerContainers)
	mux.HandleFunc("GET /api/v1/instances", handler.listInstances)
	mux.HandleFunc("POST /api/v1/instances", handler.createInstance)
	mux.HandleFunc("POST /api/v1/instances/{name}/start", handler.instanceAction("start"))
	mux.HandleFunc("POST /api/v1/instances/{name}/stop", handler.instanceAction("stop"))
	mux.HandleFunc("POST /api/v1/instances/{name}/restart", handler.instanceAction("restart"))
	mux.HandleFunc("DELETE /api/v1/instances/{name}", handler.deleteInstance)
	mux.HandleFunc("GET /api/v1/instances/{name}/comfy/status", handler.comfyStatus)
	mux.HandleFunc("GET /api/v1/models", handler.listModels)
	mux.HandleFunc("DELETE /api/v1/models", handler.deleteModel)
	mux.HandleFunc("GET /api/v1/models/catalog", handler.modelCatalog)
	mux.HandleFunc("POST /api/v1/models/catalog/refresh", handler.refreshModelCatalog)
	mux.HandleFunc("GET /api/v1/sources/status", handler.sourcesStatus)
	mux.HandleFunc("GET /api/v1/models/online/search", handler.onlineSearch)
	mux.HandleFunc("GET /api/v1/models/online/files", handler.onlineFiles)
	mux.HandleFunc("GET /api/v1/images/check", handler.checkImage)
	mux.HandleFunc("GET /api/v1/images/pull", handler.pullStatus)
	mux.HandleFunc("POST /api/v1/images/pull", handler.startPull)
	mux.HandleFunc("GET /api/v1/downloads", handler.listDownloads)
	mux.HandleFunc("POST /api/v1/downloads", handler.createDownload)
	mux.HandleFunc("POST /api/v1/imports", handler.createImport)
	mux.HandleFunc("POST /api/v1/downloads/{id}/{action}", handler.downloadAction)
	mux.HandleFunc("DELETE /api/v1/downloads/{id}", handler.deleteDownload)
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
		updated, err := h.instances.Action(r.Context(), r.PathValue("name"), action)
		if err != nil {
			h.writeInstanceError(w, err, "实例操作失败")
			return
		}
		writeJSON(w, http.StatusOK, updated)
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

func (h *Handler) deleteInstance(w http.ResponseWriter, r *http.Request) {
	if h.instances == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "实例管理未启用"})
		return
	}
	force := r.URL.Query().Get("force") == "1" || r.URL.Query().Get("force") == "true"
	if err := h.instances.Delete(r.Context(), r.PathValue("name"), force); err != nil {
		h.writeInstanceError(w, err, "删除实例失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
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
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	repos, err := h.catalog.SearchOnline(ctx, r.URL.Query().Get("source"), r.URL.Query().Get("q"))
	if err != nil {
		writeOnlineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": repos})
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

func (h *Handler) pullStatus(w http.ResponseWriter, _ *http.Request) {
	if h.images == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "镜像管理未启用"})
		return
	}
	writeJSON(w, http.StatusOK, h.images.Status())
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
	if err := h.images.Start(request.Image); err != nil {
		var validationError *image.ValidationError
		var conflictError *image.ConflictError
		switch {
		case errors.As(err, &validationError):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": validationError.Message})
		case errors.As(err, &conflictError):
			writeJSON(w, http.StatusConflict, map[string]string{"error": conflictError.Error()})
		default:
			slog.Warn("start image pull failed", "error", err)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "启动镜像拉取失败", "detail": err.Error()})
		}
		return
	}
	writeJSON(w, http.StatusAccepted, h.images.Status())
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
	switch {
	case errors.As(err, &validationError):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": validationError.Message})
	case errors.As(err, &notFoundError):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": notFoundError.Error()})
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
