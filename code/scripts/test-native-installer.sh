#!/usr/bin/env bash
# Integration test for flowpilot-native.sh. Run as root inside a disposable Ubuntu container.
set -Eeuo pipefail

BUNDLE="${1:?usage: test-native-installer.sh /path/flowpilot-VERSION-linux-ARCH.tar.gz}"
WORK="/tmp/flowpilot-native-test"
FLOWPILOT_USER_PREEXISTED=0
if id flowpilot >/dev/null 2>&1; then
  FLOWPILOT_USER_PREEXISTED=1
fi
rm -rf "${WORK}"
mkdir -p "${WORK}/bin" "${WORK}/bundle"

# Minimal systemctl shim: starts the real native binary as the flowpilot user.
cat > "${WORK}/bin/systemctl" <<'EOF'
#!/usr/bin/env bash
set -u
PID_FILE=/run/flowpilot-test.pid
start_service() {
  set -a
  . /etc/flowpilot/flowpilot.env
  set +a
  export FLOWPILOT_ADDR FLOWPILOT_STATIC_DIR FLOWPILOT_DATA_DIR FLOWPILOT_HOST_DATA_DIR
  export FLOWPILOT_HOST_ROOT FLOWPILOT_PROC_ROOT FLOWPILOT_DOCKER_SOCKET FLOWPILOT_HOST_GATEWAY
  export FLOWPILOT_IMPORT_DIRS FLOWPILOT_COMFYUI_IMAGE FLOWPILOT_COMFYUI_CONTAINER_PORT
  export FLOWPILOT_COMFYUI_MODELS_TARGET FLOWPILOT_COMFYUI_OUTPUTS_TARGET FLOWPILOT_COMFYUI_WORKFLOWS_TARGET
  su -s /bin/bash flowpilot -c 'exec /opt/flowpilot/current/flowpilotd >>/var/log/flowpilot/service.log 2>&1' &
  echo $! > "${PID_FILE}"
}
stop_service() {
  if [ -f "${PID_FILE}" ]; then
    kill "$(cat "${PID_FILE}")" >/dev/null 2>&1 || true
    sleep 1
    rm -f "${PID_FILE}"
  fi
  pkill -f '^/opt/flowpilot/current/flowpilotd$' >/dev/null 2>&1 || true
}
case "${1:-}" in
  daemon-reload|reset-failed) exit 0 ;;
  enable)
    [ "${2:-}" != "--now" ] || exit 0
    exit 0
    ;;
  disable) stop_service; exit 0 ;;
  stop) stop_service; exit 0 ;;
  restart|start) stop_service; start_service; exit 0 ;;
  status)
    [ -f "${PID_FILE}" ] && kill -0 "$(cat "${PID_FILE}")" 2>/dev/null
    exit $?
    ;;
  *) exit 0 ;;
esac
EOF
cat > "${WORK}/bin/docker" <<'EOF'
#!/usr/bin/env bash
case "${1:-}" in
  info) exit 0 ;;
  version) printf '28.0.0-test\n'; exit 0 ;;
  images) exit 0 ;;
  ps) exit 0 ;;
  *) exit 0 ;;
esac
EOF
cat > "${WORK}/bin/journalctl" <<'EOF'
#!/usr/bin/env bash
tail -n 80 /var/log/flowpilot/service.log 2>/dev/null || true
EOF
chmod +x "${WORK}/bin/systemctl" "${WORK}/bin/docker" "${WORK}/bin/journalctl"
export PATH="${WORK}/bin:${PATH}"

# A real socket is mounted by the caller; only read-only Docker API discovery may occur.
[ -S /var/run/docker.sock ] || { printf 'test requires /var/run/docker.sock\n' >&2; exit 1; }
tar -xzf "${BUNDLE}" -C "${WORK}/bundle" --strip-components=1
cp "${WORK}/bundle/install.sh" "${WORK}/installer.sh"
chmod +x "${WORK}/installer.sh"
EXPECTED_VERSION="$(tr -d '[:space:]' < "${WORK}/bundle/VERSION")"

