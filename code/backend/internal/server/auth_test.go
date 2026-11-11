package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ghoul02015/flowpilot/internal/auth"
	"github.com/ghoul02015/flowpilot/internal/catalog"
	"github.com/ghoul02015/flowpilot/internal/database"
	"github.com/ghoul02015/flowpilot/internal/docker"
)

// newAuthedHandler 构造启用认证的处理器（真实 auth manager，禁用关闭开关）。
func newAuthedHandler(t *testing.T) http.Handler {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "auth-test.db")
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("打开数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := database.Migrate(db, database.Migrations); err != nil {
		t.Fatalf("迁移失败：%v", err)
	}
	manager := auth.NewManager(db, t.TempDir(), "test-init-token-0001")
	return New(t.TempDir(), docker.NewClient("/unused/docker.sock"), nil, nil, nil, nil, nil, "", catalog.NewManager(""), nil, nil, "", manager, false, nil, "", nil, nil)
}

func TestAuthMiddlewareFullFlow(t *testing.T) {
	handler := newAuthedHandler(t)

	// 1. 健康检查豁免。
	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("health 应豁免认证：%d", response.Code)
	}

	// 2. 未初始化时业务 API 返回 setup_required。
	request = httptest.NewRequest(http.MethodGet, "/api/v1/instances", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), "setup_required") {
		t.Fatalf("未初始化应 401 setup_required，实际 %d %s", response.Code, response.Body.String())
	}

	// 3. 初始化状态接口豁免。
	request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/setup/status", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"initialized":false`) {
		t.Fatalf("setup status 异常：%d %s", response.Code, response.Body.String())
	}

	// 4. 错误令牌初始化拒绝。
	request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/setup", strings.NewReader(`{"token":"wrong","password":"FlowPilot2026"}`))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("错误令牌应 401：%d", response.Code)
	}

	// 5. 正确初始化。
	request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/setup", strings.NewReader(`{"token":"test-init-token-0001","password":"FlowPilot2026"}`))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("初始化应成功：%d %s", response.Code, response.Body.String())
	}

	// 6. 令牌重放拒绝。
	request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/setup", strings.NewReader(`{"token":"test-init-token-0001","password":"Other2026"}`))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("令牌重放应拒绝：%d", response.Code)
	}

	// 7. 未登录业务 API 401 unauthenticated。
	request = httptest.NewRequest(http.MethodGet, "/api/v1/instances", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), "unauthenticated") {
		t.Fatalf("未登录应 401 unauthenticated：%d %s", response.Code, response.Body.String())
	}

	// 8. 登录错误密码。
	request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"WrongPass1"}`))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || strings.Contains(response.Body.String(), "哈希") {
		t.Fatalf("错误密码应统一文案：%d %s", response.Code, response.Body.String())
	}

	// 9. 登录成功拿 cookie 与 csrf。
	request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"FlowPilot2026"}`))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("登录应成功：%d %s", response.Code, response.Body.String())
	}
	var loginBody struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.NewDecoder(response.Body).Decode(&loginBody); err != nil || loginBody.CSRFToken == "" {
		t.Fatalf("登录响应应含 csrfToken：%v", err)
	}
	var sessionCookie string
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == auth.CookieName {
			sessionCookie = cookie.Value
		}
	}
	if sessionCookie == "" {
		t.Fatal("登录应设置会话 Cookie")
	}
	csrf := loginBody.CSRFToken

	// 10. 带 cookie 读 API 通过（session 端点不依赖业务管理器）。
	request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	request.AddCookie(&http.Cookie{Name: auth.CookieName, Value: sessionCookie})
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), csrf) {
		t.Fatalf("已登录读 API 应通过并返回 csrfToken：%d %s", response.Code, response.Body.String())
	}

	// 11. 写方法缺 CSRF 头 403。
	request = httptest.NewRequest(http.MethodPost, "/api/v1/instances", strings.NewReader(`{}`))
	request.AddCookie(&http.Cookie{Name: auth.CookieName, Value: sessionCookie})
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("写方法缺 CSRF 应 403：%d", response.Code)
	}

	// 12. 带正确 CSRF 后通过（进入业务校验）。
	request = httptest.NewRequest(http.MethodPost, "/api/v1/instances", strings.NewReader(`{}`))
	request.AddCookie(&http.Cookie{Name: auth.CookieName, Value: sessionCookie})
	request.Header.Set(auth.CSRFHeader, csrf)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code == http.StatusForbidden || response.Code == http.StatusUnauthorized {
		t.Fatalf("带 CSRF 应通过中间件：%d %s", response.Code, response.Body.String())
	}

	// 13. Origin 不匹配 403。
	request = httptest.NewRequest(http.MethodGet, "/api/v1/instances", nil)
	request.AddCookie(&http.Cookie{Name: auth.CookieName, Value: sessionCookie})
	request.Header.Set("Origin", "http://evil.example.com")
	request.Host = "localhost:18080"
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("Origin 不匹配应 403：%d", response.Code)
	}

	// 14. 注销后失效。
	request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	request.AddCookie(&http.Cookie{Name: auth.CookieName, Value: sessionCookie})
	request.Header.Set(auth.CSRFHeader, csrf)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("注销应成功：%d", response.Code)
	}
	request = httptest.NewRequest(http.MethodGet, "/api/v1/instances", nil)
	request.AddCookie(&http.Cookie{Name: auth.CookieName, Value: sessionCookie})
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("注销后应 401：%d", response.Code)
	}

	// 15. 审计已记录登录与初始化事件。
	request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"FlowPilot2026"}`))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var secondLogin struct {
		CSRFToken string `json:"csrfToken"`
	}
	_ = json.NewDecoder(response.Body).Decode(&secondLogin)
	var cookieValue string
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == auth.CookieName {
			cookieValue = cookie.Value
		}
	}
	request = httptest.NewRequest(http.MethodGet, "/api/v1/audit/logs?limit=50", nil)
	request.AddCookie(&http.Cookie{Name: auth.CookieName, Value: cookieValue})
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "auth.login") {
		t.Fatalf("审计应记录登录事件：%d %s", response.Code, response.Body.String())
	}
}
