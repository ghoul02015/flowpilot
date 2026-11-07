#!/usr/bin/env bash
set -Eeuo pipefail

VERSION="0.0.0.5"
IMAGE="${FLOWPILOT_IMAGE:-flowpilot-web:${VERSION}}"
CONTAINER_NAME="${FLOWPILOT_CONTAINER:-flowpilot-web-demo}"
INSTALL_HOME="${FLOWPILOT_HOME:-${XDG_DATA_HOME:-${HOME}/.local/share}/flowpilot}"

if [[ "${1:-}" != "--yes" ]]; then
  if [ -t 0 ]; then
    printf '将删除 FlowPilot 容器、镜像和用户安装目录：%s\n' "${INSTALL_HOME}"
    printf '输入 yes 继续：'
    read -r answer
    [ "${answer}" = "yes" ] || { printf '已取消。\n'; exit 0; }
  else
    printf '非交互环境必须传入 --yes。\n' >&2
    exit 1
  fi
fi

if command -v docker >/dev/null 2>&1; then
  if docker ps -a --format '{{.Names}}' | grep -Fxq "${CONTAINER_NAME}"; then
    docker rm -f "${CONTAINER_NAME}" >/dev/null
    printf '已删除容器：%s\n' "${CONTAINER_NAME}"
  fi
  if docker image inspect "${IMAGE}" >/dev/null 2>&1; then
    if docker ps -a --filter 'label=flowpilot.managed=true' --format '{{.Names}}' | grep -q .; then
      printf '镜像仍被其他 FlowPilot 容器使用，已保留：%s\n' "${IMAGE}"
    else
      docker image rm "${IMAGE}" >/dev/null
      printf '已删除镜像：%s\n' "${IMAGE}"
    fi
  fi
fi

case "${INSTALL_HOME}" in
  "${HOME}"/.local/share/flowpilot|"${HOME}"/.local/share/flowpilot-*)
    rm -rf "${INSTALL_HOME}"
    printf '已删除安装目录：%s\n' "${INSTALL_HOME}"
    ;;
  *)
    printf '安装目录不在用户数据目录内，已跳过删除：%s\n' "${INSTALL_HOME}" >&2
    exit 1
    ;;
esac

printf 'FlowPilot 已卸载；未修改系统软件包、Docker 网络或其他业务容器。\n'
