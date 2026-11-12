package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/ghoul02015/flowpilot/internal/auth"
	"github.com/ghoul02015/flowpilot/internal/bundle"
)

// currentOperator 返回当前登录用户名；认证关闭时记为 local。
func (h *Handler) currentOperator(r *http.Request) string {
	if h.authDisabled {
		return "local"
	}
	if h.auth == nil {
		return ""
	}
	cookie, err := r.Cookie(auth.CookieName)
	if err != nil {
		return ""
	}
	username, _, ok := h.auth.ResolveSession(cookie.Value)
	if !ok {
		return ""
	}
	return username
}

func (h *Handler) requireBundles(w http.ResponseWriter) bool {
	if h.bundles == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "模型套装未启用", "code": "bundles_disabled"})
		return false
	}
	return true
}

func (h *Handler) writeBundleError(w http.ResponseWriter, err error) {
	var validationError *bundle.ValidationError
	switch {
	case errors.As(err, &validationError):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": validationError.Error(), "code": "invalid_argument"})
	case errors.Is(err, bundle.ErrLicenseNotConfirmed):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "需要先确认当前版本许可", "code": "license_required"})
	case errors.Is(err, bundle.ErrPreflightBlocked):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error(), "code": "preflight_blocked"})
	case errors.Is(err, bundle.ErrActiveInstall):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "该套装已有进行中的安装计划", "code": "install_active"})
	case errors.Is(err, bundle.ErrInstallNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "安装计划不存在", "code": "install_not_found"})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "服务端处理失败", "detail": err.Error()})
	}
}

// listModelBundles 返回五张套装卡片与当前主机匹配摘要。
func (h *Handler) listModelBundles(w http.ResponseWriter, r *http.Request) {
	if !h.requireBundles(w) {
		return
	}
	cards, err := h.bundles.List(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "读取套装列表失败", "detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"bundles":       cards,
		"manifest":      h.bundles.Manifest.ManifestVersion,
		"bundleVersion": h.bundles.Manifest.BundleVersion,
		"checksum":      h.bundles.Manifest.Checksum,
	})
}

