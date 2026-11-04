# FlowPilot 变更日志

所有重要变更均记录于此。详细内容见 `context/changes/`。

## 0.0.0.1 — 2026-11-04

### 新增

- 建立 FlowPilot MVP 产品需求文档。
- 建立 MVP 文档、对话上下文、代码和版本变更目录。
- 建立四段式版本管理和代码修改留痕规则。
- 保存首轮产品规划与 PRD 对话上下文。
- 新增 React＋TypeScript Web Demo，覆盖看板、实例、模型、任务和系统管理五个页面。
- 新增实例创建与启停、模型下载与暂停、任务重试、环境检测和诊断导出等可交互模拟流程。
- 新增本机 Docker 镜像、Nginx 配置和 Demo 容器管理脚本。
- 新增 Chrome 端到端验证脚本及 375px 视口检查。
- 新增代码与版本日志同步 GitHub 的强制规则。

### 调整

- 将 PRD 统一归档到 `docs/mvp/`。
- 将 PRD 项目版本统一为 `0.0.0.1`。
- 修复窄屏下数据表撑宽页面、弹窗状态重置及 Docker 重建后未更新容器的问题。

### 验证

- `npm run lint` 通过。
- `npm run build` 通过。
- `npm audit --omit=dev`：0 项已知漏洞。
- `npm run test:demo`：10 项检查通过，零控制台错误、零 HTTP 4xx／5xx 资源。
- Docker 容器 `flowpilot-web-demo` 状态为 `running／healthy`，访问返回 HTTP 200。

详细记录：[0.0.0.1](context/changes/0.0.0.1.md)
