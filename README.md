# 慧浪智算·FlowPilot

面向 GPU 服务器的轻量化 ComfyUI 一体化部署与管理平台。

- 当前版本：`0.0.9.8`
- 当前阶段：实例管理（真实 Docker 生命周期＋GPU 分配＋实时日志流＋模型在线关联）、模型中心（真实仓库扫描／删除）、镜像预置（检查＋一键拉取＋默认镜像自动探测与挂载自适应）、下载引擎（URL 下载＋本地导入＋断点续传＋SHA256 校验）已落地，任务持久化与 ComfyUI 制图状态代理上线；界面无演示数据、完全由真实后端驱动；模型市场已支持 19 条主流推荐目录＋ModelScope／HF 双源在线搜索；DGX Spark 实测完成"安装→建实例→下模型→出图"全闭环；实例镜像模块支持 ComfyUI/其他镜像分类、构建/拉取双弹窗、CPU 架构/GPU 配方/在线 ComfyUI 版本等构建参数、一键构建脚本、进度百分比、日志入口和参数简述；0.0.9.3 起镜像构建/拉取改为有界并发任务列表（2 构建＋1 拉取并行、排队、停止、删除记录、持久化中断标记、悬挂镜像自动清理、构建缓存手动清理与磁盘水位检查）；0.0.9.4 新增**硬件管理页**（物理机分组＋GPU 单卡单行表格＋驱动/CUDA 版本＋2 秒实时采集，纯只读）、首页硬件精简为简述条、实例日志 SSE 实时流弹窗、实例模型关联（符号链接共享库，增删即时生效免重建）；0.0.9.5 引入**嵌入式 SQLite 数据底座**（modernc.org/sqlite 纯 Go 免 CGO，迁移框架＋UUID 稳定对象 ID＋实例映射同步＋一致性检查＋VACUUM INTO 备份，系统管理页新增数据底座状态卡），平台事实开始从 Docker 标签向数据库迁移；0.0.9.6 上线**认证与操作审计**（一次性初始化令牌＋Argon2id 密码＋服务端会话＋CSRF／Origin 校验＋登录限流＋审计全链路，未登录业务 API 全部 401 拒绝），新增初始化页／登录页／安全与会话／操作审计界面；0.0.9.7 落地**实例数据隔离**（新实例独立 workspace：instances/<UUID>/{outputs,workflows,user,config}，UUID 稳定对象身份、模型链接目录按 UUID 命名）、删除两档（默认保留数据／彻底删除）、重建同名实例自动沿用 workspace、历史数据迁移向导（扫描→预览→备份→复制/移动→校验）；0.0.9.8 建立**应用健康与生命周期**（容器/应用/队列三层状态、独立健康检查器周期探测 /system_stats、健康事件时间线含恢复事件、启动超时不删容器、操作互斥 409、停止前队列提示、重建差异预览）。
- 核心目标：一次安装、Web 管理、Docker 部署、模型共享、数据持久化。

## 运行本机 Demo

```bash
cd code
./scripts/demo-docker.sh up
```

访问 `http://localhost:18080`。容器名为 `flowpilot-web-demo`，镜像为 `flowpilot-web:0.0.9.8`。

## 离线安装与一键卸载（生产交付）

```bash
# 打包（输出到 .tmp/release/，约 33 MB，不纳入 Git）
code/scripts/package-release.sh

# 目标服务器上安装（用户目录、无需 sudo、不修改系统包）
tar -xzf flowpilot-0.0.9.8-arm64.tar.gz -C /tmp/flowpilot-install --strip-components=1
/tmp/flowpilot-install/scripts/install.sh

# 一键卸载（只删除 FlowPilot 容器、镜像和用户目录，不碰其他业务容器）
~/.local/share/flowpilot/bin/flowpilot-uninstall --yes
```

远程部署辅助：`code/scripts/deploy-remote.sh <user@host> <发布包路径> [端口]`。卸载脚本通过 `flowpilot.managed=true` 容器标签判断镜像是否仍被其他 FlowPilot 实例使用，避免误删共享镜像。已在 DGX Spark（Ubuntu 24.04 arm64、NVIDIA GB10、Docker 29.2.1）实测通过。

Go 服务统一提供前端页面和 `/api/v1/` 接口。系统管理页读取真实 Docker Engine 与容器信息；实例管理已走真实 Docker API（实例即带 `flowpilot.instance=true` 标签的容器，配置存标签，无需数据库），支持真实 GPU 分配、端口映射与共享数据目录挂载；安装时自动探测默认 ComfyUI 镜像（`comfyui-h3:spark` 优先）并按镜像 WorkingDir 适配挂载目标，新实例开箱即见平台模型库；模型中心实时扫描共享仓库并支持界面删除，URL 下载与本地导入走同一任务引擎；任务中心展示真实下载／导入任务；实例页提供本机镜像分类管理、在线拉取弹窗和手动构建弹窗，新建实例只从本机 ComfyUI 镜像中选择；手动构建状态包含进度百分比、实时日志和参数简述；硬件／GPU 数据为实时采集。

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
- [最新对话上下文](context/conversations/2026-11-09-022-cu128-amd64构建脚本语法修复.md)

## 修改要求

每轮代码或文档修改完成后，必须更新版本号、变更索引和详细变更记录，并在同一轮提交、推送 GitHub 后核对远程提交。未记录变更、未说明验证结果的代码修改不视为完成。
