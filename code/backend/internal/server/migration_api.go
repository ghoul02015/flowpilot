package server

import (
	"encoding/json"
	"io"
	"net/http"
	"os"

	"github.com/ghoul02015/flowpilot/internal/migration"
)

// workspacesItem workspace 清单条目。
type workspacesItem struct {
	UUID         string `json:"uuid"`
	Name         string `json:"name"`
	HasContainer bool   `json:"hasContainer"`
	Orphan       bool   `json:"orphan"`
	Exists       bool   `json:"exists"`
}

// listWorkspaces 列出全部实例 workspace（含孤儿目录）。
func (h *Handler) listWorkspaces(w http.ResponseWriter, r *http.Request) {
	if h.instances == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "实例管理未启用"})
		return
	}
	instances, err := h.instances.List(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	byUUID := map[string]workspacesItem{}
	order := []string{}
	for _, item := range instances {
		if item.UUID == "" {
			continue
		}
		entry := workspacesItem{UUID: item.UUID, Name: item.Name, HasContainer: true}
		byUUID[item.UUID] = entry
		order = append(order, item.UUID)
	}
	// 目录扫描：存在但无容器的 workspace 即孤儿目录。
	if root := h.instancesWorkspaceRoot(); root != "" {
		entries, err := os.ReadDir(root)
		if err == nil {
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				if _, found := byUUID[entry.Name()]; found {
					byUUID[entry.Name()] = func(item workspacesItem) workspacesItem {
						item.Exists = true
						return item
					}(byUUID[entry.Name()])
					continue
				}
				byUUID[entry.Name()] = workspacesItem{UUID: entry.Name(), Exists: true}
				order = append(order, entry.Name())
			}
		}
	}
	list := make([]workspacesItem, 0, len(order))
	for _, uuid := range order {
		item := byUUID[uuid]
		item.Orphan = !item.HasContainer && item.Exists
		list = append(list, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"workspaces": list})
}

// migrationScan 扫描旧共享目录。
func (h *Handler) migrationScan(w http.ResponseWriter, _ *http.Request) {
	if h.migrations == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "迁移工具未启用"})
		return
	}
	writeJSON(w, http.StatusOK, h.migrations.Scan())
}

// migrationPreview 迁移预览。
func (h *Handler) migrationPreview(w http.ResponseWriter, r *http.Request) {
	if h.migrations == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "迁移工具未启用"})
		return
	}
	instance := r.URL.Query().Get("instance")
	if instance == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少 instance 参数"})
		return
	}
	writeJSON(w, http.StatusOK, h.migrations.Preview(instance))
}

// migrationRun 执行迁移。
func (h *Handler) migrationRun(w http.ResponseWriter, r *http.Request) {
	if h.migrations == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "迁移工具未启用"})
		return
	}
	var payload struct {
		Instance string   `json:"instance"`
		Scope    []string `json:"scope"`
		Mode     string   `json:"mode"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 8*1024)).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}
	if payload.Instance == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少 instance"})
		return
	}
	if len(migration.SanitizeScope(payload.Scope)) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "scope 至少包含 outputs 或 workflows"})
		return
	}
	if payload.Mode == "" {
		payload.Mode = "copy"
	}
	result, err := h.migrations.Run(payload.Instance, migration.SanitizeScope(payload.Scope), payload.Mode)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// instancesWorkspaceRoot 返回 workspace 根（反射不到就空——由装配保证非导出方法不可用，
// 这里通过 migration manager 的 sharedRoot 推导不可靠，直接用注入路径）。
func (h *Handler) instancesWorkspaceRoot() string {
	if h.workspaceRoot == "" {
		return ""
	}
	return h.workspaceRoot
}
