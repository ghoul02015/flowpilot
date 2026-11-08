#!/usr/bin/env bash
###############################################################################
# DGX Spark 一键部署：ComfyUI + MiniMax H3（容器化，局域网 Web 访问）
# 最终版 v2.0（2026-11-05）—— 已吸收一次完整实战部署中的全部踩坑修复
#
# 适用环境：NVIDIA DGX Spark（GB10，ARM64，CUDA 13.x，DGX OS / Ubuntu）
# 用法：    bash DGX_Spark_ComfyUI_H3_一键部署.sh
#
# 可用环境变量（非交互/自定义时）：
#   INSTALL_DIR=~/comfyui-h3        安装目录
#   PORT=8188                       Web 端口
#   MODEL_SET=fl2va|all             模型范围（默认交互询问）
#     - fl2va：文生视频/图生视频（首尾帧），约 40GB
#     - all  ：再加 Ref2VA 参考生视频，约 60GB
#   NONINTERACTIVE=1                跳过所有询问，全用默认值
#   SKIP_BUILD=1                    镜像已存在时跳过构建，直接下载模型/重启容器
#   PIP_INDEX_URL                   容器内 pip 源（默认清华镜像）
#   TORCH_INDEX_URL                 torch 轮子源（默认官方 cu130，GB10 需要）
###############################################################################

set -euo pipefail

# ----------------------------- 可配置变量 -----------------------------------
INSTALL_DIR="${INSTALL_DIR:-$HOME/comfyui-h3}"
PORT="${PORT:-8188}"
IMAGE_NAME="comfyui-h3:spark"
CONTAINER_NAME="comfyui-h3"
PIP_INDEX_URL="${PIP_INDEX_URL:-https://pypi.tuna.tsinghua.edu.cn/simple}"
TORCH_INDEX_URL="${TORCH_INDEX_URL:-https://download.pytorch.org/whl/cu130}"
# 国内实测可用的 Docker Hub 加速源（可多几个做冗余）
REGISTRY_MIRRORS='[
  "https://docker.m.daocloud.io",
  "https://docker.1ms.run",
  "https://docker.xuanyuan.me",
  "https://docker.mirrors.ustc.edu.cn"
]'
# GitHub 克隆通道（按优先级排序，%s 会被替换为 user/repo）。
# 低速自动放弃机制：连续 30 秒速度低于 100KB/s 即掐断，换下一家，绝不干等。
# 国内实测建议把加速通道放前面，GitHub 直连放最后兜底。
GIT_CLONE_CHANNELS=(
  "https://ghfast.top/https://github.com/%s.git"
  "https://gitclone.com/github.com/%s"
  "https://gh-proxy.com/https://github.com/%s.git"
  "https://github.com/%s.git"
)
# 低速判定：30 秒内平均速度低于该值（字节/秒）则放弃当前通道
GIT_LOW_SPEED_LIMIT="${GIT_LOW_SPEED_LIMIT:-102400}"
GIT_LOW_SPEED_TIME="${GIT_LOW_SPEED_TIME:-30}"
# 单条通道最长容忍时间（秒）
GIT_CLONE_TIMEOUT="${GIT_CLONE_TIMEOUT:-300}"

# ----------------------------- 小工具函数 -----------------------------------
log()  { printf "\033[1;32m[✓]\033[0m %s\n" "$*"; }
info() { printf "\033[1;34m[i]\033[0m %s\n" "$*"; }
warn() { printf "\033[1;33m[!]\033[0m %s\n" "$*"; }
die()  { printf "\033[1;31m[✗]\033[0m %s\n" "$*" >&2; exit 1; }

need_sudo() {
  if [ "$(id -u)" -ne 0 ]; then
    command -v sudo >/dev/null 2>&1 || die "需要 sudo 权限，请用普通用户 + sudo 运行"
    SUDO="sudo"
  else
    SUDO=""
  fi
}

# ------------------------- 0. 环境预检 --------------------------------------
info "========== 步骤 0/8：环境预检 =========="

ARCH="$(uname -m)"
[ "$ARCH" = "aarch64" ] || warn "当前架构为 $ARCH，本脚本针对 DGX Spark（aarch64）优化"

if command -v nvidia-smi >/dev/null 2>&1; then
  nvidia-smi --query-gpu=name,memory.total --format=csv,noheader || true
else
  die "未检测到 nvidia-smi，请确认这是 DGX Spark 且驱动正常"
fi

need_sudo

