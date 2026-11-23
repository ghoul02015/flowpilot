package image

import (
	"bytes"
	_ "embed"
	"fmt"
	"regexp"
	"strings"
	"text/template"
	"time"
)

// Recipe 是一套 ComfyUI 镜像构建配方，面向纯净主机的一键安装。
// 配方按显卡类型区分 PyTorch 轮子源，镜像在本机构建而非从仓库拉取。
type Recipe struct {
	ID          string `json:"id"`          // 配方标识，如 nvidia-cu126
	Label       string `json:"label"`       // 界面展示名
	Image       string `json:"image"`       // 建议的本地镜像名
	GPU         string `json:"gpu"`         // nvidia | amd | cpu
	CUDA        string `json:"cuda"`        // torch 轮子标签（cpu 配方为 cpu）
	Arch        string `json:"arch"`        // x86_64 | aarch64 | both
	MinSM       string `json:"minSM"`       // 适用最低计算能力（仅 NVIDIA）
	Description string `json:"description"` // 一句话说明适用硬件
}

// Recipes 返回全部预置构建配方，顺序即界面展示顺序。
func Recipes() []Recipe {
	return []Recipe{
		{ID: "nvidia-cu130", Label: "NVIDIA · CUDA 13.0（GB10 / DGX Spark）", Image: "flowpilot/comfyui:cu130", GPU: "nvidia", CUDA: "cu130", Arch: "both", MinSM: "12.0", Description: "Blackwell GB10（sm_121），DGX Spark 实测配方"},
		{ID: "nvidia-cu128", Label: "NVIDIA · CUDA 12.8（RTX 50 / B 系列）", Image: "flowpilot/comfyui:cu128", GPU: "nvidia", CUDA: "cu128", Arch: "x86_64", MinSM: "10.0", Description: "Blackwell 消费级与数据中心卡（sm_100 / sm_120）"},
		{ID: "nvidia-cu126", Label: "NVIDIA · CUDA 12.6（RTX 20–40 / A / H 系列）", Image: "flowpilot/comfyui:cu126", GPU: "nvidia", CUDA: "cu126", Arch: "both", MinSM: "8.0", Description: "Ampere / Ada / Hopper 主流显卡（sm_80–sm_90）"},
		{ID: "nvidia-cu121", Label: "NVIDIA · CUDA 12.1（旧卡兼容）", Image: "flowpilot/comfyui:cu121", GPU: "nvidia", CUDA: "cu121", Arch: "x86_64", MinSM: "5.0", Description: "Turing 及更早显卡（<= sm_75）兼容配方"},
		{ID: "amd-rocm", Label: "AMD · ROCm 6.2", Image: "flowpilot/comfyui:rocm6.2", GPU: "amd", CUDA: "rocm6.2", Arch: "x86_64", Description: "Radeon / Instinct 显卡，基于官方 ROCm PyTorch 镜像"},
		{ID: "cpu", Label: "纯 CPU（无显卡）", Image: "flowpilot/comfyui:cpu", GPU: "cpu", CUDA: "cpu", Arch: "both", Description: "无 GPU 主机的验证与小模型调试"},
	}
}

// RecipeByID 按标识查找配方。
func RecipeByID(id string) (Recipe, bool) {
	for _, recipe := range Recipes() {
		if recipe.ID == id {
			return recipe, true
		}
	}
	return Recipe{}, false
}

// ComfyRef 是可用于构建的 ComfyUI 源码版本或分支。
type ComfyRef struct {
	Name string `json:"name"`
	Type string `json:"type"` // branch | tag
}

//go:embed assets/install-comfyui-image.sh.tmpl
var installTemplate string

// InstallParams 是一键安装脚本的可向导化参数。
type InstallParams struct {
	Variant     string        // auto 或配方 ID
	ImageName   string        // 构建出的本地镜像名
	ComfyUIRef  string        // ComfyUI 源码分支/标签
	Source      *ComfyVersion // 服务端解析后的不可变源码坐标
	PipIndexURL string        // 容器内 pip 源
	WithManager string        // "1" 内置 ComfyUI-Manager，"0" 不含
}

var (
	imageNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/:-]{0,127}$`)
	comfyRefPattern  = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/-]{0,63}$`)
)

