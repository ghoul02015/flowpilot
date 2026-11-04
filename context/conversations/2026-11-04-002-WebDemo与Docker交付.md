# 2026-11-04 Web Demo 与 Docker 交付上下文

- 当前项目版本：0.0.0.1
- 当前阶段：纯前端 MVP Demo 已完成，本机 Docker 运行中
- 远程仓库：`https://github.com/ghoul02015/flowpilot.git`
- 主分支：`main`

## 1. 本轮用户要求

1. 首个代码构建仍从 `0.0.0.1` 开始，不升级到 `0.0.0.2`。
2. 优先输出 Web 界面 Demo。
3. 增加规则：每轮代码和版本日志同步到现有 GitHub 仓库。
4. 先部署到本机 Docker。

## 2. 已完成决策与成果

- Web Demo 使用 React＋TypeScript＋Vite＋Tailwind CSS＋shadcn/ui。
- 视觉采用深色导航、浅色工作区和青绿色强调，面向桌面管理端并适配窄屏。
- 完成首页、实例、模型、任务和系统管理五个页面。
- 实例创建、停止、模型下载、暂停、任务同步、环境检测、设置保存和诊断导出均可操作。
- Demo 不连接真实后端，数据使用浏览器 `localStorage`，页面对此有明确标识。
- Docker 使用 Node 构建和 Nginx 运行两阶段镜像。
- 容器名：`flowpilot-web-demo`。
- 镜像：`flowpilot-web:0.0.0.1`。
- 本机地址：`http://localhost:18080`。
- 管理脚本：`code/scripts/demo-docker.sh`，支持 `up/build/stop/down/logs/status`。
- `up` 会构建镜像并重建同名容器，确保运行的是最新代码。

## 3. GitHub 新规则

规则已写入 `PROJECT_RULES.md`：

- 每轮代码、版本号和版本日志在同一轮提交并推送。
- 推送后必须核对本地 `HEAD` 与 `origin/main`。
- 失败时明确标记待同步，不得声称已推送。
- 不强制推送、不改写远程历史。
- 排除依赖、构建产物、测试截图、密钥和客户数据。

## 4. 验证结论

- 前端 ESLint 通过。
- TypeScript 与 Vite 生产构建通过。
- 生产依赖安全审计为 0 项已知漏洞。
- Chrome 端到端测试 10 项通过。
- 桌面五页及移动端截图已人工查看。
- 375px 视口无页面级横向滚动。
- 零控制台错误，零 HTTP 4xx／5xx 资源。
- Docker 容器 `running／healthy`，HTTP 200。

## 5. 当前限制

- 还没有 Go 后端、真实硬件检测、Docker API、认证和模型下载。
- Demo 的 GPU、ComfyUI 版本和模型数据均为模拟内容。
- 开发工具链仍有 12 项中等级别公告，未采用带破坏性变更的强制修复。
- 浏览器测试默认使用 macOS Chrome，可通过 `CHROME_PATH` 修改。

## 6. 下一步

先让用户验收 Web Demo 的信息架构、视觉和交互。确认后进入 Go 后端与真实 Docker API 接入，并按版本规则递增版本、保存变更记录和同步 GitHub。
