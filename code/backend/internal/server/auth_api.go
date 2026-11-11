package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/ghoul02015/flowpilot/internal/auth"
)

// intQuery 解析整型查询参数（带默认值与上限钳制）。
func intQuery(r *http.Request, key string, fallback int) int {
	value, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil {
		return fallback
	}
	return value
}

// 豁免认证的 API 清单（方法+路径精确匹配）。
var authExemptions = map[string]bool{
	"GET /api/v1/health": true,
	// 只读探测端点：实例健康检查器使用（无敏感信息）。
	"GET /queue":                    true,
	"GET /system_stats":             true,
	"GET /api/v1/auth/setup/status": true,
	"POST /api/v1/auth/setup":       true,
	"POST /api/v1/auth/login":       true,
}

// requireAuth 认证中间件：豁免清单外全部 API 需有效会话；
// 写方法额外校验 CSRF 头与 Origin/Host 一致性；写操作自动审计。
func (h *Handler) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 显式关闭认证（仅限本地调试，默认必须认证）。
		if h.authDisabled {
			next.ServeHTTP(w, r)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		// 失败关闭：认证管理器不可用（数据底座故障）时拒绝全部业务 API。
		if h.auth == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "认证服务不可用（数据底座故障），已拒绝请求", "code": "auth_unavailable"})
			return
		}
		if authExemptions[r.Method+" "+r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}
		// Origin/Host 一致性（存在 Origin 时必须匹配，防跨站）。
		if origin := r.Header.Get("Origin"); origin != "" && !sameHost(origin, r.Host) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "Origin 与 Host 不一致", "code": "origin_mismatch"})
			return
		}
		cookie, err := r.Cookie(auth.CookieName)
		username, _, ok := "", "", false
		if err == nil {
			username, _, ok = h.auth.ResolveSession(cookie.Value)
		}
		if !ok {
			if h.auth != nil && !h.auth.Initialized() {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "平台未初始化", "code": "setup_required"})
			} else {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "未登录或会话已过期", "code": "unauthenticated"})
			}
			return
		}
		// CSRF：写方法必须携带与会话匹配的令牌头。
		if isWriteMethod(r.Method) {
			if r.Header.Get(auth.CSRFHeader) == "" || !h.auth.CSRFMatches(cookie.Value, r.Header.Get(auth.CSRFHeader)) {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "CSRF 校验失败，请刷新页面后重试", "code": "csrf_required"})
				return
			}
		}
		recorder := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		// 写操作自动审计（业务细节由各端点自行补充，此处兜底记录）。
		if isWriteMethod(r.Method) && !strings.HasPrefix(r.URL.Path, "/api/v1/auth/") {
			h.auditRequest(username, r, fmt.Sprintf("HTTP %d", recorder.status))
		}
	})
}

// statusWriter 记录响应状态码（用于审计）。
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Flush 透传给 SSE（http.Flusher）。
func (s *statusWriter) Flush() {
	if flusher, ok := s.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// isWriteMethod 判断是否为写方法。
func isWriteMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch:
		return true
	}
	return false
}

// sameHost 判断 Origin 是否与请求 Host 同源。
func sameHost(origin, host string) bool {
	trimmed := strings.TrimPrefix(strings.TrimPrefix(origin, "https://"), "http://")
	return trimmed == host
}

// auditRequest 记录一条写操作审计。
func (h *Handler) auditRequest(username string, r *http.Request, result string) {
	if h.auth == nil {
		return
	}
	h.auth.Audit(auth.AuditEntry{
		Actor:     username,
		Action:    strings.ToLower(r.Method) + " " + r.URL.Path,
		Object:    r.PathValue("name"),
		Result:    result,
		IPHash:    auth.IPHashOf(r),
		RequestID: requestID(),
	})
}

func requestID() string {
	buffer := make([]byte, 4)
	_, _ = rand.Read(buffer)
	return hex.EncodeToString(buffer)
}

// ---- 认证端点（豁免清单内 + 登录后可用）----

// setupStatus 返回初始化状态。
func (h *Handler) setupStatus(w http.ResponseWriter, _ *http.Request) {
	if h.auth == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "认证未启用（数据底座不可用）"})
		return
	}
	initialized, tokenReady := h.auth.SetupStatus()
	writeJSON(w, http.StatusOK, map[string]any{"initialized": initialized, "tokenReady": tokenReady})
}

