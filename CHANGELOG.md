# FlowPilot 变更日志

所有重要变更均记录于此。详细内容见 `context/changes/`。

## 0.0.0.3 — 2026-11-04

### 新增

- 首页新增完整硬件监控面板，展示主机名称、操作系统、CPU、内存、存储及使用率。
- 每个 GPU 独立展示型号、显存／统一内存、计算利用率、核心温度、功耗和在线状态。
- 新增双卡 L40S、DGX Spark、DGX Station GB300、单卡及十六卡 RTX 5090 演示机型。
- 新增独立显存、统一内存、一致性内存三种数据模式，避免共享内存重复累计。
- 新增 2 秒 Demo 遥测刷新和对应端到端断言。

### 调整

- 快捷操作改为三列布局，保留实例、任务和原有全部业务流程。
- 多卡场景使用响应式卡片网格，十六卡采用四列紧凑布局，窄屏自动降为单列。
- 项目、后端健康接口和 Docker 镜像版本统一升级为 `0.0.0.3`。

### 验证

- Go 测试和 `go vet`、前端 lint／build、生产依赖安全审计通过。
- `npm run test:demo`：18 项检查通过，零控制台错误、零 HTTP 4xx／5xx 资源。
- 已检查双卡、DGX Spark、十六卡和移动端截图；375px 视口无页面级横向滚动。
- `flowpilot-web-demo` 使用 `flowpilot-web:0.0.0.3`，状态为 `running／healthy`。

详细记录：[0.0.0.3](context/changes/0.0.0.3.md)

## 0.0.0.2 — 2026-11-04

### 新增

- 新增仅使用 Go 标准库的轻量后端，统一提供 SPA 静态页面和版本化 API。
- 新增 Unix Socket Docker 客户端及健康、系统、Engine 信息和容器列表接口。
- 新增 Docker 客户端与 HTTP 服务单元测试，以及真实接口端到端检查。
- 系统管理页接入真实后端和 Docker 信息，诊断包同步纳入运行快照。

### 修复与调整

- 修复 shadcn 主题变量格式不一致导致的新建实例弹窗透明问题，并增加不透明度回归检查。
- 一体化镜像由 Nginx 静态服务改为 Node＋Go 构建、Alpine＋Go 运行。
- Demo 容器挂载 Docker Socket，镜像升级为 `flowpilot-web:0.0.0.2`，端口保持 `18080`。
- 明确区分真实 Docker 数据与仍为 Demo 的硬件、GPU、实例、模型和任务数据。

### 验证

- Go `go test ./...`、`go vet ./...` 通过。
- `npm run lint`、`npm run build` 通过，`npm audit --omit=dev` 为 0 项已知漏洞。
- `npm run test:demo`：14 项检查通过，零控制台错误、零 HTTP 4xx／5xx 资源。
- 真实 Docker Engine `29.4.2`／API `1.54` 读取成功。
- 容器 `flowpilot-web-demo` 使用 `flowpilot-web:0.0.0.2`，状态为 `running／healthy`。

详细记录：[0.0.0.2](context/changes/0.0.0.2.md)

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
