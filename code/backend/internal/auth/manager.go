package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// CookieName 会话 Cookie 名。
	CookieName = "flowpilot_session"
	// CSRFHeader 写请求必须携带的 CSRF 头。
	CSRFHeader = "X-FlowPilot-CSRF"
	// 会话时效：闲置 30 分钟，绝对 7 天。
	idleTimeout   = 30 * time.Minute
	absoluteLimit = 7 * 24 * time.Hour
	// 登录限流：每 IP 连续 5 次失败锁定 15 分钟。
	maxLoginFailures = 5
	loginLockWindow  = 15 * time.Minute
	// 审计单页上限。
	MaxAuditLimit = 200
)

// 常见错误。
var (
	ErrSetupRequired   = errors.New("平台未初始化")
	ErrLockedOut       = errors.New("失败次数过多，请 15 分钟后重试")
	ErrTokenInvalid    = errors.New("初始化令牌不正确")
	ErrPasswordPolicy  = errors.New("密码不满足强度要求")
	ErrSessionNotFound = errors.New("会话不存在")
)

// Session 是会话的对外视图（不含 ID 本体与 csrf）。
type Session struct {
	ID              string    `json:"id"`
	Username        string    `json:"username"`
	CreatedAt       time.Time `json:"createdAt"`
	LastSeenAt      time.Time `json:"lastSeenAt"`
	IdleExpiresAt   time.Time `json:"idleExpiresAt"`
	AbsoluteExpires time.Time `json:"absoluteExpiresAt"`
	IPHash          string    `json:"ipHash"`
	UserAgent       string    `json:"userAgent"`
	Current         bool      `json:"current,omitempty"`
}

// AuditEntry 是一条审计记录。
type AuditEntry struct {
	ID        int64  `json:"id"`
	TS        string `json:"ts"`
	Actor     string `json:"actor"`
	Action    string `json:"action"`
	Object    string `json:"object"`
	Result    string `json:"result"`
	IPHash    string `json:"ipHash"`
	RequestID string `json:"requestId"`
	Detail    string `json:"detail"`
}

// loginLimiter 每 IP 失败计数与锁定时间。
type loginLimiter struct {
	failures    int
	lockedUntil time.Time
}

// Manager 单管理员认证管理器。
type Manager struct {
	db           *sql.DB
	mu           sync.Mutex
	limiters     map[string]*loginLimiter
	tokenPath    string // 初始化令牌文件路径（完成初始化后删除）
	initToken    string // 内存中的初始化令牌
	tokenUsed    bool
	initTokenSet bool
}

// NewManager 创建认证管理器；dataDir 下读取或生成一次性初始化令牌。
// 环境变量注入由调用方通过 fixedInitToken 完成（demo/E2E 场景）。
func NewManager(db *sql.DB, dataDir string, fixedInitToken string) *Manager {
	m := &Manager{db: db, limiters: map[string]*loginLimiter{}}
	tokenPath := filepath.Join(dataDir, "init-token")
	m.tokenPath = tokenPath
	if fixedInitToken != "" {
		m.initToken = fixedInitToken
		m.initTokenSet = true
		_ = os.WriteFile(tokenPath, []byte(fixedInitToken), 0o600)
		return m
	}
	if data, err := os.ReadFile(tokenPath); err == nil && len(strings.TrimSpace(string(data))) >= 16 {
		m.initToken = strings.TrimSpace(string(data))
		m.initTokenSet = true
		return m
	}
	// 生成新令牌（目录若不可写则认证进入不可初始化状态，由 setup 接口报错）。
	token := randomToken(24)
	if err := os.WriteFile(tokenPath, []byte(token), 0o600); err != nil {
		slog.Error("初始化令牌写入失败", "path", tokenPath, "error", err)
		return m
	}
	m.initToken = token
	m.initTokenSet = true
	slog.Info("已生成管理员初始化令牌", "path", tokenPath)
	return m
}

// Initialized 是否已有管理员账户。
func (m *Manager) Initialized() bool {
	var count int
	if err := m.db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		return false
	}
	return count > 0
}

// SetupStatus 返回初始化状态。
func (m *Manager) SetupStatus() (initialized bool, tokenReady bool) {
	if m.Initialized() {
		return true, false
	}
	return false, m.initTokenSet && !m.tokenUsed
}

