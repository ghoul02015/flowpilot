import { chromium } from 'playwright-core'
const base = process.argv[2]
const b = await chromium.launch({ channel: 'chrome', headless: true })
const ctx = await b.newContext({ viewport: { width: 1440, height: 900 } })
const page = await ctx.newPage()
page.on('pageerror', e => console.log('PAGEERROR', e.message))
await page.goto(base + '/dashboard', { waitUntil: 'networkidle' })
if (await page.locator('[data-testid="login-page"]').count()) {
  await page.fill('#login-username','admin'); await page.fill('#login-password','FlowPilot2026')
  await page.click('[data-testid="login-submit"]'); await page.locator('[data-testid="login-page"]').waitFor({ state:'detached', timeout:25000 })
}
async function navTo(label) {
  const link = page.getByRole('link', { name: label, exact: false }).first()
  try {
    await link.waitFor({ state: 'visible', timeout: 10000 })
    await link.click()
  } catch (err) {
    console.log('  fallback for', label, '| reason:', String(err).split('\n')[0])
    console.log('  links now:', await page.getByRole('link', { name: label, exact: false }).count(), 'url:', page.url())
    try { await page.getByRole('button', { name: label, exact: false }).first().click({ timeout: 5000 }) } catch (e2) { console.log('  button also failed:', String(e2).split('\n')[0]) }
  }
  await page.waitForTimeout(600)
}
for (const label of ['首页看板','ComfyUI 实例','镜像中心','模型中心','任务中心','硬件管理','系统管理','模型中心']) {
  await navTo(label)
  console.log(label, '→', page.url())
}
await b.close()
