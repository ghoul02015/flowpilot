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

FROM alpine:3.22
RUN addgroup -S flowpilot && adduser -S -G flowpilot flowpilot
WORKDIR /app
COPY --from=backend-build /out/flowpilotd /usr/local/bin/flowpilotd
COPY --from=frontend-build /app/dist /app/web

ENV FLOWPILOT_ADDR=:18080 \
    FLOWPILOT_STATIC_DIR=/app/web \
    FLOWPILOT_DOCKER_SOCKET=/var/run/docker.sock

EXPOSE 18080
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:18080/api/v1/health || exit 1
CMD ["flowpilotd"]
