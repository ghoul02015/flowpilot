export interface HealthResponse {
  status: string
  service: string
  version: string
}

// formatBytes 将字节数格式化为易读单位。
export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return '—'
  if (bytes < 1024) return `${bytes} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let value = bytes
  let unit = 'B'
  for (const next of units) {
    if (value < 1024) break
    value /= 1024
    unit = next
  }
  return `${value >= 100 ? value.toFixed(0) : value.toFixed(1)} ${unit}`
}

export async function getHealth(signal?: AbortSignal): Promise<HealthResponse> {
  return getJSON<HealthResponse>('/api/v1/health', signal ?? AbortSignal.timeout(5000))
}

export interface DockerEngine {
  info: {
    Name: string
    ServerVersion: string
    OperatingSystem: string
    OSType: string
    Architecture: string
    NCPU: number
    MemTotal: number
    Containers: number
    ContainersRunning: number
    ContainersPaused: number
    ContainersStopped: number
    DockerRootDir: string
  }
  version: {
    Version: string
    ApiVersion: string
    Os: string
    Arch: string
  }
}

export interface DockerContainer {
  Id: string
  Names: string[]
  Image: string
  State: string
  Status: string
  Created: number
  Labels: Record<string, string>
}

export interface RealGPU {
  index: number
  name: string
  memoryTotalMB: number | null
  memoryUsedMB: number | null
  utilizationPercent: number | null
  temperatureC: number | null
  powerDrawW: number | null
  powerLimitW: number | null
}

export interface HardwareReport {
  collectedAt: string
  hostName: string
  machineModel?: string
  os: string
  architecture: string
  cpuModel: string
  cpuCores: number
  cpuUsagePercent: number | null
  memoryTotalMB: number
  memoryUsedMB: number
  storagePath: string
  storageTotalBytes: number
  storageUsedBytes: number
  gpuAvailable: boolean
  gpuError?: string
  gpuMemoryMode: 'discrete' | 'unified' | 'none'
  /** 来自 nvidia-smi 头部；解析失败或不可用时缺省。 */
  driverVersion?: string
  cudaVersion?: string
  gpus: RealGPU[]
}

export interface RuntimeSnapshot {
  health: HealthResponse
  engine: DockerEngine
  containers: DockerContainer[]
}

export interface RealInstance {
  name: string
  containerId: string
  containerName: string
  image: string
  status: 'running' | 'stopped' | 'error'
  gpu: number
  port: number
  autoStart: boolean
  modelsTarget?: string
  outputsTarget?: string
  workflowsTarget?: string
  /** 是否支持「模型关联」（0.0.9.4 起创建的实例为 true）。 */
  modelLinks?: boolean
  createdAt: string
}

export interface InstanceDefaults {
  image: string
  containerPort: number
  dataDir: string
  modelsTarget?: string
  outputsTarget?: string
  workflowsTarget?: string
}

export interface InstanceListResponse {
  instances: RealInstance[]
  defaults: InstanceDefaults
}

export interface CreateInstanceSpec {
  name: string
  image: string
  gpu: number
  port: number
  containerPort: number
  autoStart: boolean
  modelsTarget?: string
  outputsTarget?: string
  workflowsTarget?: string
}

async function getJSON<T>(path: string, signal?: AbortSignal): Promise<T> {
  const response = await fetch(path, { headers: { Accept: 'application/json' }, signal })
  if (!response.ok) {
    const body = await response.json().catch(() => null) as { detail?: string; error?: string } | null
    throw new Error(body?.detail ?? body?.error ?? `请求失败：HTTP ${response.status}`)
  }
  return response.json() as Promise<T>
}

export async function getRuntimeSnapshot(signal?: AbortSignal): Promise<RuntimeSnapshot> {
  const [health, engine, containers] = await Promise.all([
    getJSON<HealthResponse>('/api/v1/health', signal),
    getJSON<DockerEngine>('/api/v1/docker/info', signal),
    getJSON<DockerContainer[]>('/api/v1/docker/containers', signal),
  ])
  return { health, engine, containers }
}

export async function getHardware(signal?: AbortSignal): Promise<HardwareReport> {
  // 面板每 2 秒轮询一次，请求必须带超时，避免挂起的请求持续堆积。
  return getJSON<HardwareReport>('/api/v1/hardware', signal ?? AbortSignal.timeout(4000))
}

async function sendJSON<T>(method: string, path: string, payload?: unknown): Promise<T> {
  const response = await fetch(path, {
    method,
    headers: { Accept: 'application/json', ...(payload !== undefined ? { 'Content-Type': 'application/json' } : {}), ...(csrfToken ? { 'X-FlowPilot-CSRF': csrfToken } : {}) },
    body: payload !== undefined ? JSON.stringify(payload) : undefined,
  })
  if (!response.ok) {
    if (response.status === 401) {
      unauthorizedHandler?.()
    }
    const body = await response.json().catch(() => null) as { detail?: string; error?: string } | null
    throw new Error(body?.detail ?? body?.error ?? `请求失败：HTTP ${response.status}`)
  }
  return response.json() as Promise<T>
}

export interface ModelFile {
  name: string
  category: string
  type: string
  path: string
  sizeBytes: number
  modifiedAt: string
  /** local＝本地仓库（可写）；network＝网络存储（NAS 只读）。 */
  origin: 'local' | 'network'
  /** 集群预留字段，当前恒为「本机」。 */
  node: string
}

export interface ModelReport {
  root: string
  files: ModelFile[]
  totalBytes: number
  scannedAt: string
  network?: { root: string; available: boolean }
}

// 网络存储（NAS）配置视图。
export interface NetworkStorageView {
  enabled: boolean
  type: 'nfs' | 'smb'
  address: string
  remotePath: string
  mountPoint: string
  containerPath: string
  detected: boolean
}

export async function getNetworkStorage(): Promise<NetworkStorageView> {
  return getJSON<NetworkStorageView>('/api/v1/storage/network', AbortSignal.timeout(5000))
}

export async function saveNetworkStorage(config: { enabled: boolean; type: string; address: string; remotePath: string }): Promise<NetworkStorageView> {
  return sendJSON<NetworkStorageView>('PUT', '/api/v1/storage/network', config)
}

export function networkMountScriptURL(): string {
  return '/api/v1/storage/network/mount-script'
}

export interface ImageCheckResponse {
  image: string
  exists: boolean
}

// LocalImage 是本机 Docker 镜像摘要（创建实例向导的镜像候选）。
export interface LocalImage {
  Id: string
  RepoTags: string[]
  Size: number
  Created: number
  Labels?: Record<string, string>
}

export async function getLocalImages(): Promise<LocalImage[]> {
  const body = await getJSON<{ images: LocalImage[] }>('/api/v1/images/local', AbortSignal.timeout(8000))
  return body.images
}

// ImageRecipe 是一套 ComfyUI 镜像构建配方（按显卡类型区分，本机构建）。
export interface ImageRecipe {
  id: string
  label: string
  image: string
  gpu: 'nvidia' | 'amd' | 'cpu'
  cuda: string
  arch: string
  minSM: string
  description: string
}

export async function getImageCatalog(): Promise<ImageRecipe[]> {
  const body = await getJSON<{ recipes: ImageRecipe[] }>('/api/v1/images/catalog', AbortSignal.timeout(8000))
  return body.recipes
}

export async function deleteLocalImageApi(image: string, force = false): Promise<void> {
  await sendJSON<{ status: string }>('DELETE', `/api/v1/images/local?image=${encodeURIComponent(image)}${force ? '&force=1' : ''}`)
}

export interface ImageTagResult {
  status: 'alias' | 'rename'
  action: 'alias' | 'rename'
  source: string
  target: string
}

export async function tagLocalImageApi(source: string, target: string, mode: 'alias' | 'rename' = 'alias'): Promise<ImageTagResult> {
  return sendJSON<ImageTagResult>('POST', '/api/v1/images/tag', { source, target, mode })
}

export async function importLocalImageApi(file: File): Promise<void> {
  const data = new FormData()
  data.set('file', file)
  const response = await fetch('/api/v1/images/import', { method: 'POST', body: data, headers: { Accept: 'application/json' } })
  if (!response.ok) {
    const body = await response.json().catch(() => null) as { detail?: string; error?: string } | null
    throw new Error(body?.detail ?? body?.error ?? `请求失败：HTTP ${response.status}`)
  }
}

export function exportLocalImageURL(image: string): string {
  return `/api/v1/images/export?image=${encodeURIComponent(image)}`
}

export interface ImageBuildSummary {
  label: string
  value: string
  description?: string
}

// ImageTask 是一个镜像构建／拉取任务（0.0.8.0 起为有界并发任务列表）。
export type ImageTaskState = 'queued' | 'running' | 'done' | 'error' | 'cancelled' | 'interrupted'

export interface ImageTask {
  id: string
  kind: 'build' | 'pull'
  image: string
  variant?: string
  source?: ComfyVersion
  state: ImageTaskState
  message: string
  progress: number
  summary: ImageBuildSummary[]
  logs: string[]
  createdAt: string
  updatedAt: string
}

export interface ImageBuildSpec {
  variant: string
  image: string
  ref: string
  source?: ComfyVersion | null
  pip: string
  manager: string
  platform: string
}

export type ComfyVersionKind = 'release' | 'tag' | 'branch' | 'commit'

export interface ComfyVersion {
  ref: string
  kind: ComfyVersionKind
  version?: string
  commit: string
  commitShort: string
  commitAt?: string
  publishedAt?: string
  prerelease: boolean
  repository: string
  commitUrl: string
  sourceUrl?: string
  resolvedAt?: string
  displayLabel: string
}

export interface ComfyVersionCatalog {
  repository: string
  default?: ComfyVersion
  releases: ComfyVersion[]
  tags: ComfyVersion[]
  branch?: ComfyVersion
  fetchedAt: string
  source: 'online' | 'cache'
  stale: boolean
  warnings?: string[]
}

export async function getComfyVersions(refresh = false): Promise<ComfyVersionCatalog> {
  return getJSON<ComfyVersionCatalog>(`/api/v1/images/comfyui-versions${refresh ? '?refresh=1' : ''}`, AbortSignal.timeout(12000))
}

export async function resolveComfyVersion(ref: string, kind?: string, commit?: string): Promise<ComfyVersion> {
  return sendJSON<ComfyVersion>('POST', '/api/v1/images/comfyui-resolve', { ref, kind, commit })
}

export async function getImageTasks(): Promise<ImageTask[]> {
  const body = await getJSON<{ tasks: ImageTask[] }>('/api/v1/images/tasks', AbortSignal.timeout(5000))
  return body.tasks
}

export async function startImageBuild(spec: ImageBuildSpec): Promise<ImageTask> {
  return sendJSON<ImageTask>('POST', '/api/v1/images/build', spec)
}

export async function cancelImageTask(id: string): Promise<ImageTask> {
  return sendJSON<ImageTask>('POST', `/api/v1/images/tasks/${encodeURIComponent(id)}/cancel`)
}

export async function deleteImageTask(id: string): Promise<void> {
  await sendJSON<{ status: string }>('DELETE', `/api/v1/images/tasks/${encodeURIComponent(id)}`)
}

export interface ImageDiskUsage {
  imagesBytes: number
  imagesCount: number
  buildCacheBytes: number
}

export async function getImageDiskUsage(): Promise<ImageDiskUsage> {
  return getJSON<ImageDiskUsage>('/api/v1/images/disk-usage', AbortSignal.timeout(8000))
}

export async function pruneImageBuildCache(): Promise<number> {
  const body = await sendJSON<{ status: string; reclaimedBytes: number }>('POST', '/api/v1/images/cache/prune')
  return body.reclaimedBytes
}

export interface ComfyRef {
  name: string
  type: 'branch' | 'tag'
}

export async function getComfyUIRefs(): Promise<ComfyRef[]> {
  const body = await getJSON<{ refs: ComfyRef[] }>('/api/v1/images/comfyui-refs', AbortSignal.timeout(8000))
  return body.refs
}

export function buildScriptURL(params: ImageBuildSpec): string {
  const query = new URLSearchParams()
  query.set('variant', params.variant)
  query.set('image', params.image)
  query.set('ref', params.ref)
  query.set('pip', params.pip)
  query.set('manager', params.manager)
  query.set('platform', params.platform)
  if (params.source) {
    query.set('commit', params.source.commit)
    query.set('kind', params.source.kind)
    query.set('version', params.source.version ?? '')
  }
  return `/api/v1/images/build-script?${query.toString()}`
}

export function isComfyImage(tag: string, image?: LocalImage): boolean {
  const normalized = tag.toLowerCase()
  return image?.Labels?.['flowpilot.kind'] === 'comfyui' || normalized.includes('comfyui') || normalized.includes('comfy-ui')
}

// comfyImageVersionLabel 根据镜像标签还原 ComfyUI 源码版本；旧镜像不伪造版本号。
export function comfyImageVersionLabel(image?: LocalImage): string {
  const labels = image?.Labels
  if (!labels) return '未记录版本'
  const commit = labels['flowpilot.comfyui.commit']
  const ref = labels['flowpilot.comfyui.ref'] || labels['flowpilot.comfyui.version'] || ''
  const version = labels['flowpilot.comfyui.version'] || ref
  if (commit) return `${version || ref} · ${commit.slice(0, 7)}`
  if (ref === 'master') return 'master（未固定提交）'
  if (ref) return ref
  return '未记录版本'
}

export function imageBuildSummary(image?: LocalImage): ImageBuildSummary[] {
  if (!image?.Labels) return []
  const labels = image.Labels
  const actualVersion = comfyImageVersionLabel(image)
  const summary = labels['flowpilot.build.summary']
  if (summary) {
    const items = summary.split('；').filter(Boolean).map((part) => {
      const [label, ...rest] = part.split('=')
      return { label, value: rest.join('=') }
    })
    const versionItem = items.find((item) => item.label === 'ComfyUI 版本')
    if (versionItem) {
      versionItem.value = labels['flowpilot.comfyui.commit'] ? actualVersion : versionItem.value === 'master' ? 'master（未固定提交）' : versionItem.value
    }
    return items
  }
  if (labels['flowpilot.kind'] !== 'comfyui') return []
  return [
    { label: 'GPU 配方', value: labels['flowpilot.build.variant'] ?? '—' },
    { label: 'CPU 架构', value: labels['flowpilot.build.platform'] ?? '—' },
    { label: 'ComfyUI 版本', value: actualVersion },
  ]
}

// installScriptURL 生成一键安装脚本的下载地址（固定 commit 与服务端核验参数）。
export function installScriptURL(params: { variant?: string; image?: string; ref?: string; kind?: string; commit?: string; pip?: string; manager?: string }): string {
  const query = new URLSearchParams()
  if (params.variant) query.set('variant', params.variant)
  if (params.image) query.set('image', params.image)
  if (params.ref) query.set('ref', params.ref)
  if (params.kind) query.set('kind', params.kind)
  if (params.commit) query.set('commit', params.commit)
  if (params.pip) query.set('pip', params.pip)
  if (params.manager) query.set('manager', params.manager)
  return `/api/v1/images/install-script?${query.toString()}`
}

export async function getModels(signal?: AbortSignal): Promise<ModelReport> {
  return getJSON<ModelReport>('/api/v1/models', signal ?? AbortSignal.timeout(8000))
}

// CatalogEntry 是模型市场中的一个可下载条目（ModelScope 优先，HF 镜像备选）。
export interface CatalogEntry {
  id: string
  name: string
  category: string
  typeLabel: string
  fileName: string
  description: string
  sizeBytes: number
  repo: string
  filePath: string
  url: string
  fallbackUrl: string
  fallbackGated: boolean
  reachable: boolean
  refreshMessage: string
}

export interface ModelCatalog {
  entries: CatalogEntry[]
  refreshedAt: string
  source: 'builtin' | 'cache'
}

export async function getModelCatalog(signal?: AbortSignal): Promise<ModelCatalog> {
  return getJSON<ModelCatalog>('/api/v1/models/catalog', signal ?? AbortSignal.timeout(8000))
}

export async function refreshModelCatalog(): Promise<ModelCatalog> {
  return sendJSON<ModelCatalog>('POST', '/api/v1/models/catalog/refresh')
}

// ---- 在线模型源（0.0.6.1）：实时状态、源站搜索与仓库文件列表 ----

// SourceStatus 是一个模型源（modelscope / hf）的实时可达性。
export interface SourceStatus {
  id: string
  name: string
  home: string
  reachable: boolean
  latencyMs: number
  checkedAt: string
}

// OnlineRepo 是在线搜索命中的模型仓库。
export interface OnlineRepo {
  source: string
  repo: string
  name: string
  chineseName?: string
  downloads: number
  updatedAt?: string
}

// OnlineFile 是仓库内一个可下载的权重文件（category 为推断的入库类别）。
export interface OnlineFile {
  path: string
  name: string
  sizeBytes: number
  category: string
  url: string
}

export async function getSourcesStatus(): Promise<SourceStatus[]> {
  const body = await getJSON<{ sources: SourceStatus[] }>('/api/v1/sources/status', AbortSignal.timeout(12000))
  return body.sources
}

// OnlineSearchResult 是在线搜索的完整响应（attempts 记录实际尝试过的关键词）。
export interface OnlineSearchResult {
  results: OnlineRepo[]
  attempts: string[]
}

export async function searchOnlineModels(source: string, query: string): Promise<OnlineSearchResult> {
  return getJSON<OnlineSearchResult>(`/api/v1/models/online/search?source=${encodeURIComponent(source)}&q=${encodeURIComponent(query)}`, AbortSignal.timeout(30000))
}

export async function getOnlineRepoFiles(source: string, repo: string): Promise<OnlineFile[]> {
  const body = await getJSON<{ files: OnlineFile[] }>(`/api/v1/models/online/files?source=${encodeURIComponent(source)}&repo=${encodeURIComponent(repo)}`, AbortSignal.timeout(25000))
  return body.files
}

export async function deleteModelApi(path: string): Promise<void> {
  await sendJSON<{ status: string }>('DELETE', `/api/v1/models?path=${encodeURIComponent(path)}`)
}

export async function checkImage(image: string): Promise<ImageCheckResponse> {
  return getJSON<ImageCheckResponse>(`/api/v1/images/check?image=${encodeURIComponent(image)}`, AbortSignal.timeout(8000))
}

export async function startImagePull(image: string): Promise<ImageTask> {
  return sendJSON<ImageTask>('POST', '/api/v1/images/pull', { image })
}

export interface DownloadTask {
  id: string
  kind: 'download' | 'import'
  url: string
  source: string
  category: string
  fileName: string
  path: string
  status: 'running' | 'paused' | 'completed' | 'failed' | 'cancelled'
  totalBytes: number
  downloadedBytes: number
  sha256?: string
  message: string
  createdAt: string
  updatedAt: string
}

export interface DownloadSpec {
  url: string
  category: string
  name?: string
  sha256?: string
}

export async function getDownloads(signal?: AbortSignal): Promise<DownloadTask[]> {
  const body = await getJSON<{ tasks: DownloadTask[] }>('/api/v1/downloads', signal ?? AbortSignal.timeout(5000))
  return body.tasks
}

export async function createDownloadApi(spec: DownloadSpec): Promise<DownloadTask> {
  return sendJSON<DownloadTask>('POST', '/api/v1/downloads', spec)
}

export interface ImportSpec {
  path: string
  category: string
  name?: string
  sha256?: string
}

export async function createImportApi(spec: ImportSpec): Promise<DownloadTask> {
  return sendJSON<DownloadTask>('POST', '/api/v1/imports', spec)
}

export async function downloadActionApi(id: string, action: 'pause' | 'resume' | 'cancel' | 'retry'): Promise<DownloadTask> {
  return sendJSON<DownloadTask>('POST', `/api/v1/downloads/${encodeURIComponent(id)}/${action}`)
}

export async function deleteDownloadApi(id: string): Promise<void> {
  await sendJSON<{ status: string }>('DELETE', `/api/v1/downloads/${encodeURIComponent(id)}`)
}

// modelTypeToCategory 将界面模型类型映射为 ComfyUI 仓库子目录。
export function modelTypeToCategory(type: string): string {
  const mapping: Record<string, string> = {
    Checkpoints: 'checkpoints',
    LoRA: 'loras',
    VAE: 'vae',
    ControlNet: 'controlnet',
    Embeddings: 'embeddings',
    'Upscale Models': 'upscale_models',
    'Text Encoders': 'text_encoders',
    'Diffusion Models': 'diffusion_models',
    'CLIP Vision': 'clip_vision',
    'Model Patches': 'model_patches',
  }
  return mapping[type] ?? 'other'
}

export async function getInstances(signal?: AbortSignal): Promise<InstanceListResponse> {
  return getJSON<InstanceListResponse>('/api/v1/instances', signal ?? AbortSignal.timeout(5000))
}

// ComfyStatus 是实例 ComfyUI 制图队列的摘要（不可达时 reachable=false）。
export interface ComfyStatus {
  reachable: boolean
  instanceStatus: string
  queueRunning: number
  queuePending: number
  comfyuiVersion?: string
}

export async function getComfyStatus(name: string): Promise<ComfyStatus> {
  return getJSON<ComfyStatus>(`/api/v1/instances/${encodeURIComponent(name)}/comfy/status`, AbortSignal.timeout(6000))
}

export async function createInstanceApi(spec: CreateInstanceSpec): Promise<RealInstance> {
  return sendJSON<RealInstance>('POST', '/api/v1/instances', spec)
}

// InstanceActionResponse 0.0.9.8 起含队列警告（停止/重启时有排队任务时非空）。
export interface InstanceActionResponse {
  instance: RealInstance
  queueWarning?: string
}

export async function postInstanceAction(name: string, action: 'start' | 'stop' | 'restart'): Promise<InstanceActionResponse> {
  return sendJSON<InstanceActionResponse>('POST', `/api/v1/instances/${encodeURIComponent(name)}/${action}`)
}

export async function deleteInstanceApi(name: string, force: boolean, purge = false): Promise<void> {
  await sendJSON<{ status: string }>('DELETE', `/api/v1/instances/${encodeURIComponent(name)}?force=${force ? 1 : 0}${purge ? '&purge=1' : ''}`, undefined)
}

export interface WorkspaceItem {
  uuid: string
  name: string
  hasContainer: boolean
  orphan: boolean
  exists: boolean
}

export async function listWorkspaces(): Promise<{ workspaces: WorkspaceItem[] }> {
  return getJSON<{ workspaces: WorkspaceItem[] }>('/api/v1/instances/workspaces', AbortSignal.timeout(6000))
}

export interface MigrationDirectoryStats {
  files: number
  bytes: number
  exists: boolean
  lastModified?: string
}

export interface MigrationScanReport {
  scannedAt: string
  outputs: MigrationDirectoryStats
  workflows: MigrationDirectoryStats
}

export interface MigrationPreviewPlan {
  instance: string
  workspace: string
  available: boolean
  reason?: string
  scope: Record<string, MigrationDirectoryStats>
  totalFiles: number
  totalBytes: number
  backupBytes: number
}

export interface MigrationRunResult {
  instance: string
  mode: string
  scope: string[]
  backupDir: string
  copiedFiles: Record<string, number>
  copiedBytes: Record<string, number>
  verified: boolean
  duration: string
  warnings: string[]
}

export async function migrationScan(): Promise<MigrationScanReport> {
  return getJSON<MigrationScanReport>('/api/v1/migration/scan', AbortSignal.timeout(10000))
}

export async function migrationPreview(instance: string): Promise<MigrationPreviewPlan> {
  return getJSON<MigrationPreviewPlan>(`/api/v1/migration/preview?instance=${encodeURIComponent(instance)}`, AbortSignal.timeout(10000))
}

// 0.0.9.9 起迁移任务化：立即返回任务 ID，进度与结果经统一任务接口查询。
export async function migrationRun(instance: string, scope: string[], mode: 'copy' | 'move'): Promise<{ taskId: string; status: string }> {
  return sendJSON<{ taskId: string; status: string }>('POST', '/api/v1/migration/run', { instance, scope, mode })
}

// ---- 实例日志（SSE 实时流）与模型关联（0.0.9.4）----

// InstanceLogEvent 是一行容器日志（stream 区分 stdout/stderr）。
export interface InstanceLogEvent {
  stream: 'stdout' | 'stderr'
  line: string
  time?: string
}

// InstanceLogMeta 是日志流的首帧元信息。
export interface InstanceLogMeta {
  instance: string
  tail: number
  tty: boolean
  status: string
}

// openInstanceLogs 建立实例日志 SSE 连接：先回放最近 tail 行再实时跟随。
// 调用方负责 close()；流自然结束会收到 end 事件后自动关闭。
export function openInstanceLogs(name: string, tail = 200): EventSource {
  return new EventSource(`/api/v1/instances/${encodeURIComponent(name)}/logs?tail=${tail}`)
}

// ModelLink 是实例关联的一个共享仓库模型。
export interface ModelLink {
  category: string
  name: string
}

export interface ModelLinksView {
  instance: string
  /** false 表示实例由旧版本创建、缺少关联挂载，需重建后使用。 */
  supported: boolean
  links: ModelLink[]
}

export interface ModelLinksUpdateResult extends ModelLinksView {
  added: number
  removed: number
  hint?: string
}

export async function getModelLinks(name: string): Promise<ModelLinksView> {
  return getJSON<ModelLinksView>(`/api/v1/instances/${encodeURIComponent(name)}/model-links`, AbortSignal.timeout(6000))
}

export async function putModelLinks(name: string, links: ModelLink[]): Promise<ModelLinksUpdateResult> {
  return sendJSON<ModelLinksUpdateResult>('PUT', `/api/v1/instances/${encodeURIComponent(name)}/model-links`, { links })
}

// 数据底座（0.0.9.5）：SQLite 状态、一致性检查与备份。

export interface DatabaseMigration {
  version: number
  name: string
  appliedAt: string
  result: string
}

export interface DatabaseSyncSummary {
  checkedAt: string
  instances: number
  orphans: number
  conflicts: number
  imported: number
  updated: number
  rebuilt: number
  unchanged: number
  ok: boolean
}

export interface DatabaseStatus {
  schemaVersion: number
  migrations: DatabaseMigration[]
  instanceMappings: number
  orphanMappings: number
  lastSync?: DatabaseSyncSummary
  integrity: { ok: boolean; message: string }
  driver: string
  journalMode: string
}

export interface DatabaseFieldDiff {
  field: string
  database: string
  docker: string
  adopted: string
  reason: string
}

export interface DatabaseInstanceDiff {
  name: string
  uuid: string
  kind: 'imported' | 'updated' | 'rebuilt' | 'unchanged' | 'orphan'
  orphan: boolean
  diffs?: DatabaseFieldDiff[]
  adoptedTo?: string
}

export interface DatabaseSyncConflict {
  name: string
  reason: string
  details?: string
}

export interface DatabaseSyncReport {
  checkedAt: string
  imported: string[]
  updated: string[]
  rebuilt: string[]
  orphans: string[]
  conflicts: DatabaseSyncConflict[]
  diffs: DatabaseInstanceDiff[]
  unchanged: number
  duration: string
}

export interface DatabaseBackupResult {
  path: string
  name: string
  size: number
}

export async function getDatabaseStatus(signal?: AbortSignal): Promise<DatabaseStatus> {
  return getJSON<DatabaseStatus>('/api/v1/database', signal ?? AbortSignal.timeout(6000))
}

export async function runDatabaseCheck(): Promise<DatabaseSyncReport> {
  return sendJSON<DatabaseSyncReport>('POST', '/api/v1/database/check', undefined)
}

export async function backupDatabase(): Promise<DatabaseBackupResult> {
  return sendJSON<DatabaseBackupResult>('POST', '/api/v1/database/backup', undefined)
}

// 认证与会话（0.0.9.6）。

// csrfToken 全局保存：登录/初始化成功或 session 恢复时更新；写请求自动注入。
let csrfToken = ''

export function setCSRFToken(token: string): void {
  csrfToken = token
}

export function getCSRFToken(): string {
  return csrfToken
}

// onUnauthorized 由 App 注册：任何请求遇到 401 时全局退出到登录页。
let unauthorizedHandler: (() => void) | null = null

export function setUnauthorizedHandler(handler: () => void): void {
  unauthorizedHandler = handler
}

export interface SetupStatus {
  initialized: boolean
  tokenReady: boolean
}

export interface LoginResult {
  username: string
  csrfToken: string
}

export interface SessionInfo {
  username: string
  csrfToken: string
}

export interface AuthSession {
  id: string
  username: string
  createdAt: string
  lastSeenAt: string
  idleExpiresAt: string
  absoluteExpiresAt: string
  ipHash: string
  userAgent: string
  current?: boolean
}

export interface AuditLogEntry {
  id: number
  ts: string
  actor: string
  action: string
  object: string
  result: string
  ipHash: string
  requestId: string
  detail: string
}

export interface AuditLogPage {
  logs: AuditLogEntry[]
  total: number
  limit: number
  offset: number
}

export async function getSetupStatus(signal?: AbortSignal): Promise<SetupStatus> {
  return getJSON<SetupStatus>('/api/v1/auth/setup/status', signal ?? AbortSignal.timeout(5000))
}

export async function completeSetup(token: string, password: string): Promise<{ initialized: boolean }> {
  return sendJSON<{ initialized: boolean }>('POST', '/api/v1/auth/setup', { token, password })
}

export async function login(username: string, password: string): Promise<LoginResult> {
  const result = await sendJSON<LoginResult>('POST', '/api/v1/auth/login', { username, password })
  setCSRFToken(result.csrfToken)
  return result
}

export async function logout(): Promise<void> {
  try {
    await sendJSON<{ ok: boolean }>('POST', '/api/v1/auth/logout', undefined)
  } finally {
    setCSRFToken('')
  }
}

export async function getSession(): Promise<SessionInfo> {
  const info = await getJSON<SessionInfo>('/api/v1/auth/session', AbortSignal.timeout(5000))
  setCSRFToken(info.csrfToken)
  return info
}

export async function changePassword(current: string, next: string): Promise<{ ok: boolean }> {
  return sendJSON<{ ok: boolean }>('PUT', '/api/v1/auth/password', { current, next })
}

export async function listAuthSessions(): Promise<{ sessions: AuthSession[] }> {
  return getJSON<{ sessions: AuthSession[] }>('/api/v1/auth/sessions', AbortSignal.timeout(6000))
}

export async function revokeAuthSession(id: string): Promise<{ ok: boolean }> {
  return sendJSON<{ ok: boolean }>('DELETE', `/api/v1/auth/sessions/${encodeURIComponent(id)}`, undefined)
}

export async function getAuditLogs(params: { limit?: number; offset?: number; action?: string; q?: string }): Promise<AuditLogPage> {
  const query = new URLSearchParams()
  if (params.limit) query.set('limit', String(params.limit))
  if (params.offset) query.set('offset', String(params.offset))
  if (params.action) query.set('action', params.action)
  if (params.q) query.set('q', params.q)
  return getJSON<AuditLogPage>(`/api/v1/audit/logs?${query.toString()}`, AbortSignal.timeout(6000))
}

// 应用健康与生命周期（0.0.9.8）。

export interface InstanceHealthView {
  name: string
  uuid?: string
  container: string
  app: 'ready' | 'starting' | 'degraded' | 'failed' | 'not_running' | 'unknown'
  lastCheckAt?: string
  latenciesMs: number[]
  nextCheckAt?: string
}

export interface InstanceEventEntry {
  id: number
  instanceUuid: string
  name: string
  ts: string
  kind: string
  detail: string
  latencyMs: number
}

export interface RebuildPlanField {
  field: string
  current: string
  previous: string
  changed: boolean
}

export interface RebuildPlan {
  name: string
  uuid: string
  workspaceKept: boolean
  interrupt: boolean
  fields: RebuildPlanField[]
  dataNote: string
}

export async function getInstanceHealth(name: string): Promise<{ health: InstanceHealthView; recentEvents: InstanceEventEntry[] }> {
  return getJSON<{ health: InstanceHealthView; recentEvents: InstanceEventEntry[] }>(`/api/v1/instances/${encodeURIComponent(name)}/health`, AbortSignal.timeout(5000))
}

export async function getInstanceEvents(name: string, limit = 50): Promise<{ events: InstanceEventEntry[] }> {
  return getJSON<{ events: InstanceEventEntry[] }>(`/api/v1/instances/${encodeURIComponent(name)}/events?limit=${limit}`, AbortSignal.timeout(6000))
}

export async function getRebuildPlan(name: string): Promise<RebuildPlan> {
  return getJSON<RebuildPlan>(`/api/v1/instances/${encodeURIComponent(name)}/rebuild/plan`, AbortSignal.timeout(6000))
}

// 统一任务（0.0.9.9）。

export interface UnifiedTaskView {
  id: string
  type: 'download' | 'import' | 'image-pull' | 'image-build' | 'migration' | string
  name: string
  objectType: string
  objectRef: string
  status: 'queued' | 'running' | 'paused' | 'succeeded' | 'failed' | 'cancelled' | 'partial' | string
  progress: number
  message: string
  error?: string
  cancelable: boolean
  createdAt: string
  startedAt?: string
  finishedAt?: string
}

export interface UnifiedTaskPage {
  tasks: UnifiedTaskView[]
  total: number
  limit: number
  offset: number
}

export async function getUnifiedTasks(params?: { type?: string; status?: string; q?: string; limit?: number; offset?: number }): Promise<UnifiedTaskPage> {
  const query = new URLSearchParams()
  if (params?.type) query.set('type', params.type)
  if (params?.status) query.set('status', params.status)
  if (params?.q) query.set('q', params.q)
  if (params?.limit) query.set('limit', String(params.limit))
  if (params?.offset) query.set('offset', String(params.offset))
  const suffix = query.toString() ? `?${query.toString()}` : ''
  return getJSON<UnifiedTaskPage>(`/api/v1/tasks${suffix}`, AbortSignal.timeout(6000))
}

export async function getUnifiedTask(id: string): Promise<UnifiedTaskView> {
  return getJSON<UnifiedTaskView>(`/api/v1/tasks/${encodeURIComponent(id)}`, AbortSignal.timeout(6000))
}

export async function deleteUnifiedTask(id: string): Promise<void> {
  await sendJSON<{ status: string }>('DELETE', `/api/v1/tasks/${encodeURIComponent(id)}`)
}

export async function cleanupFinishedTasks(type?: string): Promise<{ deleted: number; failed: number }> {
  const query = type ? `?type=${encodeURIComponent(type)}` : ''
  return sendJSON<{ deleted: number; failed: number }>('DELETE', `/api/v1/tasks${query}`)
}

export async function getImageTaskLogs(unifiedId: string): Promise<string[]> {
  const id = unifiedId.replace(/^img-/, '')
  const tasks = await getImageTasks()
  return tasks.find((task) => task.id === id)?.logs ?? []
}

// 备份（0.0.9.12）。

export interface BackupManifest {
  createdAt: string
  version: string
  components: Record<string, boolean>
  checksum: string
  sizeBytes: number
  instanceCount: number
  notes?: string
}

export interface BackupItem {
  path: string
  name: string
  sizeBytes: number
  checksum: string
  manifest?: BackupManifest
}

export async function listBackups(): Promise<{ backups: BackupItem[] }> {
  return getJSON<{ backups: BackupItem[] }>('/api/v1/backups', AbortSignal.timeout(6000))
}

export async function createBackup(): Promise<{ taskId: string; status: string }> {
  return sendJSON<{ taskId: string; status: string }>('POST', '/api/v1/backups', undefined)
}

// 硬件兼容性检查（0.0.9.13）。

export interface CompatItem {
  id: string
  name: string
  result: 'pass' | 'warn' | 'block' | string
  detail: string
  advice?: string
}

export interface CompatReport {
  items: CompatItem[]
  summary: Record<string, number>
}

export async function getHardwareCompat(): Promise<CompatReport> {
  return getJSON<CompatReport>('/api/v1/hardware/compat', AbortSignal.timeout(8000))
}

// ===== 模型组合套装（0.1.0.4）=====

export interface BundleLicense {
  name: string
  sourceURL: string
  snapshotDate: string
  reviewStatus: string
  noticeRequired: boolean
  commercialUIRequired: boolean
  acknowledgementVersion: string
  deliveryBoundary: string
}

export interface BundleItemState {
  key: string
  role: string
  required: boolean
  targetPath: string
  status: string
  expectedBytes: number
  actualBytes: number
  reason?: string
}

export interface BundleState {
  bundleId: string
  state: string
  items: BundleItemState[]
  requiredTotal: number
  requiredPresent: number
  optionalTotal: number
  optionalPresent: number
  requiredBytes: number
  presentBytes: number
  missingBytes: number
  scannedAt: string
}

export interface BundleInstallItem {
  itemKey: string
  ordinal: number
  required: boolean
  targetPath: string
  expectedBytes: number
  expectedSha256: string
  status: string
  childTaskId?: string
  actualBytes: number
  attempts: number
  errorMessage?: string
}

export interface BundleInstall {
  id: string
  bundleId: string
  bundleVersion: string
  variant: string
  selectedOptions: string[]
  status: string
  currentItemKey: string
  totalBytes: number
  completedBytes: number
  currentItemDownloaded?: number
  errorMessage?: string
  createdAt: string
  updatedAt: string
  startedAt?: string
  finishedAt?: string
  items?: BundleInstallItem[]
}

export interface BundleAcceptance {
  id: string
  bundleId: string
  bundleVersion: string
  installId: string
  instanceName: string
  result: string
  notes: string
  operator: string
  createdAt: string
}

export interface BundleCard {
  id: string
  version: string
  name: string
  modelName: string
  summary: string
  capability: string[]
  releaseChannel: string
  experimental: boolean
  requiredItems: number
  optionalItems: number
  requiredBytes: number
  totalBytes: number
  state: BundleState
  hardwareClass: string
  hardwareLevel: string
  hardwareNote: string
  license: BundleLicense
  certification: { hardwareClass: string; status: string; evidence: string }[]
  activeInstall?: BundleInstall
  latestAcceptance?: BundleAcceptance
}

export interface BundleCheck {
  code: string
  status: string
  title: string
  detail: string
  observed?: string
  required?: string
  remediation?: string
}

export interface BundlePreflight {
  bundleId: string
  bundleVersion: string
  variant: string
  checks: BundleCheck[]
  canInstall: boolean
  canDownloadOnly: boolean
  plan?: { totalBytes?: number; requiredBytes?: number; optionalBytes?: number }
}

export interface BundleDetail {
  card: BundleCard
  manifest: { version: string; bundleVersion: string; checksum: string; generatedAt: string }
  items: BundleItemState[]
  workflows: { name: string; officialURL: string; templateStatus: string; acceptancePreset: string }[]
  hardware: { gpus: { index: number; name: string; memoryTotalMB: number }[]; unifiedMemory: boolean; hostModel: string; error?: string }
  install?: BundleInstall
  acceptances: BundleAcceptance[]
}

export async function getModelBundles(): Promise<{ bundles: BundleCard[]; manifest: string; bundleVersion: string; checksum: string }> {
  return getJSON('/api/v1/model-bundles', AbortSignal.timeout(10000))
}

export async function getModelBundle(id: string): Promise<BundleDetail> {
  return getJSON(`/api/v1/model-bundles/${encodeURIComponent(id)}`, AbortSignal.timeout(10000))
}

export async function preflightModelBundle(id: string, variant: string, selectedOptions: string[]): Promise<BundlePreflight> {
  return sendJSON<BundlePreflight>('POST', `/api/v1/model-bundles/${encodeURIComponent(id)}/preflight`, { variant, selectedOptions })
}

export async function confirmModelBundleLicense(id: string, deliveryMethod = 'online'): Promise<void> {
  await sendJSON('POST', `/api/v1/model-bundles/${encodeURIComponent(id)}/license`, { deliveryMethod })
}

export async function createBundleInstall(id: string, payload: { variant: string; selectedOptions: string[]; requestKey: string }): Promise<BundleInstall> {
  return sendJSON<BundleInstall>('POST', `/api/v1/model-bundles/${encodeURIComponent(id)}/installs`, payload)
}

export async function getBundleInstall(id: string, installId: string): Promise<BundleInstall> {
  return getJSON(`/api/v1/model-bundles/${encodeURIComponent(id)}/installs/${encodeURIComponent(installId)}`, AbortSignal.timeout(8000))
}

export async function bundleInstallAction(id: string, installId: string, action: 'pause' | 'resume' | 'cancel' | 'retry'): Promise<BundleInstall> {
  return sendJSON<BundleInstall>('POST', `/api/v1/model-bundles/${encodeURIComponent(id)}/installs/${encodeURIComponent(installId)}/${action}`)
}

export async function linkModelBundle(id: string, installId: string, instance: string): Promise<{ added: string[] }> {
  return sendJSON('POST', `/api/v1/model-bundles/${encodeURIComponent(id)}/link`, { installId, instance })
}

export async function listBundleAcceptances(id: string): Promise<{ acceptances: BundleAcceptance[]; total: number }> {
  return getJSON(`/api/v1/model-bundles/${encodeURIComponent(id)}/acceptances`, AbortSignal.timeout(8000))
}

export async function createBundleAcceptance(id: string, payload: { installId?: string; instance: string; result: string; notes: string }): Promise<BundleAcceptance> {
  return sendJSON<BundleAcceptance>('POST', `/api/v1/model-bundles/${encodeURIComponent(id)}/acceptances`, payload)
}
