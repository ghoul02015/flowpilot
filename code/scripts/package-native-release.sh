#!/usr/bin/env bash
# Build self-contained native Linux release archives (amd64 + arm64).
set -Eeuo pipefail

VERSION="$(tr -d '[:space:]' < "$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)/VERSION")"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OUTPUT_DIR="${FLOWPILOT_RELEASE_DIR:-${ROOT_DIR}/.tmp/release-native}"
WORK_DIR="${ROOT_DIR}/.tmp/native-build"
NODE_IMAGE="${FLOWPILOT_NODE_IMAGE:-node:24-alpine}"
GO_IMAGE="${FLOWPILOT_GO_IMAGE:-golang:1.25-alpine}"
REGION="${FLOWPILOT_NETWORK_REGION:-auto}"
NPM_REGISTRY="${FLOWPILOT_NPM_REGISTRY:-https://registry.npmjs.org}"
GOPROXY="${FLOWPILOT_GOPROXY:-https://proxy.golang.org,direct}"

if [ "${REGION}" = "cn" ]; then
  NPM_REGISTRY="${FLOWPILOT_NPM_REGISTRY:-https://registry.npmmirror.com}"
  GOPROXY="${FLOWPILOT_GOPROXY:-https://goproxy.cn,direct}"
fi

command -v docker >/dev/null 2>&1 || { printf '需要 Docker 仅用于构建发布包，目标服务器运行管理服务不需要构建环境。\n' >&2; exit 1; }
docker info >/dev/null 2>&1 || { printf 'Docker Engine 不可用。\n' >&2; exit 1; }

rm -rf "${WORK_DIR}"
mkdir -p "${WORK_DIR}/frontend" "${WORK_DIR}/go-cache" "${OUTPUT_DIR}"
# 不复制本机 node_modules/dist，确保发布包来自锁文件和当前源码。
tar -C "${ROOT_DIR}/code/frontend" \
  --exclude=node_modules --exclude=dist --exclude=test-results --exclude='*.tsbuildinfo' \
  -cf - . | tar -C "${WORK_DIR}/frontend" -xf -

printf '构建前端（npm registry：%s）\n' "${NPM_REGISTRY}"
docker run --rm \
  -v "${WORK_DIR}/frontend:/app" \
  -w /app \
  "${NODE_IMAGE}" \
  sh -c "npm ci --registry='${NPM_REGISTRY}' && npm run build"

for ARCH in amd64 arm64; do
  printf '构建 linux/%s 原生服务\n' "${ARCH}"
  mkdir -p "${WORK_DIR}/bin-${ARCH}"
  docker run --rm \
    -e GOOS=linux \
    -e GOARCH="${ARCH}" \
    -e CGO_ENABLED=0 \
    -e GOPROXY="${GOPROXY}" \
    -v "${ROOT_DIR}/code/backend:/src:ro" \
    -v "${WORK_DIR}/go-cache:/go" \
    -v "${WORK_DIR}/bin-${ARCH}:/out" \
    -w /src \
    "${GO_IMAGE}" \
    go build -trimpath -ldflags='-s -w' -o /out/flowpilotd ./cmd/flowpilotd

  BUNDLE_NAME="flowpilot-${VERSION}-linux-${ARCH}"
  BUNDLE="${WORK_DIR}/${BUNDLE_NAME}"
  mkdir -p "${BUNDLE}/payload" "${BUNDLE}/docs" "${BUNDLE}/licenses/MiniMax-H3-Image" "${BUNDLE}/licenses/MiniMax-H3"
  cp "${WORK_DIR}/bin-${ARCH}/flowpilotd" "${BUNDLE}/payload/flowpilotd"
  cp -a "${WORK_DIR}/frontend/dist" "${BUNDLE}/payload/web"
  cp "${ROOT_DIR}/code/scripts/flowpilot-native.sh" "${BUNDLE}/install.sh"
  cp "${ROOT_DIR}/VERSION" "${BUNDLE}/VERSION"
  cp "${ROOT_DIR}/LICENSE" "${ROOT_DIR}/NOTICE" "${ROOT_DIR}/THIRD_PARTY_NOTICES.md" "${BUNDLE}/"
  cp "${ROOT_DIR}/docs/deployment/README.zh-CN.md" "${BUNDLE}/docs/README.zh-CN.md"
  cp "${ROOT_DIR}/docs/deployment/README.en.md" "${BUNDLE}/docs/README.en.md"
  cp "${ROOT_DIR}/code/backend/internal/bundle/assets/licenses/minimax-h3-image/LICENSE" "${BUNDLE}/licenses/MiniMax-H3-Image/LICENSE"
  cp "${ROOT_DIR}/code/backend/internal/bundle/assets/licenses/minimax-h3-image/NOTICE" "${BUNDLE}/licenses/MiniMax-H3-Image/NOTICE"
  cp "${ROOT_DIR}/code/backend/internal/bundle/assets/licenses/minimax-h3/LICENSE" "${BUNDLE}/licenses/MiniMax-H3/LICENSE"
  cp "${ROOT_DIR}/code/backend/internal/bundle/assets/licenses/minimax-h3/NOTICE" "${BUNDLE}/licenses/MiniMax-H3/NOTICE"
  chmod 0755 "${BUNDLE}/install.sh" "${BUNDLE}/payload/flowpilotd"
  COPYFILE_DISABLE=1 tar -C "${WORK_DIR}" -czf "${OUTPUT_DIR}/${BUNDLE_NAME}.tar.gz" "${BUNDLE_NAME}"
  printf '发布包：%s\n' "${OUTPUT_DIR}/${BUNDLE_NAME}.tar.gz"
done

cp "${ROOT_DIR}/code/scripts/flowpilot-native.sh" "${OUTPUT_DIR}/flowpilot-native.sh"
chmod 0755 "${OUTPUT_DIR}/flowpilot-native.sh"
(
  cd "${OUTPUT_DIR}"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "flowpilot-${VERSION}-linux-amd64.tar.gz" "flowpilot-${VERSION}-linux-arm64.tar.gz" flowpilot-native.sh > SHA256SUMS
  else
    shasum -a 256 "flowpilot-${VERSION}-linux-amd64.tar.gz" "flowpilot-${VERSION}-linux-arm64.tar.gz" flowpilot-native.sh > SHA256SUMS
  fi
)
printf '校验文件：%s\n' "${OUTPUT_DIR}/SHA256SUMS"
