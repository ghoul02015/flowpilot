# FlowPilot Web Demo

慧浪智算·FlowPilot 的 MVP 前端，当前版本 `0.0.0.2`。

## 已实现页面

- 首页看板：主机、GPU、实例、模型和任务概览。
- ComfyUI 实例：搜索筛选、新建向导、启动、停止、重启、删除和自启配置。
- 模型中心：URL 下载／本地导入模拟、进度、暂停、继续和保护性删除。
- 任务中心：任务筛选、进度、日志、取消和重试。
- 系统管理：真实后端／Docker 状态、环境检测、网络设置、操作日志和诊断 JSON 导出。

## Demo 边界

系统管理页通过 Go 后端读取真实 Docker Engine 和容器列表；实例、模型、任务及大部分硬件／GPU 信息仍使用浏览器 `localStorage` 中的 Demo 数据。后端连接失败时页面明确显示回退状态。尚未连接真实 ComfyUI 或模型仓库，清除浏览器数据会丢失 Demo 修改，侧栏“重置演示数据”可恢复初始状态。

## 本地开发

```bash
npm ci
npm run dev
```

传入自定义地址和端口：

```bash
npm run dev -- --host 0.0.0.0 --port 7100
```

独立 Vite 开发服务器没有后端代理；完整前后端联调请使用下方一体化 Docker 容器。

代码检查与构建：

```bash
npm run lint
npm run build
npm audit --omit=dev
```

Docker Demo 启动后，可运行本机 Chrome 端到端验证：

```bash
npm run test:demo
```

验证覆盖后端版本、真实 Docker API、五个页面、弹窗不透明度、实例和模型模拟流程、系统页联调、诊断导出及 375px 视口。截图和结果写入被 Git 忽略的 `test-results/`；可通过 `CHROME_PATH` 和 `FLOWPILOT_DEMO_URL` 指定浏览器及访问地址。

## 本机 Docker

在项目的 `code/` 目录执行：

```bash
./scripts/demo-docker.sh up
```

默认访问 `http://localhost:18080`。脚本挂载 `/var/run/docker.sock`，仅限受信任的本机或管理网络使用。其他命令：

```bash
./scripts/demo-docker.sh status
./scripts/demo-docker.sh logs
./scripts/demo-docker.sh stop
./scripts/demo-docker.sh down
```

可通过 `FLOWPILOT_DEMO_PORT` 修改主机端口，通过 `FLOWPILOT_DOCKER_SOCKET` 指定 Docker Socket。
