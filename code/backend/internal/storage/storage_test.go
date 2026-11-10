package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	// 生产环境中容器数据目录与宿主机共享目录是同一 bind 挂载的两端，测试用同一目录模拟。
	shared := t.TempDir()
	return NewManager(filepath.Join(shared, "network-storage.json"), filepath.Join(shared, "network"), filepath.Join(shared, "network"), filepath.Join(shared, "network-extra"))
}

func TestSaveValidation(t *testing.T) {
	manager := newTestManager(t)
	if err := manager.Save(Config{Enabled: true, Type: "ftp", Address: "192.168.0.10", RemotePath: "/vol/models"}); err == nil {
		t.Fatal("非法类型应被拒绝")
	}
	if err := manager.Save(Config{Enabled: true, Type: "nfs", Address: "192.168.0.10:8080", RemotePath: "/vol/models"}); err == nil {
		t.Fatal("带端口地址应被拒绝")
	}
	if err := manager.Save(Config{Enabled: true, Type: "nfs", Address: "192.168.0.10", RemotePath: "../escape"}); err == nil {
		t.Fatal("路径穿越应被拒绝")
	}
	if err := manager.Save(Config{Enabled: true, Type: "nfs", Address: "192.168.0.10", RemotePath: "/volume1/models"}); err != nil {
		t.Fatalf("合法配置应保存成功：%v", err)
	}
	if view := manager.View(); !view.Enabled || view.Type != "nfs" {
		t.Fatalf("保存后视图异常：%+v", view)
	}
}

func TestSavePersistsAcrossReload(t *testing.T) {
	manager := newTestManager(t)
	if err := manager.Save(Config{Enabled: true, Type: "smb", Address: "nas.local", RemotePath: "models"}); err != nil {
		t.Fatal(err)
	}
	reloaded := NewManager(manager.storePath, manager.containerPath, manager.hostPath, manager.yamlDir)
	if !reloaded.View().Enabled || reloaded.View().Address != "nas.local" {
		t.Fatal("配置未持久化")
	}
}

func TestDetectedRequiresMarker(t *testing.T) {
	manager := newTestManager(t)
	if err := manager.Save(Config{Enabled: true, Type: "nfs", Address: "192.168.0.10", RemotePath: "/vol/models"}); err != nil {
		t.Fatal(err)
	}
	if manager.Detected() {
		t.Fatal("未挂载时不应判定为已检测")
	}
	if err := os.MkdirAll(manager.containerPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if manager.Detected() {
		t.Fatal("仅有目录无标记文件不应判定为已检测")
	}
	if err := os.WriteFile(filepath.Join(manager.containerPath, MarkerFile), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !manager.Detected() {
		t.Fatal("标记文件存在应判定为已检测")
	}
}

func TestMountScript(t *testing.T) {
	manager := newTestManager(t)
	if _, err := manager.MountScript(); err == nil {
		t.Fatal("未启用时不应生成挂载脚本")
	}
	if err := manager.Save(Config{Enabled: true, Type: "nfs", Address: "192.168.0.10", RemotePath: "/volume1/models"}); err != nil {
		t.Fatal(err)
	}
	script, err := manager.MountScript()
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"mount -t nfs", "'192.168.0.10'", "'/volume1/models'", MarkerFile, "fstab", "docker restart flowpilot-web-demo"} {
		if !strings.Contains(script, fragment) {
			t.Fatalf("挂载脚本缺少片段 %q", fragment)
		}
	}
	if !strings.HasPrefix(script, "#!/usr/bin/env bash") {
		t.Fatal("挂载脚本缺少 shebang")
	}
}

func TestWriteExtraModelPaths(t *testing.T) {
	manager := newTestManager(t)
	hostFile, err := manager.WriteExtraModelPaths("comfy-01", "/app/ComfyUI")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(hostFile)
	if err != nil {
		t.Fatalf("宿主机路径文件应可读：%v", err)
	}
	content := string(data)
	if !strings.Contains(content, "base_path: /app/ComfyUI/models-net") {
		t.Fatalf("extra_model_paths 缺少 base_path：%s", content)
	}
	for _, category := range []string{"checkpoints", "loras", "text_encoders", "diffusion_models", "clip_vision", "model_patches"} {
		if !strings.Contains(content, category+": "+category) {
			t.Fatalf("extra_model_paths 缺少类别 %q", category)
		}
	}
	if strings.Contains(content, "custom_nodes") {
		t.Fatal("网络仓库不应映射 custom_nodes（可执行代码风险）")
	}
	manager.RemoveExtraModelPaths("comfy-01")
	if _, err := os.Stat(hostFile); !os.IsNotExist(err) {
		t.Fatal("清理后 yaml 应被删除")
	}
}
