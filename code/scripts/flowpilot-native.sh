#!/usr/bin/env bash
# FlowPilot native installer for Linux physical servers.
# The management service runs natively under systemd; ComfyUI instances still use Docker.
set -Eeuo pipefail

VERSION="0.1.0.16"
REPO="ghoul02015/flowpilot"
PREFIX="/opt/flowpilot"
DATA_DIR="/var/lib/flowpilot"
LOG_DIR="/var/log/flowpilot"
CONFIG_DIR="/etc/flowpilot"
SERVICE_NAME="flowpilot.service"
PORT="18080"
REGION="auto"
ARCH=""
MIRROR=""
BUNDLE=""
OFFLINE_DIR=""
ACTION="install"
ASSUME_YES=0
NON_INTERACTIVE=0
BACKGROUND=0
RESUME=0
ALLOW_NO_GPU=0
INSTALL_DOCKER=0
PURGE=0
REMOVE_INSTANCES=0
RANDOM_PASSWORD=0
PASSWORD_FILE=""
WORKER=0
SCRIPT_PATH="$(cd "$(dirname "${BASH_SOURCE[0]}")" 2>/dev/null && pwd)/$(basename "${BASH_SOURCE[0]}")"
CONFIG_FILE="${CONFIG_DIR}/installer.conf"
STATE_DIR="${DATA_DIR}/.install"
LOG_FILE=""

usage() {
  cat <<'EOF'
FlowPilot 原生安装器 / Native installer

用法：
  sudo bash flowpilot-native.sh install [选项]
  sudo bash flowpilot-native.sh resume
  sudo bash flowpilot-native.sh status
  sudo bash flowpilot-native.sh uninstall [--purge] [--remove-instances] [--yes]

安装选项：
  --port <1024-65535>       Web 端口，默认 18080
  --prefix <绝对路径>        程序目录，默认 /opt/flowpilot
  --data-dir <绝对路径>      数据目录，默认 /var/lib/flowpilot
  --log-dir <绝对路径>       日志目录，默认 /var/log/flowpilot
  --region <auto|cn|global> 网络策略，默认 auto
  --mirror <URL 前缀>        自定义 GitHub 下载镜像，例如 https://ghfast.top/
  --repo <owner/repo>       Release 仓库，默认 ghoul02015/flowpilot
  --bundle <tar.gz>         使用本地原生发布包，不访问 GitHub
  --password-file <路径>     从 0600 文件读取 admin 初始密码
  --random-password         生成随机密码，保存到数据目录 initial-credentials
  --background              后台安装；终端断开后仍继续
  --non-interactive         不向终端提问；未提供密码时自动生成随机密码
  --install-docker          Docker 缺失时使用系统 apt 仓库自动安装 docker.io
  --allow-no-gpu            无可用 NVIDIA 驱动时允许继续（仅 CPU 管理）
  --yes                     跳过确认；不会跳过密码及安全检查
  --resume                  复用配置、已下载分片和已完成状态继续安装

卸载：
  默认仅删除程序、服务与下载缓存，保留业务数据和日志。
  --purge                 同时删除业务数据、模型、工作流、输出和日志。
  --remove-instances      同时停止并删除带 flowpilot.instance=true 标签的容器。
  --yes                   非交互确认。

环境变量 FLOWPILOT_GITHUB_MIRROR 可代替 --mirror；HTTPS_PROXY/HTTP_PROXY 受支持。
EOF
}

log() {
  local level="$1"
  shift
  printf '%s [%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S%z')" "${level}" "$*"
}

fail() {
  log "ERROR" "$*"
  return 1
}

confirm() {
  local prompt="$1"
  local answer=""
  if [ "${ASSUME_YES}" = "1" ] || [ "${NON_INTERACTIVE}" = "1" ]; then
    return 0
  fi
  read -r -p "${prompt} [y/N] " answer
  [[ "${answer}" =~ ^([yY][eE][sS]|[yY])$ ]]
}

load_config() {
  if [ -f "${CONFIG_FILE}" ]; then
    # 配置由本安装器以 root 权限写入，不含密码。
    # shellcheck disable=SC1090
    . "${CONFIG_FILE}"
  fi
}

valid_path() {
  [[ "$1" =~ ^/[A-Za-z0-9._/-]+$ ]] && [[ "$1" != *"/../"* ]] && [[ "$1" != "/" ]]
}

save_config() {
  mkdir -p "${CONFIG_DIR}"
  chmod 0755 "${CONFIG_DIR}"
  {
    printf 'PREFIX=%q\n' "${PREFIX}"
    printf 'DATA_DIR=%q\n' "${DATA_DIR}"
    printf 'LOG_DIR=%q\n' "${LOG_DIR}"
    printf 'CONFIG_DIR=%q\n' "${CONFIG_DIR}"
    printf 'PORT=%q\n' "${PORT}"
    printf 'REGION=%q\n' "${REGION}"
    printf 'REPO=%q\n' "${REPO}"
    printf 'MIRROR=%q\n' "${MIRROR}"
    printf 'ARCH=%q\n' "${ARCH}"
    printf 'BUNDLE=%q\n' "${BUNDLE}"
    printf 'OFFLINE_DIR=%q\n' "${OFFLINE_DIR}"
  } > "${CONFIG_FILE}.tmp"
  chmod 0640 "${CONFIG_FILE}.tmp"
  mv -f "${CONFIG_FILE}.tmp" "${CONFIG_FILE}"
}

mark_done() {
  mkdir -p "${STATE_DIR}"
  printf '%s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" > "${STATE_DIR}/$1.done"
}

is_done() {
  [ -f "${STATE_DIR}/$1.done" ]
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) fail "不支持的 CPU 架构：$(uname -m)；仅支持 linux/amd64 与 linux/arm64" ;;
  esac
}

