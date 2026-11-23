package model

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
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

func TestScanMergesNetworkRepository(t *testing.T) {
	local := t.TempDir()
	network := t.TempDir()
	writeFile(t, local, "checkpoints/local.safetensors", 4)
	writeFile(t, network, "loras/nas.safetensors", 8)
	manager := NewManager(local)
	manager.SetNetworkSource(func() (string, bool) { return network, true })
	report, err := manager.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if report.Network == nil || !report.Network.Available {
		t.Fatal("网络仓库应可用")
	}
	if len(report.Files) != 2 {
		t.Fatalf("应合并两个仓库的文件，实际 %d", len(report.Files))
	}
	for _, file := range report.Files {
		if file.Node != "本机" {
			t.Fatal("Node 应恒为本机")
		}
		switch file.Path {
		case "checkpoints/local.safetensors":
			if file.Origin != "local" {
				t.Fatal("本地文件 origin 应为 local")
			}
		case "loras/nas.safetensors":
			if file.Origin != "network" {
				t.Fatal("NAS 文件 origin 应为 network")
			}
		default:
			t.Fatalf("未知文件 %s", file.Path)
		}
	}
	if report.TotalBytes != 4 {
		t.Fatalf("TotalBytes 只应统计本地仓库，实际 %d", report.TotalBytes)
	}
}

func TestNetworkUnavailableDoesNotFailScan(t *testing.T) {
	local := t.TempDir()
	writeFile(t, local, "vae/v.safetensors", 2)
	manager := NewManager(local)
	manager.SetNetworkSource(func() (string, bool) { return filepath.Join(t.TempDir(), "missing"), true })
	report, err := manager.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if report.Network == nil || report.Network.Available {
		t.Fatal("网络仓库缺失应标记为不可用而非报错")
	}
	if len(report.Files) != 1 {
		t.Fatal("本地仓库文件不应受网络仓库影响")
	}
}

// 删除模型文件应级联清理各实例指向它的符号链接，并保留其余链接。
func TestDeleteCascadesInstanceLinks(t *testing.T) {
	local := t.TempDir()
	writeFile(t, local, "loras/a.safetensors", 2)
	writeFile(t, local, "loras/b.safetensors", 2)
	manager := NewManager(local)

	linksRoot := t.TempDir()
	instanceDir := filepath.Join(linksRoot, "demo", "loras")
	if err := os.MkdirAll(instanceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.safetensors", "b.safetensors"} {
		if err := os.Symlink("/data/models/loras/"+name, filepath.Join(instanceDir, name)); err != nil {
			t.Fatal(err)
		}
	}
	manager.SetLinksRoot(linksRoot)

	if err := manager.Delete("loras/a.safetensors"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(instanceDir, "a.safetensors")); !os.IsNotExist(err) {
		t.Fatalf("被删模型的关联链接应被清理，得到 %v", err)
	}
	if _, err := os.Lstat(filepath.Join(instanceDir, "b.safetensors")); err != nil {
		t.Fatalf("其余关联不应受影响：%v", err)
	}
}

// TestCacheTTLAndInvalidate 本地缓存命中、写后失效可立即看到新文件。
func TestCacheTTLAndInvalidate(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "vae"), "a.safetensors", 10)
	manager := NewManager(root)
	manager.SetCacheTTL(time.Hour, time.Hour)
	first, err := manager.Scan()
	if err != nil || len(first.Files) != 1 {
		t.Fatalf("首次扫描异常：%v files=%d", err, len(first.Files))
	}
	writeFile(t, filepath.Join(root, "vae"), "b.safetensors", 20)
	cached, _ := manager.Scan()
	if len(cached.Files) != 1 {
		t.Fatalf("TTL 内应命中缓存，实际 %d 个文件", len(cached.Files))
	}
	manager.InvalidateLocal()
	after, _ := manager.Scan()
	if len(after.Files) != 2 {
		t.Fatalf("失效后应重新扫描到 2 个文件，实际 %d", len(after.Files))
	}
}

// TestNetworkFailureKeepsLocal NAS 不可用时本地结果仍可返回，网络状态标记错误。
func TestNetworkFailureKeepsLocal(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "checkpoints"), "local.safetensors", 8)
	manager := NewManager(root)
	manager.SetNetworkSource(func() (string, bool) {
		return filepath.Join(root, "missing-nas"), true
	})
	report, err := manager.Scan()
	if err != nil {
		t.Fatalf("NAS 故障不应让本地扫描失败：%v", err)
	}
	if len(report.Files) != 1 || report.Files[0].Origin != "local" {
		t.Fatalf("本地文件应保留：%+v", report.Files)
	}
	if report.Network == nil || report.Network.Available {
		t.Fatalf("网络状态应标记不可用：%+v", report.Network)
	}
	if report.Network.Error == "" {
		t.Fatal("网络不可用时应带错误说明")
	}
}

// TestNetworkInvalidateIndependent 网络失效不影响本地缓存命中。
func TestNetworkInvalidateIndependent(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "vae"), "a.safetensors", 10)
	manager := NewManager(root)
	manager.SetCacheTTL(time.Hour, time.Hour)
	if _, err := manager.Scan(); err != nil {
		t.Fatal(err)
	}
	manager.InvalidateNetwork()
	if _, err := manager.Scan(); err != nil {
		t.Fatal(err)
	}
	// 本地缓存仍有效：新增文件在 TTL 内不可见。
	writeFile(t, filepath.Join(root, "vae"), "b.safetensors", 10)
	report, _ := manager.Scan()
	if len(report.Files) != 1 {
		t.Fatalf("网络失效不应清空本地缓存，实际 %d", len(report.Files))
	}
}
