# FlowPilot 代码目录

本目录仅存放代码、部署配置和开发脚本。

| 子目录 | 职责 |
|---|---|
| `backend/` | Go 管理服务、SQLite、Docker API、任务和模型管理 |
| `frontend/` | React＋TypeScript Web 控制台 |
| `deploy/` | 安装、卸载、systemd、镜像模板和离线包配置 |
| `scripts/` | 构建、测试、诊断和开发辅助脚本 |

## 开发约束

- 技术选型与边界以 `../docs/mvp/FlowPilot_MVP_PRD.md` 为准。
- 每轮代码修改后，按 `../PROJECT_RULES.md` 更新版本和变更记录。
- 测试命令与真实结果写入对应版本的变更明细。
- 不在代码目录保存真实密钥、客户数据、模型文件或生成结果。
