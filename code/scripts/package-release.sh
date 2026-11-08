#!/usr/bin/env bash
set -Eeuo pipefail

VERSION="0.0.5.1"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OUTPUT_DIR="${FLOWPILOT_RELEASE_DIR:-${ROOT_DIR}/.tmp/release}"
IMAGE="flowpilot-web:${VERSION}"
BUNDLE="${OUTPUT_DIR}/flowpilot-${VERSION}-arm64"
IMAGE_TAR="${BUNDLE}/image/flowpilot-web-${VERSION}-arm64.tar.gz"

rm -rf "${BUNDLE}"
mkdir -p "${BUNDLE}/image" "${BUNDLE}/scripts"

"${ROOT_DIR}/code/scripts/demo-docker.sh" build
docker save "${IMAGE}" | gzip -9 > "${IMAGE_TAR}"
cp "${ROOT_DIR}/VERSION" "${BUNDLE}/VERSION"
cp "${ROOT_DIR}/code/scripts/install.sh" "${BUNDLE}/scripts/install.sh"
cp "${ROOT_DIR}/code/scripts/uninstall.sh" "${BUNDLE}/scripts/uninstall.sh"
chmod +x "${BUNDLE}/scripts/install.sh" "${BUNDLE}/scripts/uninstall.sh"

tar -C "${OUTPUT_DIR}" -czf "${OUTPUT_DIR}/flowpilot-${VERSION}-arm64.tar.gz" "$(basename "${BUNDLE}")"
printf '发布包：%s\n' "${OUTPUT_DIR}/flowpilot-${VERSION}-arm64.tar.gz"
printf '镜像包：%s\n' "${IMAGE_TAR}"
