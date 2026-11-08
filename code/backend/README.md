# FlowPilot 后端

当前版本：`0.0.2.0`。

轻量 Go 服务同时提供前端静态页面和 Docker API。仅使用 Go 标准库，通过 `/var/run/docker.sock` 访问 Docker Engine，不引入第三方 Docker SDK。

## 配置

| 环境变量 | 默认值 | 用途 |
|---|---|---|
| `FLOWPILOT_ADDR` | `:18080` | HTTP 监听地址 |
| `FLOWPILOT_STATIC_DIR` | `./web` | 前端构建产物目录 |
| `FLOWPILOT_DOCKER_SOCKET` | `/var/run/docker.sock` | Docker Engine Unix Socket |
| `FLOWPILOT_DATA_DIR` | `./data` | 平台数据目录（容器内路径） |
| `FLOWPILOT_HOST_DATA_DIR` | 回落 `FLOWPILOT_DATA_DIR` | 宿主机共享数据目录，用于实例容器挂载 |
| `FLOWPILOT_COMFYUI_IMAGE` | `comfyui:latest` | 新建实例默认镜像 |
| `FLOWPILOT_COMFYUI_CONTAINER_PORT` | `8188` | ComfyUI 容器内服务端口 |

## API

- `GET /api/v1/health`：后端健康状态和版本。
- `GET /api/v1/hardware`：真实宿主机硬件采集——nvidia-smi GPU 指标（统一内存架构显存字段为 `null`）、CPU／内存（/proc）、数据盘用量（statfs）；GPU 不可用时单项降级。
- `GET /api/v1/system/info`：当前管理服务的 Go 运行环境，不代表宿主机完整硬件或 GPU 信息。
- `GET /api/v1/docker/info`：真实 Docker `/info` 与 `/version` 摘要。
- `GET /api/v1/docker/containers`：包含已停止容器的真实 Docker 容器列表。

### 实例管理（真实 Docker 生命周期）

实例即带 `flowpilot.instance=true` 标签的 Docker 容器（命名 `flowpilot-comfyui-<name>`），名称、GPU、端口、自启动配置全部存标签，无需数据库；只管理本标签容器，绝不触碰宿主机其他容器。

- `GET /api/v1/instances`：实例列表＋默认配置 `{instances, defaults}`。
- `POST /api/v1/instances`：创建实例。请求体 `{name, image?, gpu, port, containerPort?, autoStart?}`；校验名称正则、端口 1024–65535、GPU 越界、名称/端口/GPU 冲突、镜像本地存在；`gpu=-1` 表示仅 CPU；自动挂载 `<hostDataDir>/{models,workflows,outputs}`；启动失败自动删除容器回滚。
- `POST /api/v1/instances/{name}/start|stop|restart`：启停与重启。
- `DELETE /api/v1/instances/{name}?force=1`：删除（运行中需 `force=1`）。

### 模型仓库（真实目录扫描）

模型列表即共享仓库目录（容器内 `<FLOWPILOT_DATA_DIR>/models`，宿主机 `~/.local/share/flowpilot/shared/models`）的实时扫描结果，文件即真相、无数据库；第一层子目录映射 ComfyUI 类别约定（checkpoints／loras／vae／controlnet／embeddings／upscale_models）。

- `GET /api/v1/models`：扫描结果 `{root, files[], totalBytes, scannedAt}`。
- `DELETE /api/v1/models?path=<相对路径>`：物理删除文件，拒绝路径穿越与目录删除。

### 镜像预置（后台拉取）

- `GET /api/v1/images/check?image=<镜像>`：本地是否存在。
- `POST /api/v1/images/pull` `{image}`：后台流式拉取，同一时刻仅一个任务（重复提交返回 409）。
- `GET /api/v1/images/pull`：拉取状态 `{image, state: idle|pulling|done|error, message, updatedAt}`。

## 本机无 Go 环境时验证

在项目根目录执行：

```bash
docker run --rm -v "$PWD/code/backend:/src" -w /src golang:1.25-alpine sh -c 'gofmt -w ./cmd ./internal && go test ./... && go vet ./...'
```

## 当前边界

此版本没有认证、用户权限和数据库。挂载 Docker Socket 等同于授予容器较高的 Docker 管理权限，因此服务仅应监听受信任的本机或管理网络；生产版本需要收紧接口权限并补充认证。实例自启动开关在创建时确定，运行中修改待后续版本补充。
