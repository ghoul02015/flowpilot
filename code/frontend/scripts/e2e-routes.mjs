// 路由与深链接 E2E：刷新恢复、页签参数、筛选参数、404、历史前进后退与窄屏溢出。
// 用法：node scripts/e2e-routes.mjs [baseURL]
import { chromium } from 'playwright-core'

const base = process.argv[2] ?? process.env.FLOWPILOT_BASE ?? 'http://127.0.0.1:18080'
const username = process.env.FLOWPILOT_USER ?? 'admin'
const password = process.env.FLOWPILOT_PASSWORD ?? 'FlowPilot2026'

const failures = []
function check(name, ok, detail = '') {
  if (ok) console.log(`  ✓ ${name}`)
  else {
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
  await page.goto(base + '/dashboard', { waitUntil: 'networkidle' })
  await completeSetup(page)
  if (await page.locator('[data-testid="login-page"]').count()) {
    await page.fill('#login-username', username)
    await page.fill('#login-password', password)
    await page.click('[data-testid="login-submit"]')
    await page.locator('[data-testid="login-page"]').waitFor({ state: 'detached', timeout: 25000 })
  }
  await page.getByRole('navigation', { name: '主导航' }).waitFor({ state: 'visible', timeout: 25000 })
}

async function heading(page, text) {
  return page.getByRole('heading', { name: text, exact: false }).count()
}

async function main() {
  const browser = await chromium.launch({ channel: 'chrome', headless: true })
  const page = await (await browser.newContext({ viewport: { width: 1440, height: 900 } })).newPage()
  const errors = []
  page.on('pageerror', (error) => errors.push(error.message))

  console.log(`\n[1] 登录 ${base}`)
  await login(page)
  check('登录成功进入管理台', (await page.locator('[data-testid="login-page"]').count()) === 0)

  console.log('\n[2] 深链接直接访问')
  await page.goto(base + '/models?tab=online', { waitUntil: 'networkidle' })
  await page.waitForTimeout(1200)
  check('模型中心在线获取页签', await page.getByRole('tab', { name: '在线获取', selected: true }).count() > 0)
  check('在线面板渲染', (await page.locator('#model-panel-online').count()) > 0)

  await page.goto(base + '/system/security', { waitUntil: 'networkidle' })
  await page.waitForTimeout(1200)
  check('系统安全分区', (await heading(page, '系统管理')) > 0 && (await page.getByRole('tab', { name: '安全与会话', selected: true }).count()) > 0)

  await page.goto(base + '/tasks?type=download&status=succeeded', { waitUntil: 'networkidle' })
  await page.waitForTimeout(1200)
  const typeValue = await page.locator('[data-testid="task-type-filter"]').inputValue()
  check('任务筛选写入 URL 并恢复', typeValue === 'download', `type=${typeValue}`)

  await page.goto(base + '/tasks/does-not-exist', { waitUntil: 'networkidle' })
  await page.waitForTimeout(1200)
  check('任务不存在兜底', (await heading(page, '任务不存在')) > 0)

  await page.goto(base + '/instances/does-not-exist', { waitUntil: 'networkidle' })
  await page.waitForTimeout(1200)
  check('实例不存在兜底', (await heading(page, '实例不存在')) > 0)

  await page.goto(base + '/unknown-route', { waitUntil: 'networkidle' })
  await page.waitForTimeout(800)
  check('未匹配路由 404 页面', (await page.getByText('页面不存在').count()) > 0)

  console.log('\n[3] 浏览器前进后退')
  await page.goto(base + '/dashboard', { waitUntil: 'networkidle' })
  await page.getByRole('link', { name: /模型中心/ }).first().waitFor({ state: 'visible', timeout: 10000 })
  await page.getByRole('link', { name: /模型中心/ }).first().waitFor({ state: 'visible', timeout: 10000 })
  await page.getByRole('link', { name: /模型中心/ }).first().click()
  await page.waitForTimeout(800)
  check('导航到模型中心', (await page.getByRole('heading', { name: '模型中心' }).count()) > 0)
  await page.goBack()
  await page.waitForTimeout(1000)
  check('后退回到首页', (await heading(page, '管理员')) > 0)
  await page.goForward()
  await page.waitForTimeout(1000)
  check('前进回到模型中心', (await page.getByRole('heading', { name: '模型中心' }).count()) > 0)

  console.log('\n[4] 导航 aria-current')
  const current = await page.getByRole('link', { name: /模型中心/ }).first().getAttribute('aria-current')
  check('当前导航标记 aria-current', current === 'page', `aria-current=${current}`)

  console.log('\n[5] 375px 各页面横向溢出')
  await page.setViewportSize({ width: 375, height: 812 })
  for (const path of ['/dashboard', '/instances', '/images', '/models', '/tasks', '/hardware', '/system/security']) {
    await page.goto(base + path, { waitUntil: 'networkidle' })
    await page.waitForTimeout(1000)
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
    check(`${path} 无横向溢出`, overflow <= 2, `overflow=${overflow}px`)
  }

  await browser.close()

  console.log('\n===== 结果 =====')
  if (failures.length === 0 && errors.length === 0) {
    console.log('全部通过')
    process.exit(0)
  }
  for (const item of failures) console.log(' - ' + item)
  for (const item of errors.slice(0, 5)) console.log(' 脚本错误：' + item)
  process.exit(1)
}

main().catch((error) => {
  console.error(error)
  process.exit(2)
})