// Validate 归一化并校验安装参数；非法输入回退到安全默认值。
func (p *InstallParams) Validate() {
	if p.Variant != "auto" {
		if _, ok := RecipeByID(p.Variant); !ok {
			p.Variant = "auto"
		}
	}
	if p.Variant == "" {
		p.Variant = "auto"
	}
	if !imageNamePattern.MatchString(p.ImageName) || strings.Contains(p.ImageName, "..") {
		if p.Variant != "auto" {
			if recipe, ok := RecipeByID(p.Variant); ok {
				p.ImageName = recipe.Image
			}
		}
		if !imageNamePattern.MatchString(p.ImageName) {
			p.ImageName = "flowpilot/comfyui:local"
		}
	}
	if p.Source != nil && p.Source.Ref != "" {
		p.ComfyUIRef = p.Source.Ref
	}
	if !comfyRefPattern.MatchString(p.ComfyUIRef) || strings.Contains(p.ComfyUIRef, "..") {
		p.ComfyUIRef = "master"
	}
	switch p.PipIndexURL {
	case "tsinghua", "":
		p.PipIndexURL = "https://pypi.tuna.tsinghua.edu.cn/simple"
	case "aliyun":
		p.PipIndexURL = "https://mirrors.aliyun.com/pypi/simple/"
	case "pypi":
		p.PipIndexURL = "https://pypi.org/simple"
	default:
		// 自定义源仅允许 http(s) URL，防止脚本注入。
		if !strings.HasPrefix(p.PipIndexURL, "https://") && !strings.HasPrefix(p.PipIndexURL, "http://") {
			p.PipIndexURL = "https://pypi.tuna.tsinghua.edu.cn/simple"
		}
	}
	if p.WithManager != "0" {
		p.WithManager = "1"
	}
}

// InstallScript 按向导参数渲染一键安装脚本。
func InstallScript(params InstallParams) (string, error) {
	if params.Source == nil || !fullCommitPattern.MatchString(params.Source.Commit) {
		return "", &ValidationError{Message: "安装脚本必须先固定 ComfyUI commit"}
	}
	params.Validate()
	tmpl, err := template.New("install").Parse(installTemplate)
	if err != nil {
		return "", fmt.Errorf("解析安装脚本模板失败：%w", err)
	}
	data := struct {
		InstallParams
		GeneratedAt string
		Repository  string
	}{InstallParams: params, GeneratedAt: time.Now().Format("2006-01-02 15:04"), Repository: ComfyRepository}
	var buffer bytes.Buffer
	if err := tmpl.Execute(&buffer, data); err != nil {
		return "", fmt.Errorf("渲染安装脚本失败：%w", err)
	}
	return buffer.String(), nil
}

// BuildScript 生成仅用于构建 ComfyUI 镜像的一键脚本，不安装 FlowPilot 或下载模型。
func BuildScript(params BuildParams) (string, error) {
	recipe, err := params.validate()
	if err != nil {
		return "", err
	}
	dockerfile := BuildDockerfile(params, recipe)
	platformFlag := ""
	if platform := dockerPlatform(params.Platform); platform != "" {
		platformFlag = " --platform " + shellQuote(platform)
	}
	var buffer bytes.Buffer
	buffer.WriteString("#!/usr/bin/env bash\nset -Eeuo pipefail\n\n")
	buffer.WriteString("# FlowPilot ComfyUI 镜像一键构建脚本\n")
	buffer.WriteString("# 生成时间：" + time.Now().Format("2006-01-02 15:04") + "\n")
	for _, item := range buildSummary(params, recipe) {
		buffer.WriteString("# " + item.Label + "：" + item.Value)
		if item.Description != "" {
			buffer.WriteString("（" + item.Description + "）")
		}
		buffer.WriteString("\n")
	}
	buffer.WriteString("\nIMAGE=" + shellQuote(params.ImageName) + "\n")
	buffer.WriteString("TMPDIR=$(mktemp -d)\ntrap 'rm -rf \"$TMPDIR\"' EXIT\n")
	buffer.WriteString("cat > \"$TMPDIR/Dockerfile\" <<'DOCKERFILE'\n")
	buffer.WriteString(dockerfile)
	if !strings.HasSuffix(dockerfile, "\n") {
		buffer.WriteString("\n")
	}
	buffer.WriteString("DOCKERFILE\n")
	buffer.WriteString("docker build --network host" + platformFlag + " -t \"$IMAGE\" \\\n")
	buffer.WriteString("  --build-arg PIP_INDEX_URL=" + shellQuote(params.PipIndexURL) + " \\\n")
	buffer.WriteString("  --build-arg TORCH_INDEX_URL=" + shellQuote(torchIndexURL(recipe)) + " \\\n")
	buffer.WriteString("  --build-arg COMFYUI_REF=" + shellQuote(params.ComfyUIRef) + " \\\n")
	buffer.WriteString("  --build-arg COMFYUI_COMMIT=" + shellQuote(params.Source.Commit) + " \\\n")
	buffer.WriteString("  --build-arg WITH_MANAGER=" + shellQuote(params.WithManager) + " \\\n")
	buffer.WriteString("  \"$TMPDIR\"\n")
	buffer.WriteString("echo \"构建完成：$IMAGE\"\n")
	return buffer.String(), nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
