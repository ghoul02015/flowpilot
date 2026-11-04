# FlowPilot 后端

当前版本：`0.0.0.2`。

轻量 Go 服务同时提供前端静态页面和只读 Docker API。仅使用 Go 标准库，通过 `/var/run/docker.sock` 访问 Docker Engine，不引入第三方 Docker SDK。

## 配置

| 环境变量 | 默认值 | 用途 |
|---|---|---|
| `FLOWPILOT_ADDR` | `:18080` | HTTP 监听地址 |
| `FLOWPILOT_STATIC_DIR` | `./web` | 前端构建产物目录 |
| `FLOWPILOT_DOCKER_SOCKET` | `/var/run/docker.sock` | Docker Engine Unix Socket |

## API

- `GET /api/v1/health`：后端健康状态和版本。
- `GET /api/v1/system/info`：当前管理服务的 Go 运行环境，不代表宿主机完整硬件或 GPU 信息。
- `GET /api/v1/docker/info`：真实 Docker `/info` 与 `/version` 摘要。
- `GET /api/v1/docker/containers`：包含已停止容器的真实 Docker 容器列表。

## 本机无 Go 环境时验证

在项目根目录执行：

```bash
docker run --rm -v "$PWD/code/backend:/src" -w /src golang:1.25-alpine sh -c 'gofmt -w ./cmd ./internal && go test ./... && go vet ./...'
```

## 当前边界

此版本没有认证、用户权限、数据库、宿主机 GPU 检测和 ComfyUI 容器写操作。挂载 Docker Socket 等同于授予容器较高的 Docker 管理权限，因此 Demo 仅应监听受信任的本机或管理网络；生产版本需要收紧接口权限并补充认证。
