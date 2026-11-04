#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CODE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
IMAGE="flowpilot-web:0.0.0.1"
CONTAINER="flowpilot-web-demo"
PORT="${FLOWPILOT_DEMO_PORT:-18080}"

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
  docker run -d --name "${CONTAINER}" --restart unless-stopped -p "${PORT}:80" "${IMAGE}" >/dev/null
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
