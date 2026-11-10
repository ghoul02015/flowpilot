# FlowPilot 后端

当前版本：`0.0.9.4`。

轻量 Go 服务同时提供前端静态页面和 Docker API。仅使用 Go 标准库，通过 `/var/run/docker.sock` 访问 Docker Engine，不引入第三方 Docker SDK。

## 配置

| 环境变量 | 默认值 | 用途 |
|---|---|---|
| `FLOWPILOT_ADDR` | `:18080` | HTTP 监听地址 |
| `FLOWPILOT_STATIC_DIR` | `./web` | 前端构建产物目录 |
| `FLOWPILOT_DOCKER_SOCKET` | `/var/run/docker.sock` | Docker Engine Unix Socket |
| `FLOWPILOT_DATA_DIR` | `./data` | 平台数据目录（容器内路径） |
| `FLOWPILOT_HOST_DATA_DIR` | 回落 `FLOWPILOT_DATA_DIR` | 宿主机共享数据目录，用于实例容器挂载 |
| `FLOWPILOT_COMFYUI_IMAGE` | `comfyui:latest` | 新建实例默认镜像（install.sh 自动探测注入） |
| `FLOWPILOT_COMFYUI_CONTAINER_PORT` | `8188` | ComfyUI 容器内服务端口 |
| `FLOWPILOT_COMFYUI_MODELS_TARGET` | `/data/models` | 实例内模型仓库挂载目标（install.sh 按镜像 WorkingDir 自适应） |
| `FLOWPILOT_COMFYUI_OUTPUTS_TARGET` | `/data/outputs` | 实例内输出目录挂载目标 |
| `FLOWPILOT_COMFYUI_WORKFLOWS_TARGET` | `/data/workflows` | 实例内工作流目录挂载目标 |
| `FLOWPILOT_IMPORT_DIRS` | `/data/import` | 本地导入白名单目录（冒号分隔多个） |
| `FLOWPILOT_HOST_GATEWAY` | `host.docker.internal` | 制图状态代理访问宿主机映射端口的网关主机名（容器需 `--add-host host.docker.internal:host-gateway`，安装脚本已内置） |

## API

- `GET /api/v1/health`：后端健康状态和版本。
- `GET /api/v1/hardware`：真实宿主机硬件采集——nvidia-smi GPU 指标（统一内存架构显存字段为 `null`）、CPU／内存（/proc）、数据盘用量（statfs）；GPU 不可用时单项降级。
- `GET /api/v1/system/info`：当前管理服务的 Go 运行环境，不代表宿主机完整硬件或 GPU 信息。
- `GET /api/v1/docker/info`：真实 Docker `/info` 与 `/version` 摘要。
- `GET /api/v1/docker/containers`：包含已停止容器的真实 Docker 容器列表。

### 实例管理（真实 Docker 生命周期）

实例即带 `flowpilot.instance=true` 标签的 Docker 容器（命名 `flowpilot-comfyui-<name>`），名称、GPU、端口、自启动配置全部存标签，无需数据库；只管理本标签容器，绝不触碰宿主机其他容器。

- `GET /api/v1/instances`：实例列表＋默认配置 `{instances, defaults}`。
- `POST /api/v1/instances`：创建实例。请求体 `{name, image?, gpu, port, containerPort?, autoStart?, modelsTarget?, outputsTarget?, workflowsTarget?}`；校验名称正则、端口 1024–65535、GPU 越界、名称/端口/GPU 冲突、镜像本地存在；`gpu=-1` 表示仅 CPU；自动挂载 `<hostDataDir>/{models,workflows,outputs}` 到实例内目标路径（默认 `/data/*`，install.sh 按镜像 WorkingDir 自适应为如 `/app/ComfyUI/*`；models 只读挂载，workflows/outputs 可写）；启动失败自动删除容器回滚。
- `POST /api/v1/instances/{name}/start|stop|restart`：启停与重启；启动/重启前校验 GPU 未被其他运行中实例占用；停止请求不套用默认客户端超时，失败时回查容器真实状态（已退出视为成功，幂等）。
- `DELETE /api/v1/instances/{name}?force=1`：删除（运行中需 `force=1`）。
- `GET /api/v1/instances/{name}/comfy/status`：ComfyUI 制图状态代理——运行中实例经 `http://<FLOWPILOT_HOST_GATEWAY>:<端口>` 探测 `/queue` 与 `/system_stats`，返回 `{reachable, instanceStatus, queueRunning, queuePending, comfyuiVersion}`；未运行实例直接返回 `reachable=false`；整体 4 秒超时、单次响应限 1MB。

