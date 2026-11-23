# 慧浪智算·FlowPilot 产品说明与安装使用手册

当前版本：`0.1.0.17`

英文版：[English](README.en.md)

## 1. 产品简介

慧浪智算·FlowPilot 是面向 GPU 物理服务器的轻量化 ComfyUI 一体化部署与管理平台。平台通过统一 Web 控制台，将硬件、ComfyUI 实例、镜像、模型、下载任务、备份和审计集中管理，适合单机 GPU 工作站、AI 推理服务器及受信任的内网管理环境。

从本版本开始，**FlowPilot 管理服务以原生 Go 单进程运行在 systemd 下，不再要求把管理服务自身放入 Docker**。ComfyUI 实例仍由 Docker 管理，从而保留实例隔离、镜像供应链、GPU 分配和既有数据兼容性。

### 核心功能

- **硬件与兼容性检查**：GPU 单卡信息、NVIDIA 驱动／CUDA、CPU、内存、温度、统一内存、磁盘水位和修复建议。
- **ComfyUI 实例管理**：创建、启动、停止、重启、重建、GPU 与端口分配、独立工作区、实时日志和队列健康状态。
- **镜像中心**：可信镜像识别、在线拉取、固定 ComfyUI 提交构建、导入导出、标签管理及构建任务日志。
- **模型中心**：共享模型仓库、ModelScope／Hugging Face 双源检索、URL 下载、本地导入、NAS 模型源、断点续传和 SHA256 校验。
- **模型组合套装**：MiniMax H3 组合包预检、许可确认、串行安装、暂停／继续／取消／重试和实例关联。
- **统一任务中心**：下载、导入、构建、拉取、迁移、套装和备份任务的状态、进度与日志。
- **数据保护**：SQLite 持久化、实例工作区隔离、平台备份、升级前快照及健康失败回滚。
- **安全与审计**：单管理员 `admin`、Argon2id 密码哈希、服务端会话、CSRF／Origin 校验、登录限流和操作审计。

## 2. 部署架构与边界

```text
浏览器
  │ HTTP :18080（默认）
  ▼
flowpilotd（原生 Go 进程，systemd 管理）
  ├── SQLite／模型／工作流／输出：/var/lib/flowpilot
  ├── Docker Unix Socket：/var/run/docker.sock
  └── ComfyUI 容器实例（Docker，GPU 按需分配）
```

- 目标机不需要 Go、Node.js、Python、Nginx 或 Docker Compose。
- Docker Engine 仍用于 ComfyUI 实例，必须由用户预先安装并启动；安装器只检测 Docker CLI、Engine 和 Socket，不安装或启动 Docker。
- 默认监听所有网卡的 `18080` 端口。平台没有内置 TLS，请勿直接暴露到不受信任的公网；生产环境建议在受信任管理网内使用，或自行配置 HTTPS 反向代理。
- 支持 Ubuntu／Debian、systemd、Linux `amd64`／`arm64`。同一个安装脚本自动识别 `x86_64`／`amd64` 与 `aarch64`／`arm64` 并选择对应发布包，无需维护两套脚本。NVIDIA GPU 为推荐配置；无可用驱动时可显式选择仅 CPU 管理。

## 3. 安装前准备

1. 一台 Ubuntu 22.04／24.04 或兼容 Debian 系统的物理机／虚拟机，`root` 或 `sudo` 权限。
2. 至少 1 GiB 可用磁盘用于平台；建议为 ComfyUI 镜像和模型额外预留 20 GiB 以上。
3. Docker Engine 必须预先安装并启动，当前用户或 `flowpilot` 服务账号必须能够访问 Docker Socket；安装器不会通过 `apt` 安装 Docker，也不会启用或启动 Docker 服务。
4. GPU 实例需要可用的 NVIDIA 驱动；GPU 容器还需要 NVIDIA Container Toolkit。安装器只检测驱动、驱动支持的 CUDA 能力、宿主机 CUDA Toolkit 和 NVIDIA Container Runtime，不安装或修改 NVIDIA 驱动、CUDA 或 NVIDIA Container Toolkit。
5. 能访问 GitHub Releases，或准备离线发布包／可用镜像。国内网络可使用 `--region cn`。

## 4. 在线向导安装（推荐）

```bash
curl -fL --retry 3 \
  -o flowpilot-native.sh \
  https://github.com/ghoul02015/flowpilot/releases/download/v0.1.0.17/flowpilot-native.sh
chmod +x flowpilot-native.sh
sudo ./flowpilot-native.sh install
```

向导依次确认：

1. Web 端口，默认 `18080`；
2. 程序目录，默认 `/opt/flowpilot`；
3. 数据目录，默认 `/var/lib/flowpilot`；
4. 网络策略 `auto`／`cn`／`global`；
5. 是否后台挂起安装；
6. CPU 架构、Docker、GPU 驱动与 CUDA 能力、NVIDIA Container Runtime、网络、磁盘和端口等预检；
7. `admin` 初始密码。密码隐藏输入并二次确认，至少 8 位且同时含字母和数字。

