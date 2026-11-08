# FlowPilot 后端

当前版本：`0.0.4.0`。

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

## API

- `GET /api/v1/health`：后端健康状态和版本。
- `GET /api/v1/hardware`：真实宿主机硬件采集——nvidia-smi GPU 指标（统一内存架构显存字段为 `null`）、CPU／内存（/proc）、数据盘用量（statfs）；GPU 不可用时单项降级。
- `GET /api/v1/system/info`：当前管理服务的 Go 运行环境，不代表宿主机完整硬件或 GPU 信息。
- `GET /api/v1/docker/info`：真实 Docker `/info` 与 `/version` 摘要。
- `GET /api/v1/docker/containers`：包含已停止容器的真实 Docker 容器列表。

### 实例管理（真实 Docker 生命周期）

实例即带 `flowpilot.instance=true` 标签的 Docker 容器（命名 `flowpilot-comfyui-<name>`），名称、GPU、端口、自启动配置全部存标签，无需数据库；只管理本标签容器，绝不触碰宿主机其他容器。

- `GET /api/v1/instances`：实例列表＋默认配置 `{instances, defaults}`。
- `POST /api/v1/instances`：创建实例。请求体 `{name, image?, gpu, port, containerPort?, autoStart?, modelsTarget?, outputsTarget?, workflowsTarget?}`；校验名称正则、端口 1024–65535、GPU 越界、名称/端口/GPU 冲突、镜像本地存在；`gpu=-1` 表示仅 CPU；自动挂载 `<hostDataDir>/{models,workflows,outputs}` 到实例内目标路径（默认 `/data/*`，install.sh 按镜像 WorkingDir 自适应为如 `/app/ComfyUI/*`）；启动失败自动删除容器回滚。
- `POST /api/v1/instances/{name}/start|stop|restart`：启停与重启；停止请求不套用默认客户端超时，失败时回查容器真实状态（已退出视为成功，幂等）。
- `DELETE /api/v1/instances/{name}?force=1`：删除（运行中需 `force=1`）。

### 模型仓库（真实目录扫描）

模型列表即共享仓库目录（容器内 `<FLOWPILOT_DATA_DIR>/models`，宿主机 `~/.local/share/flowpilot/shared/models`）的实时扫描结果，文件即真相、无数据库；第一层子目录映射 ComfyUI 类别约定（checkpoints／loras／vae／controlnet／embeddings／upscale_models）。

- `GET /api/v1/models`：扫描结果 `{root, files[], totalBytes, scannedAt}`。
- `DELETE /api/v1/models?path=<相对路径>`：物理删除文件，拒绝路径穿越与目录删除。

### 镜像预置（后台拉取）

- `GET /api/v1/images/check?image=<镜像>`：本地是否存在。
- `POST /api/v1/images/pull` `{image}`：后台流式拉取，同一时刻仅一个任务（重复提交返回 409）。
- `GET /api/v1/images/pull`：拉取状态 `{image, state: idle|pulling|done|error, message, updatedAt}`。

### 下载任务（真实下载引擎）

URL 下载任务保存在内存中（重启后记录清空，仓库文件不受影响）；分片写入 `<仓库>/.downloading/`，支持 Range 断点续传与可选 SHA256 校验，完成后归档入共享仓库。

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
