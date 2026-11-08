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
    headers: { Accept: 'application/json', ...(payload !== undefined ? { 'Content-Type': 'application/json' } : {}) },
    body: payload !== undefined ? JSON.stringify(payload) : undefined,
  })
  if (!response.ok) {
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
}

export interface ModelReport {
  root: string
  files: ModelFile[]
  totalBytes: number
  scannedAt: string
}

export interface ImageCheckResponse {
  image: string
  exists: boolean
}

export interface ImagePullStatus {
  image: string
  state: 'idle' | 'pulling' | 'done' | 'error'
  message: string
  updatedAt: string
}

// LocalImage 是本机 Docker 镜像摘要（创建实例向导的镜像候选）。
export interface LocalImage {
  Id: string
  RepoTags: string[]
  Size: number
  Created: number
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

// installScriptURL 生成一键安装脚本的下载地址（参数同向导可选安装项）。
export function installScriptURL(params: { variant?: string; image?: string; ref?: string; pip?: string; manager?: string }): string {
  const query = new URLSearchParams()
  if (params.variant) query.set('variant', params.variant)
  if (params.image) query.set('image', params.image)
  if (params.ref) query.set('ref', params.ref)
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

export async function startImagePull(image: string): Promise<ImagePullStatus> {
  return sendJSON<ImagePullStatus>('POST', '/api/v1/images/pull', { image })
}

export async function getImagePullStatus(): Promise<ImagePullStatus> {
  return getJSON<ImagePullStatus>('/api/v1/images/pull', AbortSignal.timeout(5000))
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

export async function postInstanceAction(name: string, action: 'start' | 'stop' | 'restart'): Promise<RealInstance> {
  return sendJSON<RealInstance>('POST', `/api/v1/instances/${encodeURIComponent(name)}/${action}`)
}

export async function deleteInstanceApi(name: string, force: boolean): Promise<void> {
  await sendJSON<{ status: string }>('DELETE', `/api/v1/instances/${encodeURIComponent(name)}${force ? '?force=1' : ''}`)
}
