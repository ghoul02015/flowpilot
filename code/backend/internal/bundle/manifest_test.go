package bundle

import (
	"encoding/json"
	"strings"
	"testing"
)

func mustDefault(t *testing.T) *Manifest {
	t.Helper()
	manifest, err := Default()
	if err != nil {
		t.Fatalf("内置清单应通过校验：%v", err)
	}
	return manifest
}

// reparse 序列化后重新解析，用于构造非法清单。
func reparse(t *testing.T, manifest *Manifest) error {
	t.Helper()
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("序列化失败：%v", err)
	}
	_, err = Parse(raw)
	return err
}

func TestDefaultManifestLoads(t *testing.T) {
	manifest := mustDefault(t)
	if len(manifest.Bundles) != 5 {
		t.Fatalf("应包含 5 个套装，实际 %d", len(manifest.Bundles))
	}
	if len(manifest.Checksum) != 64 {
		t.Fatalf("checksum 应为 64 位十六进制，实际 %q", manifest.Checksum)
	}
	wanted := []string{"h3-image-base", "h3-image-pro", "h3-av-base", "h3-av-reference", "h3-studio"}
	for _, id := range wanted {
		if _, ok := manifest.FindBundle(id); !ok {
			t.Fatalf("缺少套装 %s", id)
		}
	}
	// S2 必须完整包含 S1 的三个文件。
	base, _ := manifest.FindBundle("h3-image-base")
	pro, _ := manifest.FindBundle("h3-image-pro")
	for _, item := range base.Items {
		if _, ok := pro.Item(item.Key); !ok {
			t.Fatalf("S2 未继承 S1 的项 %s", item.Key)
		}
	}
	// S3 的 Turbo 必须同属一个互斥组且默认不选。
	av, _ := manifest.FindBundle("h3-av-base")
	group := map[string]int{}
	for _, variant := range av.Variants {
		if variant.OptionalGroup == "fl2v-turbo" {
			group[variant.Key]++
			if variant.Default {
				t.Fatalf("Turbo 变体 %s 不应默认选中", variant.Key)
			}
		}
	}
	if len(group) != 2 {
		t.Fatalf("S3 应有 4-step 与 8-step 两个 Turbo 变体，实际 %d", len(group))
	}
	// S5 必须同时保留图像 INT8 与视频 NVFP4 编码器。
	studio, _ := manifest.FindBundle("h3-studio")
	if item, ok := studio.Item("img-text-encoder-int8"); !ok || item.Sources[0].SHA256 == "" {
		t.Fatal("S5 缺少图像 INT8 编码器")
	}
	if item, ok := studio.Item("vid-text-encoder-nvfp4"); !ok || item.Sources[0].SHA256 == "" {
		t.Fatal("S5 缺少视频 NVFP4 编码器")
	}
	if studio.Items[0].Sources[0].SHA256 == "" {
		t.Fatal("资产项缺少 SHA256")
	}
}

func TestManifestRejectsFloatingRevision(t *testing.T) {
	manifest := mustDefault(t)
	manifest.Bundles[0].Items[0].Sources[0].Revision = "main"
	err := reparse(t, manifest)
	if err == nil || !strings.Contains(err.Error(), "浮动引用") {
		t.Fatalf("应拒绝浮动 revision，实际：%v", err)
	}
}

func TestManifestRejectsPathTraversal(t *testing.T) {
	cases := []string{"../etc/passwd", "/abs/path", "diffusion_models/../../x", "diffusion_models\\..\\x", "diffusion_models//x"}
	for _, target := range cases {
		manifest := mustDefault(t)
		manifest.Bundles[0].Items[0].TargetRelativePath = target
		if err := reparse(t, manifest); err == nil {
			t.Fatalf("应拒绝非法路径 %q", target)
		}
	}
}

func TestManifestRejectsHashConflictOnSamePath(t *testing.T) {
	manifest := mustDefault(t)
	// 同一目标路径写入不同哈希必须报冲突。
	manifest.Bundles[1].Items[0].Sources[0].SHA256 = strings.Repeat("a", 64)
	if err := reparse(t, manifest); err == nil || !strings.Contains(err.Error(), "不同内容哈希") {
		t.Fatalf("应拒绝同路径不同哈希，实际：%v", err)
	}
}

func TestManifestRejectsUnknownField(t *testing.T) {
	manifest := mustDefault(t)
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	broken := strings.Replace(string(raw), `"bundleVersion"`, `"bundleVersionX"`, 1)
	if _, err := Parse([]byte(broken)); err == nil {
		t.Fatal("未知字段应拒绝解析")
	}
}

func TestManifestRejectsDuplicateGroupDefault(t *testing.T) {
	manifest := mustDefault(t)
	av, _ := manifest.FindBundle("h3-av-base")
	for index := range av.Variants {
		if av.Variants[index].OptionalGroup == "fl2v-turbo" {
			av.Variants[index].Default = true
		}
	}
	if err := reparse(t, manifest); err == nil || !strings.Contains(err.Error(), "互斥组") {
		t.Fatalf("应拒绝同组多个默认项，实际：%v", err)
	}
}

func TestManifestRejectsDependencyCycle(t *testing.T) {
	manifest := mustDefault(t)
	base := &manifest.Bundles[0]
	base.Items[0].DependsOn = []string{base.Items[1].Key}
	base.Items[1].DependsOn = []string{base.Items[0].Key}
	if err := reparse(t, manifest); err == nil || !strings.Contains(err.Error(), "循环依赖") {
		t.Fatalf("应拒绝循环依赖，实际：%v", err)
	}
}

func TestManifestRejectsUncontrolledCategory(t *testing.T) {
	manifest := mustDefault(t)
	manifest.Bundles[0].Items[0].Category = "custom_models"
	manifest.Bundles[0].Items[0].TargetRelativePath = "custom_models/x.safetensors"
	if err := reparse(t, manifest); err == nil || !strings.Contains(err.Error(), "受控模型目录") {
		t.Fatalf("应拒绝非受控目录，实际：%v", err)
	}
}

func TestBundleByteTotals(t *testing.T) {
	manifest := mustDefault(t)
	base, _ := manifest.FindBundle("h3-image-base")
	pro, _ := manifest.FindBundle("h3-image-pro")
	av, _ := manifest.FindBundle("h3-av-base")
	if base.RequiredBytes() <= 0 || pro.TotalBytes() <= base.TotalBytes() {
		t.Fatalf("字节统计异常：S1=%d S2=%d", base.TotalBytes(), pro.TotalBytes())
	}
	if av.OptionalItemCount() != 2 {
		t.Fatalf("S3 应有 2 个可选项，实际 %d", av.OptionalItemCount())
	}
	if base.RequiredItemCount() != 3 {
		t.Fatalf("S1 应有 3 个必需项，实际 %d", base.RequiredItemCount())
	}
}
