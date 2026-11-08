import { mkdir, writeFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import { chromium } from 'playwright-core'

const baseURL = process.env.FLOWPILOT_DEMO_URL ?? 'http://localhost:18080'
const executablePath = process.env.CHROME_PATH ?? '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'
const outputDir = fileURLToPath(new URL('../test-results/', import.meta.url))
await mkdir(outputDir, { recursive: true })

const browser = await chromium.launch({ executablePath, headless: true })
const page = await browser.newPage({ viewport: { width: 1440, height: 1000 }, deviceScaleFactor: 1 })
const consoleErrors = []
const failedResponses = []
const checks = []

page.on('console', (message) => {
  if (message.type() === 'error') consoleErrors.push(message.text())
})
page.on('pageerror', (error) => consoleErrors.push(error.message))
page.on('response', (response) => {
  if (response.status() >= 400) failedResponses.push(`${response.status()} ${response.url()}`)
})

function check(name) {
  checks.push(name)
  console.log(`PASS ${name}`)
}

async function screenshot(name) {
  await page.screenshot({ path: `${outputDir}${name}.png`, fullPage: true })
}

try {
  const healthResponse = await page.request.get(`${baseURL}/api/v1/health`)
  const health = await healthResponse.json()
  if (!healthResponse.ok() || health.status !== 'ok' || health.version !== '0.0.7.0') throw new Error(`后端健康检查异常：${JSON.stringify(health)}`)
  check('Go 后端健康接口与版本')

  const dockerResponse = await page.request.get(`${baseURL}/api/v1/docker/info`)
  const docker = await dockerResponse.json()
  if (!dockerResponse.ok() || !docker.info?.ServerVersion || !docker.version?.ApiVersion) throw new Error(`真实 Docker API 异常：${JSON.stringify(docker)}`)
  check('真实 Docker Engine 信息接口')

  const hardwareResponse = await page.request.get(`${baseURL}/api/v1/hardware`)
  const hardware = hardwareResponse.ok() ? await hardwareResponse.json() : null
  const hasRealHardware = Boolean(hardware && hardware.collectedAt)
  if (hasRealHardware) {
    check('真实硬件采集接口')
    if (hardware.gpuAvailable) check(`真实 GPU 数据（${hardware.gpuMemoryMode} · ${hardware.gpus.length} 卡）`)
  }

  await page.goto(baseURL, { waitUntil: 'networkidle' })
  await page.getByRole('heading', { name: /凌晨好|上午好|下午好|晚上好/ }).waitFor()
  await page.getByRole('heading', { name: '硬件与实时监控' }).waitFor()
  check('首页看板与完整硬件信息加载')
  await screenshot('01-dashboard')

  const firstCollected = await page.locator('[data-hardware-collected]').getAttribute('data-hardware-collected')
  await page.waitForFunction((previous) => {
    const current = document.querySelector('[data-hardware-collected]')?.getAttribute('data-hardware-collected')
    return current && current !== previous && current !== ''
  }, firstCollected, { timeout: 6000 })
  check('GPU 遥测每 2 秒自动刷新')

  if (hasRealHardware && hardware.gpuAvailable) {
    await page.locator('[data-hardware-source="real"]').waitFor()
    await page.getByText(hardware.hostName, { exact: false }).first().waitFor()
    if (hardware.machineModel) {
      await page.getByText(hardware.machineModel, { exact: false }).first().waitFor()
    }
    if (hardware.gpuMemoryMode === 'unified') {
      await page.getByText('CPU／GPU 共享，不重复累计', { exact: false }).waitFor()
      await page.getByText('独立显存占用不适用；此处显示系统统一内存使用率。', { exact: true }).first().waitFor()
    }
    check('真实硬件面板展示宿主机数据')
    await screenshot('01-hardware-real')
  } else {
    // 无 GPU 或采集失败的生产环境：面板必须明确提示，而不是回退演示数据。
    await page.getByText(/未检测到 GPU 设备|硬件采集接口暂不可用/, { exact: false }).first().waitFor()
    check('硬件不可用状态明确提示（无演示数据回退）')
    await screenshot('01-hardware-unavailable')
  }

  // 全站不得出现演示数据标识（0.0.5.1 起移除全部模拟数据）。
  const bodyText = await page.evaluate(() => document.body.innerText)
  for (const marker of ['重置演示数据', '演示机型', '本地模拟数据', 'Demo ·', '模拟打开', '演示数据']) {
    if (bodyText.includes(marker)) throw new Error(`页面仍包含演示数据标识：${marker}`)
  }
  check('界面无演示数据标识')

  await page.getByRole('button', { name: 'ComfyUI 实例', exact: true }).click()
  await page.getByRole('heading', { name: '实例管理' }).waitFor()
  check('实例导航')

  await page.getByRole('button', { name: '新建实例', exact: true }).click()
  const createDialog = page.getByRole('dialog')
  await createDialog.waitFor()
  await page.waitForTimeout(300)
  const dialogStyle = await createDialog.evaluate((element) => ({ background: getComputedStyle(element).backgroundColor, opacity: getComputedStyle(element).opacity }))
  if (['transparent', 'rgba(0, 0, 0, 0)'].includes(dialogStyle.background) || dialogStyle.opacity !== '1') throw new Error(`新建实例弹窗未正常显示：${JSON.stringify(dialogStyle)}`)
  check('新建实例弹窗背景不透明')
  await screenshot('02-create-dialog')

  const instancesApiResponse = await page.request.get(`${baseURL}/api/v1/instances`)
  const realInstancesAvailable = instancesApiResponse.ok()

  if (realInstancesAvailable) {
    // 真实生命周期：创建 → 端口可达 → 停止 → 启动 → 删除，全程通过界面操作。
    const instanceName = 'regress-01'
    // 0.0.7.0：新增镜像相关三个接口的回归断言。
    const localImagesResponse = await page.request.get(`${baseURL}/api/v1/images/local`)
    if (!localImagesResponse.ok()) throw new Error(`本机镜像列表接口异常：HTTP ${localImagesResponse.status()}`)
    check('本机镜像列表接口')
    const catalogResponse = await page.request.get(`${baseURL}/api/v1/images/catalog`)
    const catalogBody = await catalogResponse.json()
    if (!catalogResponse.ok() || !Array.isArray(catalogBody.recipes) || catalogBody.recipes.length < 5) throw new Error(`镜像配方目录异常：${JSON.stringify(catalogBody)}`)
    check('镜像构建配方目录接口（六套多显卡配方）')
    const scriptResponse = await page.request.get(`${baseURL}/api/v1/images/install-script?variant=auto&pip=tsinghua&ref=master&manager=1`)
    const scriptText = await scriptResponse.text()
    if (!scriptResponse.ok() || !scriptText.startsWith('#!/usr/bin/env bash') || !scriptText.includes('nvidia-cu130') || !scriptText.includes('amd-rocm')) throw new Error('安装脚本生成接口异常')
    if (scriptText.includes('modelscope download') || scriptText.includes('hf download')) throw new Error('安装脚本不应包含模型下载逻辑')
    check('一键安装脚本生成接口（多显卡＋不含模型下载）')
    // 0.0.7.0：镜像来源三模式，回归走「手动输入」；容器端口移入高级设置。
    await createDialog.getByLabel('镜像来源').selectOption('custom')
    await createDialog.getByLabel('实例名称').fill(instanceName)
    await createDialog.getByLabel('容器镜像').fill(`flowpilot-web:${health.version}`)
    await createDialog.getByText('已在本机，可直接创建', { exact: false }).waitFor({ timeout: 8000 })
    check('镜像本地存在性检查（创建向导）')
    // 无 GPU 的演示机（如 Mac）没有 GPU 0 选项，保持默认「不分配 GPU」。
    const gpuSelect = createDialog.getByLabel('分配 GPU')
    if (await gpuSelect.locator('option[value="0"]').count() > 0) await gpuSelect.selectOption('0')
    await createDialog.getByLabel('访问端口').fill('18188')
    await createDialog.getByRole('button', { name: /高级设置/ }).click()
    await createDialog.getByLabel('容器端口').fill('18080')
    await page.getByRole('button', { name: /下一步/ }).click()
    await page.getByText('共享模型仓库将自动挂载进实例容器', { exact: false }).waitFor()
    await page.getByRole('button', { name: /下一步/ }).click()
    await page.getByRole('button', { name: /确认部署|正在创建容器/ }).click()
    await page.getByRole('button', { name: `停止 ${instanceName}`, exact: true }).waitFor({ timeout: 30000 })
    check('真实实例创建并启动（Docker API）')

    const instanceURL = new URL(baseURL)
    instanceURL.port = '18188'
    const instanceHealth = await page.request.get(`${instanceURL.origin}/api/v1/health`)
    if (!instanceHealth.ok()) throw new Error(`实例端口不可达：HTTP ${instanceHealth.status()}`)
    check('真实实例端口可访问')

    await page.getByRole('button', { name: `停止 ${instanceName}`, exact: true }).click()
    await page.getByRole('button', { name: `启动 ${instanceName}`, exact: true }).waitFor({ timeout: 20000 })
    check('真实实例停止')
    await page.getByRole('button', { name: `启动 ${instanceName}`, exact: true }).click()
    await page.getByRole('button', { name: `停止 ${instanceName}`, exact: true }).waitFor({ timeout: 20000 })
    check('真实实例启动')
    await screenshot('02-instances-real')

    await page.getByRole('button', { name: `停止 ${instanceName}`, exact: true }).click()
    await page.getByRole('button', { name: `启动 ${instanceName}`, exact: true }).waitFor({ timeout: 20000 })
    await page.getByRole('button', { name: `删除 ${instanceName}`, exact: true }).click()
    const deleteDialog = page.getByRole('dialog')
    await deleteDialog.getByLabel('实例名称').fill(instanceName)
    await deleteDialog.getByRole('button', { name: '确认删除' }).click()
    await page.getByRole('button', { name: `删除 ${instanceName}`, exact: true }).waitFor({ state: 'detached', timeout: 15000 })
    check('真实实例删除')

    // ComfyUI 实例链路：用默认预置镜像（comfyui-h3:spark）创建 →
    // 等待 ComfyUI /system_stats 就绪 → 停止 → 删除。
    const instancesBody = await (await page.request.get(`${baseURL}/api/v1/instances`)).json()
    const comfyImage = instancesBody.defaults?.image ?? 'comfyui:latest'
    const comfyName = 'comfy-real'
    // 默认 ComfyUI 镜像不在本机时（如无 GPU 的开发机），跳过 ComfyUI 实机链路。
    const comfyImageCheck = await (await page.request.get(`${baseURL}/api/v1/images/check?image=${encodeURIComponent(comfyImage)}`)).json()
    if (!comfyImageCheck.exists) {
      console.log(`SKIP ComfyUI 实机链路（镜像 ${comfyImage} 不在本机，需在 GPU 主机上运行）`)
    } else {
    await page.getByRole('button', { name: '新建实例', exact: true }).click()
    const comfyDialog = page.getByRole('dialog')
    await comfyDialog.waitFor()
    await comfyDialog.getByLabel('镜像来源').selectOption('custom')
    await comfyDialog.getByLabel('实例名称').fill(comfyName)
    await comfyDialog.getByLabel('容器镜像').fill(comfyImage)
    await comfyDialog.getByText('已在本机，可直接创建', { exact: false }).waitFor({ timeout: 8000 })
    const comfyGpuSelect = comfyDialog.getByLabel('分配 GPU')
    if (await comfyGpuSelect.locator('option[value="0"]').count() > 0) await comfyGpuSelect.selectOption('0')
    await comfyDialog.getByLabel('访问端口').fill('18189')
    await comfyDialog.getByRole('button', { name: /高级设置/ }).click()
    await comfyDialog.getByLabel('容器端口').fill('8188')
    await page.getByRole('button', { name: /下一步/ }).click()
    await page.getByText('共享模型仓库将自动挂载进实例容器', { exact: false }).waitFor()
    await page.getByRole('button', { name: /下一步/ }).click()
    await page.getByRole('button', { name: /确认部署|正在创建容器/ }).click()
    await page.getByRole('button', { name: `停止 ${comfyName}`, exact: true }).waitFor({ timeout: 60000 })
    check('ComfyUI 实例创建并启动（预置镜像）')

    const comfyURL = new URL(baseURL)
    comfyURL.port = '18189'
    const comfyDeadline = Date.now() + 180000
    let comfyReady = false
    while (Date.now() < comfyDeadline && !comfyReady) {
      try {
        const statsResponse = await page.request.get(`${comfyURL.origin}/system_stats`)
        if (statsResponse.ok()) {
          const stats = await statsResponse.json()
          comfyReady = Boolean(stats.system?.comfyui_version)
        }
      } catch { /* 容器尚未就绪，继续等待 */ }
      if (!comfyReady) await page.waitForTimeout(5000)
    }
    if (!comfyReady) throw new Error('ComfyUI 实例 180 秒内未就绪')
    check('ComfyUI 服务就绪（/system_stats）')
    await screenshot('02-instances-comfyui')

    // ComfyUI 状态代理与「打开应用」跳转（0.0.5.0）：经平台后端探测实例制图状态。
    const comfyStatusDeadline = Date.now() + 60000
    let comfyStatus = null
    while (Date.now() < comfyStatusDeadline && !comfyStatus) {
      try {
        const statusResponse = await page.request.get(`${baseURL}/api/v1/instances/${comfyName}/comfy/status`)
        if (statusResponse.ok()) {
          const statusBody = await statusResponse.json()
          if (statusBody.reachable) comfyStatus = statusBody
        }
      } catch { /* 代理尚未就绪，继续等待 */ }
      if (!comfyStatus) await page.waitForTimeout(3000)
    }
    if (!comfyStatus) throw new Error('comfy/status 代理 60 秒内未返回 reachable')
    if (!comfyStatus.comfyuiVersion) throw new Error('comfy/status 响应缺少 ComfyUI 版本')
    check('ComfyUI 状态代理接口（reachable 与版本）')

    await page.getByTestId(`comfy-queue-${comfyName}`).waitFor({ timeout: 15000 })
    check('实例列表制图任务徽标')

    const openButton = page.getByRole('button', { name: `打开 ${comfyName} ComfyUI` })
    await openButton.waitFor()
    const [comfyPopup] = await Promise.all([
      page.context().waitForEvent('page', { timeout: 15000 }),
      openButton.click(),
    ])
    await comfyPopup.waitForLoadState('domcontentloaded')
    if (!new URL(comfyPopup.url()).port || new URL(comfyPopup.url()).port !== '18189') throw new Error(`打开应用跳转端口异常：${comfyPopup.url()}`)
    await comfyPopup.close()
    check('打开 ComfyUI 应用链接（新窗口跳转正确端口）')

    await page.getByRole('button', { name: `停止 ${comfyName}`, exact: true }).click()
    await page.getByRole('button', { name: `启动 ${comfyName}`, exact: true }).waitFor({ timeout: 30000 })
    check('ComfyUI 实例停止')
    await page.getByRole('button', { name: `删除 ${comfyName}`, exact: true }).click()
    const comfyDeleteDialog = page.getByRole('dialog')
    await comfyDeleteDialog.getByLabel('实例名称').fill(comfyName)
    await comfyDeleteDialog.getByRole('button', { name: '确认删除' }).click()
    await page.getByRole('button', { name: `删除 ${comfyName}`, exact: true }).waitFor({ state: 'detached', timeout: 15000 })
    check('ComfyUI 实例删除')
    }

    // 实例全部删除后应展示生产空态，而非演示数据。
    await page.getByText('暂无 ComfyUI 实例', { exact: true }).waitFor()
    check('实例列表空态展示（无演示数据）')
  } else {
    throw new Error('实例接口不可用：0.0.5.1 起页面仅支持真实后端，请确认平台服务已启动')
  }

  // 模型中心：接口可用时验证真实仓库扫描与删除，否则走 Demo 流程。
  const modelsApiResponse = await page.request.get(`${baseURL}/api/v1/models`)
  const realModelsAvailable = modelsApiResponse.ok()

  await page.getByRole('button', { name: '模型中心', exact: true }).click()
  await page.getByRole('heading', { name: '模型中心' }).waitFor()

  const modelsReport = realModelsAvailable ? await modelsApiResponse.json() : null
  const seedFile = modelsReport?.files?.find((file) => file.path === 'checkpoints/regress-test.safetensors')

  if (realModelsAvailable) {
    check('真实模型仓库扫描接口')
    await page.getByText('共享仓库实时扫描', { exact: true }).waitFor()
    if (seedFile) {
      // 预置的回归文件（部署时在目标机 seed）：checkpoints/regress-test.safetensors
      const seedRow = page.getByRole('row').filter({ hasText: 'regress-test.safetensors' })
      await seedRow.waitFor()
      await seedRow.getByText('Checkpoints', { exact: true }).waitFor()
      check('真实模型文件在界面展示')
      await screenshot('03-models-real')

      await page.getByRole('button', { name: '删除 regress-test.safetensors', exact: true }).click()
      const modelDeleteDialog = page.getByRole('dialog')
      await modelDeleteDialog.getByRole('button', { name: '确认删除' }).click()
      await seedRow.waitFor({ state: 'detached', timeout: 10000 })
      // 仓库扫描存在秒级缓存窗口，轮询等待列表收敛而不是单次取快照。
      const seedDeleteDeadline = Date.now() + 15000
      let seedRemoved = false
      while (Date.now() < seedDeleteDeadline && !seedRemoved) {
        const modelsBody = await (await page.request.get(`${baseURL}/api/v1/models`)).json()
        seedRemoved = !modelsBody.files?.some((file) => file.path === 'checkpoints/regress-test.safetensors')
        if (!seedRemoved) await page.waitForTimeout(500)
      }
      if (!seedRemoved) throw new Error('模型文件删除后仍在仓库中')
      check('真实模型文件界面删除')
    } else {
      check('真实模型仓库为空（跳过种子文件删除验证）')
      await screenshot('03-models-real')
    }

    // 模型市场链路（0.0.6.0）：目录接口 → 界面筛选 → 选择 SDXL Offset LoRA（约 50 MB）
    // 直接 ModelScope 下载 → 写入共享仓库 → 界面删除。
    const catalogResponse = await page.request.get(`${baseURL}/api/v1/models/catalog`)
    const catalogBody = catalogResponse.ok() ? await catalogResponse.json() : null
    if (!catalogBody || !Array.isArray(catalogBody.entries) || catalogBody.entries.length < 10) throw new Error(`模型目录接口异常：${JSON.stringify(catalogBody)?.slice(0, 200)}`)
    if (!catalogBody.entries.every((entry) => entry.url.startsWith('https://modelscope.cn/models/'))) throw new Error('目录主地址必须全部为 ModelScope')
    check('模型市场目录接口（内置主流模型）')

    // 在线源（0.0.6.1）：源状态接口 → ModelScope 在线搜索接口（MiniMax-H3 必须命中 Comfy-Org）。
    const sourcesResponse = await page.request.get(`${baseURL}/api/v1/sources/status`)
    const sourcesBody = sourcesResponse.ok() ? await sourcesResponse.json() : null
    const msStatus = sourcesBody?.sources?.find((item) => item.id === 'modelscope')
    if (!msStatus || typeof msStatus.reachable !== 'boolean' || !sourcesBody.sources.some((item) => item.id === 'hf')) throw new Error(`源状态接口异常：${JSON.stringify(sourcesBody)?.slice(0, 200)}`)
    check('模型源连接状态接口（ModelScope＋HF 镜像）')
    const onlineResponse = await page.request.get(`${baseURL}/api/v1/models/online/search?source=modelscope&q=MiniMax-H3`)
    const onlineBody = onlineResponse.ok() ? await onlineResponse.json() : null
    if (!onlineBody?.results?.some((repo) => repo.repo === 'Comfy-Org/MiniMax-H3')) throw new Error(`在线搜索未命中 Comfy-Org/MiniMax-H3：${JSON.stringify(onlineBody)?.slice(0, 300)}`)
    check('ModelScope 在线搜索接口（MiniMax-H3）')

    // 搜索习惯（0.0.7.0）：周董实测搜 nvfp4 空结果（风控间歇空响应），后端空结果退避重试＋模糊变体，attempts 透明化。
    const habitResponse = await page.request.get(`${baseURL}/api/v1/models/online/search?source=modelscope&q=nvfp4`)
    const habitBody = habitResponse.ok() ? await habitResponse.json() : null
    if (!habitBody || !Array.isArray(habitBody.results) || habitBody.results.length === 0 || !Array.isArray(habitBody.attempts) || habitBody.attempts[0] !== 'nvfp4') throw new Error(`搜索习惯接口异常：${JSON.stringify(habitBody)?.slice(0, 300)}`)
    check('短关键词搜索命中＋attempts 透明化（nvfp4）')

    await page.getByRole('button', { name: '添加模型', exact: true }).click()
    const marketDialog = page.getByRole('dialog')
    await marketDialog.waitFor()
    await marketDialog.getByPlaceholder('搜索模型名称、文件或描述').waitFor({ timeout: 10000 })

    // 推荐目录内置 MiniMax H3 条目（ComfyUI 官方模板命名）。
    await marketDialog.getByPlaceholder('搜索模型名称、文件或描述').fill('MiniMax')
    await marketDialog.getByTestId('market-entry-minimax-h3-fl2va-w6a8').waitFor()
    check('推荐目录 MiniMax H3 条目（模板命名）')

    // 界面在线搜索：切到 ModelScope 源 → 搜索 → 展开 Comfy-Org/MiniMax-H3 → 权重文件带大小与类别。
    await marketDialog.getByTestId('source-tab-modelscope').click()
    // 全词搜索回归（0.0.6.2 修复：周董实测搜 Minimax 空结果，疑似源站风控间歇返回空，后端自动重试一次）
    await marketDialog.getByPlaceholder(/在 ModelScope 搜索模型仓库/).fill('Minimax')
    await marketDialog.getByRole('button', { name: '在线搜索' }).click()
    await marketDialog.getByTestId('online-repo-Comfy-Org/MiniMax-H3').waitFor({ timeout: 30000 })
    check('全词搜索 Minimax 命中（大小写不敏感）')
    // 弹窗横向溢出回归（0.0.6.2 修复：grid 子项 min-width:auto 被长文件名撑破）
    await marketDialog.getByTestId('online-repo-Comfy-Org/MiniMax-H3').getByRole('button').first().click()
    await marketDialog.getByText('minimax_h3_fl2va_pruned_w6a8.safetensors').waitFor({ timeout: 30000 })
    const dialogMetrics = await marketDialog.evaluate((el) => ({ clientWidth: el.clientWidth, scrollWidth: el.scrollWidth }))
    if (dialogMetrics.scrollWidth > dialogMetrics.clientWidth + 1) throw new Error(`弹窗横向溢出：scrollWidth=${dialogMetrics.scrollWidth} > clientWidth=${dialogMetrics.clientWidth}`)
    check('弹窗无横向溢出')
    await marketDialog.getByPlaceholder(/在 ModelScope 搜索模型仓库/).fill('MiniMax-H3')
    await marketDialog.getByRole('button', { name: '在线搜索' }).click()
    const minimaxRepo = marketDialog.getByTestId('online-repo-Comfy-Org/MiniMax-H3')
    await minimaxRepo.waitFor({ timeout: 30000 })
    await minimaxRepo.getByRole('button').first().click()
    await minimaxRepo.getByText('minimax_h3_fl2va_pruned_w6a8.safetensors').waitFor({ timeout: 30000 })
    await minimaxRepo.getByText(/\d+(\.\d+)? GB/).first().waitFor()
    check('在线源搜索与仓库文件列表（真实大小）')
    await screenshot('03-models-online-search')

    // 仓库直达（0.0.7.0）：粘贴 org/name 跳过搜索接口，直接展开文件列表。
    await marketDialog.getByPlaceholder(/在 ModelScope 搜索模型仓库/).fill('Comfy-Org/MiniMax-H3')
    await marketDialog.getByRole('button', { name: '在线搜索' }).click()
    const directRepo = marketDialog.getByTestId('online-repo-Comfy-Org/MiniMax-H3')
    await directRepo.getByText('minimax_h3_fl2va_pruned_w6a8.safetensors').waitFor({ timeout: 30000 })
    check('仓库名直达（跳过搜索直接展开文件列表）')
    await marketDialog.getByTestId('source-tab-curated').click()

    await marketDialog.getByPlaceholder('搜索模型名称、文件或描述').fill('Offset')
    const loraEntry = marketDialog.getByTestId('market-entry-sdxl-offset-lora')
    await loraEntry.waitFor()
    await loraEntry.getByText('SDXL Offset LoRA', { exact: true }).waitFor()
    check('模型市场搜索与条目展示')
    await screenshot('03-models-market')
    await loraEntry.getByRole('button', { name: '下载 SDXL Offset LoRA', exact: true }).click()

    const loraDeadline = Date.now() + 180000
    let loraDone = false
    while (Date.now() < loraDeadline && !loraDone) {
      const tasksBody = await (await page.request.get(`${baseURL}/api/v1/downloads`)).json()
      const modelsBody = await (await page.request.get(`${baseURL}/api/v1/models`)).json()
      loraDone = tasksBody.tasks?.some((task) => task.path === 'loras/sd_xl_offset_example-lora_1.0.safetensors' && task.status === 'completed') && modelsBody.files?.some((file) => file.path === 'loras/sd_xl_offset_example-lora_1.0.safetensors')
      if (!loraDone) await page.waitForTimeout(2000)
    }
    if (!loraDone) throw new Error('模型市场 LoRA 下载 180 秒内未完成入库')
    check('模型市场选择即下载（ModelScope 真实链路）')
    await marketDialog.getByRole('button', { name: 'Close' }).click().catch(() => page.keyboard.press('Escape'))

    // 界面列表每 3 秒刷新，可能滞后于 API：等待下载任务行消失、仅剩仓库文件行，
    // 避免误点同名 dl- 任务行的删除（那只会取消任务，不删文件）。
    await page.waitForFunction(() => {
      const name = 'sd_xl_offset_example-lora_1.0.safetensors'
      const deleteButtons = [...document.querySelectorAll(`button[aria-label="删除 ${name}"]`)]
      const taskButton = document.querySelector(`button[aria-label="暂停 ${name}"], button[aria-label="继续 ${name}"]`)
      return deleteButtons.length === 1 && !taskButton
    }, undefined, { timeout: 20000 })

    await page.getByRole('button', { name: '删除 sd_xl_offset_example-lora_1.0.safetensors', exact: true }).click()
    await page.getByRole('dialog').getByRole('button', { name: '确认删除' }).click()
    const loraDeleteDeadline = Date.now() + 15000
    let loraRemoved = false
    while (Date.now() < loraDeleteDeadline && !loraRemoved) {
      const modelsBody = await (await page.request.get(`${baseURL}/api/v1/models`)).json()
      loraRemoved = !modelsBody.files?.some((file) => file.path === 'loras/sd_xl_offset_example-lora_1.0.safetensors')
      if (!loraRemoved) await page.waitForTimeout(500)
    }
    if (!loraRemoved) throw new Error('市场下载的模型删除后仍在仓库中')
    check('市场下载模型界面删除')

    // 真实下载链路：界面创建 URL 下载 → 写入共享仓库 → 界面删除。
    // 下载源为平台自身页面（目标机 localhost），文件小、秒级完成。
    await page.getByRole('button', { name: '添加模型', exact: true }).click()
    const addDialog = page.getByRole('dialog')
    await addDialog.waitFor()
    await addDialog.getByRole('button', { name: /URL 下载/ }).click()
    await addDialog.getByLabel(/模型名称/).fill('')
    await addDialog.getByLabel(/模型类型/).selectOption('其他')
    await addDialog.getByLabel(/下载地址/).fill('http://localhost:18080/index.html')
    await addDialog.getByRole('button', { name: '开始下载' }).click()

    // 以 API 为准等待任务完成与文件入库，避免界面轮询合并行的时序干扰。
    const apiDeadline = Date.now() + 30000
    let downloadDone = false
    while (Date.now() < apiDeadline && !downloadDone) {
      const tasksBody = await (await page.request.get(`${baseURL}/api/v1/downloads`)).json()
      const modelsBody = await (await page.request.get(`${baseURL}/api/v1/models`)).json()
      downloadDone = tasksBody.tasks?.some((task) => task.path === 'index.html' && task.status === 'completed') && modelsBody.files?.some((file) => file.path === 'index.html')
      if (!downloadDone) await page.waitForTimeout(1000)
    }
    if (!downloadDone) throw new Error('下载任务未完成或文件未写入共享仓库')
    check('真实 URL 下载写入共享仓库')
    await page.getByRole('row').filter({ hasText: 'index.html' }).getByText('就绪', { exact: true }).waitFor({ timeout: 15000 })
    await screenshot('03-models-download-real')

    await page.getByRole('button', { name: '删除 index.html', exact: true }).click()
    await page.getByRole('dialog').getByRole('button', { name: '确认删除' }).click()
    const deleteDeadline = Date.now() + 15000
    let fileRemoved = false
    while (Date.now() < deleteDeadline && !fileRemoved) {
      const modelsBody = await (await page.request.get(`${baseURL}/api/v1/models`)).json()
      fileRemoved = !modelsBody.files?.some((file) => file.path === 'index.html')
      if (!fileRemoved) await page.waitForTimeout(500)
    }
    if (!fileRemoved) throw new Error('下载文件删除后仍在仓库中')
    check('下载文件界面删除')

    // 真实本地导入链路：界面创建导入任务 → 从 /data/import 复制入库 → 界面删除。
    // 源文件由部署步骤在目标机 seed 到安装目录 import 文件夹。
    await page.getByRole('button', { name: '添加模型', exact: true }).click()
    const importDialog = page.getByRole('dialog')
    await importDialog.waitFor()
    await importDialog.getByRole('button', { name: /本地导入/ }).click()
    await importDialog.getByLabel(/模型名称/).fill('')
    await importDialog.getByLabel(/模型类型/).selectOption('其他')
    await importDialog.getByLabel(/服务器文件路径/).fill('/data/import/regress-import.safetensors')
    await importDialog.getByRole('button', { name: '开始导入' }).click()

    const importDeadline = Date.now() + 30000
    let importDone = false
    while (Date.now() < importDeadline && !importDone) {
      const tasksBody = await (await page.request.get(`${baseURL}/api/v1/downloads`)).json()
      const modelsBody = await (await page.request.get(`${baseURL}/api/v1/models`)).json()
      importDone = tasksBody.tasks?.some((task) => task.kind === 'import' && task.path === 'regress-import.safetensors' && task.status === 'completed') && modelsBody.files?.some((file) => file.path === 'regress-import.safetensors')
      if (!importDone) await page.waitForTimeout(1000)
    }
    if (!importDone) throw new Error('本地导入任务未完成或文件未写入共享仓库')
    check('真实本地导入写入共享仓库')
    await page.getByRole('row').filter({ hasText: 'regress-import.safetensors' }).getByText('就绪', { exact: true }).waitFor({ timeout: 15000 })

    await page.getByRole('button', { name: '删除 regress-import.safetensors', exact: true }).click()
    await page.getByRole('dialog').getByRole('button', { name: '确认删除' }).click()
    const importDeleteDeadline = Date.now() + 15000
    let importRemoved = false
    while (Date.now() < importDeleteDeadline && !importRemoved) {
      const modelsBody = await (await page.request.get(`${baseURL}/api/v1/models`)).json()
      importRemoved = !modelsBody.files?.some((file) => file.path === 'regress-import.safetensors')
      if (!importRemoved) await page.waitForTimeout(500)
    }
    if (!importRemoved) throw new Error('导入文件删除后仍在仓库中')
    check('导入文件界面删除')
  } else {
    throw new Error('模型接口不可用：0.0.5.1 起页面仅支持真实后端，请确认平台服务已启动')
  }

  if (realModelsAvailable) {
    // 镜像预置流程：检查接口＋完整拉取链路。
    // 优先用 DGX 预置的 quay.io 镜像（层已存在时秒级完成）；
    // 开发机没有该镜像时退用 hello-world（约 20KB，真实拉取同样覆盖链路）。
    let pullImageName = 'quay.io/gpustack/runtime:pause'
    const presetCheck = await (await page.request.get(`${baseURL}/api/v1/images/check?image=${encodeURIComponent(pullImageName)}`)).json()
    if (!presetCheck.exists) pullImageName = 'hello-world:latest'
    const checkResponse = await page.request.get(`${baseURL}/api/v1/images/check?image=${encodeURIComponent(pullImageName)}`)
    const checkBody = await checkResponse.json()
    if (!checkResponse.ok() || typeof checkBody.exists !== 'boolean') throw new Error(`镜像检查接口异常：${JSON.stringify(checkBody)}`)
    check('镜像本地存在性检查接口')

    const pullResponse = await page.request.post(`${baseURL}/api/v1/images/pull`, { data: { image: pullImageName } })
    if (!pullResponse.ok()) throw new Error(`启动镜像拉取失败：HTTP ${pullResponse.status()}`)
    const pullDeadline = Date.now() + 120000
    let pullState = 'pulling'
    while (Date.now() < pullDeadline) {
      const statusBody = await (await page.request.get(`${baseURL}/api/v1/images/pull`)).json()
      pullState = statusBody.state
      if (pullState === 'done' || pullState === 'error') break
      await page.waitForTimeout(2000)
    }
    if (pullState !== 'done') throw new Error(`镜像拉取未完成：${pullState}`)
    check('镜像后台拉取链路（Docker API）')
  }

  await page.getByRole('button', { name: /^任务中心/ }).first().click()
  await page.getByRole('heading', { name: '任务中心' }).waitFor()
  await page.getByText('下载 index.html', { exact: true }).first().waitFor()
  await page.getByText('下载完成，已写入共享仓库', { exact: false }).first().waitFor()
  check('任务中心展示真实下载任务')
  await screenshot('04-tasks')

  await page.getByRole('button', { name: '系统管理', exact: true }).click()
  await page.getByRole('heading', { name: '系统管理' }).waitFor()
  await page.getByText('后端已连接', { exact: true }).waitFor()
  check('系统页后端与真实 Docker 联调')
  await page.getByRole('button', { name: '重新检测' }).click()
  await page.getByText('环境检测完成', { exact: true }).waitFor()
  check('环境重新检测')
  await screenshot('05-system')

  await page.getByRole('tab', { name: '诊断与关于' }).click()
  const downloadPromise = page.waitForEvent('download')
  await page.getByRole('button', { name: '导出诊断包' }).click()
  const download = await downloadPromise
  await download.saveAs(`${outputDir}diagnostics.json`)
  check('诊断导出')

  await page.setViewportSize({ width: 375, height: 812 })
  await page.getByRole('button', { name: '打开菜单' }).click()
  await page.getByRole('button', { name: '首页看板', exact: true }).click()
  await page.waitForTimeout(300)
  await screenshot('06-mobile-dashboard')
  const overflow = await page.evaluate(() => ({
    scrollWidth: document.documentElement.scrollWidth,
    innerWidth: window.innerWidth,
    elements: [...document.querySelectorAll('body *')].map((element) => ({ tag: element.tagName, className: String(element.getAttribute('class') ?? ''), right: Math.round(element.getBoundingClientRect().right) })).filter((item) => item.right > window.innerWidth + 1).slice(0, 12),
  }))
  if (overflow.scrollWidth > overflow.innerWidth) throw new Error(`375px 视口出现页面级横向滚动：${JSON.stringify(overflow)}`)
  check('375px 视口无页面级横向滚动')

  if (consoleErrors.length || failedResponses.length) {
    throw new Error(`浏览器异常：${JSON.stringify({ consoleErrors, failedResponses })}`)
  }
  check('无控制台错误及 HTTP 4xx／5xx 资源')

  await writeFile(`${outputDir}verification.json`, JSON.stringify({ success: true, baseURL, checks, consoleErrors, failedResponses }, null, 2))
} catch (error) {
  await writeFile(`${outputDir}verification.json`, JSON.stringify({ success: false, checks, consoleErrors, failedResponses, error: String(error) }, null, 2))
  throw error
} finally {
  await browser.close()
}
