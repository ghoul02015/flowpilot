# 慧浪智算·FlowPilot

面向 GPU 服务器的轻量化 ComfyUI 一体化部署与管理平台。

- 当前版本：`0.0.1.0`
- 当前阶段：实例管理已接入真实 Docker API（创建／启停／删除 ComfyUI 容器，真实 GPU 分配，共享数据目录挂载），在 DGX Spark 实测通过；真实硬件采集、离线安装包与一键卸载可用
- 核心目标：一次安装、Web 管理、Docker 部署、模型共享、数据持久化。

## 运行本机 Demo

```bash
cd code
./scripts/demo-docker.sh up
```

访问 `http://localhost:18080`。容器名为 `flowpilot-web-demo`，镜像为 `flowpilot-web:0.0.1.0`。

## 离线安装与一键卸载（生产交付）

```bash
# 打包（输出到 .tmp/release/，约 33 MB，不纳入 Git）
code/scripts/package-release.sh

# 目标服务器上安装（用户目录、无需 sudo、不修改系统包）
tar -xzf flowpilot-0.0.1.0-arm64.tar.gz -C /tmp/flowpilot-install --strip-components=1
/tmp/flowpilot-install/scripts/install.sh

# 一键卸载（只删除 FlowPilot 容器、镜像和用户目录，不碰其他业务容器）
~/.local/share/flowpilot/bin/flowpilot-uninstall --yes
```

远程部署辅助：`code/scripts/deploy-remote.sh <user@host> <发布包路径> [端口]`。卸载脚本通过 `flowpilot.managed=true` 容器标签判断镜像是否仍被其他 FlowPilot 实例使用，避免误删共享镜像。已在 DGX Spark（Ubuntu 24.04 arm64、NVIDIA GB10、Docker 29.2.1）实测通过。

Go 服务统一提供前端页面和 `/api/v1/` 接口。系统管理页读取真实 Docker Engine 与容器信息；实例管理已走真实 Docker API（实例即带 `flowpilot.instance=true` 标签的容器，配置存标签，无需数据库），支持真实 GPU 分配、端口映射与共享数据目录挂载；硬件／GPU 数据为实时采集；模型与任务数据仍为明确标记的 Demo，保存在浏览器 `localStorage`。

启动脚本会挂载 `/var/run/docker.sock`，仅限受信任的本机或管理网络使用。当前版本没有认证，不应直接暴露到公网。详细操作见 [Web Demo 说明](code/frontend/README.md) 和 [后端说明](code/backend/README.md)。

## 项目目录

```text
├── docs/
│   └── mvp/                  # MVP PRD 与产品文档
├── context/
│   ├── conversations/        # 对话摘要与需求决策
│   └── changes/              # 分版本详细变更记录
├── code/
│   ├── backend/              # Go 服务与 Docker API
│   ├── frontend/             # React Web Demo 与端到端测试
│   ├── deploy/               # 一体化 Dockerfile 与部署配置
│   └── scripts/              # Docker Demo 管理脚本
├── VERSION                   # 当前唯一版本号
├── CHANGELOG.md              # 版本变更索引
└── PROJECT_RULES.md          # 项目协作规则
```

## 关键文档

- [MVP 产品需求文档](docs/mvp/FlowPilot_MVP_PRD.md)
- [项目协作与版本管理规则](PROJECT_RULES.md)
- [版本变更日志](CHANGELOG.md)
- [最新对话上下文](context/conversations/2026-11-08-008-实例真实生命周期.md)

## 修改要求

每轮代码或文档修改完成后，必须更新版本号、变更索引和详细变更记录，并在同一轮提交、推送 GitHub 后核对远程提交。未记录变更、未说明验证结果的代码修改不视为完成。
