# FlowPilot 变更日志

所有重要变更均记录于此。详细内容见 `context/changes/`。

## 0.0.9.10 — 2026-11-11（镜像中心与可信供应链）

### 新增

- **镜像可信校验**：实例创建强制 `flowpilot.kind=comfyui` 标签（镜像中心构建自动携带）；逃生阀 `FLOWPILOT_IMAGE_ALLOW_UNLABELED=1`（demo 默认注入，生产默认关闭）。
- **镜像删除引用保护**：被实例使用的镜像删除返回 409（含实例名与状态）。
- **独立镜像中心**：一级导航新增「镜像中心」（从实例页 tab 独立）。

### 验证

- 后端 14 包全绿；本机 E2E 82 项全过；Spark 实机：无标签镜像创建 400 拒绝、引用镜像删除 409、comfy-01 无损。

## 0.0.9.9 — 2026-11-11（统一任务与进度框架）

### 新增

- **统一任务协议**：View 模型（类型/对象/统一状态机 queued→running→succeeded 等/进度含不确定态）；迁移 004 tasks 表。
- **统一任务 API**：`GET /api/v1/tasks`（三源实时聚合：下载/导入＋镜像构建/拉取＋迁移持久化任务，类型状态筛选分页）、`GET /api/v1/tasks/{id}`。
- **迁移任务化**：`POST /migration/run` 改异步（同步预检 400→202 taskId→后台执行→进度结果落库）。
- **任务中心重写**：统一聚合视图＋类型/状态筛选＋类型徽章＋不确定进度态；dl- 前缀操作路由保留下载任务控制。

### 验证

- 后端 14 包全绿；本机 E2E **82 项全部通过**；Spark 实机：统一列表 36 条历史聚合、旧实例预检 400、迁移任务全链路（创建→succeeded→列表可查）通过。

## 0.0.9.8 — 2026-11-11（应用健康与实例生命周期）

### 新增

- **三层状态**：容器（running/stopped/error）＋应用（ready/starting/degraded/failed/not_running）＋队列（运行/排队）；独立健康检查器后台周期探测 /system_stats（启动期 3s／稳定期 10s／失败指数退避，有界并发 8），不依赖页面轮询。
- **健康事件**：迁移 003 instance_events 表；ready/degraded/failed/recovered/stopped/started 全量记录（**恢复也记录**）；实例列表应用健康徽章、详情弹窗健康条＋事件时间线。
- **生命周期防护**：操作互斥（同实例并发操作 409 busy）；启动超时可配（FLOWPILOT_START_TIMEOUT 默认 180s，超时保留容器与日志）；停止/重启前读取队列并附 queueWarning。
- **重建差异预览**：`POST /rebuild/plan` 对比期望配置与当前容器标签，输出字段差异＋workspace 沿用说明。

### 验证

- 后端 13 包全绿（health 包 5 用例）；本机 E2E **80 项全部通过**；Spark 实机：comfy-01 三层状态 ready（延迟 1-2ms）、started/stopped/ready 事件序列完整、停止/重启/恢复全链路通过。

## 0.0.9.7 — 2026-11-11（实例数据隔离与历史迁移）

### 新增

- **独立 workspace**：0.0.9.7 起新实例自动获得 `shared/instances/<UUID>/{outputs,workflows,user,config}` 独立挂载（模型仓库仍全局只读共享）；workspace 由后端按 UUID 生成，不接受任意宿主机路径；容器标签新增 `flowpilot.instance.uuid`。
- **删除两档**：默认「删除实例，保留数据」（容器与链接目录清理，workspace 保留为孤儿可恢复）；`?purge=1`「彻底删除」（含全部实例数据）。删除弹窗双选项＋实例名确认。
- **重建沿用**：同名实例重建自动复用历史孤儿记录的 UUID 与 workspace（数据不丢）。
- **历史迁移**：`internal/migration` 新包——扫描（outputs/workflows 统计）、预览（文件数/空间/备份需求）、执行（自动备份→copy/move→数量字节校验，同名跳过不覆盖，失败中止保留源数据）。
- **API**：`GET /api/v1/instances/workspaces`（清单含孤儿目录）、`GET /api/v1/migration/{scan,preview}`、`POST /api/v1/migration/run`、`DELETE /api/v1/instances/{name}?purge=`。
- **UI**：系统管理新增「数据迁移」子页（MigrationPanel：扫描摘要→选实例→预览→复制/移动→结果报告）。

### 验证

- 后端 12 包全绿（migration 5 用例：扫描统计/预览校验/copy 保留源+备份+幂等跳过/move 清源/非法输入拒绝；instance 新增 workspace 生命周期用例：创建挂载/删除保留/重建沿用/purge 清理）。
- 前端 lint＋build 通过；本机 E2E 新增 6 项断言（workspace UUID、删除保留、孤儿状态、重建沿用、purge 清理、迁移面板）；Spark 实机回归见 `context/changes/0.0.9.7.md`。

## 0.0.9.6 — 2026-11-10（认证、安全与操作审计）

### 新增

- **认证闭环**：单管理员（admin）＋Argon2id 密码哈希（x/crypto，新依赖）；一次性初始化令牌（数据目录 `init-token` 0600，支持 `FLOWPILOT_INIT_TOKEN` 注入，完成后文件删除不可重放）；服务端会话（闲置 30 分钟／绝对 7 天滑动续期，注销与改密撤销）。
- **中间件防护**：认证覆盖全部业务 API（豁免仅 health／setup status／setup／login）；写方法强制 `X-FlowPilot-CSRF` 头＋Origin/Host 一致性校验；Cookie HttpOnly＋SameSite=Lax（HTTPS 加 Secure）；登录每 IP 连续 5 次失败锁定 15 分钟、统一错误文案防枚举；失败关闭——数据底座故障时认证拒绝全部业务请求而非放行。
- **审计**：登录成功／失败／锁定、退出、改密、初始化、会话撤销显式记录；写操作中间件兜底记录（actor／动作／对象／状态／IP 摘要／requestId）；`GET /api/v1/audit/logs` 分页查询。
- **API**：`/api/v1/auth/{setup/status,setup,login,logout,session,password,sessions}`、`DELETE /api/v1/auth/sessions/{id}`、`GET /api/v1/audit/logs`。
- **UI**：全屏初始化页（令牌＋密码策略）与登录页（版本＋管理网提示＋锁定提示）；App 认证门卫（401 全局跳登录，未登录不发起业务轮询）；顶栏退出登录；系统管理新增「安全与会话」（改密＋活跃会话撤销）与「操作审计」（搜索＋分页＋结果徽章）子页。
- **迁移 002**：users／sessions／audit_logs 三表。

### 验证

- 后端 11 包全绿：auth 包 6 用例（Argon2id 哈希校验／密码策略／令牌重放／限流锁定与其他 IP 不连坐／改密撤销其他会话／审计分页过滤）；server 认证全链路 15 步（豁免／401 语义／CSRF 403／Origin 403／注销失效／审计查询）。
- 前端 lint＋build 通过；本机 E2E **71 项全部通过**（新增认证 10 项断言；预期 401/403 探针在最终检查中豁免）。
- Spark 实机回归见 `context/changes/0.0.9.6.md`。

## 0.0.9.5 — 2026-11-10（阶段 1 开篇：数据底座与现状基线）

### 新增

