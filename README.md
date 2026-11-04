# 慧浪智算·FlowPilot

面向 GPU 服务器的轻量化 ComfyUI 一体化部署与管理平台。

- 当前版本：`0.0.0.1`
- 当前阶段：MVP Web Demo 已完成并通过本机 Docker 验证
- 核心目标：一次安装、Web 管理、Docker 部署、模型共享、数据持久化。

## 运行 Web Demo

```bash
cd code
./scripts/demo-docker.sh up
```

访问 `http://localhost:18080`。容器名为 `flowpilot-web-demo`，镜像为 `flowpilot-web:0.0.0.1`。

当前为纯前端演示，不连接真实 ComfyUI 或 Docker API；实例、模型和任务数据仅保存在当前浏览器 `localStorage`。详细操作见 [Web Demo 说明](code/frontend/README.md)。

## 项目目录

```text
├── docs/
│   └── mvp/                  # MVP PRD 与产品文档
├── context/
│   ├── conversations/        # 对话摘要与需求决策
│   └── changes/              # 分版本详细变更记录
├── code/
│   ├── backend/              # 后端代码
│   ├── frontend/             # React Web Demo 与前端测试
│   ├── deploy/               # Dockerfile 与 Nginx 配置
│   └── scripts/              # Docker Demo 管理脚本
├── VERSION                   # 当前唯一版本号
├── CHANGELOG.md              # 版本变更索引
└── PROJECT_RULES.md          # 项目协作规则
```

## 关键文档

- [MVP 产品需求文档](docs/mvp/FlowPilot_MVP_PRD.md)
- [项目协作与版本管理规则](PROJECT_RULES.md)
- [版本变更日志](CHANGELOG.md)
- [最新对话上下文](context/conversations/2026-11-04-002-WebDemo与Docker交付.md)

## 修改要求

每轮代码或文档修改完成后，必须更新版本号、变更索引和详细变更记录。未记录变更、未说明验证结果的代码修改不视为完成。
