package storage

import "strings"

// renderMountScript 渲染宿主机一键挂载脚本：安装挂载工具、挂载 NFS/SMB、
// 写入标记文件与 fstab（可选），最后提示重启平台容器使其可见。
func renderMountScript(config Config, mountPoint string) string {
	var builder strings.Builder
	builder.WriteString(`#!/usr/bin/env bash
# FlowPilot 网络模型仓库存储挂载脚本（在 GPU 宿主机上执行一次）。
# 挂载完成后平台容器需重启一次才能看到网络仓库（bind 挂载传播限制），脚本末尾自动完成。
set -Eeuo pipefail

`)
	builder.WriteString("STORAGE_TYPE=" + shellQuote(config.Type) + "\n")
	builder.WriteString("STORAGE_ADDR=" + shellQuote(config.Address) + "\n")
	builder.WriteString("STORAGE_REMOTE=" + shellQuote(config.RemotePath) + "\n")
	builder.WriteString("MOUNT_POINT=" + shellQuote(mountPoint) + "\n")
	builder.WriteString("MARKER=" + shellQuote(MarkerFile) + "\n")
	builder.WriteString(`
need_sudo() {
  if [ "$(id -u)" -eq 0 ]; then echo ""; return 0; fi
  if sudo -n true 2>/dev/null; then echo "sudo -n"; return 0; fi
  echo "需要 sudo 权限执行挂载，请先运行 sudo -v 缓存凭据后重试。" >&2
  exit 1
}
SUDO="$(need_sudo)"

echo "==> 安装挂载工具（如已安装会自动跳过）"
if command -v apt-get >/dev/null 2>&1; then
  if [ "$STORAGE_TYPE" = "nfs" ]; then
    dpkg -s nfs-common >/dev/null 2>&1 || $SUDO apt-get install -y nfs-common
  else
    dpkg -s cifs-utils >/dev/null 2>&1 || $SUDO apt-get install -y cifs-utils
  fi
fi

echo "==> 创建挂载点 $MOUNT_POINT"
mkdir -p "$MOUNT_POINT"

if [ "$STORAGE_TYPE" = "nfs" ]; then
  echo "==> 挂载 NFS $STORAGE_ADDR:$STORAGE_REMOTE"
  $SUDO mount -t nfs -o "ro,hard,intr,nfsvers=4" "$STORAGE_ADDR:$STORAGE_REMOTE" "$MOUNT_POINT"
else
  if [ ! -f "$HOME/.smbcredentials" ]; then
    echo "请先创建 $HOME/.smbcredentials（内容：username=… 与 password=… 两行，权限 600）后重试。" >&2
    exit 1
  fi
  echo "==> 挂载 SMB //$STORAGE_ADDR/$STORAGE_REMOTE"
  $SUDO mount -t cifs -o "ro,credentials=$HOME/.smbcredentials,iocharset=utf8" "//$STORAGE_ADDR/$STORAGE_REMOTE" "$MOUNT_POINT"
fi

echo "==> 写入 /etc/fstab 实现开机自动挂载（已存在则跳过）"
if [ "$STORAGE_TYPE" = "nfs" ]; then
  FSTAB_LINE="$STORAGE_ADDR:$STORAGE_REMOTE $MOUNT_POINT nfs ro,hard,intr,nfsvers=4,_netdev 0 0"
else
  FSTAB_LINE="//$STORAGE_ADDR/$STORAGE_REMOTE $MOUNT_POINT cifs ro,credentials=$HOME/.smbcredentials,iocharset=utf8,_netdev 0 0"
fi
grep -qF "$MOUNT_POINT " /etc/fstab || echo "$FSTAB_LINE" | $SUDO tee -a /etc/fstab >/dev/null

echo "==> 写入挂载标记，供平台探测"
touch "$MOUNT_POINT/$MARKER"

echo "==> 重启 FlowPilot 平台容器，使其看到网络仓库"
docker restart flowpilot-web-demo >/dev/null 2>&1 || true

echo "挂载完成：$MOUNT_POINT"
echo "在平台「系统管理 → 网络存储」中可查看探测状态。"
`)
	return builder.String()
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