- **嵌入式 SQLite 数据底座**：引入 `modernc.org/sqlite`（纯 Go、免 CGO、amd64/arm64），项目首个外部依赖；WAL＋busy_timeout＋外键＋单写连接。
- **迁移框架**：`schema_migrations` 表记录版本／名称／checksum／时间／结果；已应用迁移 checksum 校验，篡改即拒绝；重复执行幂等。迁移 001 建 `instances`（UUID 主键＋name 唯一＋container_id 映射＋orphan 标记）、`instance_configs`（期望配置快照，级联删除）、`settings`（KV）。
- **实例映射同步**：启动时与手动触发「导入→比对→报告」——新容器入库生成 UUID、字段变化更新、同名新容器视为重建沿用 UUID、容器消失标记孤儿（不删记录）、同名冲突显式报告；不随 2 秒硬件轮询写库。实例创建／删除经钩子即时写库。
- **新 API**：`GET /api/v1/database`（schema 版本／映射统计／最近同步／integrity）、`POST /api/v1/database/check`（全量对账报告含逐字段差异：数据库值→Docker 值→采用值＋原因）、`POST /api/v1/database/backup`（VACUUM INTO 一致性备份到数据目录 backups/）。
- **UI**：系统管理新增「数据底座」子页（DatabasePanel）：引擎与 schema 状态卡、异常映射统计、最近同步摘要、立即检查、备份数据库。

### 验证

- 后端 10 包 `gofmt + go vet + go test` 全绿（database 包 7 用例：迁移幂等／checksum 防篡改／导入-更新-重建-孤儿-冲突全场景／级联删除／备份可打开／integrity；server 新增 2 用例：未启用 503 分支、真实 store 三端点）。
- 前端 `npm run lint && npm run build` 通过。
- 本机 E2E 与 Spark 实机回归见 `context/changes/0.0.9.5.md`。

## 0.0.9.4 — 2026-11-10（实例日志实时流＋模型在线关联＋硬件管理页＋首页跳转）

### 新增

- **实例日志（实时流）**：实例列表新增日志按钮，弹窗通过 SSE 实时跟随容器 stdout/stderr（Docker multiplex 帧解复用、TTY 直通、RFC3339 时间戳拆分行首），支持自动贴底跟随／上滚暂停、stderr 红色区分、行数上限 2000、下载 .log；未运行实例明确提示「仅历史日志」，容器删除/流结束以 `end` 帧说明原因。
- **实例模型关联**：弹窗按类别勾选共享仓库模型，保存后写入 `extra_model_paths.yaml` 的 `flowpilot_linked` 节点并在实例内建立符号链接（目标为容器内路径），**增删即时生效、ComfyUI 页面刷新即可见，无需重启实例**；删除共享模型时级联清理各实例链接。旧版本创建的实例缺少挂载点，弹窗提示「删除并按原参数重建」（Docker 挂载创建时固定，重启不生效，重建不丢数据）。
- **硬件管理页**：物理机分组（主机/CPU/内存/存储概要与使用率）＋ GPU 单卡单行表格（卡号、型号、显存、利用率、温度、功耗、状态徽章：高温/高负载/在线），顶部展示驱动与 CUDA 版本，2 秒轮询、纯只读。
- **首页跳转与精简**：首页硬件区改为简述条（主机名、机型、卡数、驱动/CUDA chip、CPU／内存／存储三条进度条），点击机器名进入硬件管理；实例概览新增「应用」列，实例名点击进入实例管理并自动打开详情（3 秒行高亮），「打开」直达 ComfyUI；四张指标卡片可点击跳转对应页面。

### 变更

- `extra_model_paths.yaml` 生成解耦网络存储：`flowpilot_linked` 节点恒生成，`flowpilot_network` 节点仅在 NAS 启用时追加。
- 删除已被硬件管理页取代的 `HardwareMonitor` 组件（首页不再渲染完整硬件面板）。

### 验证

- 后端 `gofmt + go vet + go test ./...` 全部通过（新增日志解复用、符号链接、硬件版本解析、SSE 端点等 10+ 用例）。
- 前端 `npm run lint && npm run build` 通过（React Compiler 规则 `set-state-in-effect`／`set-state-in-render` 全绿）。
- 本机演示环境端到端回归 61 项全部通过（零控制台错误、零 4xx/5xx）；模型关联符号链接经 `docker exec` 实证增删即时生效、免重启；Spark 实机回归通过：DGX Spark 升级至 0.0.9.4（保留既有实例与数据），硬件管理页 GB10 逐卡表格、日志 SSE 实时流、模型关联符号链接免重启生效均实测确认。
- 端到端回归脚本新增 0.0.9.4 断言（硬件管理页与 GPU 逐卡表格、日志 SSE meta 帧、日志弹窗、模型关联弹窗、首页两处跳转）。

详细记录：[0.0.9.4](context/changes/0.0.9.4.md)

## 0.0.9.3 — 2026-11-10（模型中心第二批：僵尸检测＋重复检测＋批量操作）

### 新增

- **僵尸模型检测**：本地仓库中超过 30 天未更新且大于 1 GB 的模型行内标记「疑似闲置」（amber 徽章，附阈值说明），辅助清理决策。
- **重复模型检测**：按「名称＋大小」分组，同名同大小多于 1 个即标记「重复×N」（rose 徽章），跨目录冗余一目了然。
- **筛选 chips**：「仅看疑似闲置」「仅看重复」一键切换，与搜索/类型/排序组合生效。
- **批量操作**：仓库文件行复选框勾选，工具条提供「批量本地化（仅网络存储）」「批量删除（仅本地仓库）」；批量删除带确认对话框并保留运行中实例保护；下载任务与网络只读文件自动排除；勾选状态随仓库刷新自动失效。

### 验证

- 前端 `npm run lint && npm run build` 通过；后端 `gofmt + go vet + go test ./...` 全部通过（本批无后端改动，防御性复跑）。
- 实体机验证结果见详细记录。

详细记录：[0.0.9.3](context/changes/0.0.9.3.md)

## 0.0.9.2 — 2026-11-10（模型中心生产化：网络存储 NAS 直挂＋本地化＋分组视图＋市场卡片化）

### 新增

- **网络存储（NAS）模型仓库**：系统管理新增「网络存储」配置（NFS/SMB 地址＋远端路径），一键下载宿主机挂载脚本（含 /etc/fstab 开机挂载、标记文件、平台容器重启提示）；平台以标记文件探测挂载生效。
- **实例免拷贝使用 NAS 模型**：网络仓库生效后，新实例自动追加两个只读挂载——NAS 仓库（`<ComfyUI 根>/models-net`）与按实例生成的 `extra_model_paths.yaml`，ComfyUI 原生合并网络模型来源，**无需拷贝到物理机**；yaml 随实例删除自动清理。网络仓库不映射 custom_nodes（防可执行代码注入）。
- **双仓库视图与本地化**：`/api/v1/models` 合并返回本地＋网络仓库文件（`origin: local|network`，`node` 字段为集群预留）；NAS 抖动只标记不可用、不拖垮扫描。模型中心对网络模型提供「本地化」按钮，复用导入任务引擎复制进本地仓库（进度入任务中心）。
- **模型中心分组视图**：按类别折叠分组（组头显示数量与占用），进行中任务置顶成组；支持按大小/时间排序；统计条展示本地仓库、网络存储、类别数；删除保护提示列出具体运行中实例名。
- **模型市场卡片化**：条目单行信息层级（名称＋类型＋大小·可达性＋操作），描述收起一行；在线源文件按类别分组、组内按大小降序。

