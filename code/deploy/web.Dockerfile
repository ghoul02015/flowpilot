FROM node:24-alpine AS frontend-build
WORKDIR /app

COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci

COPY frontend/ ./
RUN npm run build

FROM golang:1.25-alpine AS backend-build
WORKDIR /src
COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/flowpilotd ./cmd/flowpilotd

# 运行基底使用 Ubuntu 24.04：与目标服务器 glibc 一致，
# 保证宿主机 nvidia-smi（含 NVIDIA Container Toolkit 注入的二进制）可在容器内执行。
FROM ubuntu:24.04
RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates curl \
  && rm -rf /var/lib/apt/lists/* \
  && groupadd --system flowpilot && useradd --system --gid flowpilot --home-dir /app flowpilot
WORKDIR /app
COPY --from=backend-build /out/flowpilotd /usr/local/bin/flowpilotd
COPY --from=frontend-build /app/dist /app/web

ENV FLOWPILOT_ADDR=:18080 \
    FLOWPILOT_STATIC_DIR=/app/web \
    FLOWPILOT_DOCKER_SOCKET=/var/run/docker.sock \
    FLOWPILOT_DATA_DIR=/data/flowpilot \
    FLOWPILOT_HOST_ROOT=/host

EXPOSE 18080
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD curl -fsS -o /dev/null http://127.0.0.1:18080/api/v1/health || exit 1
CMD ["flowpilotd"]
