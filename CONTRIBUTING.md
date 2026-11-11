# 贡献指南

感谢关注 FlowPilot！

## 开发环境

- Go 1.25+（后端，无 CGO 依赖）
- Node 24+（前端）
- Docker（实例管理与 E2E）

```bash
# 后端测试
cd code/backend && go test ./...
# 前端
cd code/frontend && npm ci && npm run lint && npm run build
# 本机端到端
cd code && ./scripts/demo-docker.sh up && cd frontend && npm run test:demo
```

## 贡献流程

1. Fork 并创建特性分支。
2. 保证 `go test ./...`、`npm run lint`、`npm run build`、E2E 全部通过。
3. 提交 PR 并描述变更与验证证据（变更记录格式见 context/changes/）。
4. DCO：提交信息需含 `Signed-off-by: 姓名 <邮箱>`。

## 安全问题

请勿公开提交安全漏洞——参见 SECURITY.md 的私密报告流程。