### 接口变更

- 新增 `GET /api/v1/storage/network`、`PUT /api/v1/storage/network`、`GET /api/v1/storage/network/mount-script`。
- `/api/v1/models` 文件条目新增 `origin`、`node` 字段，响应新增 `network` 状态块（均向后兼容）。

### 验证

- 后端 `gofmt + go vet + go test ./...` 全部通过；新增 storage 包 5 用例（配置校验、持久化、标记探测、挂载脚本、extra_model_paths 生成与清理）、model 双仓库合并/不可用降级 2 用例、instance 网络挂载 2 用例。
- 前端 `npm run lint && npm run build`（含 tsc 类型检查）通过。

详细记录：[0.0.9.2](context/changes/0.0.9.2.md)

## 0.0.9.1 — 2026-11-10（代码审查修复：数据竞争＋交互缺陷）

### 修复（本轮代码审查发现）

- **镜像构建任务数据竞争**：`runBuild` 记录被覆盖旧镜像 ID 时未持锁写入任务字段，与任务列表/持久化的锁内读取并发，存在数据竞争隐患；改为持锁写入。
- **构建/拉取日志弹窗突然关闭**：成功任务入库后记录会被自动清理（15 秒惰性扫描），日志弹窗按 ID 从任务列表实时查找，记录消失导致弹窗闪关；现保留打开时的任务快照，记录清理后仍可查看完整日志。
- **实例操作双击穿透**：忙碌判断基于渲染快照，同一帧内双击可能绕过禁用重复提交；增加同步 ref 集合拦截。
- **新建实例默认端口 fallback 撞车**：8188–8287 全占时旧 fallback `8188 + 实例数` 可能撞上已有实例端口；改为持续向上扫描至 65535。

### 验证

- 后端 `gofmt + go vet + go test ./...` 全部通过。
- 前端 `npm run lint && npm run build`（含 tsc 类型检查）通过。

详细记录：[0.0.9.1](context/changes/0.0.9.1.md)

## 0.0.9.0 — 2026-11-09（评审清单 P1/P2/P3 全量落地：实例体验、镜像中心合并、色系统一）

### 修复（P1）

- **实例操作可重复点击**：0.0.7.x 起前端按后端不存在的状态做禁用判断（死代码），改为以请求级忙碌集合禁用按钮，停止/启动/重启期间按钮锁定、重启图标旋转。
- **实例详情缺少工作流挂载**：补齐「工作流挂载」字段，挂载路径类字段统一整行展示。
- **任务中心假日志**：日志弹窗为前端拼接文本，易误导排障；改为「任务详情」面板，展示真实任务字段与失败原因。

### 修复与增强（P2）

- **构建弹窗版本双控件冲突**：「在线版本下拉＋手动修正输入」合并为单个可输入可下拉的组合框（datalist）。
- **默认实例名/端口易撞车**：改为自动扫描第一个空闲值（comfy-NN、8188 起）。
- **端口校验**：创建实例时新增宿主机端口真实监听探测，18080 管理端口及其他已占用端口会在创建前拦截（原仅校验实例间冲突）。
- **添加模型失败丢输入**：创建任务失败时弹窗保持打开，内容保留。
- **模型类型筛选不全**：模型中心与添加模型弹窗补齐 Text Encoders / Diffusion Models / CLIP Vision / Model Patches，后端类别展示名映射同步补齐。
- **版本号硬编码**：界面页脚与诊断包版本改读后端 `/health`，离线时回退常量。
- **GPU 检测失败静默退回 CPU**：新建实例向导在无 GPU 数据时显示琥珀色警告条。

### 结构与色系（P3）

- **实例页三个 tab 合并为两个**：「实例列表」+「镜像中心」（原「容器镜像管理」与「手动构建/拉取」内容重叠，合并为单视图：入口卡片＋镜像清单＋任务列表＋磁盘占用）。
- **新建实例向导 3 步收 2 步**：纯展示的「模型可见性」并入确认页。
- **色系一致性**：页面子菜单选中态由深灰统一为品牌 teal-700；StatusBadge 进行中状态由 cyan 统一为 teal 系。
- 实例表格「制图任务」列更名「ComfyUI 队列」，详情弹窗术语同步；操作列删除按钮与其他操作加分隔线；开机自启由禁用 checkbox 改为文字徽章。

### 验证

- 后端 `gofmt + go vet + go test ./...` 全部通过（新增宿主机端口占用回归测试）。
- 前端 `npm run lint && npm run build`（含 tsc 类型检查）通过。

详细记录：[0.0.9.0](context/changes/0.0.9.0.md)

## 0.0.8.0 — 2026-11-09（镜像构建/拉取任务化：有界并发＋排队＋停止＋记录管理）

### 新增

- **镜像任务列表**：构建与拉取从全局单任务重构为任务列表，最多 2 个构建 + 1 个拉取并行，超出自动排队；同名镜像任务互斥防重复提交。
- **任务停止与删除**：排队中/进行中的任务可随时停止（取消 context 中止 Docker 构建/拉取流）；已结束任务记录可手动删除。
- **任务持久化**：任务记录落盘 `image-tasks.json`，服务重启后未完成任务自动标记「已中断」，不再永久挂起。
- **残留治理**：构建成功覆盖同名镜像时自动清理被替换的悬挂旧镜像；新增构建缓存占用展示与手动「清理构建缓存」（`POST /api/v1/images/cache/prune`、`GET /api/v1/images/disk-usage`）；成功入库的任务记录自动清理，已结束记录保留最新 20 条。
- **磁盘水位检查**：提交构建前校验数据盘剩余空间，不足 15 GB 直接拒绝并提示。

### 接口变更

- 新增 `GET /api/v1/images/tasks`、`GET /api/v1/images/tasks/{id}`、`POST /api/v1/images/tasks/{id}/cancel`、`DELETE /api/v1/images/tasks/{id}`、`GET /api/v1/images/disk-usage`、`POST /api/v1/images/cache/prune`。
- 移除 `GET /api/v1/images/build`、`GET /api/v1/images/pull` 单任务状态接口（由任务列表取代）；`POST /api/v1/images/build`、`POST /api/v1/images/pull` 改为返回任务对象。

### 验证

- 后端 `gofmt + go vet + go test ./...` 全部通过；新增并发上限与排队、取消竞态、持久化中断、悬挂镜像清理、磁盘水位拦截等回归测试。
- 前端 `npm run lint && npm run build` 通过。

详细记录：[0.0.8.0](context/changes/0.0.8.0.md)

## 0.0.7.4 — 2026-11-09（修复：cu128/amd64 构建脚本 Dockerfile LABEL 语法）

### 修复

- **一键构建脚本 Dockerfile LABEL 续行符错误**：修复 `flowpilot/comfyui:cu128`、`linux/amd64` 等手动构建脚本中 `LABEL` 多行续行符输出为双反斜杠的问题，避免 Docker 报错 `failed to parse Dockerfile: can't find = in "\\\\"`。
- **后台构建同步修复**：平台内手动构建与下载的一键构建脚本共用同一 Dockerfile 模板，后台构建同样修复该语法问题。

