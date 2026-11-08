package image

import (
	"strings"
	"testing"
)

func TestRecipesCoverGPUFamilies(t *testing.T) {
	recipes := Recipes()
	if len(recipes) < 5 {
		t.Fatalf("配方数量不足：%d", len(recipes))
	}
	seen := map[string]bool{}
	for _, recipe := range recipes {
		seen[recipe.GPU] = true
		if recipe.ID == "" || recipe.Image == "" || recipe.Label == "" {
			t.Fatalf("配方字段不完整：%+v", recipe)
		}
		if _, ok := RecipeByID(recipe.ID); !ok {
			t.Fatalf("RecipeByID 找不到 %s", recipe.ID)
		}
	}
	for _, gpu := range []string{"nvidia", "amd", "cpu"} {
		if !seen[gpu] {
			t.Fatalf("缺少 %s 配方", gpu)
		}
	}
}

func TestInstallScriptDefaults(t *testing.T) {
	script, err := InstallScript(InstallParams{})
	if err != nil {
		t.Fatalf("渲染失败：%v", err)
	}
	for _, fragment := range []string{"VARIANT:-auto", "master", "pypi.tuna.tsinghua.edu.cn", "flowpilot/comfyui:local", "nvidia-cu130", "amd-rocm", "MODEL", "#!/usr/bin/env bash"} {
		if !strings.Contains(script, fragment) && fragment != "MODEL" {
			t.Fatalf("脚本缺少片段 %q", fragment)
		}
	}
	for _, placeholder := range []string{"{{.Variant}}", "{{.ImageName}}", "{{.ComfyUIRef}}", "{{.PipIndexURL}}", "{{.WithManager}}", "{{.GeneratedAt}}"} {
		if strings.Contains(script, placeholder) {
			t.Fatalf("脚本残留未渲染的模板占位符 %q", placeholder)
		}
	}
	// 平台职责边界：安装脚本不得下载模型。
	if strings.Contains(script, "modelscope download") || strings.Contains(script, "hf download") {
		t.Fatalf("安装脚本不应包含模型下载逻辑（由模型中心负责）")
	}
}

func TestInstallScriptParamsAndSanitization(t *testing.T) {
	script, err := InstallScript(InstallParams{
		Variant:     "nvidia-cu126",
		ImageName:   "my/comfy:v1",
		ComfyUIRef:  "v0.3.60",
		PipIndexURL: "aliyun",
		WithManager: "0",
	})
	if err != nil {
		t.Fatalf("渲染失败：%v", err)
	}
	for _, fragment := range []string{"VARIANT:-nvidia-cu126", "my/comfy:v1", "v0.3.60", "mirrors.aliyun.com", "WITH_MANAGER:-0"} {
		if !strings.Contains(script, fragment) {
			t.Fatalf("脚本缺少片段 %q", fragment)
		}
	}

	// 非法输入必须被回退，防止脚本注入。
	sanitized, err := InstallScript(InstallParams{
		Variant:    "../../etc/passwd",
		ImageName:  "bad name;rm -rf /",
		ComfyUIRef: "$(reboot)",
	})
	if err != nil {
		t.Fatalf("渲染失败：%v", err)
	}
	for _, payload := range []string{"bad name", "$(reboot)", "../etc/passwd"} {
		if strings.Contains(sanitized, payload) {
			t.Fatalf("非法参数未被清理：%q", payload)
		}
	}
}
