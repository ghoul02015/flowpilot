#!/usr/bin/env bash
set -Eeuo pipefail

VERSION="0.0.0.5"
IMAGE="flowpilot-web:${VERSION}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUNDLE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
IMAGE_TAR="${FLOWPILOT_IMAGE_TAR:-${BUNDLE_DIR}/image/${IMAGE//:/-}-arm64.tar.gz}"
INSTALL_HOME="${FLOWPILOT_HOME:-${XDG_DATA_HOME:-${HOME}/.local/share}/flowpilot}"
CONTAINER_NAME="${FLOWPILOT_CONTAINER:-flowpilot-web-demo}"
PORT="${FLOWPILOT_PORT:-18080}"
DOCKER_SOCKET="${FLOWPILOT_DOCKER_SOCKET:-/var/run/docker.sock}"

fail() {
  printf '安装失败：%s\n' "$1" >&2
  exit 1
}

command -v docker >/dev/null 2>&1 || fail '未找到 docker，请先安装 Docker Engine'
docker info >/dev/null 2>&1 || fail '当前用户无法访问 Docker，请检查 docker 用户组或 Socket 权限'
[ -S "${DOCKER_SOCKET}" ] || fail "Docker Socket 不存在：${DOCKER_SOCKET}"
[ -f "${IMAGE_TAR}" ] || fail "未找到镜像包：${IMAGE_TAR}"

if command -v ss >/dev/null 2>&1 && ss -ltn | awk '{print $4}' | grep -Eq "(^|:)${PORT}$"; then
  if ! docker ps --format '{{.Names}} {{.Ports}}' | grep -F "${CONTAINER_NAME}" | grep -Eq "[:.]${PORT}->"; then
    fail "端口 ${PORT} 已被其他进程占用"
  fi
fi

printf '安装目录：%s\n' "${INSTALL_HOME}"
printf '容器名称：%s\n' "${CONTAINER_NAME}"
printf '管理端口：%s\n' "${PORT}"

gzip -t "${IMAGE_TAR}"
gzip -dc "${IMAGE_TAR}" | docker load

mkdir -p "${INSTALL_HOME}/bin" "${INSTALL_HOME}/releases/${VERSION}" "${INSTALL_HOME}/shared/models" "${INSTALL_HOME}/shared/workflows" "${INSTALL_HOME}/shared/outputs"
cp "${BUNDLE_DIR}/scripts/uninstall.sh" "${INSTALL_HOME}/bin/flowpilot-uninstall"
cp "${BUNDLE_DIR}/VERSION" "${INSTALL_HOME}/releases/${VERSION}/VERSION"
chmod +x "${INSTALL_HOME}/bin/flowpilot-uninstall"

if docker ps -a --format '{{.Names}}' | grep -Fxq "${CONTAINER_NAME}"; then
  docker rm -f "${CONTAINER_NAME}" >/dev/null
fi

# GPU 透传：优先使用 NVIDIA Container Toolkit；缺失时手动挂载
# 宿主机 nvidia-smi、libnvidia-ml 与 /dev/nvidia* 设备，保证容器内
# 可以采集 GPU 指标。无 GPU 主机则跳过，硬件接口自动降级。
GPU_ARGS=()
if command -v nvidia-smi >/dev/null 2>&1; then
  if docker info --format '{{json .Runtimes}}' 2>/dev/null | grep -qi nvidia; then
    GPU_ARGS+=(--gpus all)
    printf 'GPU 透传：NVIDIA Container Toolkit（--gpus all）\n'
  else
    NVIDIA_BIN="$(command -v nvidia-smi)"
    GPU_ARGS+=(-v "${NVIDIA_BIN}:/usr/bin/nvidia-smi:ro")
    ML_LIB="$(ldconfig -p 2>/dev/null | awk '/libnvidia-ml\.so\.1 /{print $NF; exit}')"
    if [ -n "${ML_LIB}" ] && [ -e "${ML_LIB}" ]; then
      GPU_ARGS+=(-v "${ML_LIB}:${ML_LIB}:ro" --env "LD_LIBRARY_PATH=$(dirname "${ML_LIB}")")
    fi
    for dev in /dev/nvidia*; do
      [ -e "${dev}" ] && GPU_ARGS+=(--device "${dev}:${dev}")
    done
    printf 'GPU 透传：手动挂载 nvidia-smi 与驱动设备\n'
  fi
else
  printf 'GPU 透传：未检测到 nvidia-smi，硬件接口将只返回主机指标\n'
fi

docker run -d \
  --name "${CONTAINER_NAME}" \
  --restart unless-stopped \
  --label flowpilot.managed=true \
  --label flowpilot.version="${VERSION}" \
  -p "${PORT}:18080" \
  -v "${DOCKER_SOCKET}:/var/run/docker.sock" \
  -v "${INSTALL_HOME}/shared:/data/flowpilot" \
  -v /etc/hostname:/host/etc/hostname:ro \
  -v /etc/os-release:/host/etc/os-release:ro \
  "${GPU_ARGS[@]}" \
  "${IMAGE}" >/dev/null

cat > "${INSTALL_HOME}/environment" <<EOF
FLOWPILOT_VERSION=${VERSION}
FLOWPILOT_IMAGE=${IMAGE}
FLOWPILOT_CONTAINER=${CONTAINER_NAME}
FLOWPILOT_PORT=${PORT}
FLOWPILOT_GPU_ARGS=${GPU_ARGS[*]:-}
EOF

for _ in $(seq 1 30); do
  if docker inspect "${CONTAINER_NAME}" --format '{{.State.Health.Status}}' 2>/dev/null | grep -Fxq healthy; then
    printf 'FlowPilot %s 安装完成：http://<服务器地址>:%s\n' "${VERSION}" "${PORT}"
    printf '一键卸载：%s/bin/flowpilot-uninstall --yes\n' "${INSTALL_HOME}"
    exit 0
  fi
  sleep 1
done

docker logs --tail 80 "${CONTAINER_NAME}" >&2 || true
fail '容器启动后未进入 healthy 状态'