### 验证

- 后端 Docker Go 环境 `gofmt + go test ./...` 通过，并新增 cu128/linux-amd64 Dockerfile 与构建脚本回归断言。

详细记录：[0.0.7.4](context/changes/0.0.7.4.md)

## 0.0.7.3 — 2026-11-09（实例镜像分类＋构建/拉取弹窗＋构建参数脚本化）

### 新增

- **构建与拉取双弹窗**：实例页「手动构建 / 拉取」改为两个独立弹窗，分别填写构建参数与在线镜像拉取参数，避免主界面表单堆叠。
- **ComfyUI 镜像分类清单**：容器镜像管理将本机镜像分为「ComfyUI 镜像」与「机器其他镜像」；新建实例下拉只展示 ComfyUI 镜像，避免误选平台自身或其他业务镜像。
- **构建参数细化与脚本下载**：构建弹窗新增 CPU 芯片架构（host/linux-arm64/linux-amd64/armv7）、GPU/PyTorch 配方、在线 ComfyUI 版本列表、pip 源与 Manager 开关，并可下载同参数一键构建脚本。
- **构建进度与日志入口**：ComfyUI 镜像清单展示构建进度百分比、参数简述和日志图标，可直接打开实时构建日志弹窗。

### 验证

- 后端 Docker Go 环境 `gofmt + go test ./...` 通过；前端 `npm run lint && npm run build` 通过。Spark 实机部署验证见详细记录。

详细记录：[0.0.7.3](context/changes/0.0.7.3.md)

## 0.0.7.2 — 2026-11-09（实例模块：容器镜像管理＋手动构建日志＋实例仅选本机镜像）

### 新增

- **容器镜像管理子菜单**：实例页新增「容器镜像管理」，集中管理本机 Docker 镜像列表，支持镜像改标签、删除、tar 导入和 tar 导出，作为新建实例的唯一镜像来源。
- **手动构建 / 拉取子菜单**：新增配方化 ComfyUI 镜像构建入口，可按 NVIDIA cu130/cu128/cu126/cu121、AMD ROCm、CPU 六套配方一键后台构建，并通过轮询展示实时构建日志；在线拉取镜像也迁移到该子菜单。
- **镜像管理后端接口**：新增镜像删除、改标签、导入、导出、构建启动和构建状态/日志接口；镜像导入导出长连接不再受固定读写超时影响。

### 修改

- **新建实例向导收敛**：不再在创建弹窗内选择预置配方或手动输入镜像名，也不再承载拉取/脚本下载入口；创建实例只能选择本机已构建、已导入或已拉取镜像，环境管理动作前置到镜像子菜单。

### 验证

- 后端 Go Docker 环境 `gofmt + go test ./...` 通过；前端 `npm run lint && npm run build` 通过。DGX Spark 部署验证见详细记录。

详细记录：[0.0.7.2](context/changes/0.0.7.2.md)

## 0.0.7.1 — 2026-11-09（修复：一键安装脚本 sudo 凭据预检＋DGX 实机验证）

### 修复（DGX Spark 实机部署验证发现）

- **一键安装脚本在无人值守执行时卡死于 sudo 密码提示**：以 `NONINTERACTIVE=1 nohup` 方式在 DGX Spark（yl 用户，sudo 需密码）实测 0.0.7.0 生成脚本，步骤 2「配置 Docker Hub 镜像加速」的 `$SUDO python3` 触发 `sudo: 需要密码` 后中止。修复：`need_sudo` 增加凭据预检——`sudo -n true` 探测，交互模式先 `sudo -v` 缓存凭据，非交互模式直接报清晰错误并给出解决指引；模板测试新增 `sudo -n true` 片段断言，独立分发脚本同步重生成。

### 验证（DGX Spark 生产部署）

- 0.0.7.0 完整部署 spark-6558（Ubuntu 24.04 arm64 · GB10 · Docker 29.2.1），远程端到端回归 **52 项全部通过**：含本轮新增的「本机镜像列表／配方目录／安装脚本生成」接口断言，以及真实 GB10 上的 ComfyUI 实机链路（创建→启动→/system_stats 就绪→状态代理→打开应用跳转→停止→删除）。
- 一键脚本在真实 GB10 硬件上验证：`bash -n` 语法通过；显卡检测逻辑实测 `compute_cap=12.1 → nvidia-cu130` 配方解析正确；确认脚本不含任何模型下载逻辑。

详细记录：[0.0.7.1](context/changes/0.0.7.1.md)

## 0.0.7.0 — 2026-11-08（实例镜像可选＋多显卡一键安装脚本＋挂载路径向导化）

### 背景与评估（周董要求：优化 ComfyUI 实例模块）

- 评估确认四个缺口：新建实例无法选择镜像版本；纯净机器缺少一键安装路径且未区分显卡；模型仓库／导出资产挂载路径未在界面暴露；安装参数无法通过 UI 向导选择。参考 DGX Spark 实战脚本（多通道克隆、先装 CUDA 版 torch、pip 缓存挂载、国内加速源）固化为平台能力；模型下载明确不进入安装脚本，由模型中心统一负责。

### 新增

- **本机镜像列表接口**：`GET /api/v1/images/local`（Docker `/images/json`），向导「本机已有镜像」下拉展示标签与体积。
- **镜像构建配方目录**：`GET /api/v1/images/catalog`，六套配方——NVIDIA cu130（GB10/sm_121，DGX Spark）、cu128（sm_100/120）、cu126（sm_80–90）、cu121（旧卡兼容）、AMD ROCm 6.2、纯 CPU。
- **一键安装脚本生成**：`GET /api/v1/images/install-script`，按向导参数（显卡配方／镜像名／ComfyUI 版本／pip 源／是否内置 Manager）渲染脚本并附件下载；脚本自动检测显卡与架构、安装 Docker 与 NVIDIA Container Toolkit、配置国内镜像加速、多通道克隆 ComfyUI 源码、生成 Dockerfile 并构建，全程不下载模型；参数白名单校验防脚本注入。独立分发版同步落地 `code/scripts/install-comfyui-image.sh`。
- **创建向导升级**：镜像来源三选一（本机镜像／预置配方／手动输入）；镜像缺失时可在线拉取或展开「一键安装脚本参数」直接下载定制脚本；新增「高级设置」折叠区——模型仓库（只读）／输出（导出资产）／工作流挂载目标与容器端口均可手动设置；确认页完整展示宿主机共享目录 → 容器挂载映射与导出资产路径。
- **实例详情真实挂载回显**：输出／工作流挂载目标写入容器标签并在实例详情中展示。

### 修改

- `instance.Defaults` 增加 `outputsTarget`／`workflowsTarget`；实例标签新增 `flowpilot.instance.outputs-target`／`flowpilot.instance.workflows-target`（旧实例无标签时界面回退默认文案，兼容）。

### 验证

- Go 九包测试通过（新增 `catalog_test.go`：配方覆盖、脚本渲染、注入参数清理断言）；渲染脚本 `bash -n` 语法校验通过；前端 `tsc + vite build` 通过。

详细记录：[0.0.7.0](context/changes/0.0.7.0.md)

## 0.0.6.3 — 2026-11-08（搜索习惯优化：重试加固＋模糊变体＋仓库直达）

### 背景与评估（周董要求：从用户搜索习惯出发评估是否需要模糊搜索）

