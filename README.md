# 慧浪智算·FlowPilot

面向 GPU 服务器的轻量化 ComfyUI 一体化部署与管理平台。

- 当前版本：`0.0.0.1`
- 当前阶段：MVP 立项与技术验证准备
- 核心目标：一次安装、Web 管理、Docker 部署、模型共享、数据持久化。

## 项目目录

```text
├── docs/
│   └── mvp/                  # MVP PRD 与产品文档
├── context/
│   ├── conversations/        # 对话摘要与需求决策
│   └── changes/              # 分版本详细变更记录
├── code/
│   ├── backend/              # 后端代码
│   ├── frontend/             # 前端代码
│   ├── deploy/               # 安装、卸载与部署配置
│   └── scripts/              # 开发、测试和构建脚本
├── VERSION                   # 当前唯一版本号
├── CHANGELOG.md              # 版本变更索引
└── PROJECT_RULES.md          # 项目协作规则
```

## 关键文档

- [MVP 产品需求文档](docs/mvp/FlowPilot_MVP_PRD.md)
- [项目协作与版本管理规则](PROJECT_RULES.md)
- [版本变更日志](CHANGELOG.md)
- [最新对话上下文](context/conversations/2026-11-04-001-产品规划与项目管理.md)

## 修改要求

每轮代码或文档修改完成后，必须更新版本号、变更索引和详细变更记录。未记录变更、未说明验证结果的代码修改不视为完成。
