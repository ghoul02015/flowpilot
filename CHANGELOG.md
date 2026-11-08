# FlowPilot 变更日志

所有重要变更均记录于此。详细内容见 `context/changes/`。

## 0.0.6.2 — 2026-11-08（修复：全词搜索空结果＋弹窗横向溢出）

### 修复（周董实测发现）

- **全词搜索空结果**：实测「Minimax」空结果而「Mini」有结果。排查确认 ModelScope 搜索本身大小写不敏感且后端链路稳定（连续 6 次请求全部命中 20 条），定位为源站风控间歇返回 Code=200 空列表；后端改为空结果自动等待 600ms 重试一次，前端空结果提示补充「尝试更短关键词或切换源」引导。
- **弹窗内容横向溢出 UI**：在线搜索结果中长文件名（如 `minimax_h3_fl2v_turbo_4step_v1.0_768p_comfyui_bf16.safetensors`，truncate＝nowrap）经 grid 子项 `min-width:auto` 撑宽弹窗内容盒——实测弹窗 672px 被撑出 821px 内容（scrollWidth 845），标签栏／筛选器／卡片整体溢出右边界。修复：DialogContent 统一加 `[&>*]:min-w-0`（所有弹窗生效），市场面板根部加 `min-w-0`。修复后实测 scrollWidth=670=clientWidth。

### 验证

- 复现脚本实测溢出数据（672→821px）→ 修复后复测归零；回归脚本新增两项永久断言：「全词搜索 Minimax 命中」「弹窗无横向溢出」。
- Go 九包测试、前端 lint／build 通过；DGX Spark 实机 48 项远程回归全部通过。

详细记录：[0.0.6.2](context/changes/0.0.6.2.md)

## 0.0.6.1 — 2026-11-08（模型市场在线源：双源切换＋全站搜索）

### 新增

- **源切换与连接状态**：模型市场新增「推荐目录／ModelScope／HF 镜像」三个源选项，在线源实时显示连接状态点（绿＝可达＋延迟，红＝不可达）；`GET /api/v1/sources/status` 并发探测双源，60 秒缓存。
- **在线搜索（免维护目录）**：`GET /api/v1/models/online/search` 代理 ModelScope（PUT dolphin/models）与 HF 镜像（api/models）全站搜索，`GET /api/v1/models/online/files` 递归列出仓库权重文件（.safetensors/.pth/.gguf 等，真实大小按降序）。源站模型更新即所见，平台无需发版维护目录。
- **界面在线链路**：搜索仓库 → 展开文件列表 → 按类型筛选 → 单文件一键下载；文件名（ComfyUI 模板引用名）突出显示；类别按 ComfyUI 目录约定自动推断（覆盖 Comfy-Org 布局与 HF 的 FL2VA/text_encoder 中间段）。
- **推荐目录扩充至 19 条**：新增 MiniMax H3 全套（FL2VA FP8／W6A8 16G 优化、Qwen3VL 32B NVFP4 编码器、视频 VAE、Turbo 4 步 LoRA，Comfy-Org 官方打包仓库，与 ComfyUI 官方模板命名一致）；目录刷新改递归文件列表以覆盖子目录仓库。

### 验证

- Go 九包测试新增在线源 8 用例（双源搜索解析、文件列表与类别推断、状态探测与缓存、超时收口、参数校验），全部通过。
- DGX Spark 实机 46 项远程回归全部通过：双源状态（ModelScope 317ms／HF 860ms）、MiniMax-H3 搜索命中 Comfy-Org 首位、仓库文件列表真实大小展示，及既有 42 项链路。

详细记录：[0.0.6.1](context/changes/0.0.6.1.md)

## 0.0.6.0 — 2026-11-08（模型市场：ModelScope 目录，选中即下载）

### 新增

- **模型市场**：添加模型对话框改为三标签（模型市场／URL 下载／本地导入），默认展示 14 条内置主流模型目录（SD1.5、SDXL、FLUX.1 dev/schnell 及配套 VAE/CLIP-L/T5-XXL、SD3.5 Large、SD VAE、ControlNet Canny、Wan2.1 文生视频及 VAE、RealESRGAN x4），支持搜索与类型筛选，点击「下载」即按条目类别与文件名直接创建真实下载任务，无需手填地址。
- **ModelScope 优先、HF 镜像备选**：所有条目主地址为 ModelScope 直链（`modelscope.cn/models/.../resolve/master/...`，已逐条实测 200），国内服务器开箱可用；每条目附 hf-mirror.com 备选地址，FLUX.1 dev 与 SD3.5 Large 等 HF 受限条目标注「需授权」。
- **目录在线更新**：`GET /api/v1/models/catalog` 返回目录（缓存优先、与内置合并），`POST /api/v1/models/catalog/refresh` 并发查询 ModelScope 文件列表 API，刷新各条目真实文件大小与可达性（`reachable`），结果原子写入 `shared/catalog.json`，重装保留、离线自动回落内置目录。前端「更新目录」按钮一键触发。
- 已入库条目在市场中自动标注，避免重复下载；新增 Text Encoders／Diffusion Models 类别，分别落入仓库 `text_encoders/`、`diffusion_models/` 子目录。