- 实测 ModelScope 搜索对「nvfp4」（1106 条）、「fp8」「GGUF」「wan2.1」「minmax」（错别字 13 条）、「视频生成」（4408 条中文）均正常且大小写不敏感——周董截图的空结果定位为风控间歇返回 Code=200 空列表，0.0.6.2 的单次 600ms 重试未覆盖风控窗口。
- 评估后否决「输入即搜」防抖自动搜索：源站风控敏感，保留手动触发＋回车。

### 新增

- **空结果重试加固**：原词空结果最多重试 2 次（600ms／1500ms 退避），覆盖风控空响应窗口。
- **模糊变体自动扩展**：仍空则自动尝试变体——去版本后缀（MiniMax-H3→MiniMax、Wan2.1→Wan，字母前缀仅在分隔符后剥除）、分隔符归一为空格、小写化，最多 3 个、小写去重；响应新增 `attempts` 字段透明记录全部尝试过的关键词，前端展示「原关键词未命中，已自动扩展搜索：…」。
- **仓库直达**：识别 `org/name` 与 modelscope.cn／huggingface.co／hf-mirror.com 链接，命中即跳过搜索接口直接拉取仓库文件列表并自动展开；URL 指向另一源时自动切换源标签。

### 验证

- Go 九包测试（新增重试次数断言、变体单测、ParseRepoRef 用例）、前端 lint／build 通过；回归脚本新增两项断言：「短关键词搜索命中＋attempts 透明化（nvfp4）」「仓库名直达」。

详细记录：[0.0.6.3](context/changes/0.0.6.3.md)

## 0.0.6.2 — 2026-11-08（修复：全词搜索空结果＋弹窗横向溢出）

### 修复（周董实测发现）

- **全词搜索空结果**：实测「Minimax」空结果而「Mini」有结果。排查确认 ModelScope 搜索本身大小写不敏感且后端链路稳定（连续 6 次请求全部命中 20 条），定位为源站风控间歇返回 Code=200 空列表；后端改为空结果自动等待 600ms 重试一次，前端空结果提示补充「尝试更短关键词或切换源」引导。
- **弹窗内容横向溢出 UI**：在线搜索结果中长文件名（如 `minimax_h3_fl2v_turbo_4step_v1.0_768p_comfyui_bf16.safetensors`，truncate＝nowrap）经 grid 子项 `min-width:auto` 撑宽弹窗内容盒——实测弹窗 672px 被撑出 821px 内容（scrollWidth 845），标签栏／筛选器／卡片整体溢出右边界。修复：DialogContent 统一加 `[&>*]:min-w-0`（所有弹窗生效），市场面板根部加 `min-w-0`。修复后实测 scrollWidth=670=clientWidth。

### 验证

- 复现脚本实测溢出数据（672→821px）→ 修复后复测归零；回归脚本新增两项永久断言：「全词搜索 Minimax 命中」「弹窗无横向溢出」。
- Go 九包测试、前端 lint／build 通过；DGX Spark 实机 48 项远程回归全部通过。

详细记录：[0.0.6.2](context/changes/0.0.6.2.md)

## 0.0.6.1 — 2026-11-08（模型市场在线源：双源切换＋全站搜索）

### 新增

- **源切换与连接状态**：模型市场新增「推荐目录／ModelScope／HF 镜像」三个源选项，在线源实时显示连接状态点（绿＝可达＋延迟，红＝不可达）；`GET /api/v1/sources/status` 并发探测双源，60 秒缓存。
- **在线搜索（免维护目录）**：`GET /api/v1/models/online/search` 代理 ModelScope（PUT dolphin/models）与 HF 镜像（api/models）全站搜索，`GET /api/v1/models/online/files` 递归列出仓库权重文件（.safetensors/.pth/.gguf 等，真实大小按降序）。源站模型更新即所见，平台无需发版维护目录。
- **界面在线链路**：搜索仓库 → 展开文件列表 → 按类型筛选 → 单文件一键下载；文件名（ComfyUI 模板引用名）突出显示；类别按 ComfyUI 目录约定自动推断（覆盖 Comfy-Org 布局与 HF 的 FL2VA/text_encoder 中间段）。
- **推荐目录扩充至 19 条**：新增 MiniMax H3 全套（FL2VA FP8／W6A8 16G 优化、Qwen3VL 32B NVFP4 编码器、视频 VAE、Turbo 4 步 LoRA，Comfy-Org 官方打包仓库，与 ComfyUI 官方模板命名一致）；目录刷新改递归文件列表以覆盖子目录仓库。

### 验证

- Go 九包测试新增在线源 8 用例（双源搜索解析、文件列表与类别推断、状态探测与缓存、超时收口、参数校验），全部通过。
- DGX Spark 实机 46 项远程回归全部通过：双源状态（ModelScope 317ms／HF 860ms）、MiniMax-H3 搜索命中 Comfy-Org 首位、仓库文件列表真实大小展示，及既有 42 项链路。

详细记录：[0.0.6.1](context/changes/0.0.6.1.md)

## 0.0.6.0 — 2026-11-08（模型市场：ModelScope 目录，选中即下载）

### 新增

- **模型市场**：添加模型对话框改为三标签（模型市场／URL 下载／本地导入），默认展示 14 条内置主流模型目录（SD1.5、SDXL、FLUX.1 dev/schnell 及配套 VAE/CLIP-L/T5-XXL、SD3.5 Large、SD VAE、ControlNet Canny、Wan2.1 文生视频及 VAE、RealESRGAN x4），支持搜索与类型筛选，点击「下载」即按条目类别与文件名直接创建真实下载任务，无需手填地址。
- **ModelScope 优先、HF 镜像备选**：所有条目主地址为 ModelScope 直链（`modelscope.cn/models/.../resolve/master/...`，已逐条实测 200），国内服务器开箱可用；每条目附 hf-mirror.com 备选地址，FLUX.1 dev 与 SD3.5 Large 等 HF 受限条目标注「需授权」。
- **目录在线更新**：`GET /api/v1/models/catalog` 返回目录（缓存优先、与内置合并），`POST /api/v1/models/catalog/refresh` 并发查询 ModelScope 文件列表 API，刷新各条目真实文件大小与可达性（`reachable`），结果原子写入 `shared/catalog.json`，重装保留、离线自动回落内置目录。前端「更新目录」按钮一键触发。
- 已入库条目在市场中自动标注，避免重复下载；新增 Text Encoders／Diffusion Models 类别，分别落入仓库 `text_encoders/`、`diffusion_models/` 子目录。

### 修复（实机回归发现）

- 回归脚本按钮选择器歧义：HF 镜像按钮的 aria-label 包含主下载按钮同名前缀，strict mode 命中两个元素，改为精确匹配。
- 回归市场下载后的界面删除存在时序竞争：界面列表每 3 秒刷新、滞后于 API 轮询，点击删除时同名 dl- 下载任务行尚未消失，误触「取消任务」（409）而非删除文件；现等待任务行消失、仅剩仓库文件行后再操作。

### 验证

- Go 九包测试（新增 catalog 包 5 用例＋目录接口 2 用例）、前端 lint／build 通过。
- DGX Spark 实机 42 项远程回归全部通过：含 49.5MB SDXL Offset LoRA 从 ModelScope 真实下载、入库、界面删除全链路；目录接口断言 14 条且主地址全部为 ModelScope。
- 在线刷新实测：14 条全部 reachable，文件大小更新为 ModelScope API 真实值（如 SD1.5 3.97→4.27GB），缓存落盘 `shared/catalog.json`。