// CompleteSetup 校验令牌并创建管理员；成功后令牌立即失效。
func (m *Manager) CompleteSetup(token, password, ipHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Initialized() {
		return errors.New("平台已完成初始化")
	}
	if m.tokenUsed {
		return ErrTokenInvalid
	}
	if !m.initTokenSet || subtle.ConstantTimeCompare([]byte(token), []byte(m.initToken)) != 1 {
		return ErrTokenInvalid
	}
	if err := ValidatePasswordPolicy(password); err != nil {
		return err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if _, err := m.db.Exec("INSERT INTO users (username, password_hash, created_at, updated_at) VALUES (?, ?, ?, ?)",
		AdminUsername, hash, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("创建管理员失败：%w", err)
	}
	// 令牌一次性：删除文件并标记已用。
	_ = os.Remove(m.tokenPath)
	m.tokenUsed = true
	m.Audit(AuditEntry{TS: now.Format(time.RFC3339Nano), Actor: AdminUsername, Action: "setup.completed", Result: "success", IPHash: ipHash})
	slog.Info("平台初始化完成", "username", AdminUsername)
	return nil
}

// Login 校验限流与凭据，成功创建会话并返回 (sessionID, csrfToken)。
func (m *Manager) Login(username, password, ipHash, userAgent string) (string, string, error) {
	m.mu.Lock()
	limiter, ok := m.limiters[ipHash]
	if !ok {
		limiter = &loginLimiter{}
		m.limiters[ipHash] = limiter
	}
	if time.Now().Before(limiter.lockedUntil) {
		m.mu.Unlock()
		m.Audit(AuditEntry{TS: nowText(), Actor: username, Action: "auth.login", Result: "locked", IPHash: ipHash, Detail: "失败次数过多"})
		return "", "", ErrLockedOut
	}
	m.mu.Unlock()

	var hash string
	err := m.db.QueryRow("SELECT password_hash FROM users WHERE username = ?", username).Scan(&hash)
	if err != nil || !VerifyPassword(hash, password) {
		m.recordFailure(ipHash, username)
		return "", "", ErrInvalidCredentials
	}
	// 清零失败计数。
	m.mu.Lock()
	limiter.failures = 0
	m.mu.Unlock()

	now := time.Now().UTC()
	id := randomToken(32)
	csrf := randomToken(32)
	if _, err := m.db.Exec(`INSERT INTO sessions (id, username, csrf_token, created_at, last_seen_at,
		idle_expires_at, absolute_expires_at, revoked, ip_hash, user_agent)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, ?)`,
		id, username, csrf, nowText(), nowText(),
		now.Add(idleTimeout).Format(time.RFC3339Nano),
		now.Add(absoluteLimit).Format(time.RFC3339Nano),
		ipHash, truncate(userAgent, 200)); err != nil {
		return "", "", fmt.Errorf("创建会话失败：%w", err)
	}
	m.Audit(AuditEntry{TS: nowText(), Actor: username, Action: "auth.login", Result: "success", IPHash: ipHash})
	return id, csrf, nil
}

// recordFailure 记录一次登录失败；达到阈值锁定。
func (m *Manager) recordFailure(ipHash, username string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	limiter := m.limiters[ipHash]
	if limiter == nil {
		limiter = &loginLimiter{}
		m.limiters[ipHash] = limiter
	}
	limiter.failures++
	if limiter.failures >= maxLoginFailures {
		limiter.lockedUntil = time.Now().Add(loginLockWindow)
		limiter.failures = 0
	}
	m.Audit(AuditEntry{TS: nowText(), Actor: username, Action: "auth.login", Result: "failure", IPHash: ipHash, Detail: "凭据错误"})
}

// ResolveSession 校验会话有效性并滑动续期；返回用户名与 csrf。
func (m *Manager) ResolveSession(id string) (username, csrf string, ok bool) {
	if id == "" {
		return "", "", false
	}
	var user, csrfToken string
	var lastSeen, idleExpire, absoluteExpire string
	var revoked int
	err := m.db.QueryRow(`SELECT username, csrf_token, last_seen_at, idle_expires_at, absolute_expires_at, revoked
		FROM sessions WHERE id = ?`, id).Scan(&user, &csrfToken, &lastSeen, &idleExpire, &absoluteExpire, &revoked)
	if err != nil || revoked == 1 {
		return "", "", false
	}
	now := time.Now().UTC()
	idleAt, err1 := time.Parse(time.RFC3339Nano, idleExpire)
	absAt, err2 := time.Parse(time.RFC3339Nano, absoluteExpire)
	if err1 != nil || err2 != nil || now.After(idleAt) || now.After(absAt) {
		return "", "", false
	}
	// 滑动续期（剩余绝对寿命不超过 absoluteLimit）。
	nextIdle := now.Add(idleTimeout)
	if nextIdle.After(absAt) {
		nextIdle = absAt
	}
	_, _ = m.db.Exec("UPDATE sessions SET last_seen_at = ?, idle_expires_at = ? WHERE id = ?",
		nowText(), nextIdle.Format(time.RFC3339Nano), id)
	return user, csrfToken, true
}

// CSRFMatches 校验会话的 CSRF 令牌（常数时间）。
func (m *Manager) CSRFMatches(sessionID, token string) bool {
	var stored string
	if err := m.db.QueryRow("SELECT csrf_token FROM sessions WHERE id = ? AND revoked = 0", sessionID).Scan(&stored); err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(stored), []byte(token)) == 1
}

