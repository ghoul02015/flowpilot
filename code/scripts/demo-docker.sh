#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CODE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
IMAGE="flowpilot-web:0.1.0.6"
REGRESSION_IMAGE="flowpilot/comfyui-regress:0.1.0.6"
CONTAINER="flowpilot-web-demo"
PORT="${FLOWPILOT_DEMO_PORT:-18080}"
DOCKER_SOCKET="${FLOWPILOT_DOCKER_SOCKET:-/var/run/docker.sock}"

usage() {
  printf '用法：%s [up|build|stop|down|logs|status]\n' "$0"
  printf '环境变量：FLOWPILOT_DEMO_PORT，默认 18080\n'
}

build_image() {
  # 受限网络可通过 GOPROXY/NPM_REGISTRY 环境变量覆盖默认源（自动透传为 build-arg）。
  docker build -f "${CODE_DIR}/deploy/web.Dockerfile" \
    --build-arg GOPROXY="${GOPROXY:-https://proxy.golang.org,direct}" \
    --build-arg NPM_REGISTRY="${NPM_REGISTRY:-https://registry.npmjs.org}" \
    -t "${IMAGE}" "${CODE_DIR}"
  # 仅用于本机端到端回归：让新建实例在“只展示 ComfyUI 镜像”规则下仍可用轻量健康镜像快速验证生命周期。
  docker tag "${IMAGE}" "${REGRESSION_IMAGE}"
}

start_container() {
  if docker ps -a --format '{{.Names}}' | grep -Fxq "${CONTAINER}"; then
    docker rm -f "${CONTAINER}" >/dev/null
  fi
  DATA_DIR="$(cd "${CODE_DIR}/.." && pwd)/.tmp/flowpilot-data"
  IMPORT_DIR="$(cd "${CODE_DIR}/.." && pwd)/.tmp/flowpilot-import"
  mkdir -p "${DATA_DIR}" "${IMPORT_DIR}"
  rm -f "${DATA_DIR}/flowpilot.db" "${DATA_DIR}/flowpilot.db-shm" "${DATA_DIR}/flowpilot.db-wal" "${DATA_DIR}/init-token"
  docker run -d --name "${CONTAINER}" --restart unless-stopped \
    --add-host host.docker.internal:host-gateway \
    -p "${PORT}:18080" \
    -v "${DOCKER_SOCKET}:/var/run/docker.sock" \
    -v "${DATA_DIR}:/data/flowpilot" \
    -v "${IMPORT_DIR}:/data/import:ro" \
    -e FLOWPILOT_HOST_DATA_DIR="${DATA_DIR}" \
    -e FLOWPILOT_COMFYUI_IMAGE="${REGRESSION_IMAGE}" \
    -e FLOWPILOT_INIT_TOKEN="${FLOWPILOT_DEMO_INIT_TOKEN:-flowpilot-demo-init-2026}" \
    -e FLOWPILOT_IMAGE_ALLOW_UNLABELED=1 \
    -e FLOWPILOT_DOWNLOAD_ALLOW_HOSTS=localhost \
    "${IMAGE}" >/dev/null
  printf 'FlowPilot Demo：http://localhost:%s\n' "${PORT}"
}

case "${1:-up}" in
  up)
    build_image
    start_container
    ;;
  build)
    build_image
    ;;
  stop)
    docker stop "${CONTAINER}" >/dev/null
    ;;
  down)
    docker rm -f "${CONTAINER}" >/dev/null 2>&1 || true
    ;;
  logs)
    docker logs -f "${CONTAINER}"
    ;;
  status)
    docker ps -a --filter "name=^${CONTAINER}$"
    ;;
  *)
    usage
    exit 1
    ;;
esac
