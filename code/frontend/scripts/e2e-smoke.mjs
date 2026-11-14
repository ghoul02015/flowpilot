// 管理端冒烟 E2E：登录、一级页面可达、模型就绪展示、窄屏导航与横向溢出。
// 用法：node scripts/e2e-smoke.mjs [baseURL]
// 依赖本机已安装的 Google Chrome（playwright-core 走 channel: chrome）。
import { chromium } from 'playwright-core'

const base = process.argv[2] ?? process.env.FLOWPILOT_BASE ?? 'http://127.0.0.1:18080'
const username = process.env.FLOWPILOT_USER ?? 'admin'
const password = process.env.FLOWPILOT_PASSWORD ?? 'FlowPilot2026'

const pageNames = [
  { label: '首页看板', title: '管理员' },
  { label: 'ComfyUI 实例', title: '实例管理' },
  { label: '镜像中心', title: '镜像中心' },
  { label: '模型中心', title: '模型中心' },
  { label: '任务中心', title: '任务中心' },
  { label: '硬件管理', title: '硬件管理' },
  { label: '系统管理', title: '系统管理' },
]

const failures = []
const consoleErrors = []
const requests = []

function check(name, ok, detail = '') {
  if (ok) {
    console.log(`  ✓ ${name}`)
  } else {
    console.log(`  ✗ ${name}${detail ? ` — ${detail}` : ''}`)
    failures.push(`${name}${detail ? ` — ${detail}` : ''}`)
  }
}

// 演示环境每次 up 都会重置数据库；检测到初始化页时用固定演示令牌完成初始化。
async function completeSetup(page) {
  const tokenInput = page.locator('input').first()
  if (!(await page.getByRole('button', { name: /完成初始化/ }).count())) return false
  const token = process.env.FLOWPILOT_INIT_TOKEN ?? 'flowpilot-demo-init-2026'
  await tokenInput.fill(token)
  await page.locator('input').nth(1).fill(password)
  await page.locator('input').nth(2).fill(password)
  await page.getByRole('button', { name: /完成初始化/ }).click()
  await page.waitForTimeout(2500)
  return true
}

async function login(page) {
  await page.goto(base + '/', { waitUntil: 'networkidle' })
  await completeSetup(page)
  const loginPage = page.locator('[data-testid="login-page"]')
  await page.waitForTimeout(500)
  if (await loginPage.count()) {
    await page.fill('#login-username', username)
    await page.fill('#login-password', password)
    await page.click('[data-testid="login-submit"]')
    await loginPage.waitFor({ state: 'detached', timeout: 25000 })
  }
  await page.getByRole('navigation', { name: '主导航' }).waitFor({ state: 'visible', timeout: 25000 })
  await page.waitForTimeout(800)
}

// 懒加载页面在慢机器上需要等待分块与首屏渲染，统一用自动等待代替固定 sleep。
async function waitForHeading(page, text, timeout = 15000) {
  try {
    await page.getByRole('heading', { name: text, exact: false }).first().waitFor({ state: 'visible', timeout })
    return true
  } catch {
    return false
  }
}

async function navTo(page, label) {
  // 导航是路由链接；慢机器上可能因懒加载重渲染导致元素被替换，点击失败时重试链接本身。
  const link = page.getByRole('link', { name: label, exact: false }).first()
  for (let attempt = 0; attempt < 3; attempt += 1) {
    try {
      await link.waitFor({ state: 'visible', timeout: 8000 })
      await link.click({ timeout: 8000 })
      await page.waitForTimeout(500)
      return
    } catch {
      await page.waitForTimeout(500)
    }
  }
  await page.getByRole('button', { name: label, exact: false }).first().click({ timeout: 5000 })
  await page.waitForTimeout(500)
}

async function main() {
  const browser = await chromium.launch({ channel: 'chrome', headless: true })
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 } })
  const page = await context.newPage()
  page.on('console', (message) => {
    if (message.type() === 'error') consoleErrors.push(message.text())
  })
  page.on('pageerror', (error) => consoleErrors.push(`pageerror: ${error.message}`))
  page.on('request', (request) => {
    if (request.url().includes('/api/v1/')) requests.push(request.url())
  })

  console.log(`\n[1] 登录 ${base}`)
  await login(page)
  check('登录后进入管理台', (await page.locator('[data-testid="login-page"]').count()) === 0)
  check('侧栏渲染', (await page.getByRole('navigation', { name: '主导航' }).count()) > 0)

  console.log('\n[2] 一级页面导航')
  for (const item of pageNames) {
    await navTo(page, item.label)
    const ok = await waitForHeading(page, item.title)
    check(`${item.label} → ${item.title}`, ok)
  }

  console.log('\n[3] 模型仓库就绪状态')
  await navTo(page, '模型中心')
  await page.waitForTimeout(2500)
  const rows = await page.locator('table tbody tr').count()
  const ready = await page.getByText('就绪', { exact: true }).count()
  check('模型表格有数据行', rows > 0, `rows=${rows}`)
  check('存在「就绪」状态行', ready > 0, `ready=${ready}`)

  console.log('\n[4] 任务中心列表')
  await navTo(page, '任务中心')
  await page.waitForTimeout(1500)
  check('任务列表容器可见', (await page.locator('[data-testid="unified-task-list"]').count()) > 0)

  console.log('\n[5] 顶栏信息与版本')
  const versionText = await page.locator('aside').first().innerText().catch(() => '')
  check('侧栏展示版本号', /v\d+\.\d+\.\d+\.\d+/.test(versionText), versionText.split('\n').slice(-2).join(' '))

  console.log('\n[6] 375px 窄屏')
  await page.setViewportSize({ width: 375, height: 812 })
  await page.waitForTimeout(500)
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  check('无横向溢出', overflow <= 2, `overflow=${overflow}px`)
  const menu = page.getByRole('button', { name: '打开菜单' })
  check('窄屏显示菜单按钮', (await menu.count()) > 0)
  if (await menu.count()) {
    await menu.first().click()
    await page.waitForTimeout(400)
    await navTo(page, '模型中心')
    check('窄屏菜单导航生效', await waitForHeading(page, '模型中心'))
  }

  console.log('\n[7] 请求与错误')
  const apiCalls = requests.length
  check('登录后产生 API 请求', apiCalls > 0, `calls=${apiCalls}`)
  const apiOutside = consoleErrors.filter((text) => !text.includes('Failed to load resource'))
  check('无页面脚本错误', apiOutside.length === 0, apiOutside.slice(0, 3).join(' | '))

  await browser.close()

  console.log('\n===== 结果 =====')
  if (failures.length === 0) {
    console.log(`全部通过（API 请求 ${apiCalls} 次）`)
    process.exit(0)
  }
  console.log(`失败 ${failures.length} 项：`)
  for (const item of failures) console.log(' - ' + item)
  if (apiOutside.length) {
    console.log('脚本错误：')
    for (const item of apiOutside.slice(0, 10)) console.log(' - ' + item)
  }
  process.exit(1)
}

main().catch((error) => {
  console.error(error)
  process.exit(2)
})
