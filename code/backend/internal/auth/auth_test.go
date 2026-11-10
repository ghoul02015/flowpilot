package auth

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ghoul02015/flowpilot/internal/database"
)

func openAuthDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "auth-test.db")
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("打开测试数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := database.Migrate(db, database.Migrations); err != nil {
		t.Fatalf("迁移失败：%v", err)
	}
	return db
}

func TestPasswordHashAndVerify(t *testing.T) {
	hash, err := HashPassword("FlowPilot2026")
	if err != nil {
		t.Fatalf("哈希失败：%v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$") {
		t.Fatalf("应为 argon2id 编码：%s", hash)
	}
	if !VerifyPassword(hash, "FlowPilot2026") {
		t.Fatal("正确密码应通过校验")
	}
	if VerifyPassword(hash, "WrongPassword1") {
		t.Fatal("错误密码应拒绝")
	}
	if VerifyPassword("not-a-hash", "FlowPilot2026") {
		t.Fatal("非法哈希格式应拒绝")
	}
	// 相同密码两次哈希盐不同。
	other, _ := HashPassword("FlowPilot2026")
	if hash == other {
		t.Fatal("两次哈希应使用不同盐")
	}
}

func TestPasswordPolicy(t *testing.T) {
	cases := []struct {
		password string
		ok       bool
	}{
		{"abc12345", true},
		{"Abc12345", true},
		{"short1", false},
		{"onlyletters", false},
		{"12345678", false},
	}
	for _, item := range cases {
		err := ValidatePasswordPolicy(item.password)
		if item.ok && err != nil {
			t.Fatalf("%s 应通过策略：%v", item.password, err)
		}
		if !item.ok && err == nil {
			t.Fatalf("%s 应被策略拒绝", item.password)
		}
	}
}

func TestSetupFlowAndTokenReplay(t *testing.T) {
	db := openAuthDB(t)
	dir := t.TempDir()
	manager := NewManager(db, dir, "")

	initialized, tokenReady := manager.SetupStatus()
	if initialized || !tokenReady {
		t.Fatalf("初始状态应为未初始化且令牌就绪：%v/%v", initialized, tokenReady)
	}
	if err := manager.CompleteSetup("wrong-token", "FlowPilot2026", "ip"); err != ErrTokenInvalid {
		t.Fatalf("错误令牌应拒绝：%v", err)
	}
	if err := manager.CompleteSetup("", "FlowPilot2026", "ip"); err != ErrTokenInvalid {
		t.Fatalf("空令牌应拒绝：%v", err)
	}
	if err := manager.CompleteSetup(manager.initToken, "weak", "ip"); err == nil {
		t.Fatal("弱密码应被策略拒绝")
	}
	if err := manager.CompleteSetup(manager.initToken, "FlowPilot2026", "ip"); err != nil {
		t.Fatalf("正确初始化应成功：%v", err)
	}
	if !manager.Initialized() {
		t.Fatal("初始化后应有管理员")
	}
	// 令牌重放拒绝。
	if err := manager.CompleteSetup(manager.initToken, "Another2026", "ip"); err == nil {
		t.Fatal("令牌重放必须拒绝")
	}
	// 令牌文件已删除。
	if data, err := readFile(dir); err == nil && len(data) > 0 {
		t.Fatal("初始化令牌文件应已删除")
	}
}

func readFile(dir string) ([]byte, error) {
	return readTokenFile(filepath.Join(dir, "init-token"))
}

func TestLoginRateLimitAndSessions(t *testing.T) {
	db := openAuthDB(t)
	manager := NewManager(db, t.TempDir(), "fixed-token-123456")
	if err := manager.CompleteSetup("fixed-token-123456", "FlowPilot2026", "ip"); err != nil {
		t.Fatalf("初始化失败：%v", err)
	}

	// 正常登录。
	id, csrf, err := manager.Login(AdminUsername, "FlowPilot2026", "ip-a", "test-agent")
	if err != nil || id == "" || csrf == "" {
		t.Fatalf("登录应成功：%v", err)
	}
	if user, token, ok := manager.ResolveSession(id); !ok || user != AdminUsername || token != csrf {
		t.Fatal("会话应有效且返回用户与 csrf")
	}

	// 错误密码统一文案。
	if _, _, err := manager.Login(AdminUsername, "WrongPass1", "ip-b", ""); err != ErrInvalidCredentials {
		t.Fatalf("错误密码应返回统一错误：%v", err)
	}
	if _, _, err := manager.Login("ghost", "Whatever1", "ip-b", ""); err != ErrInvalidCredentials {
		t.Fatalf("不存在用户应与错误密码同文案：%v", err)
	}

	// 限流：ip-b 连续失败 5 次后锁定。
	for i := 0; i < 4; i++ {
		_, _, _ = manager.Login(AdminUsername, "WrongPass1", "ip-b", "")
	}
	if _, _, err := manager.Login(AdminUsername, "FlowPilot2026", "ip-b", ""); err != ErrLockedOut {
		t.Fatalf("第 5 次失败后正确密码也应被锁定（计数达到阈值）：%v", err)
	}
	// 其他 IP 不受影响。
	if _, _, err := manager.Login(AdminUsername, "FlowPilot2026", "ip-c", ""); err != nil {
		t.Fatalf("其他 IP 不应被连坐：%v", err)
	}

	// 注销后失效。
	manager.Logout(id)
	if _, _, ok := manager.ResolveSession(id); ok {
		t.Fatal("注销后应失效")
	}
}

func TestChangePasswordRevokesOtherSessions(t *testing.T) {
	db := openAuthDB(t)
	manager := NewManager(db, t.TempDir(), "fixed-token-123456")
	_ = manager.CompleteSetup("fixed-token-123456", "FlowPilot2026", "ip")

	first, _, _ := manager.Login(AdminUsername, "FlowPilot2026", "ip-1", "")
	second, _, _ := manager.Login(AdminUsername, "FlowPilot2026", "ip-2", "")

	if err := manager.ChangePassword(AdminUsername, "WrongOld1", "NewPass2026", first); err != ErrInvalidCredentials {
		t.Fatalf("原密码错误应拒绝：%v", err)
	}
	if err := manager.ChangePassword(AdminUsername, "FlowPilot2026", "NewPass2026", first); err != nil {
		t.Fatalf("改密应成功：%v", err)
	}
	if _, _, ok := manager.ResolveSession(second); ok {
		t.Fatal("改密后其他会话应被撤销")
	}
	if _, _, ok := manager.ResolveSession(first); !ok {
		t.Fatal("当前会话应保留")
	}
	if _, _, err := manager.Login(AdminUsername, "FlowPilot2026", "ip-3", ""); err != ErrInvalidCredentials {
		t.Fatal("旧密码应失效")
	}
	if _, _, err := manager.Login(AdminUsername, "NewPass2026", "ip-3", ""); err != nil {
		t.Fatalf("新密码应可登录：%v", err)
	}
}

func TestAuditLogsPagingAndFilter(t *testing.T) {
	db := openAuthDB(t)
	manager := NewManager(db, t.TempDir(), "fixed-token-123456")
	_ = manager.CompleteSetup("fixed-token-123456", "FlowPilot2026", "ip")
	for i := 0; i < 5; i++ {
		manager.Audit(AuditEntry{Actor: "admin", Action: "instance.create", Object: "demo", Result: "success"})
	}
	manager.Audit(AuditEntry{Actor: "admin", Action: "model.delete", Object: "x.safetensors", Result: "success"})

	all, total := manager.AuditLogs(10, 0, "", "")
	if total < 6 || len(all) < 6 {
		t.Fatalf("应至少 6 条审计：%d/%d", total, len(all))
	}
	filtered, filteredTotal := manager.AuditLogs(10, 0, "model.delete", "")
	if filteredTotal != 1 || filtered[0].Object != "x.safetensors" {
		t.Fatalf("action 过滤应精确命中：%+v", filtered)
	}
	searched, _ := manager.AuditLogs(10, 0, "", "safetensors")
	if len(searched) != 1 {
		t.Fatalf("模糊搜索应命中 1 条：%d", len(searched))
	}
	paged, _ := manager.AuditLogs(2, 2, "instance.create", "")
	if len(paged) != 2 {
		t.Fatalf("分页应返回 2 条：%d", len(paged))
	}
}