# 安装 Docker（DGX Spark 一般自带，这里兜底）
if ! command -v docker >/dev/null 2>&1; then
  warn "未检测到 Docker，使用阿里云镜像脚本安装..."
  curl -fsSL https://get.docker.com | bash -s docker --mirror Aliyun
  $SUDO usermod -aG docker "$USER" || true
fi

# NVIDIA Container Toolkit 检查
if ! docker info 2>/dev/null | grep -qi nvidia && ! docker info 2>/dev/null | grep -q '"gpu"'; then
  warn "Docker 未启用 NVIDIA 运行时，安装 nvidia-container-toolkit..."
  $SUDO apt-get update && $SUDO apt-get install -y nvidia-container-toolkit
  $SUDO nvidia-ctk runtime configure --runtime=docker || true
  $SUDO systemctl restart docker
fi

# docker 免 sudo（若尚未生效，本脚本内用 sudo docker）
DOCKER="docker"
docker ps >/dev/null 2>&1 || DOCKER="$SUDO docker"
log "Docker 就绪：$($DOCKER version --format '{{.Server.Version}}' 2>/dev/null || echo unknown)"

# -------------------- 1. 配置 Docker 国内镜像加速 ---------------------------
info "========== 步骤 1/8：配置 Docker Hub 国内镜像加速 =========="
$SUDO python3 - <<PYEOF
import json, os
p = "/etc/docker/daemon.json"
cfg = {}
if os.path.exists(p):
    try:
        cfg = json.load(open(p))
    except Exception:
        os.rename(p, p + ".bak"); cfg = {}
mirrors = json.loads('''$REGISTRY_MIRRORS''')
if cfg.get("registry-mirrors") != mirrors:
    cfg["registry-mirrors"] = mirrors
    json.dump(cfg, open(p, "w"), indent=2, ensure_ascii=False)
    print("UPDATED")
else:
    print("UNCHANGED")
PYEOF
$SUDO systemctl restart docker
sleep 2
log "Docker 镜像加速已配置（DaoCloud / 毫秒 / 轩辕 / 中科大）"

# -------------------- 2. 选择模型范围 ---------------------------------------
info "========== 步骤 2/8：选择模型范围 =========="
if [ -z "${MODEL_SET:-}" ]; then
  if [ "${NONINTERACTIVE:-0}" = "1" ]; then
    MODEL_SET="fl2va"
  else
    echo "  1) 仅 FL2VA —— 文生视频 / 图生视频（首尾帧），下载约 40GB【推荐先跑通】"
    echo "  2) FL2VA + Ref2VA —— 另加参考生视频（角色一致性/动作迁移），共约 60GB"
    read -r -p "请选择 [1/2，默认 1]: " choice
    case "${choice:-1}" in
      2) MODEL_SET="all" ;;
      *) MODEL_SET="fl2va" ;;
    esac
  fi
fi
log "模型范围：$MODEL_SET"

# -------------------- 3. 创建目录结构 ---------------------------------------
info "========== 步骤 3/8：创建目录 $INSTALL_DIR =========="
mkdir -p "$INSTALL_DIR"/{models/{diffusion_models,text_encoders,vae,loras},output,input,user,hf-cache}
log "目录就绪"

# -------------------- 4. 拉取 ComfyUI 源码（含国内 GitHub 加速） -------------
info "========== 步骤 4/8：获取 ComfyUI 源码（需 0.30.0+，H3 为内置节点） =========="
git_clone_fallback() {
  local repo_path="$1" dest="$2"
  if [ -d "$dest/.git" ]; then
    log "已存在，跳过克隆：$dest"
    return 0
  fi
  rm -rf "$dest"
  local tmpl url
  for tmpl in "${GIT_CLONE_CHANNELS[@]}"; do
    # shellcheck disable=SC2059
    url="$(printf "$tmpl" "$repo_path")"
    info "尝试通道：$url"
    # 低速自动掐断 + 超时兜底，避免卡死在某个慢通道上
    if timeout "$GIT_CLONE_TIMEOUT" git \
        -c http.lowSpeedLimit="$GIT_LOW_SPEED_LIMIT" \
        -c http.lowSpeedTime="$GIT_LOW_SPEED_TIME" \
        clone --depth 1 "$url" "$dest"; then
      log "克隆成功：$url"
      return 0
    fi
    warn "该通道失败或速度过慢（<100KB/s 持续 ${GIT_LOW_SPEED_TIME}s），换下一家..."
    rm -rf "$dest"
  done
  die "所有 GitHub 通道均失败。可手动克隆后放到 $dest（需含 .git 目录），再重跑本脚本"
}
git_clone_fallback "comfyanonymous/ComfyUI" "$INSTALL_DIR/ComfyUI"
git_clone_fallback "ltdrdata/ComfyUI-Manager" "$INSTALL_DIR/ComfyUI-Manager"

