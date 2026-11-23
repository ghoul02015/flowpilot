package backup

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestBackupExecAndList(t *testing.T) {
	root := t.TempDir()
	workspaces := filepath.Join(root, "instances")
	if err := os.MkdirAll(filepath.Join(workspaces, "uuid-a", "outputs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspaces, "uuid-a", "outputs", "a.png"), []byte("image"), 0o644); err != nil {
		t.Fatal(err)
	}
	dummyDB := filepath.Join(root, "flowpilot.db")
	if err := os.WriteFile(dummyDB, []byte("sqlite"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(filepath.Join(root, "backups"), workspaces, "")

	result, err := runner.Exec("0.0.9.12-test", func(target string) error {
		return os.WriteFile(target, []byte("sqlite-dump"), 0o644)
	})
	if err != nil {
		t.Fatalf("备份失败：%v", err)
	}
	if result.SizeBytes <= 0 || result.Checksum == "" {
		t.Fatalf("备份结果异常：%+v", result)
	}
	if !result.Manifest.Components["sqlite"] || !result.Manifest.Components["workspaces"] || result.Manifest.InstanceCount != 1 {
		t.Fatalf("manifest 异常：%+v", result.Manifest)
	}
	// 临时文件已改名（无 .part 残留）。
	if _, err := os.Stat(result.Path + ".part"); !os.IsNotExist(err) {
		t.Fatal("不应有 .part 临时文件残留")
	}
	// 包内容可读：manifest.json + flowpilot.db + instances/。
	file, err := os.Open(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err != nil {
			break
		}
		names[header.Name] = true
	}
	if !names["instances/uuid-a/outputs/a.png"] {
		t.Fatalf("备份包内容缺失：%v", names)
	}
	// 列表。
	list := runner.ListBackups()
	if len(list) != 1 || list[0].Name != result.Name {
		t.Fatalf("备份列表异常：%+v", list)
	}
	if list[0].Checksum != result.Checksum {
		t.Fatal("sidecar manifest 的 checksum 应一致")
	}
}
