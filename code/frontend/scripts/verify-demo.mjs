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
  if (!healthResponse.ok() || health.status !== 'ok' || health.version !== '0.0.3.0') throw new Error(`后端健康检查异常：${JSON.stringify(health)}`)
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
  await page.getByRole('heading', { name: '下午好，管理员' }).waitFor()
  await page.getByRole('heading', { name: '硬件与实时监控' }).waitFor()
  check('首页看板与完整硬件信息加载')
  await screenshot('01-dashboard')

  const firstSample = await page.locator('[data-telemetry-sample]').getAttribute('data-telemetry-sample')
  await page.waitForFunction((previous) => document.querySelector('[data-telemetry-sample]')?.getAttribute('data-telemetry-sample') !== previous, firstSample, { timeout: 4000 })
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
    await page.getByLabel('演示硬件机型').selectOption('dgx-spark')
    await page.getByText('128 GB CPU／GPU 共享，不重复累计', { exact: false }).waitFor()
    await page.getByText('独立显存占用不适用；此处显示系统统一内存使用率。', { exact: true }).waitFor()
    check('DGX Spark 统一内存展示（Demo 回退）')
    await screenshot('01-hardware-dgx-spark')

    await page.getByLabel('演示硬件机型').selectOption('dgx-station-gb300')
    await page.getByText('LPDDR5X · 与 252 GB HBM3e 构成最高 748 GB 一致性地址空间', { exact: true }).waitFor()
    check('DGX Station GB300 一致性内存展示（Demo 回退）')

    await page.getByLabel('演示硬件机型').selectOption('rtx5090-16')
    await page.getByTestId('gpu-card-15').waitFor()
    if (await page.locator('[data-testid^="gpu-card-"]').count() !== 16) throw new Error('十六卡机型未完整展示 16 个 GPU 设备')
    check('十六卡 RTX 5090 完整展示（Demo 回退）')
    await screenshot('01-hardware-16-gpu')
  }

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
    await createDialog.getByLabel('实例名称').fill(instanceName)
    await createDialog.getByLabel('容器镜像').fill(`flowpilot-web:${health.version}`)
    await createDialog.getByText('已在本机，可直接创建', { exact: false }).waitFor({ timeout: 8000 })
    check('镜像本地存在性检查（创建向导）')
    await createDialog.getByLabel('分配 GPU').selectOption('0')
    await createDialog.getByLabel('访问端口').fill('18188')
    await createDialog.getByLabel('容器端口').fill('18080')
    await page.getByRole('button', { name: /下一步/ }).click()
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
  } else {
    await page.getByLabel('分配 GPU').selectOption('1')
    await page.getByRole('button', { name: /下一步/ }).click()
    await page.getByText('选择需要预置的全局模型').waitFor()
    await page.getByRole('button', { name: /下一步/ }).click()
    await page.getByRole('button', { name: '确认部署' }).click()
    await page.getByRole('button', { name: '停止 生产绘图-04', exact: true }).waitFor({ timeout: 8000 })
    check('新建实例与模拟部署')
    await page.getByRole('button', { name: '停止 生产绘图-04', exact: true }).click()
    const newInstanceRow = page.getByRole('row').filter({ hasText: '生产绘图-04' })
    await newInstanceRow.getByText('已停止', { exact: true }).waitFor({ timeout: 5000 })
    check('实例停止')
    await screenshot('02-instances')
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
    await page.getByText('真实共享仓库扫描', { exact: true }).waitFor()
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
      const afterDelete = await (await page.request.get(`${baseURL}/api/v1/models`)).json()
      if (afterDelete.files?.some((file) => file.path === 'checkpoints/regress-test.safetensors')) throw new Error('模型文件删除后仍在仓库中')
      check('真实模型文件界面删除')
    } else {
      check('真实模型仓库为空（跳过种子文件删除验证）')
      await screenshot('03-models-real')
    }

    // 真实下载链路：界面创建 URL 下载 → 写入共享仓库 → 界面删除。
    // 下载源为平台自身页面（目标机 localhost），文件小、秒级完成。
    await page.getByRole('button', { name: '添加模型', exact: true }).click()
    const addDialog = page.getByRole('dialog')
    await addDialog.waitFor()
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
  } else {
    await page.getByRole('button', { name: '添加模型', exact: true }).click()
    await page.getByRole('button', { name: '开始下载' }).click()
    await page.getByRole('button', { name: '暂停 dreamshaper-xl.safetensors', exact: true }).waitFor()
    await page.getByRole('button', { name: '暂停 dreamshaper-xl.safetensors', exact: true }).click()
    await page.getByRole('button', { name: '继续 dreamshaper-xl.safetensors', exact: true }).waitFor()
    check('模型添加、下载与暂停')
    await screenshot('03-models')
  }

  if (realModelsAvailable) {
    // 镜像预置流程：检查接口＋完整拉取链路（层已存在时秒级完成）。
    const pullImageName = 'quay.io/gpustack/runtime:pause'
    const checkResponse = await page.request.get(`${baseURL}/api/v1/images/check?image=${encodeURIComponent(pullImageName)}`)
    const checkBody = await checkResponse.json()
    if (!checkResponse.ok() || checkBody.exists !== true) throw new Error(`镜像检查接口异常：${JSON.stringify(checkBody)}`)
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
  if (!realModelsAvailable) {
    await page.getByText('下载 dreamshaper-xl.safetensors', { exact: true }).waitFor()
    check('任务中心与模型任务同步')
  } else {
    await page.getByText('下载 index.html', { exact: true }).waitFor()
    await page.getByText('下载完成，已写入共享仓库', { exact: false }).first().waitFor()
    check('任务中心展示真实下载任务')
  }
  await screenshot('04-tasks')

  await page.getByRole('button', { name: '系统管理', exact: true }).click()
  await page.getByRole('heading', { name: '系统管理' }).waitFor()
  await page.getByText('后端已连接', { exact: true }).waitFor()
  check('系统页后端与真实 Docker 联调')
  await page.getByRole('button', { name: '重新检测' }).click()
  await page.getByText('环境检测完成', { exact: true }).waitFor()
  check('环境重新检测')
  await screenshot('05-system')

  await page.getByRole('tab', { name: '系统设置' }).click()
  await page.getByRole('button', { name: '保存设置' }).click()
  await page.getByText('系统设置已保存', { exact: true }).waitFor()
  await page.getByRole('tab', { name: '操作日志' }).click()
  await page.getByText('修改镜像仓库配置', { exact: true }).waitFor()
  await page.getByRole('tab', { name: '诊断与关于' }).click()
  const downloadPromise = page.waitForEvent('download')
  await page.getByRole('button', { name: '导出诊断包' }).click()
  const download = await downloadPromise
  await download.saveAs(`${outputDir}diagnostics.json`)
  check('系统页签、设置保存与诊断导出')

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
