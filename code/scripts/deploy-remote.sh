#!/usr/bin/env bash
set -Eeuo pipefail

usage() {
  printf '用法：%s <user@host> <发布包路径> [端口]\n' "$0"
  printf '示例：%s user@<服务器IP> .tmp/release/flowpilot-0.1.0.3-arm64.tar.gz 18080\n' "$0"
}

[ "$#" -ge 2 ] || { usage; exit 1; }
TARGET="$1"
PACKAGE="$2"
PORT="${3:-18080}"
[ -f "${PACKAGE}" ] || { printf '发布包不存在：%s\n' "${PACKAGE}" >&2; exit 1; }

REMOTE_DIR="/tmp/flowpilot-release-$(date +%s)"
scp "${PACKAGE}" "${TARGET}:/tmp/"
PACKAGE_NAME="$(basename "${PACKAGE}")"
ssh "${TARGET}" "set -e; mkdir -p '${REMOTE_DIR}'; tar -xzf '/tmp/${PACKAGE_NAME}' -C '${REMOTE_DIR}' --strip-components=1; FLOWPILOT_PORT='${PORT}' '${REMOTE_DIR}/scripts/install.sh'; rm -rf '${REMOTE_DIR}' '/tmp/${PACKAGE_NAME}'"