parse_args() {
  if [ "$#" -gt 0 ]; then
    case "$1" in
      install|resume|status|uninstall|help|-h|--help)
        ACTION="$1"
        shift
        ;;
    esac
  fi
  [ "${ACTION}" = "resume" ] && { ACTION="install"; RESUME=1; }
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --port) PORT="${2:?--port 需要参数}"; shift 2 ;;
      --prefix) PREFIX="${2:?--prefix 需要参数}"; shift 2 ;;
      --data-dir) DATA_DIR="${2:?--data-dir 需要参数}"; shift 2 ;;
      --log-dir) LOG_DIR="${2:?--log-dir 需要参数}"; shift 2 ;;
      --region) REGION="${2:?--region 需要参数}"; shift 2 ;;
      --mirror) MIRROR="${2:?--mirror 需要参数}"; shift 2 ;;
      --repo) REPO="${2:?--repo 需要参数}"; shift 2 ;;
      --bundle) BUNDLE="${2:?--bundle 需要参数}"; shift 2 ;;
      --password-file) PASSWORD_FILE="${2:?--password-file 需要参数}"; shift 2 ;;
      --random-password) RANDOM_PASSWORD=1; shift ;;
      --background) BACKGROUND=1; shift ;;
      --non-interactive) NON_INTERACTIVE=1; shift ;;
      --install-docker) INSTALL_DOCKER=1; shift ;;
      --allow-no-gpu) ALLOW_NO_GPU=1; shift ;;
      --yes|-y) ASSUME_YES=1; shift ;;
      --resume) RESUME=1; shift ;;
      --purge) PURGE=1; shift ;;
      --remove-instances) REMOVE_INSTANCES=1; shift ;;
      --worker) WORKER=1; NON_INTERACTIVE=1; ASSUME_YES=1; shift ;;
      -h|--help|help) usage; exit 0 ;;
      *) fail "未知参数：$1；使用 --help 查看帮助" ;;
    esac
  done
  MIRROR="${MIRROR:-${FLOWPILOT_GITHUB_MIRROR:-}}"
  STATE_DIR="${DATA_DIR}/.install"
  CONFIG_FILE="${CONFIG_DIR}/installer.conf"
}

init_logging() {
  mkdir -p "${LOG_DIR}/install"
  chmod 0750 "${LOG_DIR}" "${LOG_DIR}/install"
  local stamp
  stamp="$(date '+%Y%m%d-%H%M%S')"
  LOG_FILE="${LOG_DIR}/install/${ACTION}-${stamp}-$$.log"
  touch "${LOG_FILE}"
  chmod 0640 "${LOG_FILE}"
  exec > >(tee -a "${LOG_FILE}") 2>&1
  ln -sfn "${LOG_FILE}" "${LOG_DIR}/install/latest.log"
  log "INFO" "日志文件：${LOG_FILE}"
  log "INFO" "操作：${ACTION}；目标版本：${VERSION}；PID：$$"
}

on_error() {
  local rc=$?
  trap - ERR
  log "ERROR" "安装中断（退出码 ${rc}）。已保留下载分片与状态；修正问题后执行：sudo bash '${SCRIPT_PATH}' resume"
  exit "${rc}"
}

on_signal() {
  local signal="$1"
  trap - ERR
  log "WARN" "收到 ${signal}，当前步骤停止。后台服务不受影响；重新运行 resume 可继续安装。"
  exit 130
}

acquire_lock() {
  mkdir -p /run
  # 后台 worker 继承前台的同一个打开文件描述符；不能重新打开锁文件，
  # 否则前台尚未退出时，worker 会与自己发起安装的上级进程冲突。
  if [ "${WORKER}" = "1" ] && { : >&9; } 2>/dev/null; then
    if command -v flock >/dev/null 2>&1; then
      flock -n 9 || fail "后台安装未能继承安装锁，请重新执行 resume"
    fi
    log "INFO" "已继承前台安装锁，后台继续持有防并发保护"
    return 0
  fi
  exec 9> /run/flowpilot-native-install.lock
  if command -v flock >/dev/null 2>&1; then
    flock -n 9 || fail "另一个安装/卸载进程正在运行，请稍后重试"
  elif ! mkdir /run/flowpilot-native-install.lock.d 2>/dev/null; then
    fail "检测到安装锁 /run/flowpilot-native-install.lock.d；确认无安装进程后删除该锁目录"
  fi
}

require_root() {
  [ "$(id -u)" = "0" ] || fail "需要 root 权限。请使用 sudo bash '${SCRIPT_PATH}' ${ACTION}"
  [ "$(uname -s)" = "Linux" ] || fail "原生安装器仅支持 Linux 物理机/虚拟机"
  command -v systemctl >/dev/null 2>&1 || fail "未找到 systemd（systemctl）"
  [ -r /etc/os-release ] || fail "无法识别操作系统：缺少 /etc/os-release"
  # 在子进程读取，避免 Ubuntu 的 VERSION 变量覆盖 FlowPilot 产品版本。
  local os_id="" os_pretty=""
  # shellcheck disable=SC1091
  os_id="$(. /etc/os-release; printf '%s' "${ID:-}")"
  # shellcheck disable=SC1091
  os_pretty="$(. /etc/os-release; printf '%s' "${PRETTY_NAME:-${ID:-unknown}}")"
  case "${os_id}" in
    ubuntu|debian) log "INFO" "操作系统：${os_pretty}" ;;
    *) fail "当前安装器支持 Ubuntu/Debian；检测到 ${os_pretty}" ;;
  esac
}

