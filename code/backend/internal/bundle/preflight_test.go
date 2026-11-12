package bundle

import (
	"testing"
	"time"
)

func sampleBundle(t *testing.T, id string) *Bundle {
	t.Helper()
	manifest := mustDefault(t)
	b, ok := manifest.FindBundle(id)
	if !ok {
		t.Fatalf("缺少套装 %s", id)
	}
	return b
}

func TestClassifyHardware(t *testing.T) {
	cases := []struct {
		name string
		hw   HardwareSummary
		want string
	}{
		{"Spark", HardwareSummary{GPUs: []GPUInfo{{Index: 0, Name: "NVIDIA GB10"}}, UnifiedMemory: true}, "spark-128g"},
		{"PRO6000", HardwareSummary{GPUs: []GPUInfo{{Index: 0, Name: "NVIDIA RTX PRO 6000 Blackwell"}}}, "pro6000-96g"},
		{"5090", HardwareSummary{GPUs: []GPUInfo{{Index: 0, Name: "NVIDIA GeForce RTX 5090"}}}, "rtx5090-32g"},
		{"4090", HardwareSummary{GPUs: []GPUInfo{{Index: 0, Name: "NVIDIA GeForce RTX 4090"}}}, "rtx4090-24g"},
		{"其他 NVIDIA", HardwareSummary{GPUs: []GPUInfo{{Index: 0, Name: "NVIDIA A100"}}}, "other-nvidia"},
		{"无 GPU", HardwareSummary{}, "none"},
		{"采集失败", HardwareSummary{Error: "nvidia-smi 不可用"}, "none"},
	}
	for _, c := range cases {
		if got := ClassifyHardware(c.hw); got != c.want {
			t.Fatalf("%s：期望 %s，实际 %s", c.name, c.want, got)
		}
	}
}

func TestStatusProgression(t *testing.T) {
	b := sampleBundle(t, "h3-image-base")

	// 全部缺失。
	empty := ComputeState(b, NewRepoSnapshot("models", nil), nil)
	if empty.State != StateNotInstalled || empty.RequiredPresent != 0 {
		t.Fatalf("空仓库应为未安装：%+v", empty.State)
	}

	// 部分存在。
	partialSizes := map[string]int64{}
	for index, item := range b.Items {
		if index == 0 {
			partialSizes[item.TargetRelativePath] = item.Sources[0].SizeBytes
		}
	}
	partial := ComputeState(b, NewRepoSnapshot("models", partialSizes), nil)
	if partial.State != StatePartial || partial.RequiredPresent != 1 {
		t.Fatalf("部分存在应为 partial：%+v", partial)
	}

	// 全部存在。
	full := map[string]int64{}
	for _, item := range b.Items {
		full[item.TargetRelativePath] = item.Sources[0].SizeBytes
	}
	ready := ComputeState(b, NewRepoSnapshot("models", full), nil)
	if ready.State != StateFilesReady || ready.MissingBytes != 0 {
		t.Fatalf("全部存在应为 files_ready：%+v", ready)
	}
	if ready.RequiredBytes <= 0 || ready.PresentBytes != ready.RequiredBytes {
		t.Fatalf("字节统计异常：%+v", ready)
	}
}

func TestStatusSizeMismatch(t *testing.T) {
	b := sampleBundle(t, "h3-image-base")
	sizes := map[string]int64{}
	for _, item := range b.Items {
		sizes[item.TargetRelativePath] = item.Sources[0].SizeBytes
	}
	// 篡改其中一个文件大小。
	sizes[b.Items[0].TargetRelativePath] = 1
	state := ComputeState(b, NewRepoSnapshot("models", sizes), nil)
	if state.Items[0].Status != ItemHashMismatch {
		t.Fatalf("大小不符应为 hash_mismatch，实际 %s", state.Items[0].Status)
	}
	if state.State == StateFilesReady {
		t.Fatal("存在大小不符时不能判定齐备")
	}

	// 已校验哈希不一致（大小正确但内容不符）。
	fullSizes := map[string]int64{}
	for _, item := range b.Items {
		fullSizes[item.TargetRelativePath] = item.Sources[0].SizeBytes
	}
	verified := map[string]string{b.Items[0].TargetRelativePath: "deadbeef"}
	state = ComputeState(b, NewRepoSnapshot("models", fullSizes), verified)
	if state.Items[0].Status != ItemConflict {
		t.Fatalf("哈希不一致应为 conflict，实际 %s", state.Items[0].Status)
	}
}