### 模型仓库（真实目录扫描）

模型列表即共享仓库目录（容器内 `<FLOWPILOT_DATA_DIR>/models`，宿主机 `~/.local/share/flowpilot/shared/models`）的实时扫描结果，文件即真相、无数据库；第一层子目录映射 ComfyUI 类别约定（checkpoints／loras／vae／controlnet／embeddings／upscale_models）。扫描结果缓存 2 秒以吸收界面轮询压力，删除操作主动失效缓存。

- `GET /api/v1/models`：扫描结果 `{root, files[], totalBytes, scannedAt}`。
- `DELETE /api/v1/models?path=<相对路径>`：物理删除文件，拒绝路径穿越与目录删除。

### 模型目录（模型市场，0.0.6.0 新增）

内置 14 条主流模型目录（SD1.5／SDXL／FLUX.1 系列／SD3.5／VAE／ControlNet／Wan2.1／RealESRGAN），主地址为 ModelScope 直链（国内可达），备选为 hf-mirror.com（HF 受限条目标注 `fallbackGated`）。缓存优先、与内置合并；在线刷新并发查询 ModelScope 文件列表 API，更新真实文件大小与可达性，原子写入 `<FLOWPILOT_DATA_DIR>/catalog.json`（重装保留，离线回落内置）。

- `GET /api/v1/models/catalog`：目录 `{entries[], refreshedAt, source: builtin|cache}`。
- `POST /api/v1/models/catalog/refresh`：在线刷新（25 秒超时，刷新中重复提交返回 409）。

### 在线模型源（0.0.6.1 新增）

目录在线化：源站搜索与文件列表实时代理，源站更新即所见，无需发版维护。ModelScope 搜索为 `PUT /api/v1/dolphin/models`（抓包实测），文件列表 `Recursive=true` 覆盖 Comfy-Org 子目录布局；HF 走 hf-mirror.com 标准 Hub API。类别按 ComfyUI 目录约定从路径自动推断（含 FL2VA/text_encoder 中间段）。参数错误 400、上游失败 502。

- `GET /api/v1/sources/status`：双源实时可达性 `{sources[]}`（并发探测、5 秒超时、60 秒缓存）。
- `GET /api/v1/models/online/search?source=modelscope|hf&q=<关键词>`：归一化仓库结果 `{results[], attempts[]}`（repo／下载量／更新时间；attempts 记录实际尝试过的关键词，空结果自动退避重试 2 次并尝试模糊变体）。输入为 `org/name` 或源站链接时前端直达文件列表（`catalog.ParseRepoRef`）。
- `GET /api/v1/models/online/files?source=...&repo=<org/name>`：权重文件列表 `{files[]}`（真实大小降序、推断类别、直链地址，上限 100 条）。

### 容器镜像管理与构建（0.0.7.4）

