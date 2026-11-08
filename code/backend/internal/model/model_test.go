package model

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, root, rel string, size int) {
	t.Helper()
	target := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanCategorizesFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "checkpoints/sd_xl_base.safetensors", 100)
	writeFile(t, root, "loras/detail.safetensors", 50)
	writeFile(t, root, "custom_nodes/keep.py", 10) // 未知类别保留原目录名
	writeFile(t, root, "readme.txt", 5)            // 根目录文件归为其他
	writeFile(t, root, ".hidden/skip.safetensors", 999)

	report, err := NewManager(root).Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Files) != 4 {
		t.Fatalf("期望 4 个文件，实际 %d：%+v", len(report.Files), report.Files)
	}
	if report.TotalBytes != 165 {
		t.Fatalf("总大小错误：%d", report.TotalBytes)
	}
	byType := map[string]string{}
	for _, file := range report.Files {
		byType[file.Path] = file.Type
	}
	if byType["checkpoints/sd_xl_base.safetensors"] != "Checkpoints" {
		t.Fatalf("checkpoints 类型映射错误：%s", byType["checkpoints/sd_xl_base.safetensors"])
	}
	if byType["loras/detail.safetensors"] != "LoRA" {
		t.Fatalf("loras 类型映射错误：%s", byType["loras/detail.safetensors"])
	}
	if byType["readme.txt"] != "其他" {
		t.Fatalf("根目录文件应归为其他：%s", byType["readme.txt"])
	}
}

func TestScanMissingRootReturnsEmpty(t *testing.T) {
	report, err := NewManager(filepath.Join(t.TempDir(), "not-exist")).Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Files) != 0 || report.TotalBytes != 0 {
		t.Fatalf("缺失目录应返回空仓库：%+v", report)
	}
}

func TestDeleteRemovesFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "vae/ft.vae", 20)
	manager := NewManager(root)
	if err := manager.Delete("vae/ft.vae"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "vae", "ft.vae")); !os.IsNotExist(err) {
		t.Fatal("文件应已被删除")
	}
	var notFound *NotFoundError
	if err := manager.Delete("vae/ft.vae"); !errors.As(err, &notFound) {
		t.Fatalf("重复删除应返回 NotFoundError：%v", err)
	}
}

func TestDeleteRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	manager := NewManager(root)
	for _, rel := range []string{"../escape.safetensors", "/etc/passwd", "..", ".", "vae/../../escape"} {
		var validation *ValidationError
		if err := manager.Delete(rel); !errors.As(err, &validation) {
			t.Fatalf("路径 %q 应被拒绝：%v", rel, err)
		}
	}
}

// 扫描结果在 TTL 内走缓存，删除操作主动失效缓存。
func TestScanCacheAndDeleteInvalidation(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "checkpoints/a.safetensors", 10)
	manager := NewManager(root)

	first, err := manager.Scan()
	if err != nil || len(first.Files) != 1 {
		t.Fatalf("首次扫描异常：%v %+v", err, first)
	}
	// TTL 内新增文件不可见（缓存命中）。
	writeFile(t, root, "checkpoints/b.safetensors", 10)
	cached, err := manager.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(cached.Files) != 1 {
		t.Fatalf("TTL 内应返回缓存结果：%+v", cached.Files)
	}
	// 删除后缓存必须立即失效。
	if err := manager.Delete("checkpoints/a.safetensors"); err != nil {
		t.Fatal(err)
	}
	afterDelete, err := manager.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(afterDelete.Files) != 1 || afterDelete.Files[0].Path != "checkpoints/b.safetensors" {
		t.Fatalf("删除后应立即看到最新仓库：%+v", afterDelete.Files)
	}
}
