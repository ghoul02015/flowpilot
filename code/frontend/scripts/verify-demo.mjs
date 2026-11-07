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
  if (!healthResponse.ok() || health.status !== 'ok' || health.version !== '0.0.0.4') throw new Error(`后端健康检查异常：${JSON.stringify(health)}`)
  check('Go 后端健康接口与版本')

  const dockerResponse = await page.request.get(`${baseURL}/api/v1/docker/info`)
  const docker = await dockerResponse.json()
  if (!dockerResponse.ok() || !docker.info?.ServerVersion || !docker.version?.ApiVersion) throw new Error(`真实 Docker API 异常：${JSON.stringify(docker)}`)
  check('真实 Docker Engine 信息接口')

  await page.goto(baseURL, { waitUntil: 'networkidle' })
  await page.getByRole('heading', { name: '下午好，管理员' }).waitFor()
  await page.getByRole('heading', { name: '硬件与实时监控' }).waitFor()
  check('首页看板与完整硬件信息加载')
  await screenshot('01-dashboard')

  const firstSample = await page.locator('[data-telemetry-sample]').getAttribute('data-telemetry-sample')
  await page.waitForFunction((previous) => document.querySelector('[data-telemetry-sample]')?.getAttribute('data-telemetry-sample') !== previous, firstSample, { timeout: 4000 })
  check('GPU 遥测每 2 秒自动刷新')

  await page.getByLabel('演示硬件机型').selectOption('dgx-spark')
  await page.getByText('128 GB CPU／GPU 共享，不重复累计', { exact: false }).waitFor()
  await page.getByText('独立显存占用不适用；此处显示系统统一内存使用率。', { exact: true }).waitFor()
  check('DGX Spark 统一内存展示')
  await screenshot('01-hardware-dgx-spark')

  await page.getByLabel('演示硬件机型').selectOption('dgx-station-gb300')
  await page.getByText('LPDDR5X · 与 252 GB HBM3e 构成最高 748 GB 一致性地址空间', { exact: true }).waitFor()
  check('DGX Station GB300 一致性内存展示')

  await page.getByLabel('演示硬件机型').selectOption('rtx5090-16')
  await page.getByTestId('gpu-card-15').waitFor()
  if (await page.locator('[data-testid^="gpu-card-"]').count() !== 16) throw new Error('十六卡机型未完整展示 16 个 GPU 设备')
  check('十六卡 RTX 5090 完整展示')
  await screenshot('01-hardware-16-gpu')

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

  await page.getByRole('button', { name: '模型中心', exact: true }).click()
  await page.getByRole('heading', { name: '模型中心' }).waitFor()
  await page.getByRole('button', { name: '添加模型', exact: true }).click()
  await page.getByRole('button', { name: '开始下载' }).click()
  await page.getByRole('button', { name: '暂停 dreamshaper-xl.safetensors', exact: true }).waitFor()
  await page.getByRole('button', { name: '暂停 dreamshaper-xl.safetensors', exact: true }).click()
  await page.getByRole('button', { name: '继续 dreamshaper-xl.safetensors', exact: true }).waitFor()
  check('模型添加、下载与暂停')
  await screenshot('03-models')

  await page.getByRole('button', { name: /^任务中心/ }).first().click()
  await page.getByRole('heading', { name: '任务中心' }).waitFor()
  await page.getByText('下载 dreamshaper-xl.safetensors', { exact: true }).waitFor()
  check('任务中心与模型任务同步')
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
