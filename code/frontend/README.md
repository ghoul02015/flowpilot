# FlowPilot Web Demo

慧浪智算·FlowPilot 的 MVP 前端演示，当前版本 `0.0.0.1`。

## 已实现页面

- 首页看板：主机、GPU、实例、模型和任务概览。
- ComfyUI 实例：搜索筛选、新建向导、启动、停止、重启、删除和自启配置。
- 模型中心：URL 下载／本地导入模拟、进度、暂停、继续和保护性删除。
- 任务中心：任务筛选、进度、日志、取消和重试。
- 系统管理：环境检测、网络设置、操作日志和诊断 JSON 导出。

## Demo 边界

本版本为纯前端 Demo，不连接真实 Go 后端、Docker API、ComfyUI 或模型仓库。实例、模型和任务数据使用浏览器 `localStorage` 保存，仅存在于当前浏览器；清除浏览器数据会丢失修改。侧栏“重置演示数据”可恢复初始状态。

## 本地开发

```bash
npm ci
npm run dev
```

传入自定义地址和端口：

```bash
npm run dev -- --host 0.0.0.0 --port 7100
```

代码检查与构建：

```bash
npm run lint
npm run build
```

Docker Demo 启动后，可运行本机 Chrome 端到端验证：

```bash
npm run test:demo
```

验证覆盖五个页面、新建与停止实例、模型下载与暂停、任务同步、环境检测、诊断导出及 375px 视口。截图和结果写入被 Git 忽略的 `test-results/`；可通过 `CHROME_PATH` 和 `FLOWPILOT_DEMO_URL` 指定浏览器及访问地址。

## 本机 Docker

在项目的 `code/` 目录执行：

```bash
./scripts/demo-docker.sh up
```

默认访问 `http://localhost:18080`。其他命令：

```bash
./scripts/demo-docker.sh status
./scripts/demo-docker.sh logs
./scripts/demo-docker.sh stop
./scripts/demo-docker.sh down
```

可通过 `FLOWPILOT_DEMO_PORT` 修改主机端口。
