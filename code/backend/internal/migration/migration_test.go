package migration

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFileTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func newTestManager(t *testing.T) (*Manager, string, string) {
	t.Helper()
	shared := t.TempDir()
	workspaces := t.TempDir()
	backups := t.TempDir()
	writeFileTree(t, shared, map[string]string{
		"outputs/a.png":        "hello",
		"outputs/nested/b.png": "world-world",
		"workflows/flow.json":  `{"a":1}`,
	})
	// 实例 workspace（模拟新式实例）。
	writeFileTree(t, workspaces, map[string]string{
		"demo-uuid/outputs/.keep":   "",
		"demo-uuid/workflows/.keep": "",
	})
	manager := NewManager(shared, backups, func(instance string) (string, bool) {
		if instance == "demo" {
			return filepath.Join(workspaces, "demo-uuid"), true
		}
		return "", false
	})
	return manager, shared, workspaces
}

func TestScanCounts(t *testing.T) {
	manager, _, _ := newTestManager(t)
	report := manager.Scan()
	if !report.Outputs.Exists || report.Outputs.Files != 2 || report.Outputs.Bytes != int64(len("hello")+len("world-world")) {
		t.Fatalf("outputs 统计异常：%+v", report.Outputs)
	}
	if !report.Workflows.Exists || report.Workflows.Files != 1 {
		t.Fatalf("workflows 统计异常：%+v", report.Workflows)
	}
}

func TestPreviewRequiresWorkspace(t *testing.T) {
	manager, _, _ := newTestManager(t)
	plan := manager.Preview("ghost")
	if plan.Available || plan.Reason == "" {
		t.Fatalf("无 workspace 实例应不可迁移：%+v", plan)
	}
	plan = manager.Preview("demo")
	if !plan.Available || plan.TotalFiles != 3 {
		t.Fatalf("demo 预览应可用且 3 文件：%+v", plan)
	}
}

func TestRunCopyAndVerify(t *testing.T) {
	manager, shared, workspaces := newTestManager(t)
	result, err := manager.Run("demo", []string{"outputs", "workflows"}, "copy")
	if err != nil {
		t.Fatalf("copy 迁移失败：%v", err)
	}
	if !result.Verified || result.Copied["outputs"] != 2 || result.Copied["workflows"] != 1 {
		t.Fatalf("迁移结果异常：%+v", result)
	}
	// 源数据保留（copy 模式）。
	if _, err := os.Stat(filepath.Join(shared, "outputs/a.png")); err != nil {
		t.Fatal("copy 模式源数据必须保留")
	}
	// 目标数据可读。
	content, err := os.ReadFile(filepath.Join(workspaces, "demo-uuid/outputs/a.png"))
	if err != nil || string(content) != "hello" {
		t.Fatalf("目标文件内容异常：%q %v", content, err)
	}
	// 备份生成。
	if result.BackupDir == "" {
		t.Fatal("迁移必须生成备份目录")
	}
	if _, err := os.Stat(filepath.Join(result.BackupDir, "outputs/a.png")); err != nil {
		t.Fatal("备份应包含源数据")
	}
	// 重复迁移：同名跳过。
	second, err := manager.Run("demo", []string{"outputs"}, "copy")
	if err != nil {
		t.Fatalf("重复迁移失败：%v", err)
	}
	if second.Copied["outputs"] != 0 || len(second.Warnings) == 0 {
		t.Fatalf("重复迁移应跳过同名文件：%+v", second)
	}
}

func TestRunMoveRemovesSource(t *testing.T) {
	manager, shared, workspaces := newTestManager(t)
	result, err := manager.Run("demo", []string{"workflows"}, "move")
	if err != nil {
		t.Fatalf("move 迁移失败：%v", err)
	}
	if result.Copied["workflows"] != 1 {
		t.Fatalf("move 应迁移 1 个文件：%+v", result)
	}
	if _, err := os.Stat(filepath.Join(shared, "workflows/flow.json")); !os.IsNotExist(err) {
		t.Fatal("move 模式源文件应被移除")
	}
	if _, err := os.Stat(filepath.Join(workspaces, "demo-uuid/workflows/flow.json")); err != nil {
		t.Fatal("move 目标文件应存在")
	}
}

func TestRunInvalidInput(t *testing.T) {
	manager, _, _ := newTestManager(t)
	if _, err := manager.Run("demo", []string{"models"}, "copy"); err == nil {
		t.Fatal("非法 scope 应拒绝")
	}
	if _, err := manager.Run("demo", []string{"outputs"}, "mirror"); err == nil {
		t.Fatal("非法 mode 应拒绝")
	}
	if _, err := manager.Run("ghost", []string{"outputs"}, "copy"); err == nil {
		t.Fatal("无 workspace 实例应拒绝")
	}
}