详细记录：[0.0.6.0](context/changes/0.0.6.0.md)

## 0.0.5.1 — 2026-11-08（去除全部演示数据，真实生产显示）

### 变更

- **移除全部虚拟演示数据**：删除 `data/mock.ts`（模拟实例／模型／任务）与 `data/hardware.ts`（5 个演示机型），前端不再内置任何模拟业务数据；页面完全由真实后端接口驱动，接口不可用时显示空态与明确的离线提示（侧栏状态点、顶栏「后端连接失败」徽标），不再回退演示数据。
- 侧栏「重置演示数据」入口与顶栏「Demo · 本地模拟数据」徽标移除；新建实例向导第二步改为共享仓库模型可见性说明（真实实例自动可见全部就绪模型，不再做虚假的选择语义）；添加模型对话框不再预填示例地址；系统管理移除不生效的「系统设置」假表单与写死的「操作日志」假数据，环境报告全部改为真实硬件采集行。
- 后端不可用时所有操作入口统一拦截并提示「后端未连接」。

### 修复（评审发现）

- 首页「最近任务」此前展示的是浏览器模拟任务而非真实下载任务（传错数据源），系统页诊断导出同理使用模拟实例数据。
- App 启动时 GPU 列表仅单次获取且无重试：硬件接口冷启动超时（Spark 上 nvidia-smi 首次调用慢）会导致整个会话无法创建 GPU 实例，现改为失败每 3 秒自动重试。
- 首页问候语按当前时段动态生成（此前固定「下午好」）。

### 验证

- Go 八包测试、前端 lint／build 通过。
- DGX Spark 实机 38 项远程回归全部通过（新增：界面无演示数据标识、实例列表空态展示；演示机型回退、模拟创建／下载流程等演示分支断言全部移除，接口不可用时回归直接失败）。
- 排障记录：回归暴露机器上遗留手工实例 `comfy-01`（0.0.5.0 部署后界面点击创建）占用 GPU 0，经 API 删除后全绿；实例删除不影响共享模型仓库。

详细记录：[0.0.5.1](context/changes/0.0.5.1.md)

## 0.0.5.0 — 2026-11-08（任务持久化＋制图进度＋应用跳转）

### 新增

- **任务持久化**：下载／导入任务落盘 `shared/tasks.json`（原子写入，0600），平台重启／容器重建后任务列表不丢；重启时进行中的任务自动转为暂停并提示"平台重启，可断点续传／可继续"，恢复后从 `.part` 分片续传；任务 ID 序号随文件延续不回退。
- **ComfyUI 制图状态代理**：`GET /api/v1/instances/{name}/comfy/status` 经平台后端探测运行中实例的 `/queue` 与 `/system_stats`，返回服务可达性、运行中／排队任务数与 ComfyUI 版本（4 秒超时，未运行实例直接返回不可达）；容器通过 `--add-host host.docker.internal:host-gateway` 访问宿主机映射端口，`FLOWPILOT_HOST_GATEWAY` 可覆盖。
- **前端制图进度与一键跳转**：实例列表「制图任务」列实时展示 运行 X · 排队 Y／空闲／服务启动中（3 秒轮询，多实例并行探测）；运行中实例提供「打开 ComfyUI」按钮，新窗口直达 `http://<主机>:<端口>` 的真实应用；首页实例概览同步展示制图任务列。定位明确：FlowPilot 做环境与部署管理，制图操作跳转真实 ComfyUI 应用。

### 验证

- Go 八包测试全过（新增：任务跨重启恢复＋续传＋ID 延续、comfy/status 运行／停止两态代理用例）；前端 lint／build 通过。
- DGX Spark 实机持久化链路实测：120 MB 下载至 40% 时 `docker restart` → 任务恢复为暂停（文案正确）→ 一键继续 → SHA256 校验通过入库。
- 37 项远程回归全部通过（新增：状态代理 reachable＋版本、制图任务徽标、打开应用跳转正确端口）；原机业务容器按周董要求保持停止，测试产物全部清理。

详细记录：[0.0.5.0](context/changes/0.0.5.0.md)

## 0.0.4.1 — 2026-11-08（测试评审补丁）

### 修复

- **实例启动 GPU 冲突**（评审发现的真 bug）：`start`／`restart` 前校验目标 GPU 未被其他运行中实例占用——此前创建时校验可被"停止后另建实例"绕过，导致两实例共用一卡。
- 模型仓库挂载改为只读（对齐 PRD 10.1），实例容器内无法再改动或删除共享模型；工作流与输出目录保持可写。
- 下载归档前复核目标路径，不再静默覆盖用户在下载期间手工放入仓库的同名文件。
- 导入任务文案与下载区分（"导入完成"／暂停提示不再误称"断点续传"）。
- 下载 HTTP 客户端补充连接／TLS／响应头超时，对端挂起时任务不再永远卡在"正在建立下载连接"。
- 模型仓库扫描增加 2 秒缓存＋删除主动失效，消除界面轮询对大仓库的持续全盘 IO。
- 卸载脚本清理全部历史版本 `flowpilot-web:*` 镜像（无遗留 FlowPilot 容器时），避免历次升级旧镜像在客户机器上累积。

### 验证

- Go 八包测试全过（新增：启动 GPU 冲突、只读挂载断言、归档不覆盖手工文件、扫描缓存与失效）。
- DGX Spark 实机边界测试 **33 项全过**（非法输入 15 项、冲突与生命周期 11 项、下载导入 7 项）；只读挂载容器内实测；卸载→重装→卸载全链路（8 个历史镜像清零、业务容器 comfyui-h3／gpustack 无恙）。
- 34 项远程回归复跑全部通过。

详细记录：[0.0.4.1](context/changes/0.0.4.1.md)

## 0.0.4.0 — 2026-11-08

### 新增

- 本地导入真实化：`POST /api/v1/imports` 将服务器本地文件（默认白名单目录 `/data/import`，`FLOWPILOT_IMPORT_DIRS` 可配置）任务化复制入共享仓库，与下载共用任务模型（kind=import，进度、暂停、SHA256 校验、目标冲突检测一致）；前端添加模型弹窗"本地导入"接入真实接口。
- ComfyUI 镜像预置策略：install.sh 自动探测默认实例镜像（`FLOWPILOT_COMFYUI_IMAGE` 指定 → `comfyui-h3:spark` → `comfyui:latest`），并读取镜像 WorkingDir 自动适配挂载目标（如 `/app/ComfyUI/{models,output,user/workflows}`），新实例开箱即见平台模型库；`FLOWPILOT_COMFYUI_MODELS_TARGET/OUTPUTS_TARGET/WORKFLOWS_TARGET` 可覆盖，实例创建时亦可单独指定。

### 修复

- 大容器停止返回 502：停止请求改用无全局超时通道（上限为优雅停止时长＋15 秒），且请求失败时回查容器真实状态，已退出视为成功，停止操作幂等（ComfyUI 类容器不响应 SIGTERM，Docker 10 秒后 SIGKILL，此前客户端 10 秒超时先于 Docker 返回）。
- 回归套件消除两处时序脆弱点：仓库扫描缓存窗口改为轮询收敛；任务中心历史任务重名匹配取第一条。

