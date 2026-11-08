# FlowPilot 变更日志

所有重要变更均记录于此。详细内容见 `context/changes/`。

## 0.0.3.0 — 2026-11-08

### 新增

- 下载任务引擎：`POST /api/v1/downloads` 创建真实 URL 下载（类别映射 ComfyUI 仓库子目录，文件名可留空从地址推导，可选 SHA256 校验），`POST /api/v1/downloads/{id}/pause|resume|cancel|retry`、`GET /api/v1/downloads`、`DELETE` 删除记录；断点续传（Range／206），暂停保留分片，取消清理分片，完成后归档入共享仓库。
- 前端真实下载链路：添加模型弹窗真实模式（说明文案、名称可留空）；模型中心合并展示进行中下载（进度、暂停／继续、移除任务）与仓库文件；任务中心展示真实下载任务（进度、取消、重试）。

### 修复

- 已完成／失败的下载任务记录不再阻塞同目标重新下载（仅进行中的任务占用目标路径）。

### 验证

- Go 七包测试全过（新增 download 包 5 用例：完整下载＋校验、暂停续传、校验失败、参数与冲突、取消清理）；前端 lint／build 通过。
- DGX Spark 实测：界面创建 `http://localhost:18080/index.html` 下载任务→写入共享仓库→界面删除；任务中心显示真实下载任务；28 项远程回归全部通过；`comfyui-h3` 不受影响，测试产物全部清理。

详细记录：[0.0.3.0](context/changes/0.0.3.0.md)

## 0.0.2.0 — 2026-11-08

### 新增

- 模型中心接入真实共享仓库：`GET /api/v1/models` 扫描宿主机模型目录（第一层子目录映射 ComfyUI 类别：checkpoints／loras／vae／controlnet／embeddings／upscale_models），`DELETE /api/v1/models?path=` 物理删除文件并拒绝路径穿越；前端真实模式 3 秒轮询，展示文件数、仓库占用与根目录，界面可直接删除真实文件。
- 镜像预置流程：`GET /api/v1/images/check` 检查本地镜像，`POST /api/v1/images/pull` 后台流式拉取（同一时刻仅一个任务），`GET /api/v1/images/pull` 查询进度；创建实例向导实时检查镜像存在性，缺失时可一键在线拉取并显示进度。
- Docker 客户端新增无超时的流式通道与 `PullImage`，逐行解析拉取进度事件。

### 验证

- Go 六包测试全过（新增 model 扫描／删除／穿越防护 4 用例、image 拉取生命周期 3 用例、server 模型接口用例）；前端 lint／build 通过。
- DGX Spark 实测：预置 `checkpoints/regress-test.safetensors` 种子文件，界面展示（Checkpoints · 4.0 MB）并通过界面真实删除；以 `quay.io/gpustack/runtime:pause` 完成后台拉取全链路；26 项远程回归全部通过；`comfyui-h3` 不受影响，测试产物全部清理。

详细记录：[0.0.2.0](context/changes/0.0.2.0.md)

## 0.0.1.0 — 2026-11-08

### 新增

- 实例管理接入真实 Docker API：新增 `GET/POST /api/v1/instances`、`POST /api/v1/instances/{name}/start|stop|restart`、`DELETE /api/v1/instances/{name}?force=1`，实例为带 `flowpilot.instance=true` 标签的容器，配置全部存标签，无数据库。
- 创建实例：名称／端口／GPU 冲突与合法性校验（正则、1024–65535、GPU 越界、镜像必须本地存在），GPU 分配通过 `DeviceRequests`（nvidia 驱动＋DeviceIDs），支持"不分配 GPU（仅 CPU）"，自动挂载共享数据目录 `{models,workflows,outputs}`，启动失败自动删除容器回滚。
- 前端实例页真实模式：3 秒轮询真实实例列表，GPU 列显示真实型号（如 NVIDIA GB10），创建弹窗支持容器镜像、访问端口、容器端口与真实 GPU 下拉选择，"打开 ComfyUI"直达实例端口。
- 新增环境变量：`FLOWPILOT_HOST_DATA_DIR`（宿主机共享数据目录）、`FLOWPILOT_COMFYUI_IMAGE`（默认 `comfyui:latest`）、`FLOWPILOT_COMFYUI_CONTAINER_PORT`（默认 8188）。

### 验证

- Go 四包测试全过（新增实例管理 7 个用例与 API 映射用例）；前端 lint／build 通过。
- DGX Spark 实测：以 `flowpilot-web:0.0.1.0` 自身镜像作为实例镜像（Spark 上暂无 ComfyUI 镜像，容器端口 18080），通过界面完成 `regress-01` 的创建（GPU 0／端口 18188）、访问验证、停止、启动、删除全生命周期；21 项远程回归全部通过；业务容器 `comfyui-h3` 不受影响。

