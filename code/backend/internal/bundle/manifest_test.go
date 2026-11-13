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
func reparseManifest(t *testing.T, manifest *Manifest) (*Manifest, error) {
	t.Helper()
	// 内存里的 License 是 attachLicenses 挂上去的，源 JSON 并没有该字段；
	// 序列化前清掉，才能真实模拟“清单未配置某模型许可”。
	clone := *manifest
	clone.Bundles = append([]Bundle(nil), manifest.Bundles...)
	for index := range clone.Bundles {
		clone.Bundles[index].License = License{}
	}
	raw, err := json.Marshal(&clone)
	if err != nil {
		t.Fatalf("序列化失败：%v", err)
	}
	return Parse(raw)
}

func reparse(t *testing.T, manifest *Manifest) error {
	t.Helper()
	_, err := reparseManifest(t, manifest)
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

// TestLicensesAreModelScoped 固定“一个模型一份许可”的边界：
// H3-Image 与 H3 的版本、摘要、NOTICE 必须各自独立，不能互相复制。
func TestLicensesAreModelScoped(t *testing.T) {
	manifest := mustDefault(t)
	image, ok := manifest.Licenses["MiniMax-H3-Image"]
	if !ok {
		t.Fatal("缺少 MiniMax-H3-Image 许可配置")
	}
	video, ok := manifest.Licenses["MiniMax-H3"]
	if !ok {
		t.Fatal("缺少 MiniMax-H3 许可配置")
	}
	if image.Version == video.Version {
		t.Fatalf("两个模型的许可版本不应相同：%s", image.Version)
	}
	imageSummary := strings.Join(image.Summary, "\n")
	videoSummary := strings.Join(video.Summary, "\n")
	if !strings.Contains(imageSummary, "100 万美元") || !strings.Contains(imageSummary, "Hosted Service") {
		t.Fatal("H3-Image 摘要应包含 100 万美元与 Hosted Service 条款")
	}
	if strings.Contains(videoSummary, "100 万美元") || strings.Contains(videoSummary, "Hosted Service") {
		t.Fatal("H3 摘要不得套用 H3-Image 的 100 万美元与 Hosted Service 条款")
	}
	if !strings.Contains(videoSummary, "2000 万美元") {
		t.Fatal("H3 摘要应使用自身许可的 2000 万美元门槛")
	}
	for _, b := range manifest.Bundles {
		if b.License.ModelName != b.ModelName {
			t.Fatalf("套装 %s 挂载了错误模型许可：%s", b.ID, b.License.ModelName)
		}
		if b.License.NoticeText == "" || b.License.SnapshotSHA256 == "" {
			t.Fatalf("套装 %s 许可快照不完整", b.ID)
		}
	}
	// 同一模型的不同套装必须拿到完全一致的许可配置。
	s1, _ := manifest.FindBundle("h3-image-base")
	s2, _ := manifest.FindBundle("h3-image-pro")
	if s1.License.SnapshotSHA256 != s2.License.SnapshotSHA256 || s1.License.Version != s2.License.Version {
		t.Fatal("同模型套装应共享同一份许可快照")
	}
}

// TestLicenseSnapshotTamperRejected 校验快照哈希，防止只改版本号不改文件。
func TestLicenseSnapshotTamperRejected(t *testing.T) {
	manifest := mustDefault(t)
	license := manifest.Licenses["MiniMax-H3-Image"]
	license.SnapshotSHA256 = strings.Repeat("0", 64)
	if err := verifyLicenseSnapshot(license); err == nil {
		t.Fatal("许可文件被篡改时应校验失败")
	}
}

// TestUnlicensedModelNeedsNoConfirmation 未配置许可的模型允许加载，
// 预检直接通过；配置了许可的模型仍然要求确认。
func TestUnlicensedModelNeedsNoConfirmation(t *testing.T) {
	manifest := mustDefault(t)
	delete(manifest.Licenses, "MiniMax-H3")
	reparsed, err := reparseManifest(t, manifest)
	if err != nil {
		t.Fatalf("未配置许可应允许加载：%v", err)
	}
	s3, ok := reparsed.FindBundle("h3-av-base")
	if !ok {
		t.Fatal("S3 缺失")
	}
	if s3.License.Configured() {
		t.Fatal("未配置许可的模型不应被标记为需要确认")
	}
	check := LicenseCheck(s3, false)
	if check.Status != CheckPass {
		t.Fatalf("未配置许可时预检应通过，实际 %s：%s", check.Status, check.Detail)
	}
	s1, _ := reparsed.FindBundle("h3-image-base")
	if !s1.License.Configured() {
		t.Fatal("H3-Image 配置了许可，应要求确认")
	}
	if LicenseCheck(s1, false).Status != CheckBlock {
		t.Fatal("已配置许可且未确认时应阻断")
	}
	if LicenseCheck(s1, true).Status != CheckPass {
		t.Fatal("已确认后许可项应通过")
	}
}

// TestLicenseVersionSharedAcrossModelsRejected 防止把同一许可版本复制给两个模型。
func TestLicenseVersionSharedAcrossModelsRejected(t *testing.T) {
	manifest := mustDefault(t)
	shared := manifest.Licenses["MiniMax-H3"]
	shared.Version = manifest.Licenses["MiniMax-H3-Image"].Version
	manifest.Licenses["MiniMax-H3"] = shared
	if err := reparse(t, manifest); err == nil {
		t.Fatal("两个模型共用同一许可版本时应拒绝解析")
	}
}