安装完成后访问：

```text
http://<服务器地址>:18080
账号：admin
密码：安装时设置的密码
```

密码通过本机初始化 API 写入 Argon2id 哈希，不进入 systemd 环境文件、进程命令行或安装日志。

## 5. 国内网络、代理与镜像

### 自动回退

```bash
sudo ./flowpilot-native.sh install --region cn
```

- `auto`：官方地址优先，失败后尝试内置镜像。
- `cn`：内置镜像优先，最后回退官方地址。
- `global`：只使用官方地址及用户显式指定的镜像。
- 内置候选：`ghfast.top`、`gh-proxy.com`、`ghproxy.net`。第三方镜像可用性会变化，不应作为唯一长期下载渠道。

### 自定义镜像或代理

```bash
sudo ./flowpilot-native.sh install \
  --mirror https://ghfast.top/

# 也支持标准代理环境变量；日志只记录“已设置”，不输出代理地址。
export HTTPS_PROXY=http://proxy.example.com:8080
sudo -E ./flowpilot-native.sh install
```

在线发布包必须通过 Release 中 `SHA256SUMS` 的校验才会解压。校验用于发现损坏或与发布清单不一致的文件；在条件允许时，仍应从官方仓库获取安装器和校验清单。镜像仅用于下载，不要在镜像页面输入 GitHub 凭据。

构建发布包时，源码依赖也支持镜像：

```bash
FLOWPILOT_NETWORK_REGION=cn code/scripts/package-native-release.sh
# 或分别指定 FLOWPILOT_NPM_REGISTRY、FLOWPILOT_GOPROXY。
```

## 6. 后台安装、断开终端与断点续装

### 后台安装

在向导中选择后台安装，或直接执行：

```bash
sudo ./flowpilot-native.sh install --background
```

安装器使用独立会话运行，记录 worker PID 和完整日志。关闭 SSH 终端不会终止后台安装。

```bash
sudo ./flowpilot-native.sh status
sudo tail -f /var/log/flowpilot/install/latest.log
```

### 中断后继续

前台安装被 Ctrl+C、SSH 断开或网络错误中断时，已下载的 `.part` 分片、配置和阶段状态会保留。修复问题后执行：

```bash
sudo ./flowpilot-native.sh resume
```

下载使用 HTTP Range 续传；服务配置、目录创建和健康检查均可幂等重试。不要同时运行两个安装／卸载进程，安装器会通过锁拒绝并发操作。

## 7. 无人值守安装

### 指定密码文件

```bash
umask 077
printf '%s\n' '请替换为强密码123' > /root/flowpilot-password
sudo ./flowpilot-native.sh install \
  --non-interactive \
  --background \
  --password-file /root/flowpilot-password \
  --allow-no-gpu
shred -u /root/flowpilot-password 2>/dev/null || rm -f /root/flowpilot-password
```

密码文件必须为 `0600`。`--allow-no-gpu` 仅在没有可用 NVIDIA 驱动时需要；GPU 主机驱动正常时不要加。

### 随机初始密码

```bash
sudo ./flowpilot-native.sh install \
  --non-interactive --background --random-password --allow-no-gpu
sudo cat /var/lib/flowpilot/initial-credentials
```

随机凭据只保存在 root 可读的 `0600` 文件中。首次登录后请修改密码，并妥善删除该初始凭据文件。

常用非交互参数：

```text
--port 18080
--region cn
--mirror https://ghfast.top/
--background
--allow-no-gpu
```

## 8. 离线安装

从同一 Release 下载对应架构的安装包和 `SHA256SUMS`：

```text
flowpilot-0.1.0.17-linux-amd64.tar.gz
flowpilot-0.1.0.17-linux-arm64.tar.gz
SHA256SUMS
```

目标机执行：

```bash
sha256sum -c --ignore-missing SHA256SUMS
mkdir -p /tmp/flowpilot-native
tar -xzf flowpilot-0.1.0.17-linux-arm64.tar.gz \
  -C /tmp/flowpilot-native --strip-components=1
sudo /tmp/flowpilot-native/install.sh install
```

`amd64` 机器请替换包名。离线包已包含 Go 服务、前端、安装器、文档和许可文件，目标机无需访问 GitHub。安装器不会安装 Docker、NVIDIA 驱动、CUDA 或 NVIDIA Container Toolkit，这些基础环境必须预先准备；后续拉取 ComfyUI 镜像／模型仍可能需要网络。

自定义数据目录位于 `/home` 等 systemd 受保护路径时，安装器自动使用 `ProtectHome=read-only`，并仅为 `flowpilot` 账号添加父目录进入权限（ACL `--x`，不允许列出父目录内容）；`ReadWritePaths` 仍只开放数据目录和日志目录，卸载时恢复原始 ACL。此场景需要 `acl` 软件包（提供 `setfacl`／`getfacl`），缺失时安装器会尝试通过 `apt` 安装；完全离线环境请预装该软件包。默认 `/var/lib/flowpilot` 仍使用更严格的 `ProtectHome=true`。