详细记录：[0.0.1.0](context/changes/0.0.1.0.md)

## 0.0.0.6 — 2026-11-08

### 修复

- 删除 Vite 脚手架残留的死代码 `pages/Home.tsx` 与 `App.css`。
- 修复硬件面板在采集失败（总量为 0）时内存、存储百分比产生 NaN 的除零问题，统一 `safePercent`。
- 修复 `getHardware` 轮询无超时、请求可能堆积的问题，增加 4 秒超时。
- 修复后端 CPU 利用率在计数器抖动时可能越界的问题，钳制到 0–100 并补充回归用例。
- 修复侧栏、页头、系统页和诊断导出写死"慧浪 GPU-S01 · 2 × L40S"的展示错误：真实硬件可用时显示真实主机名与 GPU 概要，系统页改用真实 Docker 主机名。
- 系统页"硬件仍为 Demo"的过时文案更正为硬件实时采集说明。

### 新增

- 硬件接口返回 `machineModel`（DMI 产品名，安装脚本只读挂载），真实面板显示机型，如"NVIDIA DGX Spark · 实时采集"。

### 验证

- Go 测试新增 DMI 机型与 CPU 钳制用例，三包全过；前端 lint／build 通过。
- DGX Spark 实测：`machineModel=NVIDIA DGX Spark`，侧栏／页头／面板均显示真实主机信息；18 项远程回归全部通过；`comfyui-h3` 保持运行。

详细记录：[0.0.0.6](context/changes/0.0.0.6.md)

## 0.0.0.5 — 2026-11-07

### 新增

- 新增真实硬件采集接口 `GET /api/v1/hardware`：nvidia-smi GPU 指标（型号、利用率、温度、功耗、显存）＋ `/proc` 主机指标（CPU、内存）＋ 数据盘 statfs，纯 Go 标准库实现。
- 统一内存架构自动识别：GB10 等 `nvidia-smi` 显存返回 `[N/A]` 的机型标记为 unified，显存字段降级为系统统一内存。
- 前端硬件面板接入真实接口，2 秒轮询；采集成功显示"本机实测"，失败自动回退 Demo 并明确标注；不可用的 GPU 指标显示为"—"。
- 安装脚本新增 GPU 透传：优先 `--gpus all`（NVIDIA Container Toolkit），缺失时手动挂载宿主机 nvidia-smi、libnvidia-ml 和 `/dev/nvidia*` 设备。

### 调整

- 一体化镜像运行基底由 Alpine 改为 Ubuntu 24.04，保证宿主机 glibc 链接的 nvidia-smi 可在容器内执行；离线发布包由约 6.7 MB 增至约 33 MB。
- 容器新增挂载：共享数据目录 `/data/flowpilot`、只读宿主机信息 `/host/etc/hostname`、`/host/etc/os-release`。
- 回归脚本新增真实硬件分支断言，Demo 分支全部保留。

### 验证

- Go `go test ./...`、`go vet ./...` 通过（hardware 包 8 个用例）；前端 `lint`／`build` 通过。
- DGX Spark 实测：`/api/v1/hardware` 返回真实数据（spark-6558、NVIDIA GB10、unified、38℃、10.78 W、121.7 GB 统一内存、3.67 TB 数据盘）。
- `FLOWPILOT_DEMO_URL=http://192.168.0.77:18080 npm run test:demo`：18 项检查全部通过，含真实硬件面板断言。
- 原业务容器 `comfyui-h3` 保持 `running Paused=false`。

详细记录：[0.0.0.5](context/changes/0.0.0.5.md)

## 0.0.0.4 — 2026-11-07

### 新增

- 新增离线打包、用户目录安装、一键卸载和远程部署脚本：`package-release.sh`、`install.sh`、`uninstall.sh`、`deploy-remote.sh`。
- 安装脚本无需 sudo、不修改系统包，数据统一放入用户目录（默认 `~/.local/share/flowpilot`）。
- 卸载脚本需 `--yes` 或交互确认，只删除 FlowPilot 容器、镜像和用户目录；通过 `flowpilot.managed=true` 容器标签判断镜像是否仍被其他 FlowPilot 实例共享。
- 安装目录设白名单保护，只允许删除 `~/.local/share/flowpilot` 及其 `flowpilot-*` 变体，防止误删系统目录。

### 验证

- 在 DGX Spark（`spark-6558`，Ubuntu 24.04.4 LTS arm64，NVIDIA GB10，Docker 29.2.1／API 1.53）完成真实远程安装、API 联调和 18 项界面回归，全部通过。
- 沙箱卸载验证：共享镜像正确保留；完整卸载验证：容器、镜像、目录全部清理；重装后 `running healthy`。
- 原业务容器 `comfyui-h3` 测试期间曾暂停，最终恢复运行且未被卸载流程影响。

详细记录：[0.0.0.4](context/changes/0.0.0.4.md)

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
