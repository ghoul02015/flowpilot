package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ghoul02015/flowpilot/internal/bundle"
	"github.com/ghoul02015/flowpilot/internal/tasks"
)

// 统一状态映射。
func mapDownloadStatus(status string) string {
	switch status {
	case "running":
		return tasks.StatusRunning
	case "paused":
		return tasks.StatusPaused
	case "completed":
		return tasks.StatusSucceeded
	case "failed":
		return tasks.StatusFailed
	case "cancelled":
		return tasks.StatusCancelled
	default:
		return status
	}
}

func mapImageState(state string) string {
	switch state {
	case "queued":
		return tasks.StatusQueued
	case "active":
		return tasks.StatusRunning
	case "done":
		return tasks.StatusSucceeded
	case "error":
		return tasks.StatusFailed
	case "cancelled", "stopped":
		return tasks.StatusCancelled
	default:
		return state
	}
}

// collectTaskViews 汇总所有任务来源，供列表与摘要共用，保证口径一致。
func (h *Handler) collectTaskViews(ctx context.Context, taskType string) []tasks.View {
	views := []tasks.View{}

	// 下载／导入任务。
	if h.downloads != nil && (taskType == "" || taskType == tasks.TypeDownload || taskType == tasks.TypeImport) {
		for _, item := range h.downloads.List() {
			mappedType := tasks.TypeDownload
			if item.Kind == "import" {
				mappedType = tasks.TypeImport
			}
			if taskType != "" && mappedType != taskType {
				continue
			}
			progress := -1
			if item.TotalBytes > 0 {
				progress = int(float64(item.DownloadedBytes) / float64(item.TotalBytes) * 100)
				if progress > 100 {
					progress = 100
				}
			}
			views = append(views, tasks.View{
				ID: "dl-" + item.ID, Type: mappedType, Name: item.FileName,
				ObjectType: "model", ObjectRef: item.Path,
				Status:   mapDownloadStatus(item.Status),
				Progress: progress, Message: item.Message,
				Cancelable: item.Status == "running" || item.Status == "paused",
				CreatedAt:  item.CreatedAt.UTC().Format(time.RFC3339),
			})
		}
	}
	// 镜像任务。
	if h.images != nil && (taskType == "" || taskType == tasks.TypeImagePull || taskType == tasks.TypeImageBuild) {
		for _, item := range h.images.List() {
			mappedType := tasks.TypeImageBuild
			if item.Kind == "pull" {
				mappedType = tasks.TypeImagePull
			}
			if taskType != "" && mappedType != taskType {
				continue
			}
			views = append(views, tasks.View{
				ID: "img-" + item.ID, Type: mappedType, Name: item.Image,
				ObjectType: "image", ObjectRef: item.Image,
				Status:   mapImageState(item.State),
				Progress: item.Progress, Message: item.Message,
				Cancelable: item.State == "queued" || item.State == "active",
				CreatedAt:  item.CreatedAt.UTC().Format(time.RFC3339),
				StartedAt:  formatOptionalTime(item.StartedAt),
				FinishedAt: formatOptionalTime(item.FinishedAt),
			})
		}
	}
	// 模型套装安装（0.1.0.4）。
	if h.bundles != nil && (taskType == "" || taskType == tasks.TypeBundleInstall) {
		if items, _, err := h.bundles.Store.ListInstalls(ctx, 100, 0); err == nil {
			for _, item := range items {
				if taskType != "" && taskType != tasks.TypeBundleInstall {
					continue
				}
				views = append(views, bundleInstallView(item))
			}
		}
	}
	// 持久化任务（迁移与平台备份）。
	storedTypes := map[string]bool{tasks.TypeMigration: true, tasks.TypeBackup: true}
	if h.taskStore != nil && (taskType == "" || storedTypes[taskType]) {
		stored, _ := h.taskStore.List("", "", 500, 0)
		for _, item := range stored {
			if !storedTypes[item.Type] || (taskType != "" && item.Type != taskType) {
				continue
			}
			views = append(views, item)
		}
	}
	return views
}

