#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CODE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
IMAGE="flowpilot-web:0.0.6.3"
CONTAINER="flowpilot-web-demo"
PORT="${FLOWPILOT_DEMO_PORT:-18080}"
DOCKER_SOCKET="${FLOWPILOT_DOCKER_SOCKET:-/var/run/docker.sock}"

usage() {
  printf '用法：%s [up|build|stop|down|logs|status]\n' "$0"
  printf '环境变量：FLOWPILOT_DEMO_PORT，默认 18080\n'
}

build_image() {
  docker build -f "${CODE_DIR}/deploy/web.Dockerfile" -t "${IMAGE}" "${CODE_DIR}"
}

start_container() {
  if docker ps -a --format '{{.Names}}' | grep -Fxq "${CONTAINER}"; then
    docker rm -f "${CONTAINER}" >/dev/null
  fi
  DATA_DIR="$(cd "${CODE_DIR}/.." && pwd)/.tmp/flowpilot-data"
  IMPORT_DIR="$(cd "${CODE_DIR}/.." && pwd)/.tmp/flowpilot-import"
  mkdir -p "${DATA_DIR}" "${IMPORT_DIR}"
  docker run -d --name "${CONTAINER}" --restart unless-stopped \
    --add-host host.docker.internal:host-gateway \
    -p "${PORT}:18080" \
    -v "${DOCKER_SOCKET}:/var/run/docker.sock" \
    -v "${DATA_DIR}:/data/flowpilot" \
    -v "${IMPORT_DIR}:/data/import:ro" \
    -e FLOWPILOT_HOST_DATA_DIR="${DATA_DIR}" \
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
