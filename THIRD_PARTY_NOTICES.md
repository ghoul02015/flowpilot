# 第三方组件声明

## Go 依赖（go.mod）

| 组件 | 许可证 |
| --- | --- |
| modernc.org/sqlite | BSD-3-Clause |
| golang.org/x/crypto | BSD-3-Clause（Go 项目） |
| github.com/google/uuid | BSD-3-Clause |

## 前端依赖（package.json）

React 19（MIT）、Vite（MIT）、Tailwind CSS（MIT）、lucide-react（ISC）、
sonner（MIT）、Playwright（Apache-2.0）等——完整清单见 package-lock.json。

## 运行时组件（构建期获取，不随本仓库分发）

| 组件 | 许可证 | 说明 |
| --- | --- | --- |
| ComfyUI | GPL-3.0 | 镜像构建时从上游仓库克隆；用户需遵守 GPL-3.0 |
| ComfyUI-Manager | GPL-3.0 | 同上 |
| Python / PyTorch 等 | 各自许可证 | 镜像构建期安装 |

## 模型与素材

模型、LoRA 与工作流文件的再分发权由其原始许可证决定，不因本平台
Apache-2.0 而自动获得再分发授权。