### 验证

- Go 八包测试全过（download 新增导入正／反用例，docker 新增停止状态回查用例）；前端 lint／build 通过。
- DGX Spark 实测 34 项远程回归全部通过，新增：本地导入写入仓库＋界面删除、ComfyUI 实例（comfyui-h3:spark 预置镜像）创建→/system_stats 就绪→停止→删除。
- 出图闭环实测：平台下载引擎拉取 SD1.5（4.27 GB，hf-mirror）→ API 创建实例（GPU 0、端口 18190）→ 提交 20 步工作流真实出图（宇航员骑马，512×512）→ 实例、模型、任务全部清理；生产容器 `comfyui-h3` 全程不受影响。

详细记录：[0.0.4.0](context/changes/0.0.4.0.md)

## 0.0.3.0 — 2026-11-08

### 新增

- 下载任务引擎：`POST /api/v1/downloads` 创建真实 URL 下载（类别映射 ComfyUI 仓库子目录，文件名可留空从地址推导，可选 SHA256 校验），`POST /api/v1/downloads/{id}/pause|resume|cancel|retry`、`GET /api/v1/downloads`、`DELETE` 删除记录；断点续传（Range／206），暂停保留分片，取消清理分片，完成后归档入共享仓库。
- 前端真实下载链路：添加模型弹窗真实模式（说明文案、名称可留空）；模型中心合并展示进行中下载（进度、暂停／继续、移除任务）与仓库文件；任务中心展示真实下载任务（进度、取消、重试）。

### 修复

- 已完成／失败的下载任务记录不再阻塞同目标重新下载（仅进行中的任务占用目标路径）。

### 验证

- Go 七包测试全过（新增 download 包 5 用例：完整下载＋校验、暂停续传、校验失败、参数与冲突、取消清理）；前端 lint／build 通过。
- DGX Spark 实测：界面创建 `http://localhost:18080/index.html` 下载任务→写入共享仓库→界面删除；任务中心显示真实下载任务；28 项远程回归全部通过；`comfyui-h3` 不受影响，测试产物全部清理。

详细记录：[0.0.3.0](context/changes/0.0.3.0.md)

## 0.0.2.0 — 2026-11-08

### 新增

- 模型中心接入真实共享仓库：`GET /api/v1/models` 扫描宿主机模型目录（第一层子目录映射 ComfyUI 类别：checkpoints／loras／vae／controlnet／embeddings／upscale_models），`DELETE /api/v1/models?path=` 物理删除文件并拒绝路径穿越；前端真实模式 3 秒轮询，展示文件数、仓库占用与根目录，界面可直接删除真实文件。
- 镜像预置流程：`GET /api/v1/images/check` 检查本地镜像，`POST /api/v1/images/pull` 后台流式拉取（同一时刻仅一个任务），`GET /api/v1/images/pull` 查询进度；创建实例向导实时检查镜像存在性，缺失时可一键在线拉取并显示进度。
- Docker 客户端新增无超时的流式通道与 `PullImage`，逐行解析拉取进度事件。

### 验证

- Go 六包测试全过（新增 model 扫描／删除／穿越防护 4 用例、image 拉取生命周期 3 用例、server 模型接口用例）；前端 lint／build 通过。
- DGX Spark 实测：预置 `checkpoints/regress-test.safetensors` 种子文件，界面展示（Checkpoints · 4.0 MB）并通过界面真实删除；以 `quay.io/gpustack/runtime:pause` 完成后台拉取全链路；26 项远程回归全部通过；`comfyui-h3` 不受影响，测试产物全部清理。

详细记录：[0.0.2.0](context/changes/0.0.2.0.md)

## 0.0.1.0 — 2026-11-08

### 新增

- 实例管理接入真实 Docker API：新增 `GET/POST /api/v1/instances`、`POST /api/v1/instances/{name}/start|stop|restart`、`DELETE /api/v1/instances/{name}?force=1`，实例为带 `flowpilot.instance=true` 标签的容器，配置全部存标签，无数据库。
- 创建实例：名称／端口／GPU 冲突与合法性校验（正则、1024–65535、GPU 越界、镜像必须本地存在），GPU 分配通过 `DeviceRequests`（nvidia 驱动＋DeviceIDs），支持"不分配 GPU（仅 CPU）"，自动挂载共享数据目录 `{models,workflows,outputs}`，启动失败自动删除容器回滚。
- 前端实例页真实模式：3 秒轮询真实实例列表，GPU 列显示真实型号（如 NVIDIA GB10），创建弹窗支持容器镜像、访问端口、容器端口与真实 GPU 下拉选择，"打开 ComfyUI"直达实例端口。
- 新增环境变量：`FLOWPILOT_HOST_DATA_DIR`（宿主机共享数据目录）、`FLOWPILOT_COMFYUI_IMAGE`（默认 `comfyui:latest`）、`FLOWPILOT_COMFYUI_CONTAINER_PORT`（默认 8188）。

### 验证

- Go 四包测试全过（新增实例管理 7 个用例与 API 映射用例）；前端 lint／build 通过。
- DGX Spark 实测：以 `flowpilot-web:0.0.1.0` 自身镜像作为实例镜像（Spark 上暂无 ComfyUI 镜像，容器端口 18080），通过界面完成 `regress-01` 的创建（GPU 0／端口 18188）、访问验证、停止、启动、删除全生命周期；21 项远程回归全部通过；业务容器 `comfyui-h3` 不受影响。

详细记录：[0.0.1.0](context/changes/0.0.1.0.md)

## 0.0.0.6 — 2026-11-08

### 修复

- 删除 Vite 脚手架残留的死代码 `pages/Home.tsx` 与 `App.css`。
- 修复硬件面板在采集失败（总量为 0）时内存、存储百分比产生 NaN 的除零问题，统一 `safePercent`。
- 修复 `getHardware` 轮询无超时、请求可能堆积的问题，增加 4 秒超时。
- 修复后端 CPU 利用率在计数器抖动时可能越界的问题，钳制到 0–100 并补充回归用例。
- 修复侧栏、页头、系统页和诊断导出写死"慧浪 GPU-S01 · 2 × L40S"的展示错误：真实硬件可用时显示真实主机名与 GPU 概要，系统页改用真实 Docker 主机名。
- 系统页"硬件仍为 Demo"的过时文案更正为硬件实时采集说明。

### 新增

- 硬件接口返回 `machineModel`（DMI 产品名，安装脚本只读挂载），真实面板显示机型，如"NVIDIA DGX Spark · 实时采集"。

### 验证

- Go 测试新增 DMI 机型与 CPU 钳制用例，三包全过；前端 lint／build 通过。
- DGX Spark 实测：`machineModel=NVIDIA DGX Spark`，侧栏／页头／面板均显示真实主机信息；18 项远程回归全部通过；`comfyui-h3` 保持运行。

详细记录：[0.0.0.6](context/changes/0.0.0.6.md)

## 0.0.0.5 — 2026-11-07

### 新增