data_dir_in_protected_home() {
  case "${DATA_DIR}" in
    /home/*|/root|/root/*|/run/user/*) return 0 ;;
    *) return 1 ;;
  esac
}

ensure_tools() {
  local missing=() packages=()
  if ! command -v curl >/dev/null 2>&1; then
    missing+=(curl)
    packages+=(ca-certificates curl)
  fi
  if ! command -v tar >/dev/null 2>&1; then
    missing+=(tar)
    packages+=(tar)
  fi
  if ! command -v sha256sum >/dev/null 2>&1; then
    missing+=(coreutils)
    packages+=(coreutils)
  fi
  if data_dir_in_protected_home && ! command -v setfacl >/dev/null 2>&1; then
    missing+=(acl)
    packages+=(acl)
  fi
  if [ "${#missing[@]}" -gt 0 ]; then
    log "INFO" "安装基础工具：${missing[*]}"
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -o Acquire::Retries=3
    apt-get install -y --no-install-recommends "${packages[@]}"
  fi
}

probe_url() {
  curl -fsSIL --connect-timeout 5 --max-time 10 --retry 1 -o /dev/null "$1" >/dev/null 2>&1
}

network_check() {
  local http_state="未设置" https_state="未设置"
  [ -z "${HTTP_PROXY:-}${http_proxy:-}" ] || http_state="已设置"
  [ -z "${HTTPS_PROXY:-}${https_proxy:-}" ] || https_state="已设置"
  log "INFO" "网络策略：${REGION}；HTTP 代理：${http_state}；HTTPS 代理：${https_state}"
  local direct="https://github.com/${REPO}/releases"
  if probe_url "${direct}"; then
    log "INFO" "GitHub 官方地址可达：${direct}"
  else
    log "WARN" "GitHub 官方地址探测失败；下载阶段将按网络策略尝试镜像"
  fi
  if [ "${REGION}" = "cn" ] || [ "${REGION}" = "auto" ]; then
    local prefix=""
    for prefix in ${MIRROR:-} "https://ghfast.top/" "https://gh-proxy.com/" "https://ghproxy.net/"; do
      [ -n "${prefix}" ] || continue
      if probe_url "${prefix}${direct}"; then
        log "INFO" "GitHub 镜像可达：${prefix}"
        break
      fi
      log "WARN" "GitHub 镜像暂不可达：${prefix}"
    done
  fi
  if command -v getent >/dev/null 2>&1 && ! getent hosts github.com >/dev/null 2>&1; then
    log "WARN" "DNS 未能解析 github.com；请检查 DNS、代理或使用 --mirror"
  fi
}

check_port() {
  [[ "${PORT}" =~ ^[0-9]+$ ]] || fail "端口必须是数字"
  [ "${PORT}" -ge 1024 ] && [ "${PORT}" -le 65535 ] || fail "端口范围为 1024–65535"
  local listeners=""
  if command -v ss >/dev/null 2>&1; then
    listeners="$(ss -ltn 2>/dev/null || true)"
  elif command -v netstat >/dev/null 2>&1; then
    listeners="$(netstat -ltn 2>/dev/null || true)"
  fi
  if printf '%s\n' "${listeners}" | awk '{print $4}' | grep -Eq "(^|:)${PORT}$"; then
    if curl --noproxy '*' -fsS --max-time 3 "http://127.0.0.1:${PORT}/api/v1/health" 2>/dev/null | grep -q 'flowpilot\|"status"'; then
      log "INFO" "端口 ${PORT} 上检测到现有 FlowPilot，将按升级/修复模式处理"
    else
      fail "端口 ${PORT} 已被其他进程占用，请用 --port 指定其他端口"
    fi
  fi
}

check_gpu() {
  if command -v nvidia-smi >/dev/null 2>&1 && nvidia-smi >/dev/null 2>&1; then
    log "INFO" "NVIDIA 驱动检测通过：$(nvidia-smi --query-gpu=driver_version --format=csv,noheader 2>/dev/null | head -n 1)"
    nvidia-smi --query-gpu=name,memory.total,driver_version --format=csv,noheader 2>/dev/null || true
    if command -v docker >/dev/null 2>&1 && docker info --format '{{json .Runtimes}}' 2>/dev/null | grep -qi nvidia; then
      log "INFO" "NVIDIA Container Runtime 已注册"
    else
      log "WARN" "未检测到 NVIDIA Container Runtime；创建 GPU ComfyUI 实例前需配置 NVIDIA Container Toolkit"
    fi
    return 0
  fi
  if command -v lspci >/dev/null 2>&1 && lspci 2>/dev/null | grep -qi 'NVIDIA'; then
    log "WARN" "检测到 NVIDIA 硬件，但驱动不可用（nvidia-smi 缺失或执行失败）"
  else
    log "WARN" "未检测到可用 NVIDIA 驱动；平台可运行，但只能管理 CPU 实例"
  fi
  if [ "${ALLOW_NO_GPU}" != "1" ] && [ "${NON_INTERACTIVE}" != "1" ]; then
    confirm "仍要继续安装吗？" && return 0
    fail "用户取消；可先修复 NVIDIA 驱动，或使用 --allow-no-gpu"
  fi
  [ "${ALLOW_NO_GPU}" = "1" ] || fail "无可用 NVIDIA 驱动；非交互安装需显式传入 --allow-no-gpu"
}

ensure_docker() {
  if ! command -v docker >/dev/null 2>&1; then
    if [ "${INSTALL_DOCKER}" != "1" ] && [ "${NON_INTERACTIVE}" != "1" ]; then
      if confirm "未检测到 Docker。是否通过系统 apt 仓库安装 docker.io？"; then
        INSTALL_DOCKER=1
      fi
    fi
    [ "${INSTALL_DOCKER}" = "1" ] || fail "ComfyUI 实例管理需要 Docker Engine；请安装后重试，或使用 --install-docker"
    log "INFO" "通过系统软件源安装 Docker Engine（docker.io）"
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -o Acquire::Retries=3
    apt-get install -y --no-install-recommends docker.io
  fi
  systemctl enable --now docker >/dev/null 2>&1 || true
  docker info >/dev/null 2>&1 || fail "Docker Engine 不可用；请检查 docker 服务与 /var/run/docker.sock"
  [ -S /var/run/docker.sock ] || fail "Docker Socket 不存在：/var/run/docker.sock"
  log "INFO" "Docker：$(docker version --format '{{.Server.Version}}' 2>/dev/null || printf '已就绪')"
}

preflight() {
  log "INFO" "开始驱动、系统资源、网络与运行环境检查"
  valid_path "${PREFIX}" || fail "程序目录必须是仅含字母、数字、点、下划线、斜杠和连字符的绝对路径"
  valid_path "${DATA_DIR}" || fail "数据目录格式不安全：${DATA_DIR}"
  valid_path "${LOG_DIR}" || fail "日志目录格式不安全：${LOG_DIR}"
  [[ "${REGION}" =~ ^(auto|cn|global)$ ]] || fail "--region 仅支持 auto/cn/global"
  detect_arch
  check_port
  local available_kb=""
  available_kb="$(df -Pk "$(dirname "${DATA_DIR}")" 2>/dev/null | awk 'NR==2 {print $4}')"
  if [ -n "${available_kb}" ] && [ "${available_kb}" -lt 1048576 ]; then
    fail "数据目录所在磁盘剩余空间不足 1 GiB"
  fi
  if [ -n "${available_kb}" ] && [ "${available_kb}" -lt 20971520 ]; then
    log "WARN" "磁盘剩余不足 20 GiB；平台可安装，但 ComfyUI 镜像和模型可能空间不足"
  fi
  ensure_docker
  check_gpu
  network_check
  if command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -q '^Status: active'; then
    log "WARN" "UFW 已启用；远程访问前请放行端口：sudo ufw allow ${PORT}/tcp"
  fi
  mark_done preflight
}

prompt_wizard() {
  [ "${NON_INTERACTIVE}" = "1" ] && return 0
  printf '\n========== FlowPilot %s 原生安装向导 ==========\n' "${VERSION}"
  printf '管理平台：systemd 原生进程（非 Docker）\nComfyUI 实例：继续由 Docker 管理\n\n'
  local answer=""
  read -r -p "Web 端口 [${PORT}]：" answer
  [ -z "${answer}" ] || PORT="${answer}"
  read -r -p "程序目录 [${PREFIX}]：" answer
  [ -z "${answer}" ] || PREFIX="${answer}"
  read -r -p "数据目录 [${DATA_DIR}]：" answer
  [ -z "${answer}" ] || DATA_DIR="${answer}"
  read -r -p "网络策略 auto/cn/global [${REGION}]：" answer
  [ -z "${answer}" ] || REGION="${answer}"
  if [ "${BACKGROUND}" != "1" ]; then
    read -r -p "是否后台挂起安装（终端断开后仍继续）？[y/N] " answer
    [[ "${answer}" =~ ^([yY][eE][sS]|[yY])$ ]] && BACKGROUND=1
  fi
  STATE_DIR="${DATA_DIR}/.install"
}

password_policy_ok() {
  local password="$1"
  [ "${#password}" -ge 8 ] || return 1
  [[ "${password}" =~ [A-Za-z] ]] || return 1
  [[ "${password}" =~ [0-9] ]] || return 1
  ! LC_ALL=C printf '%s' "${password}" | grep -q '[[:cntrl:]]'
}

prepare_password() {
  mkdir -p "${STATE_DIR}"
  chmod 0700 "${STATE_DIR}"
  if curl --noproxy '*' -fsS --max-time 3 "http://127.0.0.1:${PORT}/api/v1/auth/setup/status" 2>/dev/null | grep -q '"initialized":true'; then
    log "INFO" "管理员已初始化，不修改现有密码"
    return 0
  fi
  if [ -n "${PASSWORD_FILE}" ]; then
    [ -f "${PASSWORD_FILE}" ] || fail "密码文件不存在：${PASSWORD_FILE}"
    [ "$(stat -c '%a' "${PASSWORD_FILE}" 2>/dev/null)" = "600" ] || fail "密码文件权限必须为 0600"
    cp "${PASSWORD_FILE}" "${STATE_DIR}/admin-password"
  elif [ -s "${STATE_DIR}/admin-password" ]; then
    log "INFO" "复用上次安全暂存的初始密码"
  elif [ "${RANDOM_PASSWORD}" = "1" ] || [ "${NON_INTERACTIVE}" = "1" ]; then
    local random=""
    random="$(od -An -N24 -tx1 /dev/urandom | tr -d ' \n')"
    printf 'Fp%s\n' "${random}" > "${STATE_DIR}/admin-password"
    touch "${STATE_DIR}/random-password"
  else
    local first="" second=""
    while true; do
      read -r -s -p "请设置 admin 密码（至少 8 位，含字母和数字）：" first
      printf '\n'
      read -r -s -p "请再次输入：" second
      printf '\n'
      if [ "${first}" != "${second}" ]; then
        log "WARN" "两次输入不一致，请重试"
        continue
      fi
      if ! password_policy_ok "${first}"; then
        log "WARN" "密码不符合策略：至少 8 位、含字母和数字，且不能包含控制字符"
        continue
      fi
      printf '%s\n' "${first}" > "${STATE_DIR}/admin-password"
      break
    done
  fi
  chmod 0600 "${STATE_DIR}/admin-password"
  local password=""
  password="$(head -n 1 "${STATE_DIR}/admin-password")"
  password_policy_ok "${password}" || fail "admin 初始密码不符合策略：至少 8 位且含字母和数字"
}

start_background() {
  [ "${BACKGROUND}" = "1" ] && [ "${WORKER}" != "1" ] || return 1
  mkdir -p "${STATE_DIR}"
  if [ -x "$(dirname "${SCRIPT_PATH}")/payload/flowpilotd" ]; then
    OFFLINE_DIR="$(cd "$(dirname "${SCRIPT_PATH}")" && pwd)"
  fi
  cp "${SCRIPT_PATH}" "${STATE_DIR}/installer.sh"
  chmod 0700 "${STATE_DIR}/installer.sh"
  save_config
  log "INFO" "切换到后台安装；终端断开不会影响任务"
  local worker_log=""
  worker_log="${LOG_DIR}/install/background-$(date '+%Y%m%d-%H%M%S').log"
  if command -v setsid >/dev/null 2>&1; then
    nohup setsid bash "${STATE_DIR}/installer.sh" install --worker --resume >> "${worker_log}" 2>&1 < /dev/null &
  else
    nohup bash "${STATE_DIR}/installer.sh" install --worker --resume >> "${worker_log}" 2>&1 < /dev/null &
  fi
  local pid=$!
  printf '%s\n' "${pid}" > "${STATE_DIR}/worker.pid"
  chmod 0600 "${STATE_DIR}/worker.pid"
  printf '\n后台安装已启动（PID：%s）\n' "${pid}"
  printf '查看进度：tail -f %q\n' "${worker_log}"
  printf '查看状态：sudo bash %q status\n' "${STATE_DIR}/installer.sh"
  printf '完成后访问：http://<服务器地址>:%s\n' "${PORT}"
  exit 0
}

mirror_candidates() {
  if [ -n "${MIRROR}" ]; then
    printf '%s\n' "${MIRROR%/}/"
  fi
  if [ "${REGION}" = "cn" ]; then
    printf '%s\n' "https://ghfast.top/" "https://gh-proxy.com/" "https://ghproxy.net/" ""
  elif [ "${REGION}" = "auto" ]; then
    printf '%s\n' "" "https://ghfast.top/" "https://gh-proxy.com/" "https://ghproxy.net/"
  else
    printf '%s\n' ""
  fi
}

fetch_file() {
  local url="$1" destination="$2" prefix="" attempt=0
  mkdir -p "$(dirname "${destination}")"
  while IFS= read -r prefix; do
    attempt=$((attempt + 1))
    [ -n "${prefix}" ] || [ "${REGION}" != "cn" ] || [ -n "${MIRROR}" ] || true
    log "INFO" "下载尝试 ${attempt}：${prefix}${url}"
    if curl -fL --show-error --connect-timeout 10 --max-time 1800 \
      --retry 2 --retry-delay 3 --speed-time 60 --speed-limit 1024 \
      -C - -o "${destination}.part" "${prefix}${url}"; then
      mv -f "${destination}.part" "${destination}"
      return 0
    fi
    log "WARN" "下载线路失败，保留分片并尝试下一线路：${prefix:-官方直连}"
  done < <(mirror_candidates | awk '!seen[$0]++')
  fail "所有下载线路均失败：${url}"
}

asset_name() {
  printf 'flowpilot-%s-linux-%s.tar.gz' "${VERSION}" "${ARCH}"
}

verify_archive() {
  local archive="$1" sums="$2" name="$3" expected="" actual=""
  expected="$(awk -v file="${name}" '$2 == file {print $1}' "${sums}" | head -n 1)"
  [ -n "${expected}" ] || fail "SHA256SUMS 中找不到 ${name}"
  actual="$(sha256sum "${archive}" | awk '{print $1}')"
  [ "${actual}" = "${expected}" ] || fail "发布包 SHA256 校验失败：${name}"
  log "INFO" "SHA256 校验通过：${name}"
}

safe_extract() {
  local archive="$1" destination="$2"
  if tar -tzf "${archive}" | awk 'BEGIN{bad=0} /^\// || /(^|\/)\.\.($|\/)/ {bad=1} END{exit bad?0:1}'; then
    fail "发布包包含绝对路径或 .. 路径，已拒绝解压"
  fi
  rm -rf "${destination}"
  mkdir -p "${destination}"
  tar -xzf "${archive}" --strip-components=1 -C "${destination}"
  [ -x "${destination}/payload/flowpilotd" ] || fail "发布包缺少可执行文件 payload/flowpilotd"
  [ -f "${destination}/payload/web/index.html" ] || fail "发布包缺少前端 payload/web/index.html"
}

prepare_payload() {
  local source_dir="${OFFLINE_DIR:-$(dirname "${SCRIPT_PATH}")}"
  if [ -x "${source_dir}/payload/flowpilotd" ] && [ -f "${source_dir}/payload/web/index.html" ]; then
    PAYLOAD_DIR="$(cd "${source_dir}" && pwd)"
    log "INFO" "使用本地离线发布包：${PAYLOAD_DIR}"
  else
    local name archive sums url
    name="$(asset_name)"
    archive="${STATE_DIR}/${name}"
    sums="${STATE_DIR}/SHA256SUMS"
    url="https://github.com/${REPO}/releases/download/v${VERSION}/${name}"
    if [ -n "${BUNDLE}" ]; then
      [ -f "${BUNDLE}" ] || fail "本地发布包不存在：${BUNDLE}"
      cp "${BUNDLE}" "${archive}.part"
      mv -f "${archive}.part" "${archive}"
    elif [ ! -f "${archive}" ]; then
      fetch_file "https://github.com/${REPO}/releases/download/v${VERSION}/SHA256SUMS" "${sums}"
      fetch_file "${url}" "${archive}"
    elif [ ! -f "${sums}" ]; then
      fetch_file "https://github.com/${REPO}/releases/download/v${VERSION}/SHA256SUMS" "${sums}"
    else
      log "INFO" "复用已下载发布包：${archive}"
    fi
    if [ -f "${sums}" ]; then
      verify_archive "${archive}" "${sums}" "${name}"
    elif [ -z "${BUNDLE}" ]; then
      fail "缺少 SHA256SUMS，拒绝安装未校验的在线发布包"
    fi
    PAYLOAD_DIR="${STATE_DIR}/bundle"
    safe_extract "${archive}" "${PAYLOAD_DIR}"
  fi
  mark_done payload
}

ensure_user() {
  if ! id flowpilot >/dev/null 2>&1; then
    useradd --system --home-dir "${DATA_DIR}" --shell /usr/sbin/nologin flowpilot
    touch "${CONFIG_DIR}/created-user"
    log "INFO" "已创建系统用户 flowpilot"
  fi
  local socket_group=""
  socket_group="$(stat -c '%G' /var/run/docker.sock 2>/dev/null || true)"
  if [ -n "${socket_group}" ] && getent group "${socket_group}" >/dev/null 2>&1; then
    usermod -a -G "${socket_group}" flowpilot
  fi
}

backup_before_upgrade() {
  [ -L "${PREFIX}/current" ] || return 0
  local previous=""
  previous="$(readlink -f "${PREFIX}/current" 2>/dev/null || true)"
  [ -n "${previous}" ] && [ "${previous}" != "${PREFIX}/releases/${VERSION}" ] || return 0
  PREVIOUS_RELEASE="${previous}"
  log "INFO" "检测到旧版本：${PREVIOUS_RELEASE}；升级前停止服务并备份 SQLite"
  systemctl stop "${SERVICE_NAME}" >/dev/null 2>&1 || true
  if [ -f "${DATA_DIR}/flowpilot.db" ]; then
    local stamp
    stamp="$(date '+%Y%m%d-%H%M%S')"
    mkdir -p "${DATA_DIR}/backups"
    cp -a "${DATA_DIR}/flowpilot.db" "${DATA_DIR}/backups/pre-upgrade-${stamp}.db"
    for suffix in wal shm; do
      [ ! -f "${DATA_DIR}/flowpilot.db-${suffix}" ] || cp -a "${DATA_DIR}/flowpilot.db-${suffix}" "${DATA_DIR}/backups/pre-upgrade-${stamp}.db-${suffix}"
    done
    log "INFO" "SQLite 备份：${DATA_DIR}/backups/pre-upgrade-${stamp}.db"
  fi
}

configure_data_dir_access() {
  data_dir_in_protected_home || return 0
  command -v setfacl >/dev/null 2>&1 || fail "Home 下自定义数据目录需要 setfacl；请安装 acl 软件包后重试"
  command -v getfacl >/dev/null 2>&1 || fail "Home 下自定义数据目录需要 getfacl；请安装 acl 软件包后重试"
  local state="${CONFIG_DIR}/data-dir-access-parents"
  touch "${state}"
  chmod 0600 "${state}"
  local parent="" original="" original_mask="" original_acl=""
  parent="$(dirname "${DATA_DIR}")"
  while [ "${parent}" != "/" ]; do
    [ -d "${parent}" ] || fail "数据目录父路径不存在：${parent}"
    if ! awk -F '\t' -v path="${parent}" '$1 == path { found=1 } END { exit !found }' "${state}"; then
      original_acl="$(getfacl -cp "${parent}")"
      original="$(printf '%s\n' "${original_acl}" | awk -F: '$1 == "user" && $2 == "flowpilot" { print $3; exit }')"
      original_mask="$(printf '%s\n' "${original_acl}" | awk -F: '$1 == "mask" { print $3; exit }')"
      printf '%s\t%s\t%s\n' "${parent}" "${original:--}" "${original_mask:--}" >> "${state}"
    fi
    # 仅允许进入目录，不允许列出或读取父目录中的其他内容。
    setfacl -m u:flowpilot:--x "${parent}"
    parent="$(dirname "${parent}")"
  done
  log "INFO" "已为 Home 下数据目录配置 flowpilot 父目录进入权限（仅 ACL --x）"
}

install_files() {
  ensure_user
  backup_before_upgrade
  local release="${PREFIX}/releases/${VERSION}" temp="${PREFIX}/releases/.${VERSION}.tmp.$$"
  mkdir -p "${PREFIX}/releases" "${PREFIX}/bin" "${DATA_DIR}" "${LOG_DIR}"
  touch "${PREFIX}/.flowpilot-install-root" "${DATA_DIR}/.flowpilot-data-root" "${LOG_DIR}/.flowpilot-log-root"
  rm -rf "${temp}"
  mkdir -p "${temp}"
  cp -a "${PAYLOAD_DIR}/payload/flowpilotd" "${temp}/flowpilotd"
  cp -a "${PAYLOAD_DIR}/payload/web" "${temp}/web"
  chmod 0755 "${temp}/flowpilotd"
  if [ -f "${PAYLOAD_DIR}/VERSION" ]; then
    cp -a "${PAYLOAD_DIR}/VERSION" "${temp}/VERSION"
  else
    printf '%s\n' "${VERSION}" > "${temp}/VERSION"
  fi
  if [ -d "${PAYLOAD_DIR}/docs" ]; then
    cp -a "${PAYLOAD_DIR}/docs" "${temp}/docs"
  fi
  for file in LICENSE NOTICE THIRD_PARTY_NOTICES.md; do
    [ ! -f "${PAYLOAD_DIR}/${file}" ] || cp -a "${PAYLOAD_DIR}/${file}" "${temp}/${file}"
  done
  if [ -d "${release}" ]; then
    rm -rf "${release}.old"
    mv "${release}" "${release}.old"
  fi
  mv "${temp}" "${release}"
  rm -rf "${release}.old"
  ln -sfn "${release}" "${PREFIX}/current"
  # 通过 /usr/local/bin/flowpilot-native 复装时，SCRIPT_PATH 经软链接
  # 指向目标自身；同文件直接复用，避免 cp 报“are the same file”。
  if ! [ "${SCRIPT_PATH}" -ef "${PREFIX}/bin/flowpilot-native" ]; then
    cp "${SCRIPT_PATH}" "${PREFIX}/bin/flowpilot-native"
  fi
  chmod 0755 "${PREFIX}/bin/flowpilot-native"
  ln -sfn "${PREFIX}/bin/flowpilot-native" /usr/local/bin/flowpilot-native
  mkdir -p "${DATA_DIR}/models" "${DATA_DIR}/instances" "${DATA_DIR}/outputs" \
    "${DATA_DIR}/workflows" "${DATA_DIR}/import" "${DATA_DIR}/backups" \
    "${DATA_DIR}/cache" "${DATA_DIR}/network" "${DATA_DIR}/network-extra" \
    "${DATA_DIR}/instance-links"
  chown -R flowpilot:flowpilot "${DATA_DIR}"
  chown -R root:root "${STATE_DIR}"
  chmod 0750 "${DATA_DIR}"
  chmod 0700 "${STATE_DIR}"
  configure_data_dir_access
  mark_done files
  log "INFO" "程序目录：${PREFIX}/current"
  log "INFO" "数据目录：${DATA_DIR}"
}

write_service() {
  local comfy_image="" comfy_workdir="" candidate="" protect_home="true"
  local models_target="/data/models" outputs_target="/data/outputs" workflows_target="/data/workflows"
  if data_dir_in_protected_home; then
    # ReadWritePaths 仍只开放数据目录；read-only 仅让该路径穿过 Home 隔离。
    protect_home="read-only"
  fi
  while IFS= read -r candidate; do
    [ -n "${candidate}" ] && [ "${candidate}" != "<none>:<none>" ] || continue
    if [ "$(docker image inspect "${candidate}" --format '{{ index .Config.Labels "flowpilot.kind" }}' 2>/dev/null || true)" = "comfyui" ]; then
      comfy_image="${candidate}"
      break
    fi
  done < <(docker images --format '{{.Repository}}:{{.Tag}}' 2>/dev/null || true)
  if [ -n "${comfy_image}" ]; then
    comfy_workdir="$(docker image inspect "${comfy_image}" --format '{{.Config.WorkingDir}}' 2>/dev/null || true)"
    if [ -n "${comfy_workdir}" ] && [ "${comfy_workdir}" != "/" ]; then
      models_target="${comfy_workdir}/models"
      outputs_target="${comfy_workdir}/output"
      workflows_target="${comfy_workdir}/user/workflows"
    fi
    log "INFO" "默认可信 ComfyUI 镜像：${comfy_image}"
  else
    log "WARN" "未找到带 flowpilot.kind=comfyui 标签的本地镜像；安装后请在镜像中心构建或拉取"
  fi
  cat > "${CONFIG_DIR}/flowpilot.env" <<EOF
FLOWPILOT_ADDR=:${PORT}
FLOWPILOT_STATIC_DIR=${PREFIX}/current/web
FLOWPILOT_DATA_DIR=${DATA_DIR}
FLOWPILOT_HOST_DATA_DIR=${DATA_DIR}
FLOWPILOT_HOST_ROOT=/
FLOWPILOT_PROC_ROOT=/proc
FLOWPILOT_DOCKER_SOCKET=/var/run/docker.sock
FLOWPILOT_HOST_GATEWAY=127.0.0.1
FLOWPILOT_IMPORT_DIRS=${DATA_DIR}/import
FLOWPILOT_COMFYUI_IMAGE=${comfy_image:-comfyui:latest}
FLOWPILOT_COMFYUI_CONTAINER_PORT=8188
FLOWPILOT_COMFYUI_MODELS_TARGET=${models_target}
FLOWPILOT_COMFYUI_OUTPUTS_TARGET=${outputs_target}
FLOWPILOT_COMFYUI_WORKFLOWS_TARGET=${workflows_target}
EOF
  chown root:flowpilot "${CONFIG_DIR}/flowpilot.env" "${CONFIG_FILE}"
  chmod 0640 "${CONFIG_DIR}/flowpilot.env" "${CONFIG_FILE}"
  cat > "/etc/systemd/system/${SERVICE_NAME}" <<EOF
[Unit]
Description=FlowPilot ComfyUI Management Platform
Documentation=https://github.com/${REPO}
After=network-online.target docker.service
Wants=network-online.target
Requires=docker.service

[Service]
Type=simple
User=flowpilot
Group=flowpilot
EnvironmentFile=${CONFIG_DIR}/flowpilot.env
ExecStart=${PREFIX}/current/flowpilotd
Restart=on-failure
RestartSec=5s
TimeoutStopSec=20s
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=full
ProtectHome=${protect_home}
ReadWritePaths=${DATA_DIR} ${LOG_DIR}
StandardOutput=append:${LOG_DIR}/service.log
StandardError=append:${LOG_DIR}/service.log

[Install]
WantedBy=multi-user.target
EOF
  chmod 0644 "/etc/systemd/system/${SERVICE_NAME}"
  touch "${LOG_DIR}/service.log"
  chown flowpilot:flowpilot "${LOG_DIR}/service.log"
  chmod 0640 "${LOG_DIR}/service.log"
  systemctl daemon-reload
  systemctl enable "${SERVICE_NAME}" >/dev/null
  systemctl restart "${SERVICE_NAME}"
  mark_done service
}

wait_healthy() {
  local attempt=0
  for attempt in $(seq 1 60); do
    if curl --noproxy '*' -fsS --max-time 3 "http://127.0.0.1:${PORT}/api/v1/health" 2>/dev/null | grep -q "\"version\":\"${VERSION}\""; then
      log "INFO" "健康检查通过：FlowPilot ${VERSION}"
      return 0
    fi
    sleep 2
  done
  journalctl -u "${SERVICE_NAME}" -n 80 --no-pager 2>/dev/null || true
  if [ -n "${PREVIOUS_RELEASE:-}" ]; then
    log "ERROR" "新版本健康检查失败，回滚到 ${PREVIOUS_RELEASE}"
    ln -sfn "${PREVIOUS_RELEASE}" "${PREFIX}/current"
    systemctl restart "${SERVICE_NAME}" || true
  fi
  fail "服务未通过健康检查；日志：${LOG_DIR}/service.log"
}

json_escape() {
  local value="$1"
  value="${value//\\/\\\\}"
  value="${value//\"/\\\"}"
  printf '%s' "${value}"
}

initialize_admin() {
  local status=""
  status="$(curl --noproxy '*' -fsS --max-time 5 "http://127.0.0.1:${PORT}/api/v1/auth/setup/status")"
  if printf '%s' "${status}" | grep -q '"initialized":true'; then
    rm -f "${STATE_DIR}/admin-password"
    log "INFO" "管理员已经初始化，保留原账号密码"
    mark_done auth
    return 0
  fi
  [ -s "${DATA_DIR}/init-token" ] || fail "未生成初始化令牌：${DATA_DIR}/init-token"
  [ -s "${STATE_DIR}/admin-password" ] || fail "缺少初始密码文件，无法初始化管理员"
  local token="" password="" payload="" response=""
  token="$(head -n 1 "${DATA_DIR}/init-token")"
  password="$(head -n 1 "${STATE_DIR}/admin-password")"
  payload="${STATE_DIR}/setup.json"
  printf '{"token":"%s","password":"%s"}\n' "$(json_escape "${token}")" "$(json_escape "${password}")" > "${payload}"
  chmod 0600 "${payload}"
  response="$(curl --noproxy '*' -fsS --max-time 20 \
    -H 'Content-Type: application/json' \
    --data-binary "@${payload}" \
    "http://127.0.0.1:${PORT}/api/v1/auth/setup")"
  printf '%s' "${response}" | grep -q '"initialized":true' || fail "管理员初始化接口未返回成功"
  rm -f "${payload}"
  if [ -f "${STATE_DIR}/random-password" ]; then
    umask 077
    {
      printf 'username=admin\n'
      printf 'password=%s\n' "${password}"
      printf 'created_at=%s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
    } > "${DATA_DIR}/initial-credentials"
    chmod 0600 "${DATA_DIR}/initial-credentials"
    log "INFO" "随机初始密码已保存（仅 root 可读）：${DATA_DIR}/initial-credentials"
  fi
  rm -f "${STATE_DIR}/admin-password"
  mark_done auth
  log "INFO" "管理员初始化完成：admin（密码未写入日志）"
}

print_summary() {
  local primary_ip=""
  primary_ip="$(hostname -I 2>/dev/null | awk '{print $1}')"
  cat <<EOF

============================================================
FlowPilot ${VERSION} 原生安装完成
访问地址：http://${primary_ip:-<服务器地址>}:${PORT}
本机地址：http://127.0.0.1:${PORT}
登录账号：admin
登录密码：$([ -f "${DATA_DIR}/initial-credentials" ] && printf '见 %s（0600）' "${DATA_DIR}/initial-credentials" || printf '安装时设置的密码')
程序目录：${PREFIX}/current
数据目录：${DATA_DIR}
服务日志：${LOG_DIR}/service.log
安装日志：${LOG_FILE}
服务管理：systemctl status|restart|stop ${SERVICE_NAME}
卸载命令：sudo flowpilot-native uninstall
彻底清理：sudo flowpilot-native uninstall --purge
============================================================
EOF
}

install_main() {
  require_root
  init_logging
  [ "${RESUME}" != "1" ] || log "INFO" "按断点续装模式恢复配置、分片和阶段状态"
  acquire_lock
  trap on_error ERR
  trap 'on_signal HUP' HUP
  trap 'on_signal INT' INT
  trap 'on_signal TERM' TERM
  prompt_wizard
  ensure_tools
  save_config
  if ! is_done preflight; then
    preflight
  else
    log "INFO" "复用预检状态，并重新检查端口、Docker 与网络"
    detect_arch
    check_port
    ensure_docker
    network_check
  fi
  prepare_password
  if start_background; then
    :
  fi
  prepare_payload
  install_files
  write_service
  wait_healthy
  initialize_admin
  rm -f "${STATE_DIR}/worker.pid"
  # complete.done 表示调用方已经可以安全地发起下一次安装或卸载。
  # worker 的 tee 也继承锁描述符，因此显式解锁，不能只等待进程退出。
  if command -v flock >/dev/null 2>&1; then
    flock -u 9
  fi
  exec 9>&-
  rmdir /run/flowpilot-native-install.lock.d 2>/dev/null || true
  mark_done complete
  print_summary
}

status_main() {
  require_root
  init_logging
  printf 'FlowPilot 原生部署状态\n'
  printf '程序目录：%s\n' "${PREFIX}/current"
  printf '数据目录：%s\n' "${DATA_DIR}"
  printf '日志目录：%s\n' "${LOG_DIR}"
  printf '访问端口：%s\n' "${PORT}"
  systemctl status "${SERVICE_NAME}" --no-pager || true
  if [ -s "${STATE_DIR}/worker.pid" ]; then
    local pid=""
    pid="$(cat "${STATE_DIR}/worker.pid")"
    if kill -0 "${pid}" 2>/dev/null; then
      printf '后台安装：运行中（PID %s）\n' "${pid}"
    else
      printf '后台安装：未运行；可执行 sudo flowpilot-native resume\n'
    fi
  fi
}

remove_instances() {
  command -v docker >/dev/null 2>&1 || return 0
  local names=""
  names="$(docker ps -aq --filter 'label=flowpilot.instance=true' 2>/dev/null || true)"
  if [ -n "${names}" ]; then
    # 仅处理产品明确标签管理的容器，不触碰其他业务容器。
    # shellcheck disable=SC2086
    docker rm -f ${names}
  fi
}

owned_tree() {
  local path="$1" marker="$2" default_path="$3"
  valid_path "${path}" || return 1
  [ "${path}" = "${default_path}" ] || [ -f "${path}/${marker}" ]
}

restore_data_dir_access() {
  local state="${CONFIG_DIR}/data-dir-access-parents"
  [ -f "${state}" ] || return 0
  command -v setfacl >/dev/null 2>&1 || fail "恢复数据目录父路径 ACL 需要 setfacl；请安装 acl 软件包后重试卸载"
  local parent="" original="" original_mask=""
  while IFS=$'\t' read -r parent original original_mask; do
    [ -n "${parent}" ] || continue
    valid_path "${parent}" || fail "ACL 恢复记录包含不安全路径：${parent}"
    case "${parent}" in
      /home|/home/*|/root|/run|/run/user|/run/user/*) ;;
      *) fail "ACL 恢复记录超出允许范围：${parent}" ;;
    esac
    [ -d "${parent}" ] || continue
    if [ "${original:--}" = "-" ]; then
      setfacl -x u:flowpilot "${parent}"
    else
      [[ "${original}" =~ ^[rwx-]{3}$ ]] || fail "ACL 恢复记录格式无效：${parent}"
      setfacl -m "u:flowpilot:${original}" "${parent}"
    fi
    if [ "${original_mask:--}" = "-" ]; then
      setfacl -x mask "${parent}"
    else
      [[ "${original_mask}" =~ ^[rwx-]{3}$ ]] || fail "ACL mask 恢复记录格式无效：${parent}"
      setfacl -m "mask:${original_mask}" "${parent}"
    fi
  done < "${state}"
  rm -f "${state}"
  log "INFO" "已恢复数据目录父路径原始 ACL"
}

uninstall_main() {
  require_root
  init_logging
  acquire_lock
  trap on_error ERR
  owned_tree "${PREFIX}" ".flowpilot-install-root" "/opt/flowpilot" || fail "拒绝删除未标记的 FlowPilot 程序目录：${PREFIX}"
  if [ "${PURGE}" = "1" ]; then
    owned_tree "${DATA_DIR}" ".flowpilot-data-root" "/var/lib/flowpilot" || fail "--purge 拒绝删除未标记的数据目录：${DATA_DIR}"
    owned_tree "${LOG_DIR}" ".flowpilot-log-root" "/var/log/flowpilot" || fail "--purge 拒绝删除未标记的日志目录：${LOG_DIR}"
  fi
  if [ "${ASSUME_YES}" != "1" ]; then
    local answer=""
    if [ "${PURGE}" = "1" ]; then
      printf '警告：将永久删除 %s（含模型、工作流、输出、数据库）及 %s。\n' "${DATA_DIR}" "${LOG_DIR}"
    else
      printf '将删除原生程序、systemd 服务和下载缓存，保留 %s 与日志。\n' "${DATA_DIR}"
    fi
    [ "${REMOVE_INSTANCES}" = "1" ] && printf '同时删除所有带 flowpilot.instance=true 标签的 ComfyUI 容器。\n'
    read -r -p "输入 yes 继续：" answer
    [ "${answer}" = "yes" ] || { log "INFO" "已取消卸载"; exit 0; }
  fi
  systemctl disable --now "${SERVICE_NAME}" >/dev/null 2>&1 || true
  rm -f "/etc/systemd/system/${SERVICE_NAME}"
  systemctl daemon-reload
  systemctl reset-failed "${SERVICE_NAME}" >/dev/null 2>&1 || true
  restore_data_dir_access
  [ "${REMOVE_INSTANCES}" != "1" ] || remove_instances
  rm -f /usr/local/bin/flowpilot-native
  rm -rf -- "${PREFIX}"
  rm -rf /var/cache/flowpilot
  if [ "${PURGE}" = "1" ]; then
    rm -rf -- "${DATA_DIR}"
    if [ -f "${CONFIG_DIR}/created-user" ] && id flowpilot >/dev/null 2>&1; then
      local user_removed=0 attempt=0
      # systemctl 停止与服务账户进程完全退出之间可能存在短暂竞态。
      # 不再隐藏 userdel 错误；重试后仍失败必须让卸载明确失败。
      for attempt in 1 2 3 4 5; do
        if userdel flowpilot; then
          user_removed=1
          break
        fi
        log "WARN" "删除 flowpilot 系统用户失败（${attempt}/5），等待进程退出后重试"
        sleep 1
      done
      [ "${user_removed}" = "1" ] || fail "flowpilot 系统用户删除失败；配置目录保留，请检查残留进程并手动完成用户清理"
      log "INFO" "已删除安装器创建的 flowpilot 系统用户"
    fi
    rm -rf "${CONFIG_DIR}"
    log "INFO" "已彻底删除程序、配置、业务数据与日志；Docker、NVIDIA 驱动未修改"
    rm -rf -- "${LOG_DIR}"
  else
    rm -f "${CONFIG_DIR}/flowpilot.env"
    log "INFO" "卸载完成；业务数据保留在 ${DATA_DIR}，日志保留在 ${LOG_DIR}"
    log "INFO" "Docker、NVIDIA 驱动、ComfyUI 容器与镜像均未修改"
  fi
}

load_config
parse_args "$@"
case "${ACTION}" in
  install) install_main ;;
  status) status_main ;;
  uninstall) uninstall_main ;;
  help|-h|--help) usage ;;
  *) usage; exit 1 ;;
esac
