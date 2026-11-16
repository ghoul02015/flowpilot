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
  if (!healthResponse.ok() || health.status !== 'ok' || health.version !== '0.1.0.11') throw new Error(`后端健康检查异常：${JSON.stringify(health)}`)
  check('Go 后端健康接口与版本')

  // 认证（0.0.9.6）：未登录先看到登录页 → 初始化 → 登录 → 会话恢复。
  await page.goto(baseURL, { waitUntil: 'domcontentloaded' })
  await page.getByTestId('setup-page').waitFor({ timeout: 15000 })
  check('未初始化时显示初始化页')

  const setupResponse = await page.request.post(`${baseURL}/api/v1/auth/setup`, { data: { token: 'flowpilot-demo-init-2026', password: 'FlowPilot2026' } })
  if (!setupResponse.ok()) throw new Error(`初始化失败：${await setupResponse.text()}`)
  check('一次性令牌初始化')

  const replayResponse = await page.request.post(`${baseURL}/api/v1/auth/setup`, { data: { token: 'flowpilot-demo-init-2026', password: 'Another2026' } })
  if (replayResponse.ok()) throw new Error('初始化令牌重放必须被拒绝')
  check('初始化令牌重放拒绝')

  const noAuthResponse = await page.request.get(`${baseURL}/api/v1/instances`)
  if (noAuthResponse.status() !== 401) throw new Error(`未登录业务 API 应 401，实际 ${noAuthResponse.status()}`)
  check('未认证业务 API 拒绝（401）')

  const badLogin = await page.request.post(`${baseURL}/api/v1/auth/login`, { data: { username: 'admin', password: 'WrongPass1' } })
  if (badLogin.ok() || (await badLogin.json()).error !== '用户名或密码错误') throw new Error('错误密码应统一文案')
  check('登录失败统一文案')

  const loginResponse = await page.request.post(`${baseURL}/api/v1/auth/login`, { data: { username: 'admin', password: 'FlowPilot2026' } })
  if (!loginResponse.ok()) throw new Error(`登录失败：${await loginResponse.text()}`)
  const { csrfToken } = await loginResponse.json()
  check('管理员登录')

  const noCsrfResponse = await page.request.post(`${baseURL}/api/v1/instances`, { data: {} })
  if (noCsrfResponse.status() !== 403) throw new Error(`写请求缺 CSRF 头应 403，实际 ${noCsrfResponse.status()}`)
  check('写请求 CSRF 校验（403）')

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
  const hardwareBrief = page.getByTestId('hardware-brief')
  await hardwareBrief.waitFor()
  const briefText = await hardwareBrief.innerText()
  for (const marker of ['每 2 秒自动刷新', '点击机器名进入硬件管理']) {
    if (!briefText.includes(marker)) throw new Error(`首页硬件简述条缺少提示：${marker}`)
  }
  check('首页看板与硬件简述条（精简指标）')
  await screenshot('01-dashboard')

  if (hasRealHardware) {
    await hardwareBrief.getByText(hardware.hostName, { exact: false }).first().waitFor()
    if (await hardwareBrief.locator('[role="progressbar"]').count() < 3) throw new Error('首页硬件简述缺少 CPU／内存／存储三条使用率')
    check('首页硬件简述条展示主机名与三条使用率')
  }

  // 0.0.9.4：首页机器名 → 硬件管理页（只读）；一台物理机多卡时按卡单行分组展示。
  await hardwareBrief.getByRole('button', { name: '打开硬件管理' }).click()
  await page.getByRole('heading', { name: '硬件管理', exact: true }).waitFor()
  const machinePanel = page.getByTestId('hardware-machine')
  await machinePanel.waitFor()
  check('首页机器名跳转硬件管理页')

  const firstCollected = await machinePanel.getAttribute('data-hardware-collected')
  await page.waitForFunction((previous) => {
    const current = document.querySelector('[data-testid="hardware-machine"]')?.getAttribute('data-hardware-collected')
    return Boolean(current) && current !== '' && current !== previous
  }, firstCollected, { timeout: 8000 })
  check('硬件数据每 2 秒自动刷新')

  // 兼容性检查（0.0.9.13）。
  const compatResponse = await page.request.get(`${baseURL}/api/v1/hardware/compat`)
  const compatBody = await compatResponse.json()
  if (!compatResponse.ok() || !Array.isArray(compatBody.items) || compatBody.items.length === 0) throw new Error(`兼容性检查异常：${JSON.stringify(compatBody)}`)
  check(`硬件兼容性检查（${compatBody.items.length} 项 · 通过 ${compatBody.summary.pass}）`)

  const normalized = (value) => value.replace(/\s+/g, ' ').trim()
  if (hasRealHardware && hardware.gpuAvailable) {
    await machinePanel.getByText(hardware.hostName, { exact: false }).first().waitFor()
    if (hardware.machineModel) {
      await machinePanel.getByText(hardware.machineModel, { exact: false }).first().waitFor()
    }
    const gpuTable = page.getByTestId('hardware-gpu-table')
    await gpuTable.waitFor()
    for (const gpu of hardware.gpus) {
      const row = page.getByTestId(`hardware-gpu-${gpu.index}`)
      await row.waitFor()
      const rowText = normalized(await row.innerText())
      if (!rowText.includes(`GPU ${gpu.index}`) || !rowText.includes(normalized(gpu.name))) throw new Error(`GPU 行与采集数据不一致：${rowText}`)
    }
    check(`GPU 逐卡单行表格（${hardware.gpus.length} 卡同属一台物理机）`)
    if (hardware.gpuMemoryMode === 'unified') {
      await gpuTable.getByRole('columnheader', { name: '统一内存' }).waitFor()
      check('统一内存架构列头（GPU 显存为系统共享）')
    }
    if (hardware.driverVersion) {
      await machinePanel.getByText(hardware.driverVersion, { exact: false }).first().waitFor()
      check(`驱动版本展示（${hardware.driverVersion}）`)
    }
    if (hardware.cudaVersion) {
      await machinePanel.getByText(hardware.cudaVersion, { exact: false }).first().waitFor()
      check(`CUDA 版本展示（${hardware.cudaVersion}）`)
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

  // 0.0.9.4：首页实例名 → 实例管理并自动打开详情；实例日志实时流；模型关联弹窗。
  const preInstancesBody = await page.request.get(`${baseURL}/api/v1/instances`)
  const preInstances = preInstancesBody.ok() ? ((await preInstancesBody.json()).instances ?? []) : []
  if (preInstances.length > 0) {
    const focusName = preInstances[0].name
    await page.getByRole('button', { name: '首页看板', exact: true }).click()
    await page.getByRole('heading', { name: /凌晨好|上午好|下午好|晚上好/ }).waitFor()
    await page.getByRole('button', { name: focusName, exact: true }).first().click()
    // 详情弹窗是 Radix 模态：打开时其余界面被标记 aria-hidden，因此先断言弹窗再断言页面标题。
    const focusDialog = page.getByRole('dialog')
    await focusDialog.waitFor()
    await focusDialog.getByText(focusName, { exact: true }).first().waitFor()
    check('首页点击实例名跳转实例管理并打开详情')
    await focusDialog.getByRole('button', { name: '完成' }).click()
    await focusDialog.waitFor({ state: 'detached' })
    await page.getByRole('heading', { name: '实例管理', exact: true }).waitFor()
    await page.getByTestId(`instance-row-${focusName}`).waitFor()

    // 日志弹窗：SSE 直读（meta 帧先行）＋ 界面 role="log" 日志区。
    const sseProbe = await page.evaluate(async (name) => {
      const controller = new AbortController()
      const timer = setTimeout(() => controller.abort(), 5000)
      let text = ''
      try {
        const response = await fetch(`/api/v1/instances/${encodeURIComponent(name)}/logs?tail=20`, { signal: controller.signal })
        if (!response.ok || !response.body) return `HTTP ${response.status}`
        const reader = response.body.getReader()
        const decoder = new TextDecoder()
        while (!text.includes('event: meta') && !text.includes('event: end')) {
          const { value, done } = await reader.read()
          if (done) break
          text += decoder.decode(value, { stream: true })
        }
      } catch {
        // abort 属预期：拿到首帧后主动断开长连接
      } finally {
        clearTimeout(timer)
        controller.abort()
      }
      return text
    }, focusName)
    if (!sseProbe.includes('event: meta')) throw new Error(`实例日志 SSE 未返回 meta 帧：${sseProbe.slice(0, 200)}`)
    check('实例日志 SSE 端点（meta 帧先行）')

    await page.getByRole('button', { name: `查看 ${focusName} 日志`, exact: true }).click()
    const logsDialog = page.getByRole('dialog')
    await logsDialog.waitFor()
    await logsDialog.locator('[role="log"]').waitFor()
    await logsDialog.getByTestId('instance-log-count').waitFor()
    check('实例日志弹窗（role=log 实时日志区）')
    await screenshot('02-instance-logs')
    await page.keyboard.press('Escape')
    await logsDialog.waitFor({ state: 'detached' })

    // 模型关联弹窗：接口 supported 与界面「旧实例重建提示」必须一致。
    const linksApiBody = await page.request.get(`${baseURL}/api/v1/instances/${encodeURIComponent(focusName)}/model-links`)
    const linksApi = linksApiBody.ok() ? await linksApiBody.json() : null
    if (!linksApi || typeof linksApi.supported !== 'boolean' || !Array.isArray(linksApi.links)) throw new Error(`模型关联接口异常：${JSON.stringify(linksApi)}`)
    await page.getByRole('button', { name: `关联 ${focusName} 模型`, exact: true }).click()
    const linksDialog = page.getByRole('dialog')
    await linksDialog.waitFor()
    await linksDialog.getByText(`模型关联 - ${focusName}`, { exact: false }).waitFor()
    const linksDialogText = await linksDialog.innerText()
    const legacyNotice = linksDialogText.includes('该实例由旧版本创建')
    if (legacyNotice === linksApi.supported) throw new Error(`模型关联弹窗与接口状态不一致：supported=${linksApi.supported}`)
    check(`实例模型关联弹窗（${linksApi.supported ? '可在线关联' : '旧实例提示重建'}）`)
    await screenshot('02-instance-model-links')
    await page.keyboard.press('Escape')
    await linksDialog.waitFor({ state: 'detached' })
  } else {
    console.log('SKIP 实例日志／模型关联／首页跳转断言（当前无任何实例）')
  }

  await page.getByRole('button', { name: '新建实例', exact: true }).click()
  const createDialog = page.getByRole('dialog')
  await createDialog.waitFor()
  await page.waitForTimeout(300)
  const dialogStyle = await createDialog.evaluate((element) => ({ background: getComputedStyle(element).backgroundColor, opacity: getComputedStyle(element).opacity }))
  if (['transparent', 'rgba(0, 0, 0, 0)'].includes(dialogStyle.background) || dialogStyle.opacity !== '1') throw new Error(`新建实例弹窗未正常显示：${JSON.stringify(dialogStyle)}`)
  check('新建实例弹窗背景不透明')
  await screenshot('02-create-dialog')
  await page.keyboard.press('Escape')
  await createDialog.waitFor({ state: 'detached' })

  const instancesApiResponse = await page.request.get(`${baseURL}/api/v1/instances`)
  const realInstancesAvailable = instancesApiResponse.ok()

  if (realInstancesAvailable) {
    // 真实生命周期：创建 → 端口可达 → 停止 → 启动 → 删除，全程通过界面操作。
    const instanceName = 'regress-01'
    // 0.0.8.0：镜像任务列表接口回归断言（有界并发＋排队＋停止/删除）。
    const localImagesResponse = await page.request.get(`${baseURL}/api/v1/images/local`)
    const localImagesBody = await localImagesResponse.json()
    if (!localImagesResponse.ok() || !Array.isArray(localImagesBody.images)) throw new Error(`本机镜像列表接口异常：HTTP ${localImagesResponse.status()}`)
    check('本机镜像列表接口')
    const catalogResponse = await page.request.get(`${baseURL}/api/v1/images/catalog`)
    const catalogBody = await catalogResponse.json()
    if (!catalogResponse.ok() || !Array.isArray(catalogBody.recipes) || catalogBody.recipes.length < 5) throw new Error(`镜像配方目录异常：${JSON.stringify(catalogBody)}`)
    check('镜像构建配方目录接口（六套多显卡配方）')
    const refsResponse = await page.request.get(`${baseURL}/api/v1/images/comfyui-refs`)
    const refsBody = refsResponse.ok() ? await refsResponse.json() : null
    if (!refsResponse.ok() || !Array.isArray(refsBody.refs) || !refsBody.refs.some((item) => item.name === 'master')) throw new Error(`ComfyUI 在线版本接口异常：${JSON.stringify(refsBody)}`)
    check('ComfyUI 在线版本接口')
    const tasksResponse = await page.request.get(`${baseURL}/api/v1/images/tasks`)
    const tasksBody = tasksResponse.ok() ? await tasksResponse.json() : null
    if (!tasksResponse.ok() || !Array.isArray(tasksBody.tasks)) throw new Error(`镜像任务列表接口异常：HTTP ${tasksResponse.status()}`)
    check('镜像构建/拉取任务列表接口')
    const diskUsageResponse = await page.request.get(`${baseURL}/api/v1/images/disk-usage`)
    if (!diskUsageResponse.ok()) throw new Error(`镜像磁盘占用接口异常：HTTP ${diskUsageResponse.status()}`)
    check('镜像磁盘占用接口')
    // 0.0.9.3：网络存储配置接口（未配置时应返回 enabled=false 且不报错）。
    const storageResponse = await page.request.get(`${baseURL}/api/v1/storage/network`)
    const storageBody = storageResponse.ok() ? await storageResponse.json() : null
    if (!storageResponse.ok() || typeof storageBody.enabled !== 'boolean' || typeof storageBody.detected !== 'boolean' || !storageBody.mountPoint) throw new Error(`网络存储接口异常：${JSON.stringify(storageBody)}`)
    check('网络存储配置接口（未启用默认态）')
    // 0.0.9.0：创建实例前探测宿主机端口真实占用，18080 管理端口必须被拦截。
    const occupiedPortResponse = await page.request.post(`${baseURL}/api/v1/instances`, { headers: { 'X-FlowPilot-CSRF': csrfToken }, data: { name: 'regress-port-probe', image: `flowpilot/comfyui-regress:${health.version}`, gpu: -1, port: 18080 } })
    if (occupiedPortResponse.ok()) throw new Error('宿主机已占用端口（18080）创建实例未被拦截')
    check('宿主机端口占用创建拦截（18080 管理端口）')
    // ComfyUI 官方版本（0.1.0.2）：Release/Tag/Branch 解析成不可变 commit。
    const versionCatalogResponse = await page.request.get(`${baseURL}/api/v1/images/comfyui-versions?refresh=1`)
    const versionCatalog = await versionCatalogResponse.json()
    if (!versionCatalogResponse.ok() || !versionCatalog.default?.commit || versionCatalog.default.prerelease) throw new Error(`版本目录异常：${JSON.stringify(versionCatalog)}`)
    if (!Array.isArray(versionCatalog.releases) || !Array.isArray(versionCatalog.tags) || !versionCatalog.branch?.commit) throw new Error('版本目录缺少 Release/Tag/Branch 分组')
    check(`ComfyUI 官方版本目录（默认 ${versionCatalog.default.version || versionCatalog.default.ref}）`)
    const releaseResolveResponse = await page.request.post(`${baseURL}/api/v1/images/comfyui-resolve`, { headers: { 'X-FlowPilot-CSRF': csrfToken }, data: { ref: versionCatalog.default.ref, kind: 'release' } })
    const resolvedRelease = await releaseResolveResponse.json()
    if (!releaseResolveResponse.ok() || resolvedRelease.commit !== versionCatalog.default.commit || resolvedRelease.kind !== 'release') throw new Error(`Release 解析异常：${JSON.stringify(resolvedRelease)}`)
    check('ComfyUI Release 解析固定 commit')
    const branchResolveResponse = await page.request.post(`${baseURL}/api/v1/images/comfyui-resolve`, { headers: { 'X-FlowPilot-CSRF': csrfToken }, data: { ref: 'master', kind: 'branch' } })
    const resolvedBranch = await branchResolveResponse.json()
    if (!branchResolveResponse.ok() || resolvedBranch.kind !== 'branch' || !resolvedBranch.commit) throw new Error(`master 分支解析异常：${JSON.stringify(resolvedBranch)}`)
    check('ComfyUI master 分支解析固定 commit')

    const buildQuery = new URLSearchParams({ variant: 'nvidia-cu130', image: 'flowpilot/comfyui:regress', ref: resolvedRelease.ref, kind: resolvedRelease.kind, commit: resolvedRelease.commit, version: resolvedRelease.version || '', pip: 'tsinghua', manager: '1', platform: 'linux/arm64' })
    const buildScriptResponse = await page.request.get(`${baseURL}/api/v1/images/build-script?${buildQuery.toString()}`)
    const buildScriptText = await buildScriptResponse.text()
    if (!buildScriptResponse.ok() || !buildScriptText.startsWith('#!/usr/bin/env bash') || !buildScriptText.includes('--platform') || !buildScriptText.includes('flowpilot.build.summary') || !buildScriptText.includes(resolvedRelease.commit)) throw new Error('一键构建脚本生成接口异常')
    if (buildScriptText.includes('modelscope download') || buildScriptText.includes('hf download')) throw new Error('构建脚本不应包含模型下载逻辑')
    check('一键构建脚本生成接口（固定 commit＋参数简述）')
    const installQuery = new URLSearchParams({ variant: 'auto', pip: 'tsinghua', ref: resolvedRelease.ref, kind: resolvedRelease.kind, commit: resolvedRelease.commit, manager: '1' })
    const scriptResponse = await page.request.get(`${baseURL}/api/v1/images/install-script?${installQuery.toString()}`)
    const scriptText = await scriptResponse.text()
    if (!scriptResponse.ok() || !scriptText.startsWith('#!/usr/bin/env bash') || !scriptText.includes('nvidia-cu130') || !scriptText.includes('amd-rocm') || !scriptText.includes(resolvedRelease.commit) || !scriptText.includes('flowpilot.kind="comfyui"')) throw new Error('安装脚本生成接口异常')
    if (scriptText.includes('modelscope download') || scriptText.includes('hf download')) throw new Error('安装脚本不应包含模型下载逻辑')
    check('一键安装脚本生成接口（固定 commit＋认证标签）')
    await page.getByRole('button', { name: '镜像中心', exact: true }).click()
    await page.getByRole('heading', { name: '镜像中心' }).waitFor()
    await page.getByRole('heading', { name: 'ComfyUI 镜像' }).waitFor()
    await page.getByRole('heading', { name: '机器其他镜像' }).waitFor()
    check('镜像中心合并视图（ComfyUI/其他镜像分类）')
    await page.getByRole('button', { name: /一键构建 ComfyUI 镜像/ }).click()
    const buildDialog = page.getByRole('dialog')
    await buildDialog.getByLabel('CPU 芯片架构').waitFor()
    await buildDialog.getByLabel('GPU / PyTorch 配方').waitFor()
    const versionSelect = buildDialog.getByLabel(/ComfyUI 版本/)
    await versionSelect.waitFor()
    // 首次打开需要等待在线版本目录返回，不能立即读值。
    let selectedVersionValue = ''
    const versionDeadline = Date.now() + 25000
    while (Date.now() < versionDeadline && !selectedVersionValue) {
      selectedVersionValue = await versionSelect.inputValue()
      if (!selectedVersionValue) await page.waitForTimeout(500)
    }
    if (!selectedVersionValue) throw new Error('构建弹窗未默认选择稳定 Release')
    await buildDialog.getByTestId('comfy-version-picker').getByText('本次构建固定到该 commit', { exact: false }).waitFor({ timeout: 20000 })
    check('构建弹窗默认稳定 Release 并展示固定 commit')
    await page.keyboard.press('Escape')
    await buildDialog.waitFor({ state: 'detached' })
    await page.getByRole('button', { name: /拉取在线容器镜像/ }).click()
    const pullDialog = page.getByRole('dialog')
    await pullDialog.getByLabel('在线镜像名').waitFor()
    await page.keyboard.press('Escape')
    await pullDialog.waitFor({ state: 'detached' })
    check('构建与拉取双弹窗（镜像中心入口）')
    await page.getByRole('button', { name: 'ComfyUI 实例', exact: true }).click()
    await page.getByRole('button', { name: '新建实例', exact: true }).click()
    await createDialog.waitFor()
    await createDialog.getByLabel('实例名称').fill(instanceName)
    const regressionImage = `flowpilot/comfyui-regress:${health.version}`
    const imageSelect = createDialog.getByLabel('容器镜像（本机已有）')
    const optionDeadline = Date.now() + 8000
    while (Date.now() < optionDeadline && await imageSelect.locator('option').count() === 0) await page.waitForTimeout(200)
    if (await imageSelect.locator('option').count() === 0) throw new Error('新建实例没有任何 ComfyUI 镜像候选')
    if (await imageSelect.locator(`option[value="${regressionImage}"]`).count() > 0) {
      await imageSelect.selectOption(regressionImage)
    }
    const selectedCreateImage = await imageSelect.evaluate((node) => node.value)
    if (!selectedCreateImage.toLowerCase().includes('comfyui')) throw new Error(`新建实例出现非 ComfyUI 镜像候选：${selectedCreateImage}`)
    await createDialog.getByText('将使用本机镜像', { exact: false }).waitFor({ timeout: 8000 })
    check('新建实例仅选择 ComfyUI 本机镜像')
    const runFastLifecycle = selectedCreateImage === regressionImage
    if (!runFastLifecycle) {
      console.log(`SKIP 快速实例生命周期（未找到回归健康镜像 ${regressionImage}，当前候选为 ${selectedCreateImage}）`)
      await page.keyboard.press('Escape')
      await createDialog.waitFor({ state: 'detached' })
    } else {
    // 无 GPU 的演示机（如 Mac）没有 GPU 0 选项，保持默认「不分配 GPU」。
    const gpuSelect = createDialog.getByLabel('分配 GPU')
    await gpuSelect.selectOption('-1')
    await createDialog.getByLabel('访问端口').fill('18188')
    await createDialog.getByRole('button', { name: /高级设置/ }).click()
    await createDialog.getByLabel('容器端口').fill('18080')
    await page.getByRole('button', { name: /下一步/ }).click()
    await page.getByText('共享模型仓库将自动挂载进实例容器', { exact: false }).waitFor()
    await page.getByRole('button', { name: /确认部署|正在创建容器/ }).click()
    await page.getByRole('button', { name: `停止 ${instanceName}`, exact: true }).waitFor({ timeout: 30000 })
    check('真实实例创建并启动（Docker API）')
    await page.locator(`[data-testid="app-health-ready"], [data-testid="app-health-starting"]`).first().waitFor({ timeout: 20000 })
    check('实例列表应用健康徽章')

    const instanceURL = new URL(baseURL)
    instanceURL.port = '18188'
    const instanceHealth = await page.request.get(`${instanceURL.origin}/api/v1/health`)
    if (!instanceHealth.ok()) throw new Error(`实例端口不可达：HTTP ${instanceHealth.status()}`)
    check('真实实例端口可访问')

    // 数据隔离（0.0.9.7）：新实例应有独立 workspace（UUID 目录）。
    const workspacesResponse = await page.request.get(`${baseURL}/api/v1/instances/workspaces`)
    const workspacesBody = await workspacesResponse.json()
    const myWorkspace = workspacesBody.workspaces?.find((item) => item.name === instanceName)
    if (!workspacesResponse.ok() || !myWorkspace || !myWorkspace.hasContainer || myWorkspace.uuid.length < 32) {
      throw new Error(`新实例应有 workspace：${JSON.stringify(workspacesBody)}`)
    }
    check('新实例独立 workspace（UUID 标识）')

    // 应用健康（0.0.9.8）：三层状态 API + 事件时间线 + 重建差异预览。
    const healthResponse = await page.request.get(`${baseURL}/api/v1/instances/${encodeURIComponent(instanceName)}/health`)
    const healthBody = await healthResponse.json()
    if (!healthResponse.ok() || !['ready', 'starting', 'degraded'].includes(healthBody.health?.app)) throw new Error(`应用健康异常：${JSON.stringify(healthBody)}`)
    check(`应用健康三层状态（app=${healthBody.health.app}）`)
    const eventsResponse = await page.request.get(`${baseURL}/api/v1/instances/${encodeURIComponent(instanceName)}/events?limit=20`)
    const eventsBody = await eventsResponse.json()
    if (!eventsResponse.ok() || !Array.isArray(eventsBody.events)) throw new Error(`事件接口异常：${JSON.stringify(eventsBody)}`)
    check('实例事件时间线接口')
    const planResponse = await page.request.post(`${baseURL}/api/v1/instances/${encodeURIComponent(instanceName)}/rebuild/plan`, { headers: { 'X-FlowPilot-CSRF': csrfToken } })
    const planBody = await planResponse.json()
    if (!planResponse.ok() || !Array.isArray(planBody.fields) || typeof planBody.workspaceKept !== 'boolean') throw new Error(`重建预览异常：${JSON.stringify(planBody)}`)
    check('重建差异预览接口')

    // 镜像标签（0.1.0.1）：产品化弹窗、别名不复制镜像、目标冲突与实例引用保护。
    const aliasTag = `${regressionImage}-alias`
    const renameTag = `${regressionImage}-renamed`
    // 清理上次中断运行可能遗留的测试别名，保证用例幂等。
    for (const staleTag of [aliasTag, renameTag]) {
      await page.request.delete(`${baseURL}/api/v1/images/local?image=${encodeURIComponent(staleTag)}`, { headers: { 'X-FlowPilot-CSRF': csrfToken } })
    }
    const imagesBeforeTag = await (await page.request.get(`${baseURL}/api/v1/images/local`)).json()
    const sourceImage = imagesBeforeTag.images?.find((item) => (item.RepoTags ?? []).includes(regressionImage))
    if (!sourceImage) throw new Error(`未找到回归镜像：${regressionImage}`)
    const aliasResponse = await page.request.post(`${baseURL}/api/v1/images/tag`, { headers: { 'X-FlowPilot-CSRF': csrfToken }, data: { source: regressionImage, target: aliasTag, mode: 'alias' } })
    const aliasBody = await aliasResponse.json()
    if (!aliasResponse.ok() || aliasBody.action !== 'alias') throw new Error(`添加镜像别名失败：${JSON.stringify(aliasBody)}`)
    const imagesAfterAlias = await (await page.request.get(`${baseURL}/api/v1/images/local`)).json()
    const aliasImage = imagesAfterAlias.images?.find((item) => (item.RepoTags ?? []).includes(aliasTag))
    if (!aliasImage || aliasImage.Id !== sourceImage.Id) throw new Error('别名应与源标签指向同一镜像 ID')
    check('镜像别名指向同一镜像 ID（不复制数据）')
    const conflictResponse = await page.request.post(`${baseURL}/api/v1/images/tag`, { headers: { 'X-FlowPilot-CSRF': csrfToken }, data: { source: regressionImage, target: aliasTag, mode: 'alias' } })
    if (conflictResponse.status() !== 409) throw new Error(`重复标签应返回 409，实际 ${conflictResponse.status()}`)
    check('镜像目标标签冲突保护（409）')
    const renameResponse = await page.request.post(`${baseURL}/api/v1/images/tag`, { headers: { 'X-FlowPilot-CSRF': csrfToken }, data: { source: regressionImage, target: renameTag, mode: 'rename' } })
    if (renameResponse.status() !== 409) throw new Error(`被实例引用的源标签重命名应返回 409，实际 ${renameResponse.status()}`)
    check('镜像重命名实例引用保护（409）')

    await page.getByRole('button', { name: '镜像中心', exact: true }).click()
    await page.getByRole('heading', { name: '镜像中心' }).waitFor()
    const aliasRow = page.getByRole('row').filter({ hasText: aliasTag }).first()
    await aliasRow.getByRole('button', { name: /管理标签/ }).click()
    const tagDialog = page.getByRole('dialog')
    await tagDialog.getByRole('heading', { name: '编辑镜像标签' }).waitFor()
    await tagDialog.getByText('不会复制镜像层', { exact: false }).waitFor()
    await tagDialog.getByText('同一镜像', { exact: false }).first().waitFor()
    check('镜像标签产品化弹窗与同镜像分组')
    await page.keyboard.press('Escape')
    await tagDialog.waitFor({ state: 'detached' })

    const aliasDelete = await page.request.delete(`${baseURL}/api/v1/images/local?image=${encodeURIComponent(aliasTag)}`, { headers: { 'X-FlowPilot-CSRF': csrfToken } })
    if (!aliasDelete.ok()) throw new Error(`删除别名标签失败：HTTP ${aliasDelete.status()}`)
    const imagesAfterAliasDelete = await (await page.request.get(`${baseURL}/api/v1/images/local`)).json()
    if (!imagesAfterAliasDelete.images?.some((item) => (item.RepoTags ?? []).includes(regressionImage))) throw new Error('删除别名后源标签应保留')
    check('删除镜像别名不影响源标签')
    await page.getByRole('button', { name: 'ComfyUI 实例', exact: true }).click()

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
    // 默认档「删除实例，保留数据」。
    await deleteDialog.getByTestId('delete-keep-data').click()
    await deleteDialog.getByLabel('实例名称').fill(instanceName)
    await deleteDialog.getByRole('button', { name: '确认删除' }).click()
    await page.getByRole('button', { name: `删除 ${instanceName}`, exact: true }).waitFor({ state: 'detached', timeout: 15000 })
    check('真实实例删除（默认保留数据）')

    // 0.0.9.7：删除后 workspace 保留为孤儿目录（Docker 列表存在最终一致性，轮询等待）。
    let orphanWorkspace = null
    for (let attempt = 0; attempt < 15; attempt += 1) {
      const afterDeleteWorkspaces = await page.request.get(`${baseURL}/api/v1/instances/workspaces`).then((response) => response.json())
      const candidate = afterDeleteWorkspaces.workspaces?.find((item) => item.uuid === myWorkspace.uuid)
      if (candidate && candidate.orphan && !candidate.hasContainer) {
        orphanWorkspace = candidate
        break
      }
      await page.waitForTimeout(1000)
    }
    if (!orphanWorkspace) {
      throw new Error('删除后 workspace 应保留为孤儿（等待 15 秒未出现）')
    }
    check('删除后实例数据保留（孤儿 workspace）')

    // 重建同名实例：沿用同一 workspace（UUID 不变）。
    await page.getByRole('button', { name: '新建实例', exact: true }).click()
    const rebuildDialog = page.getByRole('dialog')
    await rebuildDialog.getByLabel('实例名称').fill(instanceName)
    const rebuildImageSelect = rebuildDialog.getByLabel('容器镜像（本机已有）')
    const rebuildOptionDeadline = Date.now() + 8000
    while (Date.now() < rebuildOptionDeadline && await rebuildImageSelect.locator('option').count() === 0) await page.waitForTimeout(200)
    const rebuildImage = `flowpilot/comfyui-regress:${health.version}`
    if (await rebuildImageSelect.locator(`option[value="${rebuildImage}"]`).count() > 0) {
      await rebuildImageSelect.selectOption(rebuildImage)
    }
    await rebuildDialog.getByLabel('分配 GPU').selectOption('-1')
    await rebuildDialog.getByLabel('访问端口').fill('18188')
    await rebuildDialog.getByRole('button', { name: /高级设置/ }).click()
    await rebuildDialog.getByLabel('容器端口').fill('18080')
    await page.getByRole('button', { name: '下一步' }).click()
    await page.getByRole('button', { name: /确认部署|正在创建容器/ }).click()
    await page.getByRole('button', { name: `停止 ${instanceName}`, exact: true }).waitFor({ timeout: 30000 })
    let rebuiltWorkspace = null
    for (let attempt = 0; attempt < 15; attempt += 1) {
      const rebuiltWorkspaces = await page.request.get(`${baseURL}/api/v1/instances/workspaces`).then((response) => response.json())
      const candidate = rebuiltWorkspaces.workspaces?.find((item) => item.name === instanceName)
      if (candidate && candidate.uuid === myWorkspace.uuid && candidate.hasContainer) {
        rebuiltWorkspace = candidate
        break
      }
      await page.waitForTimeout(1000)
    }
    if (!rebuiltWorkspace) {
      throw new Error(`重建应沿用 workspace UUID：${myWorkspace.uuid}（等待 15 秒未确认）`)
    }
    check('重建同名实例沿用 workspace')

    // 彻底删除（purge）：workspace 一并清理。
    await page.getByRole('button', { name: `停止 ${instanceName}`, exact: true }).click()
    await page.getByRole('button', { name: `删除 ${instanceName}`, exact: true }).waitFor({ timeout: 20000 })
    await page.getByRole('button', { name: `删除 ${instanceName}`, exact: true }).click()
    const purgeDialog = page.getByRole('dialog')
    await purgeDialog.getByTestId('delete-purge-data').click()
    await purgeDialog.getByLabel('实例名称').fill(instanceName)
    await purgeDialog.getByRole('button', { name: '彻底删除', exact: true }).click()
    await page.getByRole('button', { name: `删除 ${instanceName}`, exact: true }).waitFor({ state: 'detached', timeout: 15000 })
    let purgedStillThere = true
    for (let attempt = 0; attempt < 15; attempt += 1) {
      const purgedWorkspaces = await page.request.get(`${baseURL}/api/v1/instances/workspaces`).then((response) => response.json())
      if (!purgedWorkspaces.workspaces?.some((item) => item.uuid === myWorkspace.uuid)) {
        purgedStillThere = false
        break
      }
      await page.waitForTimeout(1000)
    }
    if (purgedStillThere) {
      throw new Error('彻底删除后 workspace 不应存在（等待 15 秒仍在）')
    }
    check('彻底删除清理 workspace')
    }

    // ComfyUI 实例链路：用默认预置镜像（comfyui-h3:spark）创建 →
    // 等待 ComfyUI /system_stats 就绪 → 停止 → 删除。
    const instancesBody = await (await page.request.get(`${baseURL}/api/v1/instances`)).json()
    const comfyImage = instancesBody.defaults?.image ?? 'comfyui:latest'
    const comfyName = 'comfy-real'
    const gpu0Busy = instancesBody.instances?.some((item) => item.gpu === 0 && item.status === 'running')
    // 默认 ComfyUI 镜像不在本机或 GPU 0 已被既有业务实例占用时，跳过破坏性实机链路。
    const comfyImageCheck = await (await page.request.get(`${baseURL}/api/v1/images/check?image=${encodeURIComponent(comfyImage)}`)).json()
    if (comfyImage.includes('comfyui-regress')) {
      console.log(`SKIP ComfyUI 实机链路（${comfyImage} 是本机回归健康镜像，不是真实 ComfyUI 服务）`)
    } else if (!comfyImageCheck.exists) {
      console.log(`SKIP ComfyUI 实机链路（镜像 ${comfyImage} 不在本机，需在 GPU 主机上运行）`)
    } else if (gpu0Busy) {
      console.log('SKIP ComfyUI 实机链路（GPU 0 已被既有运行实例占用，避免影响业务容器）')
    } else {
    await page.getByRole('button', { name: '新建实例', exact: true }).click()
    const comfyDialog = page.getByRole('dialog')
    await comfyDialog.waitFor()
    await comfyDialog.getByLabel('实例名称').fill(comfyName)
    await comfyDialog.getByLabel('容器镜像（本机已有）').selectOption(comfyImage)
    await comfyDialog.getByText('将使用本机镜像', { exact: false }).waitFor({ timeout: 8000 })
    const comfyGpuSelect = comfyDialog.getByLabel('分配 GPU')
    if (await comfyGpuSelect.locator('option[value="0"]').count() > 0) await comfyGpuSelect.selectOption('0')
    await comfyDialog.getByLabel('访问端口').fill('18189')
    await comfyDialog.getByRole('button', { name: /高级设置/ }).click()
    await comfyDialog.getByLabel('容器端口').fill('8188')
    await page.getByRole('button', { name: /下一步/ }).click()
    await page.getByText('共享模型仓库将自动挂载进实例容器', { exact: false }).waitFor()
    await page.getByRole('button', { name: /确认部署|正在创建容器/ }).click()
    await page.getByRole('button', { name: `停止 ${comfyName}`, exact: true }).waitFor({ timeout: 60000 })
    check('ComfyUI 实例创建并启动（预置镜像）')

    const comfyURL = new URL(baseURL)
    comfyURL.port = '18189'
    // ComfyUI-Manager 首次启动需在线安装依赖，实测冷启动可超过 9 分钟（Spark 2026-11-10 实测 581 秒）。
    const comfyDeadline = Date.now() + 900000
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
    if (!comfyReady) throw new Error('ComfyUI 实例 900 秒内未就绪')
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
    check('实例列表 ComfyUI 队列徽标')

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

    // 实例全部删除后应展示生产空态；若远端已有业务实例，则确认没有回退演示数据。
    const afterInstances = await (await page.request.get(`${baseURL}/api/v1/instances`)).json()
    if ((afterInstances.instances ?? []).length === 0) {
      await page.getByText('暂无 ComfyUI 实例', { exact: true }).waitFor()
      check('实例列表空态展示（无演示数据）')
    } else {
      check('实例列表保留既有真实实例（无演示数据）')
    }
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
    await page.getByText('本地仓库', { exact: true }).first().waitFor()
    await page.getByText('每 3 秒自动刷新', { exact: true }).waitFor()
    check('模型中心分组统计条')

    // 组合套装（0.1.0.4）：清单、详情、许可闸门与四项预检。
    const bundlesResponse = await page.request.get(`${baseURL}/api/v1/model-bundles`)
    const bundlesBody = await bundlesResponse.json()
    if (!bundlesResponse.ok() || bundlesBody.bundles?.length !== 5) throw new Error(`套装列表异常：${JSON.stringify(bundlesBody).slice(0, 200)}`)
    if (!bundlesBody.checksum) throw new Error('套装列表缺少清单校验值')
    check('组合套装列表接口（5 个套装 + 清单校验值）')

    const bundleDetail = await (await page.request.get(`${baseURL}/api/v1/model-bundles/h3-image-base`)).json()
    if (bundleDetail.items?.length !== 3 || !bundleDetail.manifest?.checksum) throw new Error('套装详情文件项或清单校验值异常')
    check('套装详情接口（S1 三个文件项）')

    const preflightBefore = await page.request.post(`${baseURL}/api/v1/model-bundles/h3-image-base/preflight`, { headers: { 'X-FlowPilot-CSRF': csrfToken }, data: { variant: 'default' } })
    const beforeBody = await preflightBefore.json()
    if (beforeBody.canInstall !== false) throw new Error('许可未确认时不应允许安装')
    if (!beforeBody.checks?.some((item) => item.code === 'license' && item.status === 'block')) throw new Error('许可预检应阻断')
    check('套装预检：许可未确认阻断安装')

    const installBlocked = await page.request.post(`${baseURL}/api/v1/model-bundles/h3-image-base/installs`, { headers: { 'X-FlowPilot-CSRF': csrfToken }, data: { variant: 'default' } })
    if (installBlocked.status() !== 422) throw new Error(`缺少许可确认应返回 422，实际 ${installBlocked.status()}`)
    check('套装安装受许可确认约束（422）')

    const licenseResponse = await page.request.post(`${baseURL}/api/v1/model-bundles/h3-image-base/license`, { headers: { 'X-FlowPilot-CSRF': csrfToken }, data: { deliveryMethod: 'online' } })
    if (!licenseResponse.ok()) throw new Error('许可确认接口失败')
    const afterBody = await (await page.request.post(`${baseURL}/api/v1/model-bundles/h3-image-base/preflight`, { headers: { 'X-FlowPilot-CSRF': csrfToken }, data: { variant: 'default' } })).json()
    if (!Array.isArray(afterBody.checks) || afterBody.checks.length !== 4) throw new Error('预检应返回四项结论')
    check('套装许可确认与四项预检')

    // 许可按“模型 + 版本”生效：S2 复用 S1 的确认，S3 必须单独确认。
    const s2Detail = await (await page.request.get(`${baseURL}/api/v1/model-bundles/h3-image-pro`)).json()
    if (s2Detail.card?.licenseConfirmed !== true) throw new Error('S2 应复用 MiniMax-H3-Image 的确认')
    const s3Detail = await (await page.request.get(`${baseURL}/api/v1/model-bundles/h3-av-base`)).json()
    if (s3Detail.card?.licenseConfirmed === true) throw new Error('S3 不应复用 H3-Image 的确认')
    if (s2Detail.card?.license?.version === s3Detail.card?.license?.version) throw new Error('两个模型的许可版本不应相同')
    check('许可确认按模型生效（S2 复用、S3 独立）')

    // 许可文件与官方原文一致，且 H3 不套用 H3-Image 文案。
    const licenseFile = await page.request.get(`${baseURL}/api/v1/model-bundles/h3-image-base/license/license`)
    const licenseText = await licenseFile.text()
    if (!licenseFile.ok() || !licenseText.includes('MiniMax-H3-Image COMMUNITY LICENSE AGREEMENT')) throw new Error('LICENSE 下载内容异常')
    const h3Notice = await (await page.request.get(`${baseURL}/api/v1/model-bundles/h3-av-base/license/notice`)).text()
    if (!h3Notice.includes('MiniMax H3 is licensed under') || h3Notice.includes('MiniMax-H3-Image is licensed')) throw new Error('H3 NOTICE 套用了 H3-Image 文案')
    check('许可原文与 NOTICE 按模型区分')

    await page.getByRole('tab', { name: '组合套装' }).click()
    await page.getByText('S1 H3-Image 基础创作包').waitFor()
    const s1Card = page.getByRole('article').filter({ hasText: 'S1 H3-Image 基础创作包' })
    await s1Card.getByText('模型：MiniMax-H3-Image', { exact: true }).waitFor()
    await s1Card.getByText('已确认', { exact: true }).waitFor()
    await s1Card.getByRole('button', { name: '查看许可', exact: true }).click()
    await page.getByRole('dialog').getByText('许可名称', { exact: true }).waitFor()
    await page.getByRole('dialog').getByText('MiniMax-H3-Image Community License Agreement', { exact: true }).waitFor()
    await page.getByRole('dialog').getByRole('link', { name: '下载 LICENSE' }).waitFor()
    check('卡片完整模型名、许可文本状态与详情许可声明')
    await page.keyboard.press('Escape')
    await page.getByRole('dialog').waitFor({ state: 'detached' })

    // 首次安装前必须出现许可确认弹窗，未勾选不能继续。
    const s3Card = page.getByRole('article').filter({ hasText: 'S3 H3 音视频基础包' })
    await s3Card.getByRole('button', { name: '预检安装', exact: true }).click()
    await page.getByRole('dialog').getByRole('button', { name: '运行预检' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '确认许可并继续', exact: true }).click()
    const licenseDialog = page.getByRole('dialog')
    await licenseDialog.getByRole('heading', { name: '使用 MiniMax-H3 前请阅读许可' }).waitFor()
    const confirmButton = licenseDialog.getByRole('button', { name: '确认并继续下载' })
    if (await confirmButton.isDisabled()) throw new Error('许可弹窗应可直接点击确认，不应再要求勾选')
    await licenseDialog.getByText('点击确认即表示你已阅读该许可。', { exact: true }).waitFor()
    check('下载前许可提示与一次点击确认')
    await licenseDialog.getByRole('button', { name: '取消', exact: true }).click()
    await page.keyboard.press('Escape')
    await page.getByRole('dialog').waitFor({ state: 'detached' })
    await page.getByRole('tab', { name: '本地仓库' }).click()
    if (seedFile) {
      // 预置的回归文件（部署时在目标机 seed）：checkpoints/regress-test.safetensors
      // 0.0.9.3 起按类别分组：类别在组头，行内不再重复展示。
      await page.getByRole('button', { name: /Checkpoints/ }).first().waitFor()
      const seedRow = page.getByRole('row').filter({ hasText: 'regress-test.safetensors' })
      await seedRow.waitFor()
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

    // 0.0.9.3 第二批：僵尸模型检测 / 重复检测 / 批量删除。
    // 预置种子（部署时在目标机 seed）：
    //   checkpoints/regress-stale.safetensors（1.2 GB、mtime 40 天前）→ 疑似闲置
    //   checkpoints/regress-dup.safetensors + loras/regress-dup.safetensors（同名同大小）→ 重复×2
    const staleSeed = modelsReport?.files?.find((file) => file.path === 'checkpoints/regress-stale.safetensors')
    const dupSeeds = modelsReport?.files?.filter((file) => file.path.endsWith('regress-dup.safetensors')) ?? []
    if (staleSeed && dupSeeds.length === 2) {
      await page.getByText('疑似闲置', { exact: true }).first().waitFor()
      check('僵尸模型徽章（疑似闲置）')
      await page.getByText('重复×2', { exact: true }).first().waitFor()
      check('重复模型徽章（重复×2）')

      // 筛选 chips：仅看疑似闲置 / 仅看重复。
      await page.getByRole('button', { name: '仅看疑似闲置', exact: true }).click()
      await page.getByRole('row').filter({ hasText: 'regress-stale.safetensors' }).waitFor()
      await page.getByRole('button', { name: '仅看疑似闲置', exact: true }).click()
      await page.getByRole('button', { name: '仅看重复', exact: true }).click()
      await page.getByRole('row').filter({ hasText: 'regress-dup.safetensors' }).first().waitFor()
      await page.getByRole('button', { name: '仅看重复', exact: true }).click()
      check('闲置/重复筛选切换')

      // 批量删除：勾选三个种子文件 → 确认 → 轮询仓库清空。
      await page.getByRole('checkbox', { name: '选择 regress-stale.safetensors', exact: true }).click()
      await page.getByRole('checkbox', { name: '选择 regress-dup.safetensors', exact: true }).first().click()
      await page.getByRole('checkbox', { name: '选择 regress-dup.safetensors', exact: true }).nth(1).click()
      await page.getByRole('button', { name: /批量删除/ }).click()
      await page.getByRole('dialog').getByRole('button', { name: /确认删除 3 个/ }).click()
      const batchDeadline = Date.now() + 20000
      let batchRemoved = false
      while (Date.now() < batchDeadline && !batchRemoved) {
        const modelsBody = await (await page.request.get(`${baseURL}/api/v1/models`)).json()
        batchRemoved = !modelsBody.files?.some((file) => file.path.includes('regress-stale') || file.path.includes('regress-dup'))
        if (!batchRemoved) await page.waitForTimeout(500)
      }
      if (!batchRemoved) throw new Error('批量删除后种子文件仍在仓库中')
      check('批量删除模型文件')
    } else {
      check('模型中心第二批种子缺失（跳过徽章与批量删除验证）')
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

    // 搜索习惯（0.0.7.2）：周董实测搜 nvfp4 空结果（风控间歇空响应），后端空结果退避重试＋模糊变体，attempts 透明化。
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

    // 仓库直达（0.0.7.2）：粘贴 org/name 跳过搜索接口，直接展开文件列表。
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
    const loraPath = 'loras/sd_xl_offset_example-lora_1.0.safetensors'
    const loraDownloadButton = loraEntry.getByRole('button', { name: '下载 SDXL Offset LoRA', exact: true })
    const loraClicked = await loraDownloadButton.count() > 0
    if (loraClicked) await loraDownloadButton.click()

    const loraDeadline = Date.now() + 180000
    let loraDone = false
    while (Date.now() < loraDeadline && !loraDone) {
      const tasksBody = await (await page.request.get(`${baseURL}/api/v1/downloads`)).json()
      const modelsBody = await (await page.request.get(`${baseURL}/api/v1/models`)).json()
      loraDone = modelsBody.files?.some((file) => file.path === loraPath) && (await loraDownloadButton.count() === 0 || tasksBody.tasks?.some((task) => task.path === loraPath && task.status === 'completed'))
      if (!loraDone) await page.waitForTimeout(2000)
    }
    if (!loraDone) throw new Error('模型市场 LoRA 下载 180 秒内未完成入库')
    check(loraClicked ? '模型市场选择即下载（ModelScope 真实链路）' : '模型市场已入库状态识别')
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

  // SSRF 防护（0.0.9.11）：内网目标应被拦截。
  const ssrfResponse = await page.request.post(`${baseURL}/api/v1/downloads`, { headers: { 'X-FlowPilot-CSRF': csrfToken }, data: { url: 'http://192.168.0.1/model.safetensors', category: 'other' } })
  if (ssrfResponse.ok()) throw new Error('内网下载地址必须被 SSRF 防护拦截')
  check('SSRF 防护拦截内网目标')
    await page.getByRole('row').filter({ hasText: 'index.html' }).getByText('就绪', { exact: true }).waitFor({ timeout: 30000 })
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
    // 界面每 3 秒刷新且后端扫描有秒级缓存：以 DOM 为准等待「文件行出现 + 就绪徽章 + 导入任务行退场」。
    await page.waitForFunction(() => {
      const name = 'regress-import.safetensors'
      const deleteButtons = document.querySelectorAll(`button[aria-label="删除 ${name}"]`)
      const readyRow = [...document.querySelectorAll('tbody tr')].some((row) => row.innerText.includes(name) && row.innerText.includes('就绪'))
      return deleteButtons.length === 1 && readyRow
    }, undefined, { timeout: 30000 })

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

    const pullResponse = await page.request.post(`${baseURL}/api/v1/images/pull`, { headers: { 'X-FlowPilot-CSRF': csrfToken }, data: { image: pullImageName } })
    if (!pullResponse.ok()) throw new Error(`启动镜像拉取失败：HTTP ${pullResponse.status()}`)
    const pullTask = await pullResponse.json()
    // 0.0.8.0 起拉取为任务制：轮询任务状态直至收口。
    const pullDeadline = Date.now() + 120000
    let pullState = 'pulling'
    while (Date.now() < pullDeadline) {
      const statusBody = await (await page.request.get(`${baseURL}/api/v1/images/tasks/${pullTask.id}`)).json()
      pullState = statusBody.state
      if (pullState === 'done' || pullState === 'error' || pullState === 'cancelled') break
      await page.waitForTimeout(2000)
    }
    if (pullState !== 'done') throw new Error(`镜像拉取未完成：${pullState}`)
    check('镜像后台拉取链路（Docker API 任务制）')
  }

  await page.getByRole('button', { name: /^任务中心/ }).first().click()
  await page.getByRole('heading', { name: '任务中心' }).waitFor()
  await page.getByText('index.html', { exact: true }).first().waitFor()
  await page.getByTestId('task-type-badge-download').first().waitFor()
  await page.getByText('已写入共享仓库', { exact: false }).first().waitFor()
  check('任务中心展示真实下载任务')
  await screenshot('04-tasks')

  await page.getByRole('button', { name: '系统管理', exact: true }).click()
  await page.getByRole('heading', { name: '系统管理' }).waitFor()
  await page.getByText('管理服务已连接', { exact: true }).waitFor()
  check('系统页管理服务与 Docker 联调')
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

  // 数据底座（0.0.9.5）：状态卡 + 一致性检查 + 备份。
  const databaseResponse = await page.request.get(`${baseURL}/api/v1/database`)
  const databaseStatus = await databaseResponse.json()
  if (!databaseResponse.ok() || databaseStatus.schemaVersion !== 6 || !databaseStatus.integrity.ok) throw new Error(`数据底座状态异常：${JSON.stringify(databaseStatus)}`)
  check('数据底座状态接口（schema v6 · integrity ok）')
  await page.getByRole('tab', { name: '数据底座' }).click()
  await page.getByTestId('database-panel').waitFor()
  await page.getByText('integrity_check 通过', { exact: true }).waitFor()
  check('数据底座状态卡渲染')
  await page.getByRole('button', { name: '立即检查' }).click()
  await page.getByText(/检查报告 ·/, { exact: false }).waitFor()
  const checkReport = await page.request.post(`${baseURL}/api/v1/database/check`, { headers: { 'X-FlowPilot-CSRF': csrfToken } }).then((response) => response.json())
  if (typeof checkReport.unchanged !== 'number' || !Array.isArray(checkReport.diffs)) throw new Error(`一致性检查报告异常：${JSON.stringify(checkReport)}`)
  check('数据底座一致性检查')
  const backupResponse = await page.request.post(`${baseURL}/api/v1/database/backup`, { headers: { 'X-FlowPilot-CSRF': csrfToken } })
  const backupResult = await backupResponse.json()
  if (!backupResponse.ok() || backupResult.size <= 0) throw new Error(`数据库备份异常：${JSON.stringify(backupResult)}`)
  check(`数据库备份（${backupResult.name}）`)
  await screenshot('05-system-database')

  // 安全与会话（0.0.9.6）：修改密码与活跃会话。
  await page.getByRole('tab', { name: '安全与会话' }).click()
  await page.getByTestId('security-panel').waitFor()
  await page.getByTestId('auth-session-current').waitFor()
  check('安全与会话面板（当前会话标识）')

  // 操作审计（0.0.9.6）：登录与写操作事件。
  await page.getByRole('tab', { name: '操作审计' }).click()
  await page.getByTestId('audit-panel').waitFor()
  await page.getByTestId('audit-table').waitFor()
  const auditResponse = await page.request.get(`${baseURL}/api/v1/audit/logs?limit=50`)
  const auditBody = await auditResponse.json()
  if (!auditResponse.ok() || !auditBody.logs.some((entry) => entry.action === 'auth.login' && entry.result === 'success')) throw new Error(`审计应包含登录成功事件：${JSON.stringify(auditBody.logs.slice(0, 5))}`)
  check('操作审计记录（登录事件可查）')
  await screenshot('05-system-audit')

  // 数据迁移（0.0.9.7）：扫描与面板渲染。
  await page.getByRole('tab', { name: '数据迁移' }).click()
  await page.getByTestId('migration-panel').waitFor()
  await page.getByRole('button', { name: '扫描共享目录' }).click()
  await page.getByTestId('migration-scan').waitFor({ timeout: 15000 })
  check('数据迁移面板与共享目录扫描')

  // 统一任务（0.0.9.9）：聚合下载/导入/镜像/迁移任务。
  const unifiedTasksResponse = await page.request.get(`${baseURL}/api/v1/tasks?limit=50`)
  const unifiedTasks = await unifiedTasksResponse.json()
  if (!unifiedTasksResponse.ok() || !Array.isArray(unifiedTasks.tasks)) throw new Error(`统一任务接口异常：${JSON.stringify(unifiedTasks)}`)
  const downloadInUnified = unifiedTasks.tasks.some((task) => task.type === 'download' || task.type === 'import')
  check(`统一任务列表（含下载任务：${downloadInUnified}）`)
  await page.getByRole('button', { name: '任务中心', exact: true }).click()
  await page.getByTestId('unified-task-list').waitFor({ timeout: 15000 })
  await page.getByTestId('task-type-filter').selectOption('download')
  await page.waitForTimeout(600)
  check('任务中心统一列表与类型筛选')
  await page.getByRole('button', { name: '清理已结束' }).waitFor()
  await page.getByLabel('搜索任务').fill('index.html')
  await page.waitForTimeout(700)
  await page.getByText('index.html', { exact: true }).first().waitFor()
  check('任务中心紧凑视图、搜索与批量清理入口')
  await page.getByLabel('搜索任务').fill('')
  await page.getByRole('button', { name: '系统管理', exact: true }).click()

  // 备份（0.0.9.12）：创建任务 → 列表可见。
  const backupTask = await page.request.post(`${baseURL}/api/v1/backups`, { headers: { 'X-FlowPilot-CSRF': csrfToken } })
  const backupBody = await backupTask.json()
  if (!backupTask.ok() || !backupBody.taskId) throw new Error(`创建备份任务失败：${JSON.stringify(backupBody)}`)
  check('平台备份任务创建')
  await page.getByRole('tab', { name: '存储与备份' }).click()
  await page.getByTestId('backup-panel').waitFor()
  await page.waitForTimeout(4000)
  await page.getByRole('button', { name: '刷新' }).click()
  await page.waitForTimeout(1500)
  const backupListResponse = await page.request.get(`${baseURL}/api/v1/backups`)
  const backupList = await backupListResponse.json()
  if (!backupListResponse.ok() || backupList.backups.length === 0) throw new Error('备份列表应至少 1 条')
  const latestBackup = backupList.backups[0]
  if (!latestBackup.manifest?.components?.sqlite) throw new Error(`备份应含 SQLite 快照：${JSON.stringify(latestBackup)}`)
  check('平台备份完成（含 SQLite 快照与 manifest）')
  let backupState = 'running'
  const backupStateDeadline = Date.now() + 20000
  while (Date.now() < backupStateDeadline && !['succeeded', 'failed', 'partial'].includes(backupState)) {
    const taskResponse = await page.request.get(`${baseURL}/api/v1/tasks/${encodeURIComponent(backupBody.taskId)}`)
    if (taskResponse.ok()) backupState = (await taskResponse.json()).status
    if (!['succeeded', 'failed', 'partial'].includes(backupState)) await page.waitForTimeout(500)
  }
  if (backupState !== 'succeeded') throw new Error(`备份任务未完成：${backupState}`)
  const backupTasksResponse = await page.request.get(`${baseURL}/api/v1/tasks?type=backup&limit=20`)
  const backupTasksBody = await backupTasksResponse.json()
  if (!backupTasksResponse.ok() || !backupTasksBody.tasks?.some((task) => task.id === backupBody.taskId)) throw new Error(`统一任务应聚合平台备份：${JSON.stringify(backupTasksBody)}`)
  check('统一任务聚合平台备份')
  await page.getByRole('button', { name: '任务中心', exact: true }).click()
  await page.getByRole('heading', { name: '任务中心' }).waitFor()
  await page.getByTestId('task-type-filter').selectOption('backup')
  const backupTaskRow = page.getByRole('row').filter({ hasText: '平台备份' }).first()
  await backupTaskRow.getByRole('button', { name: '删除记录' }).click()
  await page.getByRole('dialog').getByRole('button', { name: '删除记录', exact: true }).click()
  await backupTaskRow.waitFor({ state: 'detached', timeout: 10000 })
  const backupAfterDelete = await page.request.get(`${baseURL}/api/v1/tasks?type=backup&limit=20`)
  const backupAfterDeleteBody = await backupAfterDelete.json()
  if (!backupAfterDelete.ok() || backupAfterDeleteBody.tasks?.some((task) => task.id === backupBody.taskId)) throw new Error('任务记录删除后不应再出现在统一列表')
  check('任务记录删除仅清理记录')
  await page.getByRole('button', { name: '系统管理', exact: true }).click()
  await page.getByRole('heading', { name: '系统管理' }).waitFor()

  // 退出登录：回到登录页，业务 API 再次 401。
  await page.getByRole('tab', { name: '安全与会话' }).click()
  await page.getByTestId('security-panel').getByRole('button', { name: '退出登录' }).click()
  await page.getByTestId('login-page').waitFor({ timeout: 10000 })
  const afterLogout = await page.request.get(`${baseURL}/api/v1/instances`)
  if (afterLogout.status() !== 401) throw new Error(`退出后业务 API 应 401，实际 ${afterLogout.status()}`)
  check('退出登录后会话失效')
  await page.request.post(`${baseURL}/api/v1/auth/login`, { data: { username: 'admin', password: 'FlowPilot2026' } })
  // 重新登录后刷新页面恢复业务界面（移动端视口断言需要主布局）。
  await page.goto(baseURL, { waitUntil: 'domcontentloaded' })
  await page.getByRole('button', { name: '首页看板', exact: true }).first().waitFor({ timeout: 15000 })

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

  // 认证流程中 401/403 是预期探针（未登录探测、CSRF 缺失验证、退出后失效验证、
  // 会话恢复尝试），仅拦截其他非预期 4xx/5xx 与控制台业务错误。
  const expectedAuthProbes = (message) => message.startsWith('401') || message.startsWith('403')
  const unexpectedResponses = failedResponses.filter((message) => !expectedAuthProbes(message))
  const unexpectedErrors = consoleErrors.filter((message) => message !== '未登录或会话已过期' && !message.includes('status of 401'))
  if (unexpectedErrors.length || unexpectedResponses.length) {
    throw new Error(`浏览器异常：${JSON.stringify({ consoleErrors: unexpectedErrors, failedResponses: unexpectedResponses })}`)
  }
  check('无控制台错误及非预期 HTTP 4xx／5xx 资源')

  await writeFile(`${outputDir}verification.json`, JSON.stringify({ success: true, baseURL, checks, consoleErrors, failedResponses }, null, 2))
} catch (error) {
  await writeFile(`${outputDir}verification.json`, JSON.stringify({ success: false, checks, consoleErrors, failedResponses, error: String(error) }, null, 2))
  throw error
} finally {
  await browser.close()
}
