package bundle

import (
	"strings"
	"testing"
)

func TestExpandImageBaseDefault(t *testing.T) {
	manifest := mustDefault(t)
	plan, err := Expand(manifest, "h3-image-base", "default", nil)
	if err != nil {
		t.Fatalf("展开失败：%v", err)
	}
	if len(plan.Items) != 3 {
		t.Fatalf("S1 应展开 3 项，实际 %d", len(plan.Items))
	}
	if plan.OptionalBytes != 0 || plan.RequiredBytes <= 0 {
		t.Fatalf("S1 字节统计异常：required=%d optional=%d", plan.RequiredBytes, plan.OptionalBytes)
	}
	if plan.ManifestChecksum == "" || plan.AcknowledgementVersion == "" {
		t.Fatal("快照必须绑定清单校验值和许可版本")
	}
	for _, item := range plan.Items {
		if len(item.SHA256) != 64 || item.Revision == "" {
			t.Fatalf("计划项缺少不可变坐标：%+v", item.Key)
		}
	}
}

func TestExpandAVBaseTurboExclusive(t *testing.T) {
	manifest := mustDefault(t)
	base, err := Expand(manifest, "h3-av-base", "default", nil)
	if err != nil {
		t.Fatalf("默认变体展开失败：%v", err)
	}
	if len(base.Items) != 4 {
		t.Fatalf("S3 默认应展开 4 项（不含 Turbo），实际 %d", len(base.Items))
	}
	for _, item := range base.Items {
		if strings.HasPrefix(item.Key, "lora-") {
			t.Fatalf("默认变体不应包含 Turbo LoRA：%s", item.Key)
		}
	}

	turbo, err := Expand(manifest, "h3-av-base", "turbo-4step", nil)
	if err != nil {
		t.Fatalf("Turbo 展开失败：%v", err)
	}
	if len(turbo.Items) != 5 {
		t.Fatalf("Turbo 4-step 应展开 5 项，实际 %d", len(turbo.Items))
	}
	found := false
	for _, item := range turbo.Items {
		if item.Key == "lora-fl2v-turbo-4step" {
			found = true
		}
		if item.Key == "lora-fl2v-turbo-8step" {
			t.Fatal("4-step 变体不应包含 8-step")
		}
	}
	if !found {
		t.Fatal("4-step 变体缺少对应 LoRA")
	}
	excluded := false
	for _, item := range turbo.Excluded {
		if item.Key == "lora-fl2v-turbo-8step" {
			excluded = true
		}
	}
	if !excluded {
		t.Fatal("应记录被互斥排除的 8-step 及原因")
	}

	// 同时选择两个成员必须被拒绝。
	if _, err := Expand(manifest, "h3-av-base", "default", []string{"lora-fl2v-turbo-4step", "lora-fl2v-turbo-8step"}); err == nil {
		t.Fatal("同组两个选项应报冲突")
	}
}

func TestExpandRejectsRequiredAsOption(t *testing.T) {
	manifest := mustDefault(t)
	if _, err := Expand(manifest, "h3-image-base", "default", []string{"img-diffusion-int8"}); err == nil {
		t.Fatal("必需项不能作为可选参数")
	}
}

func TestExpandDependencyClosure(t *testing.T) {
	manifest := mustDefault(t)
	// 仅选择 Ref2V Turbo，应自动带入其依赖的全参考扩散模型。
	plan, err := Expand(manifest, "h3-studio", "default", []string{"lora-ref2v-turbo-4step"})
	if err != nil {
		t.Fatalf("依赖展开失败：%v", err)
	}
	hasRef, hasLora := false, false
	for _, item := range plan.Items {
		if item.Key == "vid-diffusion-ref2va-int8" {
			hasRef = true
		}
		if item.Key == "lora-ref2v-turbo-4step" {
			hasLora = true
		}
	}
	if !hasLora || !hasRef {
		t.Fatalf("应同时包含 Turbo 与依赖模型：lora=%v ref=%v", hasLora, hasRef)
	}
}

func TestExpandStudioKeepsBothEncoders(t *testing.T) {
	manifest := mustDefault(t)
	plan, err := Expand(manifest, "h3-studio", "with-reference", nil)
	if err != nil {
		t.Fatalf("S5 展开失败：%v", err)
	}
	keys := map[string]bool{}
	for _, item := range plan.Items {
		keys[item.Key] = true
	}
	if !keys["img-text-encoder-int8"] || !keys["vid-text-encoder-nvfp4"] {
		t.Fatal("S5 必须同时保留图像 INT8 与视频 NVFP4 编码器")
	}
	if !keys["vid-diffusion-ref2va-int8"] {
		t.Fatal("含全参考变体应包含 Ref2VA 模型")
	}
	if keys["lora-fl2v-turbo-4step"] || keys["lora-fl2v-turbo-8step"] {
		t.Fatal("未选择 Turbo 时不应包含 LoRA")
	}
}

func TestExpandDeterministicOrder(t *testing.T) {
	manifest := mustDefault(t)
	first, err := Expand(manifest, "h3-studio", "default", nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Expand(manifest, "h3-studio", "default", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != len(second.Items) {
		t.Fatal("两次展开项数不一致")
	}
	for index := range first.Items {
		if first.Items[index].Key != second.Items[index].Key || first.Items[index].Ordinal != second.Items[index].Ordinal {
			t.Fatalf("展开顺序不稳定：%s vs %s", first.Items[index].Key, second.Items[index].Key)
		}
	}
}