// Logout 撤销当前会话。
func (m *Manager) Logout(id string) {
	if id == "" {
		return
	}
	if _, err := m.db.Exec("UPDATE sessions SET revoked = 1 WHERE id = ?", id); err != nil {
		logSQLError("Logout", err)
	}
	m.Audit(AuditEntry{TS: nowText(), Actor: AdminUsername, Action: "auth.logout", Result: "success"})
}

// ChangePassword 修改密码并撤销除当前会话外的全部会话。
func (m *Manager) ChangePassword(username, current, next, currentSessionID string) error {
	var hash string
	if err := m.db.QueryRow("SELECT password_hash FROM users WHERE username = ?", username).Scan(&hash); err != nil {
		return ErrInvalidCredentials
	}
	if !VerifyPassword(hash, current) {
		m.Audit(AuditEntry{TS: nowText(), Actor: username, Action: "auth.password_change", Result: "failure", Detail: "原密码错误"})
		return ErrInvalidCredentials
	}
	if err := ValidatePasswordPolicy(next); err != nil {
		return err
	}
	newHash, err := HashPassword(next)
	if err != nil {
		return err
	}
	if _, err := m.db.Exec("UPDATE users SET password_hash = ?, updated_at = ? WHERE username = ?",
		newHash, nowText(), username); err != nil {
		return fmt.Errorf("更新密码失败：%w", err)
	}
	if currentSessionID != "" {
		if _, err := m.db.Exec("UPDATE sessions SET revoked = 1 WHERE username = ? AND id != ?", username, currentSessionID); err != nil {
			logSQLError("ChangePassword revoke others", err)
		}
	}
	m.Audit(AuditEntry{TS: nowText(), Actor: username, Action: "auth.password_change", Result: "success"})
	return nil
}

// ListSessions 返回未撤销且未过期的会话（current 标记当前会话）。
func (m *Manager) ListSessions(username, currentID string) []Session {
	rows, err := m.db.Query(`SELECT id, username, created_at, last_seen_at, idle_expires_at, absolute_expires_at, ip_hash, user_agent
		FROM sessions WHERE username = ? AND revoked = 0 ORDER BY last_seen_at DESC`, username)
	if err != nil {
		logSQLError("ListSessions", err)
		return []Session{}
	}
	defer rows.Close()
	now := time.Now().UTC()
	list := []Session{}
	for rows.Next() {
		var item Session
		var createdAt, lastSeen, idleExpire, absoluteExpire string
		if err := rows.Scan(&item.ID, &item.Username, &createdAt, &lastSeen, &idleExpire, &absoluteExpire, &item.IPHash, &item.UserAgent); err != nil {
			continue
		}
		item.CreatedAt = parseTime(createdAt)
		item.LastSeenAt = parseTime(lastSeen)
		item.IdleExpiresAt = parseTime(idleExpire)
		item.AbsoluteExpires = parseTime(absoluteExpire)
		if now.After(item.IdleExpiresAt) || now.After(item.AbsoluteExpires) {
			continue
		}
		prefix := item.ID
		if len(prefix) > 8 {
			prefix = prefix[:8]
		}
		item.ID = prefix + "…"
		item.Current = strings.HasPrefix(currentID, prefix)
		list = append(list, item)
	}
	return list
}

