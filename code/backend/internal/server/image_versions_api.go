package server

import (
	"encoding/json"
	"net/http"

	"github.com/ghoul02015/flowpilot/internal/image"
)

// comfyVersions 返回 ComfyUI 官方版本目录；refresh=1 强制在线刷新。
func (h *Handler) comfyVersions(w http.ResponseWriter, r *http.Request) {
	refresh := r.URL.Query().Get("refresh") == "1"
	catalog, err := image.ComfyVersions(r.Context(), refresh)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "读取 ComfyUI 官方版本失败", "detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, catalog)
}

// comfyVersionResolve 将 Release／Tag／Branch／Commit 解析为不可变提交。
func (h *Handler) comfyVersionResolve(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Ref    string `json:"ref"`
		Kind   string `json:"kind"`
		Commit string `json:"commit"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024)).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误", "detail": err.Error()})
		return
	}
	resolved, err := image.ResolveComfyVersion(r.Context(), request.Ref, request.Kind, request.Commit)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resolved)
}
