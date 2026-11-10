# FlowPilot Web 管理台

慧浪智算·FlowPilot 的 Web 前端，当前版本 `0.0.9.5`。0.0.6.2 起界面不含任何演示数据，全部展示来自真实后端接口。

## 已实现页面

- 首页看板：完整主机、CPU、内存、存储和逐卡 GPU 监控，以及实例、模型和任务概览。
- 硬件监控：支持双卡 L40S、DGX Spark、DGX Station GB300、单卡／十六卡 RTX 5090 演示机型，遥测每 2 秒刷新。
- ComfyUI 实例：搜索筛选、新建向导、启动、停止、重启、删除和自启配置；实例页内置「容器镜像管理」与「手动构建 / 拉取」子菜单，支持 ComfyUI/其他镜像分类、本机镜像增删改、tar 导入导出、构建/拉取双弹窗、构建参数脚本下载、进度百分比和实时日志。
- 模型中心：模型市场（19 条主流模型推荐目录＋ModelScope／HF 镜像双源在线搜索，源连接状态实时显示，仓库文件按类型筛选、真实大小、文件名对齐 ComfyUI 官方模板，选中即下载、已入库标注）、URL 下载／本地导入、进度、暂停、继续和保护性删除。
- 任务中心：任务筛选、进度、日志、取消和重试。
- 系统管理：真实后端／Docker 状态、环境检测、网络设置、操作日志和诊断 JSON 导出。

## 数据边界

全部业务数据来自真实后端：系统管理页读取真实 Docker Engine 与容器列表；首页硬件面板通过 `/api/v1/hardware` 实时采集宿主机指标（2 秒轮询，接口失败显示告警并自动重试，无演示数据回退）；实例管理走真实 Docker API（3 秒轮询真实容器，新建向导只允许选择本机 ComfyUI 镜像，真实 GPU 下拉与端口配置保留）；镜像管理走真实 Docker API，支持本机镜像分类、改标签、删除、tar 导入导出、后台拉取、按 CPU 架构/GPU 配方/ComfyUI 版本后台构建、同参数脚本下载、实时日志和进度轮询；运行中实例的制图任务进度（运行／排队数、ComfyUI 版本）经 `/api/v1/instances/{name}/comfy/status` 代理实时展示，并提供「打开 ComfyUI」按钮新窗口跳转真实应用——平台只做环境与部署管理，制图操作在实例应用内完成；模型中心展示共享仓库实时扫描结果并支持界面删除真实文件，URL 下载与本地导入走同一真实任务引擎；任务中心展示真实下载／导入任务。后端连接失败时侧栏与顶栏明确显示离线状态，所有操作入口拦截提示，不展示任何模拟数据。

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

验证覆盖后端版本、真实 Docker API、完整硬件信息、2 秒遥测刷新、DGX 两种内存架构、十六卡渲染、五个页面、弹窗不透明度、实例真实生命周期、模型市场（目录接口＋选中即下载真实链路）与 URL 下载／本地导入、系统页联调、诊断导出及 375px 视口。截图和结果写入被 Git 忽略的 `test-results/`；可通过 `CHROME_PATH` 和 `FLOWPILOT_DEMO_URL` 指定浏览器及访问地址。

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

## 离线安装与一键卸载

面向目标服务器的生产安装使用 `code/scripts/` 下的离线包流程，无需 sudo、不修改系统包：

```bash
# 打包（在仓库根目录执行，产物在 .tmp/release/）
code/scripts/package-release.sh

# 目标服务器安装（默认容器 flowpilot-web-demo，端口 18080）
/tmp/flowpilot-install/scripts/install.sh

# 一键卸载（只删除 FlowPilot 容器、镜像和用户目录）
~/.local/share/flowpilot/bin/flowpilot-uninstall --yes
```

支持 `FLOWPILOT_HOME`、`FLOWPILOT_CONTAINER`、`FLOWPILOT_PORT` 环境变量覆盖默认值。已在 DGX Spark（Ubuntu 24.04 arm64）实测，远程回归命令：`FLOWPILOT_DEMO_URL=http://192.168.0.77:18080 npm run test:demo`。