- `GET /api/v1/images/check?image=<镜像>`：本地是否存在。
- `GET /api/v1/images/local`：本机 Docker 镜像列表，返回 Docker 标签与 Labels；前端按 `flowpilot.kind=comfyui` 或标签名将其分为 ComfyUI 镜像和其他镜像。
- `DELETE /api/v1/images/local?image=<镜像>[&force=1]`：删除本机镜像标签；镜像被容器使用时由 Docker 拒绝，除非显式 `force=1`。
- `POST /api/v1/images/tag` `{source, target}`：给已有镜像增加/修改标签。
- `GET /api/v1/images/export?image=<镜像>`：以 `docker save` tar 流导出镜像。
- `POST /api/v1/images/import`：multipart 上传 `file`，以 `docker load` 导入镜像 tar。
- `GET /api/v1/images/catalog`：六套 ComfyUI 构建配方（NVIDIA cu130/cu128/cu126/cu121、AMD ROCm、CPU）。
- `GET /api/v1/images/comfyui-refs`：在线获取 ComfyUI `master` 与最近标签；网络失败时兜底 `master`。
- `GET /api/v1/images/build-script?variant=&image=&ref=&pip=&manager=&platform=`：下载同参数一键构建脚本，仅构建镜像，不安装 FlowPilot、不下载模型。
- `POST /api/v1/images/build` `{variant,image,ref,pip,manager,platform}`：按配方后台构建 ComfyUI 镜像，同一时刻仅一个构建任务；`platform` 支持 `host`、`linux/arm64`、`linux/amd64`、`linux/arm/v7`。
- `GET /api/v1/images/build`：构建状态、进度、参数简述与最近日志 `{image, variant, state, message, progress, summary[], logs[], updatedAt}`。
- `POST /api/v1/images/pull` `{image}`：后台流式拉取，同一时刻仅一个拉取任务（重复提交返回 409）。
- `GET /api/v1/images/pull`：拉取状态 `{image, state: idle|pulling|done|error, message, updatedAt}`。
- `GET /api/v1/images/install-script`：保留独立分发的一键安装脚本下载；Web 新建实例向导不再承载构建/拉取入口。

### 下载任务（真实下载引擎）

下载与导入任务持久化到 `<FLOWPILOT_DATA_DIR>/tasks.json`（宿主机 `~/.local/share/flowpilot/shared/tasks.json`，原子写入、0600）：注册／状态迁移／暂停／取消／删除时落盘，进度推进不落盘（恢复进度以 `.part` 分片文件偏移为准）；平台重启后终态任务原样恢复，进行中的任务统一转为暂停并提示"平台重启……可断点续传／可继续"，任务 ID 序号随文件延续。分片写入 `<仓库>/.downloading/`，支持 Range 断点续传与可选 SHA256 校验，完成后归档入共享仓库。

- `GET /api/v1/downloads`：任务列表 `{tasks[]}`（含下载与导入，`kind` 区分）。
- `POST /api/v1/downloads` `{url, category, name?, sha256?}`：创建下载；`name` 留空从 URL 推导；`category` 即仓库子目录（`other` 落根目录）。
- `POST /api/v1/downloads/{id}/pause|resume|cancel|retry`：暂停（保留分片）、继续（断点续传）、取消（清理分片）、重试。
- `DELETE /api/v1/downloads/{id}`：删除终态任务记录。

### 本地导入（真实复制引擎）

- `POST /api/v1/imports` `{path, category, name?, sha256?}`：将白名单目录（`FLOWPILOT_IMPORT_DIRS`，默认 `/data/import`，即宿主机安装目录下 `import/`）内的文件任务化复制入共享仓库；与下载共用任务模型（`kind=import`），支持暂停与 SHA256 校验；源路径越出白名单或非普通文件一律拒绝。

## 本机无 Go 环境时验证

在项目根目录执行：

```bash
docker run --rm -v "$PWD/code/backend:/src" -w /src golang:1.25-alpine sh -c 'gofmt -w ./cmd ./internal && go test ./... && go vet ./...'
```

## 当前边界

此版本没有认证、用户权限和数据库。挂载 Docker Socket 等同于授予容器较高的 Docker 管理权限，因此服务仅应监听受信任的本机或管理网络；生产版本需要收紧接口权限并补充认证。实例自启动开关在创建时确定，运行中修改待后续版本补充。
