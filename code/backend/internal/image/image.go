// Package image 管理 ComfyUI 实例镜像的本地预置、构建配方与 Dockerfile 生成。
// 构建／拉取任务管理见 tasks.go（0.0.8.0 起为有界并发任务列表）。
package image

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const maxBuildLogLines = 500

// BuildParamSummary 是一次构建参数的简述，用于镜像清单回显。
type BuildParamSummary struct {
	Label       string `json:"label"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
}

// BuildParams 是平台内手动构建 ComfyUI 镜像的参数。
// Source 是服务端解析后的不可变源码坐标，构建时只认其中的 commit SHA。
type BuildParams struct {
	Variant     string        `json:"variant"`
	ImageName   string        `json:"image"`
	ComfyUIRef  string        `json:"ref"`
	Source      *ComfyVersion `json:"source,omitempty"`
	PipIndexURL string        `json:"pip"`
	WithManager string        `json:"manager"`
	Platform    string        `json:"platform"`
}

// ConflictError 表示已有同名镜像的任务在进行中。
type ConflictError struct{ Image string }

func (e *ConflictError) Error() string {
	return fmt.Sprintf("镜像 %s 正在处理中，请勿重复提交", e.Image)
}

// ValidationError 表示请求参数不合法。
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func (p *BuildParams) validate() (Recipe, error) {
	p.Variant = strings.TrimSpace(p.Variant)
	recipe, ok := RecipeByID(p.Variant)
	if !ok {
		return Recipe{}, &ValidationError{Message: "请选择有效的镜像构建配方"}
	}
	p.ImageName = strings.TrimSpace(p.ImageName)
	if p.ImageName == "" {
		p.ImageName = recipe.Image
	}
	if err := validateImage(p.ImageName); err != nil {
		return Recipe{}, err
	}
	p.ComfyUIRef = strings.TrimSpace(p.ComfyUIRef)
	if p.Source == nil || !fullCommitPattern.MatchString(p.Source.Commit) {
		return Recipe{}, &ValidationError{Message: "请先选择并解析 ComfyUI 版本，构建必须固定到具体 commit"}
	}
	p.Source.Commit = strings.ToLower(strings.TrimSpace(p.Source.Commit))
	if p.ComfyUIRef == "" {
		p.ComfyUIRef = p.Source.Ref
	}
	if p.Source.Ref != "" && p.Source.Ref != p.ComfyUIRef {
		return Recipe{}, &ValidationError{Message: "ComfyUI 版本引用与解析结果不一致，请重新选择版本"}
	}
	if !comfyRefPattern.MatchString(p.ComfyUIRef) || strings.Contains(p.ComfyUIRef, "..") {
		return Recipe{}, &ValidationError{Message: "ComfyUI 版本引用格式不合法"}
	}
	switch p.PipIndexURL {
	case "tsinghua", "":
		p.PipIndexURL = "https://pypi.tuna.tsinghua.edu.cn/simple"
	case "aliyun":
		p.PipIndexURL = "https://mirrors.aliyun.com/pypi/simple/"
	case "pypi":
		p.PipIndexURL = "https://pypi.org/simple"
	default:
		if !strings.HasPrefix(p.PipIndexURL, "https://") && !strings.HasPrefix(p.PipIndexURL, "http://") {
			p.PipIndexURL = "https://pypi.tuna.tsinghua.edu.cn/simple"
		}
	}
	p.Platform = normalizePlatform(p.Platform)
	if p.WithManager != "0" {
		p.WithManager = "1"
	}
	return recipe, nil
}

// BuildContext 生成 Docker API /build 所需的 tar 构建上下文。
func BuildContext(params BuildParams, recipe Recipe) (io.Reader, error) {
	dockerfile := BuildDockerfile(params, recipe)
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	if err := writer.WriteHeader(&tar.Header{Name: "Dockerfile", Mode: 0o600, Size: int64(len(dockerfile))}); err != nil {
		return nil, err
	}
	if _, err := writer.Write([]byte(dockerfile)); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return bytes.NewReader(buffer.Bytes()), nil
}

// BuildDockerfile 返回平台内构建 ComfyUI 镜像的 Dockerfile。
// aptSources 镜像源加固：依次尝试国内 http 镜像，全部失败才回退官方源。
// 两个关键约束：
//  1. 基础镜像不带 CA 证书，必须用 http，否则 https 源全部握手失败；
//  2. apt-get update 在索引全部拉取失败时仍会返回 0（只打印 W:/Err:），
//     因此必须按日志判定成败，不能依赖退出码。
func aptSources() string {
	return `RUN set -eux; \
    SOURCES=/etc/apt/sources.list.d/ubuntu.sources; \
    [ -f "$SOURCES" ] || SOURCES=/etc/apt/sources.list; \
    cp -a "$SOURCES" /tmp/apt-orig; \
    apt_try() { \
      apt-get update -o Acquire::Retries=3 >/tmp/apt-update.log 2>&1 || true; \
      ! grep -qE '^(Err|E):|Failed to fetch|Some index files failed' /tmp/apt-update.log; \
    }; \
    apt_ok=0; \
    for MIRROR in http://mirrors.huaweicloud.com http://mirrors.tuna.tsinghua.edu.cn http://mirrors.aliyun.com; do \
      cp -a /tmp/apt-orig "$SOURCES"; \
      sed -i -e "s|http://ports.ubuntu.com/ubuntu-ports|$MIRROR/ubuntu-ports|g" \
             -e "s|http://archive.ubuntu.com/ubuntu|$MIRROR/ubuntu|g" \
             -e "s|http://security.ubuntu.com/ubuntu|$MIRROR/ubuntu|g" \
             "$SOURCES"; \
      echo "尝试 APT 镜像：$MIRROR"; \
      if apt_try; then echo "APT 镜像可用：$MIRROR"; apt_ok=1; break; fi; \
      echo "该镜像索引不完整，继续下一个"; \
    done; \
    if [ "$apt_ok" = "0" ]; then \
      echo "国内镜像均不可用，回退官方源"; \
      cp -a /tmp/apt-orig "$SOURCES"; \
      if apt_try; then apt_ok=1; fi; \
    fi; \
    if [ "$apt_ok" = "0" ]; then \
      echo "APT 索引不可用，最近一次输出如下："; \
      tail -30 /tmp/apt-update.log; \
      exit 1; \
    fi`
}

// aptInstall 安装运行期依赖：glib 在 Ubuntu 24.04 已改名为 t64，按索引实际存在的包名选择。
func aptInstall(packages string) string {
	return "RUN set -eux; \\\n" +
		"    GLIB=\"$(apt-cache show libglib2.0-0t64 >/dev/null 2>&1 && echo libglib2.0-0t64 || echo libglib2.0-0)\"; \\\n" +
		"    apt-get install -y --no-install-recommends " + packages + " \"$GLIB\"; \\\n" +
		"    rm -rf /var/lib/apt/lists/*"
}

func BuildDockerfile(params BuildParams, recipe Recipe) string {
	labels := buildLabels(params, recipe)
	sourceFile := buildSourceFileCommand(params)
	sources := aptSources()
	commonEnv := `ENV DEBIAN_FRONTEND=noninteractive \
    PYTHONUNBUFFERED=1 \
    HF_ENDPOINT=https://hf-mirror.com \
    PYTORCH_CUDA_ALLOC_CONF=expandable_segments:True`
	clone := `ARG COMFYUI_REF=master
ARG COMFYUI_COMMIT=
ARG WITH_MANAGER=1
RUN set -eux; \
    test -n "$COMFYUI_COMMIT"; \
    mkdir -p /app; \
    git init /app/ComfyUI; \
    cd /app/ComfyUI; \
    git remote add origin https://github.com/Comfy-Org/ComfyUI.git; \
    if ! git fetch --depth 1 origin "$COMFYUI_COMMIT"; then \
      FETCHED=0; \
      for MIRROR in https://gh-proxy.com/https://github.com/Comfy-Org/ComfyUI.git https://gitclone.com/github.com/Comfy-Org/ComfyUI; do \
        git remote set-url origin "$MIRROR"; \
        if git fetch --depth 1 origin "$COMFYUI_COMMIT"; then FETCHED=1; break; fi; \
      done; \
      test "$FETCHED" = "1"; \
    fi; \
    git checkout --detach "$COMFYUI_COMMIT"; \
    test "$(git rev-parse HEAD)" = "$COMFYUI_COMMIT"; \
    if [ "$WITH_MANAGER" = "1" ]; then \
      git clone --depth 1 https://github.com/ltdrdata/ComfyUI-Manager.git /app/ComfyUI/custom_nodes/ComfyUI-Manager \
        || git clone --depth 1 https://gh-proxy.com/https://github.com/ltdrdata/ComfyUI-Manager.git /app/ComfyUI/custom_nodes/ComfyUI-Manager \
        || git clone --depth 1 https://gitclone.com/github.com/ltdrdata/ComfyUI-Manager /app/ComfyUI/custom_nodes/ComfyUI-Manager; \
    fi; \
    ` + sourceFile
	if recipe.ID == "amd-rocm" {
		return fmt.Sprintf(`FROM rocm/pytorch:rocm6.2_ubuntu22.04_py3.10_pytorch_release_2.3.0
ARG PIP_INDEX_URL=https://pypi.tuna.tsinghua.edu.cn/simple
%s
%s
%s
%s
%s
WORKDIR /app/ComfyUI
RUN grep -vE '^(torch|torchvision|torchaudio)\b' requirements.txt > /tmp/req.txt \
    && ( pip install --timeout 120 --retries 5 -r /tmp/req.txt -i $PIP_INDEX_URL \
      || pip install --timeout 120 --retries 5 -r /tmp/req.txt -i https://mirrors.aliyun.com/pypi/simple/ \
      || pip install --timeout 120 --retries 5 -r /tmp/req.txt -i https://pypi.org/simple )
RUN pip install --timeout 120 --retries 5 accelerate sentencepiece einops opencv-python-headless gitpython -i $PIP_INDEX_URL \
    && python -c "import torch; print('torch', torch.__version__, 'hip', torch.version.hip)" \
    && python -c "import torchsde, einops, safetensors, transformers, aiohttp; print('依赖自检通过')"
EXPOSE 8188
CMD ["python", "main.py", "--listen", "0.0.0.0", "--port", "8188"]
`, labels, commonEnv, sources, aptInstall("git curl ca-certificates ffmpeg libgl1"), clone)
	}
	return fmt.Sprintf(`FROM ubuntu:24.04
ARG PIP_INDEX_URL=https://pypi.tuna.tsinghua.edu.cn/simple
ARG TORCH_INDEX_URL=%s
%s
%s
%s
%s
%s
RUN python3 -m venv /opt/venv
ENV PATH="/opt/venv/bin:$PATH"
RUN pip install --timeout 120 --retries 5 -U pip setuptools wheel -i $PIP_INDEX_URL
RUN pip install --timeout 300 --retries 5 torch torchvision torchaudio --index-url $TORCH_INDEX_URL
WORKDIR /app/ComfyUI
RUN grep -vE '^(torch|torchvision|torchaudio)\b' requirements.txt > /tmp/req.txt \
    && ( pip install --timeout 120 --retries 5 -r /tmp/req.txt -i $PIP_INDEX_URL \
      || pip install --timeout 120 --retries 5 -r /tmp/req.txt -i https://mirrors.aliyun.com/pypi/simple/ \
      || pip install --timeout 120 --retries 5 -r /tmp/req.txt -i https://pypi.org/simple )
RUN pip install --timeout 120 --retries 5 accelerate sentencepiece einops opencv-python-headless gitpython -i $PIP_INDEX_URL \
    && python -c "import torch; print('torch', torch.__version__, 'cuda', torch.version.cuda)" \
    && python -c "import torchsde, einops, safetensors, transformers, aiohttp; print('依赖自检通过')"
EXPOSE 8188
CMD ["python", "main.py", "--listen", "0.0.0.0", "--port", "8188"]
`, torchIndexURL(recipe), labels, commonEnv, sources, aptInstall("python3 python3-venv python3-dev git curl ca-certificates ffmpeg libgl1 build-essential"), clone)
}

func buildLabels(params BuildParams, recipe Recipe) string {
	manager := "否"
	if params.WithManager != "0" {
		manager = "是"
	}
	source := params.Source
	if source == nil {
		source = &ComfyVersion{Ref: params.ComfyUIRef}
	}
	version := source.Version
	if version == "" {
		version = source.DisplayLabel
	}
	if version == "" {
		version = source.Ref
	}
	return fmt.Sprintf(`LABEL org.opencontainers.image.title="ComfyUI" \
      org.opencontainers.image.revision="%s" \
      org.opencontainers.image.version="%s" \
      org.opencontainers.image.source="%s" \
      flowpilot.kind="comfyui" \
      flowpilot.build.variant="%s" \
      flowpilot.build.gpu="%s" \
      flowpilot.build.cuda="%s" \
      flowpilot.build.platform="%s" \
      flowpilot.comfyui.ref="%s" \
      flowpilot.comfyui.version="%s" \
      flowpilot.comfyui.commit="%s" \
      flowpilot.comfyui.channel="%s" \
      flowpilot.comfyui.commit_date="%s" \
      flowpilot.comfyui.published_at="%s" \
      flowpilot.comfyui.resolved_at="%s" \
      flowpilot.comfyui.repository="%s" \
      flowpilot.comfyui.manager="%s" \
      flowpilot.build.summary="%s"`,
		dockerLabelValue(source.Commit), dockerLabelValue(version), dockerLabelValue(ComfyRepositoryURL),
		dockerLabelValue(recipe.ID), dockerLabelValue(recipe.GPU), dockerLabelValue(recipe.CUDA), dockerLabelValue(params.Platform),
		dockerLabelValue(params.ComfyUIRef), dockerLabelValue(version), dockerLabelValue(source.Commit), dockerLabelValue(source.Kind),
		dockerLabelValue(source.CommitAt), dockerLabelValue(source.PublishedAt), dockerLabelValue(source.ResolvedAt), dockerLabelValue(ComfyRepository),
		dockerLabelValue(manager), dockerLabelValue(buildSummaryText(params, recipe)))
}

// buildSourceFileCommand 在镜像内写入可机器读取的源码档案。
func buildSourceFileCommand(params BuildParams) string {
	source := params.Source
	if source == nil {
		return `true`
	}
	metadata, _ := json.Marshal(map[string]any{
		"repository":  ComfyRepository,
		"ref":         params.ComfyUIRef,
		"version":     source.Version,
		"commit":      source.Commit,
		"channel":     source.Kind,
		"commitAt":    source.CommitAt,
		"publishedAt": source.PublishedAt,
		"resolvedAt":  source.ResolvedAt,
	})
	value := strings.ReplaceAll(string(metadata), "'", "'\\''")
	return `mkdir -p /opt/flowpilot && printf '%s\n' '` + value + `' > /opt/flowpilot/comfyui-source.json`
}

// comfyVersionDisplay 返回构建参数与镜像清单使用的版本文案。
func comfyVersionDisplay(source *ComfyVersion, fallback string) string {
	if source == nil {
		if fallback == "master" {
			return "master（未固定提交）"
		}
		return fallback
	}
	if source.DisplayLabel != "" {
		return source.DisplayLabel
	}
	if source.Version != "" {
		return source.Version + " · " + source.CommitShort
	}
	return source.Ref + "@" + source.CommitShort
}

func buildSummary(params BuildParams, recipe Recipe) []BuildParamSummary {
	manager := "不内置"
	managerDesc := "实例启动后可自行安装 ComfyUI-Manager"
	if params.WithManager != "0" {
		manager = "内置"
		managerDesc = "构建时克隆 ComfyUI-Manager 到 custom_nodes"
	}
	return []BuildParamSummary{
		{Label: "CPU 架构", Value: platformLabel(params.Platform), Description: "用于跨主机/集群分发镜像，Docker 构建时写入 --platform"},
		{Label: "GPU 配方", Value: recipe.Label, Description: recipe.Description},
		{Label: "ComfyUI 版本", Value: comfyVersionDisplay(params.Source, params.ComfyUIRef), Description: "构建时固定到该不可变 commit，不跟随分支漂移"},
		{Label: "pip 源", Value: params.PipIndexURL, Description: "容器内 Python 依赖安装源"},
		{Label: "ComfyUI-Manager", Value: manager, Description: managerDesc},
	}
}

func buildSummaryText(params BuildParams, recipe Recipe) string {
	parts := make([]string, 0, 5)
	for _, item := range buildSummary(params, recipe) {
		parts = append(parts, item.Label+"="+item.Value)
	}
	return strings.Join(parts, "；")
}

func dockerLabelValue(value string) string {
	value = strings.ReplaceAll(value, `\\`, `/`)
	value = strings.ReplaceAll(value, `"`, `'`)
	value = strings.ReplaceAll(value, "\n", " ")
	return value
}

func normalizePlatform(platform string) string {
	switch strings.TrimSpace(platform) {
	case "linux/amd64", "amd64", "x86_64":
		return "linux/amd64"
	case "linux/arm64", "arm64", "aarch64":
		return "linux/arm64"
	case "linux/arm/v7", "arm/v7":
		return "linux/arm/v7"
	default:
		return "host"
	}
}

func dockerPlatform(platform string) string {
	if platform == "host" || platform == "" {
		return ""
	}
	return platform
}

func platformLabel(platform string) string {
	switch platform {
	case "linux/amd64":
		return "linux/amd64（x86_64）"
	case "linux/arm64":
		return "linux/arm64（aarch64）"
	case "linux/arm/v7":
		return "linux/arm/v7"
	default:
		return "宿主机架构"
	}
}

func inferBuildProgress(line string) int {
	var current, total int
	if _, err := fmt.Sscanf(line, "Step %d/%d", &current, &total); err == nil && total > 0 {
		progress := current * 90 / total
		if progress < 1 {
			return 1
		}
		if progress > 95 {
			return 95
		}
		return progress
	}
	lower := strings.ToLower(line)
	switch {
	case strings.Contains(lower, "successfully tagged") || strings.Contains(lower, "naming to") || strings.Contains(lower, "writing image"):
		return 95
	case strings.Contains(lower, "install"):
		return 45
	case strings.Contains(lower, "clone"):
		return 25
	default:
		return 0
	}
}

func torchIndexURL(recipe Recipe) string {
	switch recipe.ID {
	case "nvidia-cu130":
		return "https://download.pytorch.org/whl/cu130"
	case "nvidia-cu128":
		return "https://download.pytorch.org/whl/cu128"
	case "nvidia-cu126":
		return "https://download.pytorch.org/whl/cu126"
	case "nvidia-cu121":
		return "https://download.pytorch.org/whl/cu121"
	case "amd-rocm":
		return "https://download.pytorch.org/whl/rocm6.2"
	default:
		return "https://download.pytorch.org/whl/cpu"
	}
}

// validateImage 校验镜像名格式，拒绝明显非法输入。
func validateImage(image string) error {
	if image == "" || len(image) > 255 || strings.ContainsAny(image, " \t\n\"'") {
		return &ValidationError{Message: "镜像名称不合法"}
	}
	return nil
}