### 修复（实机回归发现）

- 回归脚本按钮选择器歧义：HF 镜像按钮的 aria-label 包含主下载按钮同名前缀，strict mode 命中两个元素，改为精确匹配。
- 回归市场下载后的界面删除存在时序竞争：界面列表每 3 秒刷新、滞后于 API 轮询，点击删除时同名 dl- 下载任务行尚未消失，误触「取消任务」（409）而非删除文件；现等待任务行消失、仅剩仓库文件行后再操作。

### 验证

- Go 九包测试（新增 catalog 包 5 用例＋目录接口 2 用例）、前端 lint／build 通过。
- DGX Spark 实机 42 项远程回归全部通过：含 49.5MB SDXL Offset LoRA 从 ModelScope 真实下载、入库、界面删除全链路；目录接口断言 14 条且主地址全部为 ModelScope。
- 在线刷新实测：14 条全部 reachable，文件大小更新为 ModelScope API 真实值（如 SD1.5 3.97→4.27GB），缓存落盘 `shared/catalog.json`。

详细记录：[0.0.6.0](context/changes/0.0.6.0.md)

## 0.0.5.1 — 2026-11-08（去除全部演示数据，真实生产显示）

### 变更

- **移除全部虚拟演示数据**：删除 `data/mock.ts`（模拟实例／模型／任务）与 `data/hardware.ts`（5 个演示机型），前端不再内置任何模拟业务数据；页面完全由真实后端接口驱动，接口不可用时显示空态与明确的离线提示（侧栏状态点、顶栏「后端连接失败」徽标），不再回退演示数据。
- 侧栏「重置演示数据」入口与顶栏「Demo · 本地模拟数据」徽标移除；新建实例向导第二步改为共享仓库模型可见性说明（真实实例自动可见全部就绪模型，不再做虚假的选择语义）；添加模型对话框不再预填示例地址；系统管理移除不生效的「系统设置」假表单与写死的「操作日志」假数据，环境报告全部改为真实硬件采集行。
- 后端不可用时所有操作入口统一拦截并提示「后端未连接」。

### 修复（评审发现）

- 首页「最近任务」此前展示的是浏览器模拟任务而非真实下载任务（传错数据源），系统页诊断导出同理使用模拟实例数据。
- App 启动时 GPU 列表仅单次获取且无重试：硬件接口冷启动超时（Spark 上 nvidia-smi 首次调用慢）会导致整个会话无法创建 GPU 实例，现改为失败每 3 秒自动重试。
- 首页问候语按当前时段动态生成（此前固定「下午好」）。

### 验证

- Go 八包测试、前端 lint／build 通过。
- DGX Spark 实机 38 项远程回归全部通过（新增：界面无演示数据标识、实例列表空态展示；演示机型回退、模拟创建／下载流程等演示分支断言全部移除，接口不可用时回归直接失败）。
- 排障记录：回归暴露机器上遗留手工实例 `comfy-01`（0.0.5.0 部署后界面点击创建）占用 GPU 0，经 API 删除后全绿；实例删除不影响共享模型仓库。

详细记录：[0.0.5.1](context/changes/0.0.5.1.md)

## 0.0.5.0 — 2026-11-08（任务持久化＋制图进度＋应用跳转）

### 新增

- **任务持久化**：下载／导入任务落盘 `shared/tasks.json`（原子写入，0600），平台重启／容器重建后任务列表不丢；重启时进行中的任务自动转为暂停并提示"平台重启，可断点续传／可继续"，恢复后从 `.part` 分片续传；任务 ID 序号随文件延续不回退。
- **ComfyUI 制图状态代理**：`GET /api/v1/instances/{name}/comfy/status` 经平台后端探测运行中实例的 `/queue` 与 `/system_stats`，返回服务可达性、运行中／排队任务数与 ComfyUI 版本（4 秒超时，未运行实例直接返回不可达）；容器通过 `--add-host host.docker.internal:host-gateway` 访问宿主机映射端口，`FLOWPILOT_HOST_GATEWAY` 可覆盖。
- **前端制图进度与一键跳转**：实例列表「制图任务」列实时展示 运行 X · 排队 Y／空闲／服务启动中（3 秒轮询，多实例并行探测）；运行中实例提供「打开 ComfyUI」按钮，新窗口直达 `http://<主机>:<端口>` 的真实应用；首页实例概览同步展示制图任务列。定位明确：FlowPilot 做环境与部署管理，制图操作跳转真实 ComfyUI 应用。