printf '\n前置检查：Docker 缺失时只提示，不安装、不启动\n'
mv "${WORK}/bin/docker" "${WORK}/bin/docker.available"
if "${WORK}/installer.sh" install --non-interactive --random-password --allow-no-gpu \
  --region global --bundle "${BUNDLE}" > "${WORK}/docker-missing.log" 2>&1; then
  printf 'expected missing Docker preflight failure\n' >&2
  exit 1
fi
grep -q '安装器不会安装 Docker' "${WORK}/docker-missing.log"
[ ! -e /var/lib/flowpilot/.install/preflight.done ]
if grep -Eq 'apt-get.*docker|systemctl enable --now docker' "${WORK}/docker-missing.log"; then
  printf 'installer attempted to modify Docker\n' >&2
  exit 1
fi
mv "${WORK}/bin/docker.available" "${WORK}/bin/docker"

# 多行 nvidia-smi 输出用于覆盖 pipefail 下提前关闭管道导致的 141 回归。
cat > "${WORK}/bin/nvidia-smi" <<'EOF'
#!/usr/bin/env bash
case "$*" in
  *--query-gpu=driver_version*) printf '580.159.03\n580.159.03\n' ;;
  *--query-gpu=name,memory.total,driver_version*) printf 'NVIDIA GB10, 128 GiB, 580.159.03\nNVIDIA GB10, 128 GiB, 580.159.03\n' ;;
  *) printf 'NVIDIA-SMI 580.159.03  Driver Version: 580.159.03  CUDA Version: 13.0\n' ;;
esac
EOF
chmod +x "${WORK}/bin/nvidia-smi"

printf '\n[1/7] 注入缺失包故障，确认安装安全退出\n'
if "${WORK}/installer.sh" install --non-interactive --random-password --allow-no-gpu \
  --region global --bundle "${WORK}/missing.tar.gz"; then
  printf 'expected injected installation failure\n' >&2
  exit 1
fi
grep -q 'NVIDIA 驱动检测通过：580.159.03' /var/log/flowpilot/install/latest.log
grep -q 'NVIDIA 驱动支持的 CUDA 版本：13.0' /var/log/flowpilot/install/latest.log
if grep -q '退出码 141' /var/log/flowpilot/install/latest.log; then
  printf 'NVIDIA preflight terminated with SIGPIPE\n' >&2
  exit 1
fi
[ -s /var/lib/flowpilot/.install/admin-password ]
[ -f /var/lib/flowpilot/.install/preflight.done ]

printf '\n[2/7] 断点续装并转入后台\n'
"${WORK}/installer.sh" resume --non-interactive --background --allow-no-gpu \
  --region global --bundle "${BUNDLE}"
for _ in $(seq 1 120); do
  [ -f /var/lib/flowpilot/.install/complete.done ] && break
  sleep 1