// taskSummary 返回导航所需的轻量任务计数，不携带任务明细与日志。
func (h *Handler) taskSummary(w http.ResponseWriter, r *http.Request) {
	views := h.collectTaskViews(r.Context(), "")
	counts := map[string]int{
		tasks.StatusQueued: 0, tasks.StatusRunning: 0, tasks.StatusPaused: 0,
		tasks.StatusSucceeded: 0, tasks.StatusFailed: 0, tasks.StatusCancelled: 0,
	}
	for _, item := range views {
		if _, ok := counts[item.Status]; ok {
			counts[item.Status]++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"counts":    counts,
		"total":     len(views),
		"active":    counts[tasks.StatusQueued] + counts[tasks.StatusRunning] + counts[tasks.StatusPaused],
		"updatedAt": time.Now().UTC().Format(time.RFC3339),
	})
}

// listUnifiedTasks 统一任务列表：聚合下载/导入、镜像构建/拉取与迁移任务，
// 映射为统一状态与进度语义；迁移任务来自 tasks 表，其余实时读取各子系统。
func (h *Handler) listUnifiedTasks(w http.ResponseWriter, r *http.Request) {
	taskType := r.URL.Query().Get("type")
	status := r.URL.Query().Get("status")
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	limit := intQuery(r, "limit", 50)
	offset := intQuery(r, "offset", 0)
	views := h.collectTaskViews(r.Context(), taskType)
	// 状态过滤 + 时间倒序；total 必须反映筛选后的总数而非分页前聚合量。
	filtered := make([]tasks.View, 0, len(views))
	for _, item := range views {
		if status != "" && item.Status != status {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(item.Name+" "+item.ObjectRef), query) {
			continue
		}
		filtered = append(filtered, item)
	}
	sortTasksByCreated(filtered)
	total := len(filtered)
	if offset < len(filtered) {
		page := filtered[offset:]
		if len(page) > limit {
			page = page[:limit]
		}
		filtered = page
	} else {
		filtered = []tasks.View{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": filtered, "total": total, "limit": limit, "offset": offset})
}

// getUnifiedTask 任务详情。
func (h *Handler) getUnifiedTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if h.taskStore != nil {
		if view, ok := h.taskStore.Get(id); ok {
			writeJSON(w, http.StatusOK, view)
			return
		}
	}
	// 前缀路由到各子系统实时状态。
	if len(id) > 3 && id[:3] == "dl-" && h.downloads != nil {
		for _, item := range h.downloads.List() {
			if "dl-"+item.ID == id {
				writeJSON(w, http.StatusOK, tasks.View{ID: id, Type: tasks.TypeDownload, Name: item.FileName, Status: mapDownloadStatus(item.Status), Message: item.Message, Progress: -1, CreatedAt: item.CreatedAt.UTC().Format(time.RFC3339)})
				return
			}
		}
	}
	if strings.HasPrefix(id, "bundle-") && h.bundles != nil {
		if install, err := h.findBundleInstall(r.Context(), id); err == nil {
			writeJSON(w, http.StatusOK, bundleInstallView(install))
			return
		}
	}
	if len(id) > 4 && id[:4] == "img-" && h.images != nil {
		for _, item := range h.images.List() {
			if "img-"+item.ID == id {
				writeJSON(w, http.StatusOK, tasks.View{ID: id, Type: tasks.TypeImageBuild, Name: item.Image, Status: mapImageState(item.State), Progress: item.Progress, Message: item.Message, CreatedAt: item.CreatedAt.UTC().Format(time.RFC3339)})
				return
			}
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "任务不存在"})
}

// deleteUnifiedTask 删除已结束的统一任务记录；不影响模型、镜像、备份或实例数据。
func (h *Handler) deleteUnifiedTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	switch {
	case strings.HasPrefix(id, "dl-"):
		if h.downloads == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "下载任务未启用"})
			return
		}
		if err := h.downloads.Delete(strings.TrimPrefix(id, "dl-")); err != nil {
			h.writeDownloadError(w, err, "删除任务记录失败")
			return
		}
	case strings.HasPrefix(id, "bundle-"):
		if h.bundles == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "模型套装未启用"})
			return
		}
		if err := h.deleteBundleInstall(r.Context(), id); err != nil {
			switch {
			case errors.Is(err, bundle.ErrInstallNotFound):
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "安装计划不存在"})
			case errors.Is(err, bundle.ErrActiveInstall):
				writeJSON(w, http.StatusConflict, map[string]string{"error": "安装仍在进行，请先取消"})
			default:
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "删除安装记录失败"})
			}
			return
		}
	case strings.HasPrefix(id, "img-"):
		if h.images == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "镜像任务未启用"})
			return
		}
		if err := h.images.Delete(strings.TrimPrefix(id, "img-")); err != nil {
			h.writeImageTaskError(w, err, "删除任务记录失败")
			return
		}
	default:
		if h.taskStore == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "任务存储未启用"})
			return
		}
		if err := h.taskStore.Delete(id); err != nil {
			switch {
			case errors.Is(err, tasks.ErrNotFound):
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "任务不存在"})
			case errors.Is(err, tasks.ErrTaskActive):
				writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			default:
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "删除任务记录失败"})
			}
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// runMigrationAsync 迁移任务化：立即返回任务 ID，后台执行并写进度。
func (h *Handler) runMigrationAsync(w http.ResponseWriter, r *http.Request) {
	if h.migrations == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "迁移工具未启用"})
		return
	}
	var payload struct {
		Instance string   `json:"instance"`
		Scope    []string `json:"scope"`
		Mode     string   `json:"mode"`
	}
	if err := readJSON(r, &payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if payload.Instance == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少 instance"})
		return
	}
	if payload.Mode == "" {
		payload.Mode = "copy"
	}
	// 预检：实例需具备独立 workspace（同步校验，避免误导性任务创建）。
	plan := h.migrations.Preview(payload.Instance)
	if !plan.Available {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": plan.Reason})
		return
	}
	taskID := fmt.Sprintf("mig-%s-%d", payload.Instance, time.Now().Unix())
	if h.taskStore != nil {
		h.taskStore.RecordMigrationTask(taskID, payload.Instance, tasks.StatusRunning, -1, "迁移执行中（备份→复制/移动→校验）", "")
	}
	go func() {
		result, err := h.migrations.Run(payload.Instance, payload.Scope, payload.Mode)
		if h.taskStore == nil {
			return
		}
		if err != nil {
			h.taskStore.RecordMigrationTask(taskID, payload.Instance, tasks.StatusFailed, -1, "迁移失败", err.Error())
			return
		}
		status := tasks.StatusSucceeded
		if !result.Verified {
			status = tasks.StatusPartial
		}
		h.taskStore.RecordMigrationTask(taskID, payload.Instance, status, 100, fmt.Sprintf("完成：%d 文件 · 耗时 %s", result.Copied["outputs"]+result.Copied["workflows"], result.Duration), "")
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{"taskId": taskID, "status": tasks.StatusRunning})
}