### 验证

- Go 八包测试全过（新增：任务跨重启恢复＋续传＋ID 延续、comfy/status 运行／停止两态代理用例）；前端 lint／build 通过。
- DGX Spark 实机持久化链路实测：120 MB 下载至 40% 时 `docker restart` → 任务恢复为暂停（文案正确）→ 一键继续 → SHA256 校验通过入库。
- 37 项远程回归全部通过（新增：状态代理 reachable＋版本、制图任务徽标、打开应用跳转正确端口）；原机业务容器按周董要求保持停止，测试产物全部清理。

详细记录：[0.0.5.0](context/changes/0.0.5.0.md)

## 0.0.4.1 — 2026-11-08（测试评审补丁）

### 修复

- **实例启动 GPU 冲突**（评审发现的真 bug）：`start`／`restart` 前校验目标 GPU 未被其他运行中实例占用——此前创建时校验可被"停止后另建实例"绕过，导致两实例共用一卡。
- 模型仓库挂载改为只读（对齐 PRD 10.1），实例容器内无法再改动或删除共享模型；工作流与输出目录保持可写。
- 下载归档前复核目标路径，不再静默覆盖用户在下载期间手工放入仓库的同名文件。
- 导入任务文案与下载区分（"导入完成"／暂停提示不再误称"断点续传"）。
- 下载 HTTP 客户端补充连接／TLS／响应头超时，对端挂起时任务不再永远卡在"正在建立下载连接"。
- 模型仓库扫描增加 2 秒缓存＋删除主动失效，消除界面轮询对大仓库的持续全盘 IO。
- 卸载脚本清理全部历史版本 `flowpilot-web:*` 镜像（无遗留 FlowPilot 容器时），避免历次升级旧镜像在客户机器上累积。

### 验证

- Go 八包测试全过（新增：启动 GPU 冲突、只读挂载断言、归档不覆盖手工文件、扫描缓存与失效）。
- DGX Spark 实机边界测试 **33 项全过**（非法输入 15 项、冲突与生命周期 11 项、下载导入 7 项）；只读挂载容器内实测；卸载→重装→卸载全链路（8 个历史镜像清零、业务容器 comfyui-h3／gpustack 无恙）。
- 34 项远程回归复跑全部通过。

详细记录：[0.0.4.1](context/changes/0.0.4.1.md)

## 0.0.4.0 — 2026-11-08

### 新增

- 本地导入真实化：`POST /api/v1/imports` 将服务器本地文件（默认白名单目录 `/data/import`，`FLOWPILOT_IMPORT_DIRS` 可配置）任务化复制入共享仓库，与下载共用任务模型（kind=import，进度、暂停、SHA256 校验、目标冲突检测一致）；前端添加模型弹窗"本地导入"接入真实接口。
- ComfyUI 镜像预置策略：install.sh 自动探测默认实例镜像（`FLOWPILOT_COMFYUI_IMAGE` 指定 → `comfyui-h3:spark` → `comfyui:latest`），并读取镜像 WorkingDir 自动适配挂载目标（如 `/app/ComfyUI/{models,output,user/workflows}`），新实例开箱即见平台模型库；`FLOWPILOT_COMFYUI_MODELS_TARGET/OUTPUTS_TARGET/WORKFLOWS_TARGET` 可覆盖，实例创建时亦可单独指定。

### 修复

- 大容器停止返回 502：停止请求改用无全局超时通道（上限为优雅停止时长＋15 秒），且请求失败时回查容器真实状态，已退出视为成功，停止操作幂等（ComfyUI 类容器不响应 SIGTERM，Docker 10 秒后 SIGKILL，此前客户端 10 秒超时先于 Docker 返回）。
- 回归套件消除两处时序脆弱点：仓库扫描缓存窗口改为轮询收敛；任务中心历史任务重名匹配取第一条。

### 验证

- Go 八包测试全过（download 新增导入正／反用例，docker 新增停止状态回查用例）；前端 lint／build 通过。
- DGX Spark 实测 34 项远程回归全部通过，新增：本地导入写入仓库＋界面删除、ComfyUI 实例（comfyui-h3:spark 预置镜像）创建→/system_stats 就绪→停止→删除。
- 出图闭环实测：平台下载引擎拉取 SD1.5（4.27 GB，hf-mirror）→ API 创建实例（GPU 0、端口 18190）→ 提交 20 步工作流真实出图（宇航员骑马，512×512）→ 实例、模型、任务全部清理；生产容器 `comfyui-h3` 全程不受影响。

详细记录：[0.0.4.0](context/changes/0.0.4.0.md)

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