- 新增真实硬件采集接口 `GET /api/v1/hardware`：nvidia-smi GPU 指标（型号、利用率、温度、功耗、显存）＋ `/proc` 主机指标（CPU、内存）＋ 数据盘 statfs，纯 Go 标准库实现。
- 统一内存架构自动识别：GB10 等 `nvidia-smi` 显存返回 `[N/A]` 的机型标记为 unified，显存字段降级为系统统一内存。
- 前端硬件面板接入真实接口，2 秒轮询；采集成功显示"本机实测"，失败自动回退 Demo 并明确标注；不可用的 GPU 指标显示为"—"。
- 安装脚本新增 GPU 透传：优先 `--gpus all`（NVIDIA Container Toolkit），缺失时手动挂载宿主机 nvidia-smi、libnvidia-ml 和 `/dev/nvidia*` 设备。

### 调整

- 一体化镜像运行基底由 Alpine 改为 Ubuntu 24.04，保证宿主机 glibc 链接的 nvidia-smi 可在容器内执行；离线发布包由约 6.7 MB 增至约 33 MB。
- 容器新增挂载：共享数据目录 `/data/flowpilot`、只读宿主机信息 `/host/etc/hostname`、`/host/etc/os-release`。
- 回归脚本新增真实硬件分支断言，Demo 分支全部保留。

### 验证

- Go `go test ./...`、`go vet ./...` 通过（hardware 包 8 个用例）；前端 `lint`／`build` 通过。
- DGX Spark 实测：`/api/v1/hardware` 返回真实数据（spark-6558、NVIDIA GB10、unified、38℃、10.78 W、121.7 GB 统一内存、3.67 TB 数据盘）。
- `FLOWPILOT_DEMO_URL=http://192.168.0.77:18080 npm run test:demo`：18 项检查全部通过，含真实硬件面板断言。
- 原业务容器 `comfyui-h3` 保持 `running Paused=false`。

详细记录：[0.0.0.5](context/changes/0.0.0.5.md)

## 0.0.0.4 — 2026-11-07

### 新增

- 新增离线打包、用户目录安装、一键卸载和远程部署脚本：`package-release.sh`、`install.sh`、`uninstall.sh`、`deploy-remote.sh`。
- 安装脚本无需 sudo、不修改系统包，数据统一放入用户目录（默认 `~/.local/share/flowpilot`）。
- 卸载脚本需 `--yes` 或交互确认，只删除 FlowPilot 容器、镜像和用户目录；通过 `flowpilot.managed=true` 容器标签判断镜像是否仍被其他 FlowPilot 实例共享。
- 安装目录设白名单保护，只允许删除 `~/.local/share/flowpilot` 及其 `flowpilot-*` 变体，防止误删系统目录。

### 验证

- 在 DGX Spark（`spark-6558`，Ubuntu 24.04.4 LTS arm64，NVIDIA GB10，Docker 29.2.1／API 1.53）完成真实远程安装、API 联调和 18 项界面回归，全部通过。
- 沙箱卸载验证：共享镜像正确保留；完整卸载验证：容器、镜像、目录全部清理；重装后 `running healthy`。
- 原业务容器 `comfyui-h3` 测试期间曾暂停，最终恢复运行且未被卸载流程影响。

详细记录：[0.0.0.4](context/changes/0.0.0.4.md)

## 0.0.0.3 — 2026-11-04

### 新增

- 首页新增完整硬件监控面板，展示主机名称、操作系统、CPU、内存、存储及使用率。
- 每个 GPU 独立展示型号、显存／统一内存、计算利用率、核心温度、功耗和在线状态。
- 新增双卡 L40S、DGX Spark、DGX Station GB300、单卡及十六卡 RTX 5090 演示机型。
- 新增独立显存、统一内存、一致性内存三种数据模式，避免共享内存重复累计。
- 新增 2 秒 Demo 遥测刷新和对应端到端断言。

### 调整

- 快捷操作改为三列布局，保留实例、任务和原有全部业务流程。
- 多卡场景使用响应式卡片网格，十六卡采用四列紧凑布局，窄屏自动降为单列。
- 项目、后端健康接口和 Docker 镜像版本统一升级为 `0.0.0.3`。

### 验证

- Go 测试和 `go vet`、前端 lint／build、生产依赖安全审计通过。
- `npm run test:demo`：18 项检查通过，零控制台错误、零 HTTP 4xx／5xx 资源。
- 已检查双卡、DGX Spark、十六卡和移动端截图；375px 视口无页面级横向滚动。
- `flowpilot-web-demo` 使用 `flowpilot-web:0.0.0.3`，状态为 `running／healthy`。

详细记录：[0.0.0.3](context/changes/0.0.0.3.md)

## 0.0.0.2 — 2026-11-04

### 新增

- 新增仅使用 Go 标准库的轻量后端，统一提供 SPA 静态页面和版本化 API。
- 新增 Unix Socket Docker 客户端及健康、系统、Engine 信息和容器列表接口。
- 新增 Docker 客户端与 HTTP 服务单元测试，以及真实接口端到端检查。
- 系统管理页接入真实后端和 Docker 信息，诊断包同步纳入运行快照。

### 修复与调整

- 修复 shadcn 主题变量格式不一致导致的新建实例弹窗透明问题，并增加不透明度回归检查。
- 一体化镜像由 Nginx 静态服务改为 Node＋Go 构建、Alpine＋Go 运行。
- Demo 容器挂载 Docker Socket，镜像升级为 `flowpilot-web:0.0.0.2`，端口保持 `18080`。
- 明确区分真实 Docker 数据与仍为 Demo 的硬件、GPU、实例、模型和任务数据。

### 验证

- Go `go test ./...`、`go vet ./...` 通过。
- `npm run lint`、`npm run build` 通过，`npm audit --omit=dev` 为 0 项已知漏洞。
- `npm run test:demo`：14 项检查通过，零控制台错误、零 HTTP 4xx／5xx 资源。
- 真实 Docker Engine `29.4.2`／API `1.54` 读取成功。
- 容器 `flowpilot-web-demo` 使用 `flowpilot-web:0.0.0.2`，状态为 `running／healthy`。

详细记录：[0.0.0.2](context/changes/0.0.0.2.md)

## 0.0.0.1 — 2026-11-04

### 新增

- 建立 FlowPilot MVP 产品需求文档。
- 建立 MVP 文档、对话上下文、代码和版本变更目录。
- 建立四段式版本管理和代码修改留痕规则。
- 保存首轮产品规划与 PRD 对话上下文。
- 新增 React＋TypeScript Web Demo，覆盖看板、实例、模型、任务和系统管理五个页面。
- 新增实例创建与启停、模型下载与暂停、任务重试、环境检测和诊断导出等可交互模拟流程。
- 新增本机 Docker 镜像、Nginx 配置和 Demo 容器管理脚本。
- 新增 Chrome 端到端验证脚本及 375px 视口检查。
- 新增代码与版本日志同步 GitHub 的强制规则。

### 调整

- 将 PRD 统一归档到 `docs/mvp/`。
- 将 PRD 项目版本统一为 `0.0.0.1`。
- 修复窄屏下数据表撑宽页面、弹窗状态重置及 Docker 重建后未更新容器的问题。

### 验证

- `npm run lint` 通过。
- `npm run build` 通过。
- `npm audit --omit=dev`：0 项已知漏洞。
- `npm run test:demo`：10 项检查通过，零控制台错误、零 HTTP 4xx／5xx 资源。
- Docker 容器 `flowpilot-web-demo` 状态为 `running／healthy`，访问返回 HTTP 200。

详细记录：[0.0.0.1](context/changes/0.0.0.1.md)
