# 慧浪智算·FlowPilot

面向 GPU 服务器的轻量化 ComfyUI 一体化部署与管理平台。

- 当前版本：`0.0.0.3`
- 当前阶段：Go 后端与真实 Docker 只读接口已联调，Web Demo 业务流程保留
- 核心目标：一次安装、Web 管理、Docker 部署、模型共享、数据持久化。

## 运行本机 Demo

```bash
cd code
./scripts/demo-docker.sh up
```

访问 `http://localhost:18080`。容器名为 `flowpilot-web-demo`，镜像为 `flowpilot-web:0.0.0.3`。

Go 服务统一提供前端页面和 `/api/v1/` 接口。系统管理页读取真实 Docker Engine 与容器信息；实例、模型、任务和大部分硬件／GPU 数据仍为明确标记的 Demo，保存在浏览器 `localStorage`。尚未实现真实 ComfyUI 生命周期管理。

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
- [最新对话上下文](context/conversations/2026-11-04-004-首页多机型硬件监控.md)

## 修改要求

每轮代码或文档修改完成后，必须更新版本号、变更索引和详细变更记录，并在同一轮提交、推送 GitHub 后核对远程提交。未记录变更、未说明验证结果的代码修改不视为完成。