done
[ -f /var/lib/flowpilot/.install/complete.done ] || {
  tail -n 120 /var/log/flowpilot/install/*.log >&2
  exit 1
}
[ ! -e /var/lib/flowpilot/.install/admin-password ]

printf '\n[3/7] 验证真实健康接口与管理员登录\n'
health="$(curl --noproxy '*' -fsS http://127.0.0.1:18080/api/v1/health)"
printf '%s\n' "${health}" | grep -q "\"version\":\"${EXPECTED_VERSION}\""
grep -q '^ProtectHome=true$' /etc/systemd/system/flowpilot.service
password="$(sed -n 's/^password=//p' /var/lib/flowpilot/initial-credentials)"
[ -n "${password}" ]
[ "$(stat -c '%a' /var/lib/flowpilot/initial-credentials)" = "600" ]
login="$(curl --noproxy '*' -fsS -H 'Content-Type: application/json' \
  --data-binary "{\"username\":\"admin\",\"password\":\"${password}\"}" \
  http://127.0.0.1:18080/api/v1/auth/login)"
printf '%s\n' "${login}" | grep -q '"username":"admin"'
printf '%s\n' "${login}" | grep -q '"csrfToken"'
if grep -R -F "${password}" /var/log/flowpilot/install /var/log/flowpilot/service.log >/dev/null 2>&1; then
  printf 'password leaked into logs\n' >&2
  exit 1
fi

printf '\n[4/7] 再次 resume，验证幂等与现有账号保留\n'
flowpilot-native resume --non-interactive --allow-no-gpu --region global
curl --noproxy '*' -fsS http://127.0.0.1:18080/api/v1/auth/setup/status | grep -q '"initialized":true'

printf '\n[5/7] 默认卸载保留业务数据\n'
printf 'preserve-me\n' > /var/lib/flowpilot/purge-test-sentinel
flowpilot-native uninstall --yes
[ -f /var/lib/flowpilot/purge-test-sentinel ]
[ ! -e /opt/flowpilot ]
[ ! -e /etc/systemd/system/flowpilot.service ]

printf '\n[6/7] Home 自定义目录离线后台重装、最小 ACL 与彻底卸载\n'
HOME_PARENT=/home/flowpilot-native-test
HOME_DATA="${HOME_PARENT}/nested/shared"
mkdir -p "${HOME_DATA}"
chmod 0700 "${HOME_PARENT}" "${HOME_PARENT}/nested"
command -v getfacl >/dev/null 2>&1 || {
  apt-get update -o Acquire::Retries=3
  apt-get install -y --no-install-recommends acl
}
# 同时覆盖预先存在 mask 与没有扩展 ACL 的父目录，卸载后两者都必须逐字恢复。
setfacl -m mask:--- "${HOME_PARENT}/nested"
getfacl -p /home "${HOME_PARENT}" "${HOME_PARENT}/nested" > "${WORK}/home-acl.before"
"${WORK}/bundle/install.sh" install --non-interactive --background --random-password --allow-no-gpu \
  --region global --data-dir "${HOME_DATA}"
for _ in $(seq 1 120); do
  [ -f "${HOME_DATA}/.install/complete.done" ] && break
  sleep 1
done
[ -f "${HOME_DATA}/.install/complete.done" ] || {
  tail -n 120 /var/log/flowpilot/install/*.log >&2
  exit 1
}
curl --noproxy '*' -fsS http://127.0.0.1:18080/api/v1/health | grep -q "\"version\":\"${EXPECTED_VERSION}\""
grep -q '^ProtectHome=read-only$' /etc/systemd/system/flowpilot.service
grep -Fq "ReadWritePaths=${HOME_DATA} /var/log/flowpilot" /etc/systemd/system/flowpilot.service
getfacl -cp "${HOME_PARENT}" | grep -q '^user:flowpilot:--x$'
getfacl -cp "${HOME_PARENT}/nested" | grep -q '^user:flowpilot:--x$'
su -s /bin/bash flowpilot -c "printf 'home-data-write-ok\n' > '${HOME_DATA}/acl-write-test'"
[ "$(cat "${HOME_DATA}/acl-write-test")" = "home-data-write-ok" ]
if su -s /bin/bash flowpilot -c "ls '${HOME_PARENT}' >/dev/null 2>&1"; then
  printf 'flowpilot must not list the protected home parent\n' >&2
  exit 1
fi
printf 'preserve-home-data\n' > "${HOME_DATA}/purge-test-sentinel"
flowpilot-native uninstall --purge --yes
[ ! -e "${HOME_DATA}" ]
[ ! -e /opt/flowpilot ]
[ ! -e /etc/flowpilot ]
[ ! -e /var/log/flowpilot ]
getfacl -p /home "${HOME_PARENT}" "${HOME_PARENT}/nested" > "${WORK}/home-acl.after"
cmp -s "${WORK}/home-acl.before" "${WORK}/home-acl.after" || {
  printf 'home parent ACLs were not restored\n' >&2
  exit 1
}
if [ "${FLOWPILOT_USER_PREEXISTED}" = "1" ]; then
  # 安装器不得删除底镜像或物理机上原本就存在的同名账户。
  id flowpilot >/dev/null 2>&1 || {
    printf 'preexisting flowpilot user was incorrectly removed\n' >&2
    exit 1
  }
elif id flowpilot >/dev/null 2>&1; then
  printf 'installer-created flowpilot user was not removed\n' >&2
  exit 1
fi

printf '\n[7/7] 全部原生安装集成测试通过\n'