// RevokeSession 管理员撤销指定会话（按前缀匹配）。
func (m *Manager) RevokeSession(actor, prefix string) error {
	rows, err := m.db.Query("SELECT id FROM sessions WHERE revoked = 0 AND substr(id, 1, 8) = ?", prefix)
	if err != nil {
		return err
	}
	defer rows.Close()
	revoked := 0
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		if _, err := m.db.Exec("UPDATE sessions SET revoked = 1 WHERE id = ?", id); err == nil {
			revoked++
		}
	}
	m.Audit(AuditEntry{TS: nowText(), Actor: actor, Action: "auth.session_revoked", Object: prefix, Result: fmt.Sprintf("revoked=%d", revoked)})
	if revoked == 0 {
		return ErrSessionNotFound
	}
	return nil
}

// Audit 写入一条审计记录（失败仅日志，不阻断业务）。
func (m *Manager) Audit(entry AuditEntry) {
	if entry.TS == "" {
		entry.TS = nowText()
	}
	if _, err := m.db.Exec(`INSERT INTO audit_logs (ts, actor, action, object, result, ip_hash, request_id, detail)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.TS, entry.Actor, entry.Action, entry.Object, entry.Result, entry.IPHash, entry.RequestID, entry.Detail); err != nil {
		logSQLError("Audit "+entry.Action, err)
	}
}

// AuditLogs 分页查询审计（时间倒序）；action 精确过滤，q 模糊匹配 action/object/detail。
func (m *Manager) AuditLogs(limit, offset int, action, q string) ([]AuditEntry, int) {
	if limit <= 0 || limit > MaxAuditLimit {
		limit = 50
	}
	where := "1=1"
	args := []any{}
	if action != "" {
		where += " AND action = ?"
		args = append(args, action)
	}
	if q != "" {
		where += " AND (action LIKE ? OR object LIKE ? OR detail LIKE ? OR actor LIKE ?)"
		pattern := "%" + q + "%"
		args = append(args, pattern, pattern, pattern, pattern)
	}
	var total int
	_ = m.db.QueryRow("SELECT COUNT(*) FROM audit_logs WHERE "+where, args...).Scan(&total)
	rows, err := m.db.Query("SELECT id, ts, actor, action, object, result, ip_hash, request_id, detail FROM audit_logs WHERE "+where+" ORDER BY id DESC LIMIT ? OFFSET ?",
		append(args, limit, offset)...)
	if err != nil {
		logSQLError("AuditLogs", err)
		return []AuditEntry{}, total
	}
	defer rows.Close()
	list := []AuditEntry{}
	for rows.Next() {
		var item AuditEntry
		if err := rows.Scan(&item.ID, &item.TS, &item.Actor, &item.Action, &item.Object, &item.Result, &item.IPHash, &item.RequestID, &item.Detail); err != nil {
			continue
		}
		list = append(list, item)
	}
	return list, total
}

// CleanupStartup 启动清理：撤销遗留的已过期会话。
func (m *Manager) CleanupStartup() {
	result, err := m.db.Exec("UPDATE sessions SET revoked = 1 WHERE revoked = 0 AND (idle_expires_at < ? OR absolute_expires_at < ?)",
		nowText(), nowText())
	if err == nil {
		if count, _ := result.RowsAffected(); count > 0 {
			slog.Info("启动清理过期会话", "revoked", count)
		}
	}
}

// SessionCookie 构造会话 Cookie（HttpOnly＋SameSite=Lax；HTTPS 时 Secure）。
func (m *Manager) SessionCookie(id string, secure bool, maxAge int) *http.Cookie {
	cookie := &http.Cookie{
		Name:     CookieName,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
	if secure {
		cookie.Secure = true
	}
	if maxAge > 0 {
		cookie.MaxAge = maxAge
	}
	return cookie
}

// IPHashOf 计算客户端 IP 摘要（sha256 前 16 hex，不留存明文）。
func IPHashOf(request *http.Request) string {
	ip := request.RemoteAddr
	if index := strings.LastIndex(ip, ":"); index > 0 {
		ip = ip[:index]
	}
	if forwarded := request.Header.Get("X-Forwarded-For"); forwarded != "" {
		ip = strings.TrimSpace(strings.Split(forwarded, ",")[0])
	}
	sum := sha256.Sum256([]byte(ip))
	return hex.EncodeToString(sum[:8])
}

func nowText() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func parseTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func logSQLError(context string, err error) {
	slog.Error("认证数据库操作失败", "context", context, "error", err)
}
