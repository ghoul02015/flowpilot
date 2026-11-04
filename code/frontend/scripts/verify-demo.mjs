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
  await page.goto(baseURL, { waitUntil: 'networkidle' })
  await page.getByRole('heading', { name: '下午好，管理员' }).waitFor()
  check('首页看板加载')
  await screenshot('01-dashboard')

  await page.getByRole('button', { name: 'ComfyUI 实例', exact: true }).click()
  await page.getByRole('heading', { name: '实例管理' }).waitFor()
  check('实例导航')

  await page.getByRole('button', { name: '新建实例', exact: true }).click()
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
