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
2. 保证 `go test ./...`、`npm run lint`、`npm run build` 及相关 E2E 验证通过。
3. 提交 PR，清晰说明变更内容、影响范围和实际验证结果。
4. 不提交密码、访问令牌、内部地址、客户数据、模型文件或本地诊断产物。
5. DCO：提交信息需含 `Signed-off-by: 姓名 <邮箱>`。

## 安全问题

请勿公开提交安全漏洞——参见 `SECURITY.md` 的私密报告流程。
