# FlowPilot 代码目录

本目录存放产品源码、部署配置和开发脚本。

| 子目录 | 职责 |
|---|---|
| `backend/` | Go 管理服务、SQLite、Docker API、任务和模型管理 |
| `frontend/` | React＋TypeScript Web 控制台 |
| `deploy/` | 安装、卸载、systemd、镜像模板和离线包配置 |
| `scripts/` | 构建、测试、诊断和开发辅助脚本 |

## 开发与验证

- 后端、前端和部署脚本分别查看对应目录的 README 与脚本说明。
- 提交前运行相关单元测试、构建和端到端验证，并在 Pull Request 中说明真实结果。
- 不在代码目录保存真实密钥、客户数据、模型文件或生成结果。
- 参与贡献请先阅读根目录的 `CONTRIBUTING.md` 与 `SECURITY.md`。