func TestHardwareCheckLevels(t *testing.T) {
	spark := sampleBundle(t, "h3-av-base")
	check := HardwareCheck(spark, HardwareSummary{GPUs: []GPUInfo{{Index: 0, Name: "NVIDIA GB10"}}, UnifiedMemory: true})
	if check.Status != CheckPass {
		t.Fatalf("Spark 对 S3 应为 pass，实际 %s（%s）", check.Status, check.Detail)
	}

	ref := sampleBundle(t, "h3-av-reference")
	check = HardwareCheck(ref, HardwareSummary{GPUs: []GPUInfo{{Index: 0, Name: "NVIDIA GB10"}}, UnifiedMemory: true})
	if check.Status != CheckWarn {
		t.Fatalf("条件认证应为 warn，实际 %s", check.Status)
	}

	noGPU := HardwareCheck(spark, HardwareSummary{})
	if noGPU.Status != CheckUnknown {
		t.Fatalf("无 GPU 应为 unknown，实际 %s", noGPU.Status)
	}

	failed := HardwareCheck(spark, HardwareSummary{Error: "nvidia-smi timeout"})
	if failed.Status != CheckUnknown {
		t.Fatalf("采集失败应为 unknown，实际 %s", failed.Status)
	}
}

func TestMirrorCheck(t *testing.T) {
	b := sampleBundle(t, "h3-image-base")

	if got := MirrorCheck(b, "", ""); got.Status != CheckUnknown {
		t.Fatalf("缺版本元数据应为 unknown，实际 %s", got.Status)
	}
	if got := MirrorCheck(b, "0.44.0", "0.11.200"); got.Status != CheckBlock {
		t.Fatalf("Core 低于要求应阻断，实际 %s", got.Status)
	}
	if got := MirrorCheck(b, "0.45.0", ""); got.Status != CheckUnknown {
		t.Fatalf("模板未知应为 unknown 而非 pass，实际 %s", got.Status)
	}
	if got := MirrorCheck(b, "v0.45.0", "0.11.200"); got.Status != CheckPass {
		t.Fatalf("满足要求应 pass，实际 %s（%s）", got.Status, got.Detail)
	}
}

func TestDiskCheckReuseAndBlock(t *testing.T) {
	b := sampleBundle(t, "h3-image-base")

	// 已存在一个文件，所需空间应相应减少。
	sizes := map[string]int64{b.Items[0].TargetRelativePath: b.Items[0].Sources[0].SizeBytes}
	check, needed := DiskCheck(b, NewRepoSnapshot("models", sizes), 1<<62)
	if check.Status != CheckPass {
		t.Fatalf("空间充足应 pass：%s", check.Detail)
	}
	if needed <= 0 || needed >= b.RequiredBytes() {
		t.Fatalf("已存在文件应减少需求：needed=%d total=%d", needed, b.RequiredBytes())
	}

	blocked, _ := DiskCheck(b, NewRepoSnapshot("models", nil), 1)
	if blocked.Status != CheckBlock {
		t.Fatalf("空间不足应阻断，实际 %s", blocked.Status)
	}
}

func TestPreflightCombination(t *testing.T) {
	b := sampleBundle(t, "h3-image-base")
	manifest := mustDefault(t)
	plan, err := Expand(manifest, "h3-image-base", "default", nil)
	if err != nil {
		t.Fatal(err)
	}
	hw := HardwareSummary{GPUs: []GPUInfo{{Index: 0, Name: "NVIDIA GB10"}}, UnifiedMemory: true, CollectedAt: time.Now()}

	// 许可未确认必须阻断。
	result := Preflight(b, plan, hw, "0.45.0", "0.11.200", NewRepoSnapshot("models", nil), 1<<62, false)
	if result.CanInstall {
		t.Fatal("许可未确认时不应允许安装")
	}
	licenseState := ""
	for _, check := range result.Checks {
		if check.Code == "license" {
			licenseState = check.Status
		}
	}
	if licenseState != CheckBlock {
		t.Fatalf("许可应为 block，实际 %s", licenseState)
	}

	// 全部满足时可安装。
	result = Preflight(b, plan, hw, "0.45.0", "0.11.200", NewRepoSnapshot("models", nil), 1<<40, true)
	if !result.CanInstall {
		t.Fatalf("全部满足应可安装：%+v", result.Checks)
	}
	if len(result.Checks) != 4 {
		t.Fatalf("应返回 4 项预检，实际 %d", len(result.Checks))
	}
}