// getModelBundle 返回套装详情。
func (h *Handler) getModelBundle(w http.ResponseWriter, r *http.Request) {
	if !h.requireBundles(w) {
		return
	}
	detail, err := h.bundles.Detail(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeBundleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

// preflightModelBundle 生成预检结果，不产生任何写操作。
func (h *Handler) preflightModelBundle(w http.ResponseWriter, r *http.Request) {
	if !h.requireBundles(w) {
		return
	}
	var request struct {
		Variant         string   `json:"variant"`
		SelectedOptions []string `json:"selectedOptions"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误", "detail": err.Error()})
		return
	}
	result, err := h.bundles.Preflight(r.Context(), r.PathValue("id"), request.Variant, request.SelectedOptions, h.currentOperator(r))
	if err != nil {
		h.writeBundleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// createModelBundleInstall 创建幂等安装计划。
func (h *Handler) createModelBundleInstall(w http.ResponseWriter, r *http.Request) {
	if !h.requireBundles(w) {
		return
	}
	var request bundle.CreateInstallRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误", "detail": err.Error()})
		return
	}
	install, err := h.bundles.CreateInstall(r.Context(), r.PathValue("id"), request, h.currentOperator(r))
	if err != nil {
		h.writeBundleError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, install)
}

// getModelBundleInstall 返回安装计划与逐项进度。
func (h *Handler) getModelBundleInstall(w http.ResponseWriter, r *http.Request) {
	if !h.requireBundles(w) {
		return
	}
	install, err := h.bundles.GetInstall(r.Context(), r.PathValue("installId"))
	if err != nil {
		h.writeBundleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, install)
}

// modelBundleInstallAction 执行 pause／resume／cancel／retry。
func (h *Handler) modelBundleInstallAction(w http.ResponseWriter, r *http.Request) {
	if !h.requireBundles(w) {
		return
	}
	if err := h.bundles.Action(r.Context(), r.PathValue("installId"), r.PathValue("action")); err != nil {
		h.writeBundleError(w, err)
		return
	}
	install, err := h.bundles.GetInstall(r.Context(), r.PathValue("installId"))
	if err != nil {
		h.writeBundleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, install)
}

// confirmModelBundleLicense 记录许可确认（只代表已阅读）。
func (h *Handler) confirmModelBundleLicense(w http.ResponseWriter, r *http.Request) {
	if !h.requireBundles(w) {
		return
	}
	var request struct {
		DeliveryMethod string `json:"deliveryMethod"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&request)
	if request.DeliveryMethod == "" {
		request.DeliveryMethod = "online"
	}
	if err := h.bundles.ConfirmLicense(r.Context(), r.PathValue("id"), request.DeliveryMethod, h.currentOperator(r)); err != nil {
		h.writeBundleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "confirmed"})
}

// linkModelBundle 把齐备文件关联到现有实例。
func (h *Handler) linkModelBundle(w http.ResponseWriter, r *http.Request) {
	if !h.requireBundles(w) {
		return
	}
	var request struct {
		InstallID string `json:"installId"`
		Instance  string `json:"instance"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误", "detail": err.Error()})
		return
	}
	added, err := h.bundles.Link(r.Context(), r.PathValue("id"), request.InstallID, request.Instance, h.currentOperator(r))
	if err != nil {
		h.writeBundleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "linked", "added": added, "requiresWorkflowRun": true})
}

// createModelBundleAcceptance 保存人工验收记录。
func (h *Handler) createModelBundleAcceptance(w http.ResponseWriter, r *http.Request) {
	if !h.requireBundles(w) {
		return
	}
	var request struct {
		InstallID   string          `json:"installId"`
		Instance    string          `json:"instance"`
		Result      string          `json:"result"`
		Preset      json.RawMessage `json:"preset"`
		Environment json.RawMessage `json:"environment"`
		Metrics     json.RawMessage `json:"metrics"`
		Notes       string          `json:"notes"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误", "detail": err.Error()})
		return
	}
	acceptance, err := h.bundles.AddAcceptance(r.Context(), r.PathValue("id"), bundle.Acceptance{
		InstallID:    request.InstallID,
		InstanceName: request.Instance,
		Result:       request.Result,
		Preset:       request.Preset,
		Environment:  request.Environment,
		Metrics:      request.Metrics,
		Notes:        request.Notes,
		Operator:     h.currentOperator(r),
	})
	if err != nil {
		h.writeBundleError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, acceptance)
}

// listModelBundleAcceptances 分页返回验收历史。
func (h *Handler) listModelBundleAcceptances(w http.ResponseWriter, r *http.Request) {
	if !h.requireBundles(w) {
		return
	}
	limit := 20
	offset := 0
	if value := r.URL.Query().Get("limit"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}
	if value := r.URL.Query().Get("offset"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed >= 0 {
			offset = parsed
		}
	}
	list, total, err := h.bundles.ListAcceptances(r.Context(), r.PathValue("id"), limit, offset)
	if err != nil {
		h.writeBundleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"acceptances": list, "total": total, "limit": limit, "offset": offset})
}

// exportModelBundleAcceptance 导出脱敏 JSON：不含主机名、IP、绝对路径与操作者。
func (h *Handler) exportModelBundleAcceptance(w http.ResponseWriter, r *http.Request) {
	if !h.requireBundles(w) {
		return
	}
	list, _, err := h.bundles.ListAcceptances(r.Context(), r.PathValue("id"), 100, 0)
	if err != nil {
		h.writeBundleError(w, err)
		return
	}
	var target *bundle.Acceptance
	for index := range list {
		if list[index].ID == r.PathValue("acceptanceId") {
			target = &list[index]
			break
		}
	}
	if target == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "验收记录不存在", "code": "acceptance_not_found"})
		return
	}
	export := map[string]any{
		"bundleId":      target.BundleID,
		"bundleVersion": target.BundleVersion,
		"result":        target.Result,
		"preset":        target.Preset,
		"environment":   target.Environment,
		"metrics":       target.Metrics,
		"notes":         target.Notes,
		"createdAt":     target.CreatedAt,
		"disclaimer":    "人工验收记录，不代表慧浪认证；导出内容已去除主机名、IP、路径与操作者信息。",
	}
	w.Header().Set("Content-Disposition", `attachment; filename="bundle-acceptance-`+target.ID+`.json"`)
	writeJSON(w, http.StatusOK, export)
}
