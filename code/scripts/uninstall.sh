#!/usr/bin/env bash
set -Eeuo pipefail

VERSION="0.0.4.1"
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
  if docker ps -a --filter 'label=flowpilot.managed=true' --format '{{.Names}}' | grep -q .; then
    printf '仍有其他 FlowPilot 容器，已保留全部 flowpilot-web 镜像：%s\n' "${IMAGE}"
  else
    # 没有遗留 FlowPilot 容器时，清理本产品的全部版本镜像，
    # 避免历次升级的旧版本在客户机器上持续累积。
    for image in $(docker images --format '{{.Repository}}:{{.Tag}}' | grep '^flowpilot-web:' || true); do
      docker image rm "${image}" >/dev/null 2>&1 && printf '已删除镜像：%s\n' "${image}"
    done
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
