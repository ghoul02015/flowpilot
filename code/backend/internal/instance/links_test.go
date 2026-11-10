package instance

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ghoul02015/flowpilot/internal/docker"
)

// setupLinksManager 构造带模型关联配置的管理器、假 Docker 与临时目录。
func setupLinksManager(t *testing.T) (*Manager, *fakeDocker, string, string) {
	t.Helper()
	base := t.TempDir()
	modelRoot := filepath.Join(base, "models")
	linksDir := filepath.Join(base, "instance-links")
	for _, relative := range []string{"loras/a.safetensors", "loras/sub/b.safetensors", "vae/c.pt"} {
		full := filepath.Join(modelRoot, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	fake := &fakeDocker{
		images: map[string]bool{"comfyui:test": true},
		containers: []docker.Container{{
			ID:      "abcdefabcdef",
			Names:   []string{"/flowpilot-comfyui-demo"},
			Image:   "comfyui:test",
			State:   "running",
			Created: time.Now().Unix(),
			Labels: map[string]string{
				LabelInstance:     "true",
				labelName:         "demo",
				labelGPU:          "0",
				labelPort:         "18188",
				labelModelsTarget: "/data/models",
				labelModelLinks:   "true",
			},
		}},
	}
	manager := newManager(fake, 1)
	manager.SetModelLinks(filepath.Join(base, "hostlinks"), linksDir, modelRoot)
	return manager, fake, base, linksDir
}

// 全量设置语义：新增、移除、幂等，且符号链接目标为实例容器内路径。
func TestSetModelLinksDiffAndSymlinkTarget(t *testing.T) {
	manager, _, _, linksDir := setupLinksManager(t)
	ctx := context.Background()

	added, removed, err := manager.SetModelLinksFor(ctx, "demo", []ModelLink{
		{Category: "loras", Name: "a.safetensors"},
		{Category: "loras", Name: "sub/b.safetensors"},
	})
	if err != nil || added != 2 || removed != 0 {
		t.Fatalf("首次设置应新增 2 个链接，得到 added=%d removed=%d err=%v", added, removed, err)
	}
	target, err := os.Readlink(filepath.Join(linksDir, "demo", "loras", "a.safetensors"))
	if err != nil {
		t.Fatal(err)
	}
	if target != "/data/models/loras/a.safetensors" {
		t.Fatalf("符号链接目标应为容器内路径，得到 %q", target)
	}

	// 幂等：重复相同集合不产生变更。
	added, removed, err = manager.SetModelLinksFor(ctx, "demo", []ModelLink{
		{Category: "loras", Name: "a.safetensors"},
		{Category: "loras", Name: "sub/b.safetensors"},
	})
	if err != nil || added != 0 || removed != 0 {
		t.Fatalf("重复设置应幂等，得到 added=%d removed=%d err=%v", added, removed, err)
	}

	// 收缩集合：移除一个并清理空目录。
	added, removed, err = manager.SetModelLinksFor(ctx, "demo", []ModelLink{{Category: "loras", Name: "a.safetensors"}})
	if err != nil || added != 0 || removed != 1 {
		t.Fatalf("收缩应移除 1 个链接，得到 added=%d removed=%d err=%v", added, removed, err)
	}
	if _, statErr := os.Lstat(filepath.Join(linksDir, "demo", "loras", "sub")); !os.IsNotExist(statErr) {
		t.Fatalf("空子目录应被清理，得到 %v", statErr)
	}

	supported, links, err := manager.ListModelLinks(ctx, "demo")
	if err != nil || !supported || len(links) != 1 || links[0].Name != "a.safetensors" {
		t.Fatalf("列表应仅剩 1 个关联，得到 supported=%v links=%+v err=%v", supported, links, err)
	}
}

// 非法输入：路径穿越、绝对路径、不存在的模型均应拒绝。
func TestSetModelLinksRejectsInvalidInput(t *testing.T) {
	manager, _, _, _ := setupLinksManager(t)
	ctx := context.Background()

	cases := []ModelLink{
		{Category: "loras", Name: "../secret.txt"},
		{Category: "loras", Name: "/etc/passwd"},
		{Category: "../models", Name: "a.safetensors"},
		{Category: "loras", Name: "missing.safetensors"},
	}
	for _, link := range cases {
		var validation *ValidationError
		if _, _, err := manager.SetModelLinksFor(ctx, "demo", []ModelLink{link}); !errors.As(err, &validation) {
			t.Fatalf("应拒绝 %+v，得到 %v", link, err)
		}
	}
}

// 无模型关联挂载的旧实例应返回 NotSupported。
func TestSetModelLinksUnsupportedInstance(t *testing.T) {
	manager, fake, _, _ := setupLinksManager(t)
	fake.containers[0].Labels[labelModelLinks] = ""
	ctx := context.Background()

	var notSupported *NotSupportedError
	if _, _, err := manager.SetModelLinksFor(ctx, "demo", nil); !errors.As(err, &notSupported) {
		t.Fatalf("旧实例应返回 NotSupported，得到 %v", err)
	}
	supported, links, err := manager.ListModelLinks(ctx, "demo")
	if err != nil || supported || len(links) != 0 {
		t.Fatalf("旧实例列表应 supported=false，得到 %v %+v %v", supported, links, err)
	}
}

// 删除实例应级联清理其关联目录。
func TestDeleteClearsInstanceLinks(t *testing.T) {
	manager, _, _, linksDir := setupLinksManager(t)
	ctx := context.Background()
	if _, _, err := manager.SetModelLinksFor(ctx, "demo", []ModelLink{{Category: "loras", Name: "a.safetensors"}}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Delete(ctx, "demo", true, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(linksDir, "demo")); !os.IsNotExist(err) {
		t.Fatalf("实例删除后关联目录应被清理，得到 %v", err)
	}
}
