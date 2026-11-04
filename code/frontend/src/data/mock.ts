import type { Instance, ModelItem, TaskItem } from '@/types'

export const initialInstances: Instance[] = [
  {
    id: 'ins-prod-01',
    name: '生产绘图-01',
    template: 'ComfyUI 标准版',
    version: 'v0.3.76',
    status: 'running',
    gpu: 0,
    port: 8188,
    autoStart: true,
    models: 8,
    createdAt: '2026-11-04 14:32',
    uptime: '运行 1 小时 42 分',
  },
  {
    id: 'ins-video-02',
    name: '视频工作流-02',
    template: 'ComfyUI 视频版',
    version: 'v0.3.76',
    status: 'stopped',
    gpu: 1,
    port: 8189,
    autoStart: false,
    models: 5,
    createdAt: '2026-11-04 11:18',
    uptime: '已停止 36 分钟',
  },
  {
    id: 'ins-test-03',
    name: '节点测试-03',
    template: 'ComfyUI 标准版',
    version: 'v0.3.75',
    status: 'error',
    gpu: 1,
    port: 8190,
    autoStart: false,
    models: 3,
    createdAt: '2026-11-03 18:07',
    uptime: '健康检查失败',
  },
]

export const initialModels: ModelItem[] = [
  { id: 'mod-001', name: 'sd_xl_base_1.0.safetensors', type: 'Checkpoints', size: '6.94 GB', source: '内部模型库', status: 'ready', progress: 100, createdAt: '2026-11-04 13:20' },
  { id: 'mod-002', name: 'sd_xl_refiner_1.0.safetensors', type: 'Checkpoints', size: '6.08 GB', source: '内部模型库', status: 'ready', progress: 100, createdAt: '2026-11-04 13:18' },
  { id: 'mod-003', name: 'detail-tweaker-xl.safetensors', type: 'LoRA', size: '256 MB', source: 'URL 下载', status: 'ready', progress: 100, createdAt: '2026-11-04 12:42' },
  { id: 'mod-004', name: 'sdxl_vae.safetensors', type: 'VAE', size: '335 MB', source: '随机模型包', status: 'ready', progress: 100, createdAt: '2026-11-04 10:06' },
  { id: 'mod-005', name: 'controlnet-canny-sdxl.safetensors', type: 'ControlNet', size: '2.50 GB', source: 'URL 下载', status: 'downloading', progress: 68, createdAt: '2026-11-04 16:21' },
  { id: 'mod-006', name: '4x-UltraSharp.pth', type: 'Upscale Models', size: '67 MB', source: '内部模型库', status: 'ready', progress: 100, createdAt: '2026-11-03 17:55' },
]

export const initialTasks: TaskItem[] = [
  { id: 'task-001', name: '下载 controlnet-canny-sdxl', type: '模型下载', target: 'controlnet-canny-sdxl.safetensors', status: 'running', progress: 68, createdAt: '2026-11-04 16:21', message: '已下载 1.70 GB／2.50 GB，当前速度 42 MB/s', modelId: 'mod-005' },
  { id: 'task-002', name: '部署 生产绘图-01', type: '实例部署', target: '生产绘图-01', status: 'completed', progress: 100, createdAt: '2026-11-04 14:32', message: '镜像校验、容器创建及健康检查均已通过', instanceId: 'ins-prod-01' },
  { id: 'task-003', name: '导入 sdxl_vae', type: '模型导入', target: 'sdxl_vae.safetensors', status: 'completed', progress: 100, createdAt: '2026-11-04 10:06', message: 'SHA256 校验通过，模型已写入共享仓库' },
  { id: 'task-004', name: '启动 节点测试-03', type: '实例部署', target: '节点测试-03', status: 'failed', progress: 82, createdAt: '2026-11-04 15:48', message: '健康检查超时：ComfyUI 端口未在 60 秒内响应', instanceId: 'ins-test-03' },
]