## 9. 安装目录与文件

| 路径 | 用途 |
|---|---|
| `/opt/flowpilot/releases/0.1.0.17/` | 当前版本程序与前端 |
| `/opt/flowpilot/current` | 当前版本软链接，用于升级与回滚 |
| `/opt/flowpilot/bin/flowpilot-native` | 安装、续装、状态与卸载统一入口 |
| `/usr/local/bin/flowpilot-native` | 全局命令软链接 |
| `/etc/systemd/system/flowpilot.service` | 原生服务单元 |
| `/etc/flowpilot/flowpilot.env` | 非敏感运行配置 |
| `/etc/flowpilot/installer.conf` | 续装配置，不含密码 |
| `/var/lib/flowpilot/flowpilot.db` | SQLite 数据库 |
| `/var/lib/flowpilot/models/` | 共享模型仓库 |
| `/var/lib/flowpilot/instances/` | 实例独立工作区 |
| `/var/lib/flowpilot/import/` | 本地模型导入白名单目录 |
| `/var/lib/flowpilot/backups/` | 平台与升级前备份 |
| `/var/lib/flowpilot/.install/` | 下载分片、安装状态和短生命周期初始化文件 |
| `/var/log/flowpilot/install/` | 完整安装／卸载日志 |
| `/var/log/flowpilot/service.log` | 原生服务标准输出与错误日志 |

## 10. 服务与日志

```bash
sudo systemctl status flowpilot
sudo systemctl restart flowpilot
sudo systemctl stop flowpilot
sudo systemctl enable flowpilot

sudo tail -f /var/log/flowpilot/service.log
sudo journalctl -u flowpilot -n 200 --no-pager
sudo tail -f /var/log/flowpilot/install/latest.log
```

健康检查：

```bash
curl http://127.0.0.1:18080/api/v1/health
```

安装日志记录各阶段、下载线路、检查结果、服务状态和错误位置。密码、初始化令牌和代理地址不会主动写入日志；不要把密码直接放在命令行参数中。

## 11. 升级与回滚

使用新版本安装器执行 `install` 即可。安装器会：

1. 保留旧版本 release 目录；
2. 停止服务后复制 SQLite 及 WAL／SHM 文件到 `backups/`；
3. 原子切换 `current` 软链接；
4. 执行健康检查，失败时把软链接切回旧版本并尝试重启。

数据库升级可能包含向前迁移。若需要完整回滚，请同时恢复对应升级前数据库备份，并先停止服务。不要只替换数据库文件而忽略 `-wal`／`-shm` 一致性。

## 12. 卸载与彻底清理

### 默认卸载：保留业务数据

```bash
sudo flowpilot-native uninstall
# 非交互：
sudo flowpilot-native uninstall --yes
```

删除原生程序、systemd 服务和下载缓存；保留 `/var/lib/flowpilot`、日志、Docker、NVIDIA 驱动、ComfyUI 容器与镜像。

### 彻底清理平台数据

```bash
sudo flowpilot-native uninstall --purge
```

额外永久删除模型、工作流、输出、SQLite、备份、配置和日志。此操作不可撤销，脚本会要求输入 `yes`；自动化场景必须显式加 `--yes`。

### 同时删除平台管理的 ComfyUI 容器

```bash
sudo flowpilot-native uninstall --purge --remove-instances
```

只处理带 `flowpilot.instance=true` 标签的容器，不处理其他业务容器。Docker 镜像和系统软件包仍保留，避免影响其他业务。

## 13. 常见问题

### GitHub 无法连接

使用 `--region cn`、`--mirror`、标准 HTTPS 代理，或改用离线包。若所有线路失败，检查 DNS、时间、CA 证书和出口防火墙后执行 `resume`。

### 端口被占用

换用其他端口：

```bash
sudo ./flowpilot-native.sh install --port 28080
```

远程访问还需在防火墙中放行对应 TCP 端口，例如 `sudo ufw allow 18080/tcp`。

### NVIDIA 驱动存在但 GPU 容器不可用

检查 `nvidia-smi` 和 `docker info | grep -i nvidia`。安装器不会自动修改 NVIDIA Container Toolkit 或驱动，避免因驱动变更影响物理机其他任务。

### 服务启动失败

先查看 `/var/log/flowpilot/service.log` 和 `journalctl -u flowpilot`，重点检查 Docker Socket、`/var/lib/flowpilot` 权限、端口及 `flowpilot` 用户的 Docker 组权限。

### 忘记密码

使用同一管理员账号登录后在安全设置中修改密码。若无法登录，请按组织安全流程处理数据库恢复；不要把真实数据库、令牌或密码提交到公开 Issue。

## 14. 开源与许可

项目采用 **Apache License 2.0**。该协议允许使用、修改和分发，并包含明确的专利授权及 NOTICE 保留要求。分发时请保留 `LICENSE`、`NOTICE` 和适用的第三方声明。模型及其工作流可能有独立许可，平台 Apache-2.0 不改变模型本身的授权条件。

仓库：<https://github.com/ghoul02015/flowpilot>