# -------------------- 5. 生成 Dockerfile 并构建镜像 --------------------------
info "========== 步骤 5/8：构建容器镜像（Ubuntu 24.04 + PyTorch cu130 ARM64） =========="
cat > "$INSTALL_DIR/Dockerfile" <<EOF
FROM ubuntu:24.04
ARG PIP_INDEX_URL=${PIP_INDEX_URL}
ARG TORCH_INDEX_URL=${TORCH_INDEX_URL}
ENV DEBIAN_FRONTEND=noninteractive \
    PYTHONUNBUFFERED=1 \
    HF_ENDPOINT=https://hf-mirror.com \
    PYTORCH_CUDA_ALLOC_CONF=expandable_segments:True

RUN apt-get update && apt-get install -y --no-install-recommends \
      python3 python3-venv python3-dev git curl ca-certificates \
      ffmpeg libgl1 libglib2.0-0 \
      build-essential \
    && rm -rf /var/lib/apt/lists/*

RUN python3 -m venv /opt/venv
ENV PATH="/opt/venv/bin:\$PATH"

# 所有 pip 步骤挂缓存目录：轮子下载一次永久留存宿主机，
# 今后 Dockerfile 改动导致层失效时，pip 本地命中，不再重复下载
RUN --mount=type=cache,target=/root/.cache/pip \
    pip install -U pip setuptools wheel -i \$PIP_INDEX_URL

# 【关键】先装 CUDA 13 版 PyTorch，且必须在 requirements 之前：
# kornia 等包声明依赖 torch>=x.y，若此时 torch 未安装，pip 会从 PyPI 拉取
# 整套 nvidia-* CUDA 轮子（单 cudnn 就近 600MB，共数 GB），既极慢又占空间。
# 先装好 cu130 版后，pip 判定依赖已满足，自动跳过这些巨型轮子。
# 若官方源慢，可改用阿里云镜像：-e TORCH_INDEX_URL=https://mirrors.aliyun.com/pytorch-wheels/cu130
RUN --mount=type=cache,target=/root/.cache/pip \
    pip install torch torchvision torchaudio --index-url \$TORCH_INDEX_URL

# ComfyUI 依赖（排除 torch 系列声明，防止重复解析；torch 已装好不受影响）
# 多源降级：清华镜像对 comfyui-workflow-templates-media-assets 等大素材包同步滞后，
# 失败后自动换阿里云（全量镜像），再不行走 PyPI 官方源
COPY ComfyUI /app/ComfyUI
WORKDIR /app/ComfyUI
RUN --mount=type=cache,target=/root/.cache/pip \
    grep -vE '^(torch|torchvision|torchaudio)\b' requirements.txt > /tmp/req.txt \
    && ( pip install -r /tmp/req.txt -i \$PIP_INDEX_URL \
      || pip install -r /tmp/req.txt -i https://mirrors.aliyun.com/pypi/simple/ \
      || pip install -r /tmp/req.txt -i https://pypi.org/simple )

# 补充工具包 + 构建期自检：确认 torch 是 CUDA 13 版（GB10 Blackwell sm_121 必需），
# 且 torchsde 等关键依赖完整（防止依赖被误排除后容器启动即崩）
RUN --mount=type=cache,target=/root/.cache/pip \
    pip install accelerate sentencepiece einops opencv-python-headless gitpython -i \$PIP_INDEX_URL \
    && python -c "import torch; v=torch.version.cuda; assert v and v.startswith('13'), f'需要 CUDA 13 版 torch，当前: {v}'; print('torch', torch.__version__, 'cuda', v)" \
    && python -c "import torchsde, einops, safetensors, transformers, aiohttp; print('依赖自检通过')"

COPY ComfyUI-Manager /app/ComfyUI/custom_nodes/ComfyUI-Manager

EXPOSE 8188
CMD ["python", "main.py", "--listen", "0.0.0.0", "--port", "8188"]
EOF

# SKIP_BUILD=1 时：镜像已存在则跳过构建（适合镜像建好后只需下载模型/重启容器）
if [ "${SKIP_BUILD:-0}" = "1" ] && $DOCKER image inspect "$IMAGE_NAME" >/dev/null 2>&1; then
  log "SKIP_BUILD=1 且镜像已存在，跳过构建"
else
  info "开始构建（首次构建需下载约 3GB 依赖，请耐心等待）..."
  DOCKER_BUILDKIT=1 $DOCKER build \
    --network=host \
    --platform linux/arm64 \
    --build-arg PIP_INDEX_URL="$PIP_INDEX_URL" \
    --build-arg TORCH_INDEX_URL="$TORCH_INDEX_URL" \
    -t "$IMAGE_NAME" \
    -f "$INSTALL_DIR/Dockerfile" "$INSTALL_DIR"
  log "镜像构建完成：$IMAGE_NAME"
fi

# -------------------- 6. 下载 H3 模型（魔搭 → hf-mirror → curl 直链） --------
info "========== 步骤 6/8：下载 MiniMax H3 模型 =========="

# DGX OS 的 Python 3.12 启用了 PEP 668 保护，禁止 pip 直装系统环境。
# 用独立 venv 安装下载工具，彻底绕开限制；装不上也能用 curl 兜底。
DL_VENV="$INSTALL_DIR/.venv-dl"
if [ ! -x "$DL_VENV/bin/python" ]; then
  $SUDO apt-get install -y python3-venv >/dev/null 2>&1 || true
  python3 -m venv "$DL_VENV"
fi
"$DL_VENV/bin/pip" install -U -q modelscope "huggingface_hub[cli]" -i "$PIP_INDEX_URL" \
  || "$DL_VENV/bin/pip" install -U -q modelscope "huggingface_hub[cli]" -i https://mirrors.aliyun.com/pypi/simple/ \
  || warn "下载工具安装失败，将使用 curl 直链兜底"
MS_CLI="$DL_VENV/bin/modelscope"
HF_CLI="$DL_VENV/bin/hf"
HF_MIRROR="https://hf-mirror.com"
REPO="Comfy-Org/MiniMax-H3"

# download_model <仓库相对路径> <目标子目录>
download_model() {
  local repo_file="$1" subdir="$2"
  local fname dest staging
  fname="$(basename "$repo_file")"
  dest="$INSTALL_DIR/models/$subdir/$fname"
  if [ -s "$dest" ]; then
    log "已存在，跳过：$fname"
    return 0
  fi
  staging="$INSTALL_DIR/.staging"
  rm -rf "$staging" && mkdir -p "$staging"

  # 通道 1：魔搭 ModelScope（国内最快）
  if [ -x "$MS_CLI" ]; then
    info "下载 $repo_file（魔搭 ModelScope）..."
    "$MS_CLI" download --model "$REPO" "$repo_file" --local_dir "$staging" || true
    # 新版 modelscope 可能把真实文件放进 ~/.cache/modelscope、local_dir 里只留符号链接，
    # 因此在两处都找，并用 cp -L 复制真实内容（避免把断链移进 models 目录）
    local found
    found="$(find -L "$staging" "$HOME/.cache/modelscope" -name "$fname" -type f 2>/dev/null | head -1)"
    if [ -n "$found" ]; then
      cp -L "$found" "$dest"
      rm -rf "$staging"; log "完成：$fname"; return 0
    fi
    warn "魔搭通道失败，换 hf-mirror CLI ..."
  fi

  # 通道 2：hf CLI + hf-mirror 端点
  if [ -x "$HF_CLI" ]; then
    rm -rf "$staging" && mkdir -p "$staging"
    if HF_ENDPOINT="$HF_MIRROR" "$HF_CLI" download "$REPO" "$repo_file" --local-dir "$staging" \
       && find "$staging" -name "$fname" | grep -q .; then
      find "$staging" -name "$fname" -exec mv -f {} "$dest" \;
      rm -rf "$staging"; log "完成：$fname"; return 0
    fi
    warn "hf-mirror CLI 通道失败，换 curl 直链 ..."
  fi

  # 通道 3：curl 直链 hf-mirror（无任何 Python 依赖，支持断点续传）
  rm -rf "$staging"
  info "curl 直链下载：$HF_MIRROR/$REPO/resolve/main/$repo_file"
  curl -fL -C - --retry 5 --retry-delay 5 --connect-timeout 30 \
       -o "$dest.part" "$HF_MIRROR/$REPO/resolve/main/$repo_file" \
    && mv -f "$dest.part" "$dest"

  [ -s "$dest" ] || die "下载失败：$repo_file（三个通道均不可用，请检查网络后重跑，已下载部分会自动续传）"
  log "完成：$fname"
}

# 扩散模型：FL2VA（文生/图生，INT8 量化）
download_model "diffusion_models/minimax_h3_fl2va_pruned_int8_convrot.safetensors" "diffusion_models"
# 参考生视频（可选）
if [ "$MODEL_SET" = "all" ]; then
  download_model "diffusion_models/minimax_h3_ref2va_pruned_int8_convrot.safetensors" "diffusion_models"
fi
# 文本编码器（NVFP4，GB10 Blackwell 原生支持）+ 视频/音频 VAE
download_model "text_encoders/qwen3vl_32b_minimax_h3_nvfp4_awq.safetensors" "text_encoders"
download_model "vae/minimax_h3_video_vae_fp16.safetensors" "vae"
download_model "vae/minimax_h3_audio_vae_fp32.safetensors" "vae"
# 8 步蒸馏加速 LoRA（H3 官方模板默认使用，显著提速；模板里选 FP16 版视频 VAE 即可）
download_model "loras/minimax_h3_fl2v_turbo_8step_v1.0_comfyui_bf16.safetensors" "loras"
log "模型全部就绪"

# -------------------- 7. 启动容器 --------------------------------------------
info "========== 步骤 7/8：启动 ComfyUI 容器 =========="
$DOCKER rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true
# 显式指定 entrypoint 与启动参数：即使镜像配置被意外污染（如 commit 固化错误命令），
# 容器也能以正确方式启动 ComfyUI
$DOCKER run -d \
  --name "$CONTAINER_NAME" \
  --gpus all \
  --ipc=host \
  --restart unless-stopped \
  -p "${PORT}:8188" \
  -v "$INSTALL_DIR/models:/app/ComfyUI/models" \
  -v "$INSTALL_DIR/output:/app/ComfyUI/output" \
  -v "$INSTALL_DIR/input:/app/ComfyUI/input" \
  -v "$INSTALL_DIR/user:/app/ComfyUI/user" \
  -v "$INSTALL_DIR/hf-cache:/root/.cache/huggingface" \
  --entrypoint /opt/venv/bin/python \
  "$IMAGE_NAME" main.py --listen 0.0.0.0 --port 8188
log "容器已启动：$CONTAINER_NAME"

# 防火墙自动放行（若 ufw 处于启用状态）
if command -v ufw >/dev/null 2>&1 && $SUDO ufw status 2>/dev/null | grep -q "Status: active"; then
  $SUDO ufw allow "${PORT}"/tcp >/dev/null 2>&1 || true
  log "防火墙已放行 ${PORT}/tcp"
fi

# -------------------- 8. 验证与输出 ------------------------------------------
info "========== 步骤 8/8：等待服务就绪 =========="
ok=0
for i in $(seq 1 100); do
  st="$($DOCKER inspect -f '{{.State.Status}} {{.State.ExitCode}}' "$CONTAINER_NAME" 2>/dev/null || echo 'unknown 1')"
  case "$st" in
    running*) ;;
    *) warn "容器状态：$st（等待恢复中，Ctrl+C 可退出观察）" ;;
  esac
  if curl -fsS "http://127.0.0.1:${PORT}/" >/dev/null 2>&1; then ok=1; break; fi
  sleep 5
done
[ "$ok" = "1" ] || warn "服务尚未就绪，请查看日志：docker logs --tail 50 $CONTAINER_NAME"

LAN_IP="$(hostname -I 2>/dev/null | awk '{print $1}')"
echo
log "部署完成！局域网内任意设备浏览器访问："
echo
echo "      http://${LAN_IP}:${PORT}"
echo
echo "  常用命令："
echo "    查看日志：   docker logs -f $CONTAINER_NAME"
echo "    停止服务：   docker stop $CONTAINER_NAME"
echo "    启动服务：   docker start $CONTAINER_NAME   （开机自动拉起已配置）"
echo "    模型目录：   $INSTALL_DIR/models"
echo "    输出目录：   $INSTALL_DIR/output"
echo
echo "  使用提示："
echo "    1. 打开 Web 界面 → 模板(Templates) → 搜索 MiniMax H3 → 选 T2V / I2V"
echo "       $( [ "$MODEL_SET" = "all" ] && echo "/ R2V 工作流" )"
echo "    2. 首次生成建议先跑 5 秒 768p 短片验证管线"
echo "    3. 若局域网无法访问，检查防火墙：sudo ufw allow ${PORT}/tcp"