// setupSubmit 提交初始化：一次性令牌＋管理员密码。
func (h *Handler) setupSubmit(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "认证未启用（数据底座不可用）"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 8*1024))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体读取失败"})
		return
	}
	var payload struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}
	if err := h.auth.CompleteSetup(payload.Token, payload.Password, auth.IPHashOf(r)); err != nil {
		status := http.StatusBadRequest
		if err == auth.ErrTokenInvalid {
			status = http.StatusUnauthorized
		}
		writeJSON(w, status, map[string]string{"error": err.Error(), "code": "setup_token_invalid"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"initialized": true})
}

// loginSubmit 管理员登录；成功设置会话 Cookie 并返回 csrfToken。
func (h *Handler) loginSubmit(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "认证未启用（数据底座不可用）"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 8*1024))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体读取失败"})
		return
	}
	var payload struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}
	id, csrf, err := h.auth.Login(payload.Username, payload.Password, auth.IPHashOf(r), r.UserAgent())
	if err != nil {
		status := http.StatusUnauthorized
		if err == auth.ErrLockedOut {
			status = http.StatusTooManyRequests
		}
		writeJSON(w, status, map[string]string{"error": err.Error(), "code": "login_failed"})
		return
	}
	http.SetCookie(w, h.auth.SessionCookie(id, isSecureRequest(r), 0))
	writeJSON(w, http.StatusOK, map[string]any{"username": payload.Username, "csrfToken": csrf})
}

// logoutSubmit 注销当前会话。
func (h *Handler) logoutSubmit(w http.ResponseWriter, r *http.Request) {
	if h.auth != nil {
		if cookie, err := r.Cookie(auth.CookieName); err == nil {
			h.auth.Logout(cookie.Value)
		}
	}
	// 清除 Cookie。
	http.SetCookie(w, &http.Cookie{Name: auth.CookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// currentSession 返回当前会话信息（用户、csrfToken、到期）。
func (h *Handler) currentSession(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(auth.CookieName)
	if err != nil || h.auth == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "未登录", "code": "unauthenticated"})
		return
	}
	username, csrf, ok := h.auth.ResolveSession(cookie.Value)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "会话已过期", "code": "unauthenticated"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"username": username, "csrfToken": csrf})
}

// changePassword 修改密码（撤销其他会话）。
func (h *Handler) changePassword(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(auth.CookieName)
	if err != nil || h.auth == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "未登录", "code": "unauthenticated"})
		return
	}
	username, _, ok := h.auth.ResolveSession(cookie.Value)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "会话已过期", "code": "unauthenticated"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 8*1024))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体读取失败"})
		return
	}
	var payload struct {
		Current string `json:"current"`
		Next    string `json:"next"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}
	if err := h.auth.ChangePassword(username, payload.Current, payload.Next, cookie.Value); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// listSessions 当前用户活跃会话列表。
func (h *Handler) listSessions(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(auth.CookieName)
	if err != nil || h.auth == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "未登录", "code": "unauthenticated"})
		return
	}
	username, _, ok := h.auth.ResolveSession(cookie.Value)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "会话已过期", "code": "unauthenticated"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": h.auth.ListSessions(username, cookie.Value)})
}

// revokeSession 撤销指定会话（前缀匹配）。
func (h *Handler) revokeSession(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(auth.CookieName)
	if err != nil || h.auth == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "未登录", "code": "unauthenticated"})
		return
	}
	username, _, ok := h.auth.ResolveSession(cookie.Value)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "会话已过期", "code": "unauthenticated"})
		return
	}
	prefix := r.PathValue("id")
	if len(prefix) < 8 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "会话标识不合法"})
		return
	}
	if err := h.auth.RevokeSession(username, prefix); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// auditLogsPage 分页查询审计。
func (h *Handler) auditLogsPage(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "认证未启用"})
		return
	}
	limit := intQuery(r, "limit", 50)
	offset := intQuery(r, "offset", 0)
	logs, total := h.auth.AuditLogs(limit, offset, r.URL.Query().Get("action"), r.URL.Query().Get("q"))
	writeJSON(w, http.StatusOK, map[string]any{"logs": logs, "total": total, "limit": limit, "offset": offset})
}

// isSecureRequest 判断是否 HTTPS（自身 TLS 或反向代理声明）。
func isSecureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return r.Header.Get("X-Forwarded-Proto") == "https"
}