// readJSON 简化 JSON 读取（限 8KB）。
func readJSON(r *http.Request, target any) error {
	defer r.Body.Close()
	return json.NewDecoder(io.LimitReader(r.Body, 8*1024)).Decode(target)
}

// activeTaskStatus 判断任务是否仍占用执行资源。
func activeTaskStatus(status string) bool {
	return status == tasks.StatusQueued || status == tasks.StatusRunning || status == tasks.StatusPaused
}

// sortTasksByCreated 活跃任务优先，同组内按创建时间倒序（简单插入排序，任务量级小）。
func sortTasksByCreated(list []tasks.View) {
	less := func(left, right tasks.View) bool {
		leftActive := activeTaskStatus(left.Status)
		rightActive := activeTaskStatus(right.Status)
		if leftActive != rightActive {
			return leftActive
		}
		return left.CreatedAt > right.CreatedAt
	}
	for i := 1; i < len(list); i++ {
		current := list[i]
		j := i - 1
		for j >= 0 && less(current, list[j]) {
			list[j+1] = list[j]
			j--
		}
		list[j+1] = current
	}
}

// cleanupFinishedTasks 批量删除已结束任务记录（不删除任何业务产物）。
// type 为空时清理全部类型；运行中、排队中与暂停任务永不删除。
func (h *Handler) cleanupFinishedTasks(w http.ResponseWriter, r *http.Request) {
	taskType := r.URL.Query().Get("type")
	matches := func(kind string) bool { return taskType == "" || taskType == kind }
	deleted := 0
	failed := 0
	countResult := func(err error) {
		if err != nil {
			failed++
			return
		}
		deleted++
	}
	if h.downloads != nil {
		for _, item := range h.downloads.List() {
			kind := tasks.TypeDownload
			if item.Kind == "import" {
				kind = tasks.TypeImport
			}
			status := mapDownloadStatus(item.Status)
			if !matches(kind) || activeTaskStatus(status) {
				continue
			}
			countResult(h.downloads.Delete(item.ID))
		}
	}
	if h.images != nil {
		for _, item := range h.images.List() {
			kind := tasks.TypeImageBuild
			if item.Kind == "pull" {
				kind = tasks.TypeImagePull
			}
			status := mapImageState(item.State)
			if !matches(kind) || activeTaskStatus(status) {
				continue
			}
			countResult(h.images.Delete(item.ID))
		}
	}
	if h.taskStore != nil {
		stored, _ := h.taskStore.List("", "", 500, 0)
		for _, item := range stored {
			if item.Type != tasks.TypeMigration && item.Type != tasks.TypeBackup {
				continue
			}
			if !matches(item.Type) || activeTaskStatus(item.Status) {
				continue
			}
			countResult(h.taskStore.Delete(item.ID))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": deleted, "failed": failed})
}

// formatOptionalTime 输出 RFC3339 时间；零值返回空串，便于前端显示“未记录”。
func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

// bundleInstallView 把套装安装计划映射为统一任务视图。
func bundleInstallView(install bundle.Install) tasks.View {
	progress := -1
	if install.TotalBytes > 0 {
		progress = int(float64(install.CompletedBytes) / float64(install.TotalBytes) * 100)
		if progress > 100 {
			progress = 100
		}
	}
	message := install.ErrorMessage
	if message == "" {
		message = bundleStatusText(install.Status)
	}
	if install.CurrentItemKey != "" && install.Status == bundle.StateInstalling {
		message = "正在处理 " + install.CurrentItemKey
	}
	view := tasks.View{
		// install.ID 本身已带 bundle- 前缀（历史记录），统一任务 ID 只保留一层。
		ID:         "bundle-" + strings.TrimPrefix(install.ID, "bundle-"),
		Type:       tasks.TypeBundleInstall,
		Name:       "模型套装 · " + install.BundleID,
		ObjectType: "bundle",
		ObjectRef:  install.BundleID,
		Status:     mapBundleStatus(install.Status),
		Progress:   progress,
		Message:    message,
		Error:      install.ErrorMessage,
		Cancelable: install.Status == bundle.StateQueued || install.Status == bundle.StateInstalling,
		CreatedAt:  formatOptionalTime(install.CreatedAt),
		StartedAt:  formatOptionalTimePtr(install.StartedAt),
		FinishedAt: formatOptionalTimePtr(install.FinishedAt),
	}
	return view
}

func formatOptionalTimePtr(value *time.Time) string {
	if value == nil {
		return ""
	}
	return formatOptionalTime(*value)
}

func mapBundleStatus(status string) string {
	switch status {
	case bundle.StateQueued:
		return tasks.StatusQueued
	case bundle.StateInstalling:
		return tasks.StatusRunning
	case bundle.StatePaused:
		return tasks.StatusPaused
	case bundle.StateFilesReady:
		return tasks.StatusSucceeded
	case bundle.StateFailed:
		return tasks.StatusFailed
	case bundle.StateCancelled:
		return tasks.StatusCancelled
	default:
		return tasks.StatusQueued
	}
}

func bundleStatusText(status string) string {
	switch status {
	case bundle.StateQueued:
		return "排队等待安装"
	case bundle.StateInstalling:
		return "正在安装"
	case bundle.StatePaused:
		return "已暂停，待继续"
	case bundle.StateFilesReady:
		return "文件齐备，待关联与人工验收"
	case bundle.StateFailed:
		return "安装失败"
	case bundle.StateCancelled:
		return "已取消，已完成文件保留"
	default:
		return status
	}
}

// findBundleInstall 兼容两种历史 ID：带 bundle- 前缀与纯数字。
func (h *Handler) findBundleInstall(ctx context.Context, taskID string) (bundle.Install, error) {
	base := strings.TrimPrefix(taskID, "bundle-")
	if install, err := h.bundles.Store.GetInstall(ctx, base); err == nil {
		return install, nil
	}
	return h.bundles.Store.GetInstall(ctx, "bundle-"+base)
}

// deleteBundleInstall 与 findBundleInstall 同样的前缀兼容逻辑。
func (h *Handler) deleteBundleInstall(ctx context.Context, taskID string) error {
	base := strings.TrimPrefix(taskID, "bundle-")
	err := h.bundles.Store.DeleteInstall(ctx, base)
	if errors.Is(err, bundle.ErrInstallNotFound) {
		err = h.bundles.Store.DeleteInstall(ctx, "bundle-"+base)
	}
	return err
}
