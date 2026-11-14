import { chromium } from 'playwright-core'
const base = process.argv[2]
const b = await chromium.launch({ channel: 'chrome', headless: true })
const p = await b.newPage({ viewport: { width: 1440, height: 900 } })
p.on('pageerror', e => console.log('PAGEERROR', e.message))
p.on('console', m => { if (m.type() === 'error') console.log('CONSOLE', m.text().slice(0, 200)) })
await p.goto(base + '/dashboard', { waitUntil: 'networkidle' })
if (await p.locator('[data-testid="login-page"]').count()) {
  await p.fill('#login-username', 'admin'); await p.fill('#login-password', 'FlowPilot2026')
  await p.click('[data-testid="login-submit"]'); await p.locator('[data-testid="login-page"]').waitFor({ state: 'detached', timeout: 25000 })
}
for (const label of ['首页看板','ComfyUI 实例','镜像中心','模型中心','任务中心','硬件管理','系统管理']) {
  const link = p.getByRole('link', { name: label, exact: false }).first()
  try { await link.waitFor({ state: 'visible', timeout: 8000 }); await link.click() }
  catch { console.log('no link', label, 'body:', (await p.locator('body').innerText()).slice(0,60).replace(/\n/g,' | ')) }
  await p.waitForTimeout(700)
  console.log(label, 'url=', p.url(), 'links=', await p.getByRole('link').count())
}
await p.waitForTimeout(3000)
console.log('after 3s url=', p.url(), 'links=', await p.getByRole('link').count(), 'nav=', await p.locator('nav').count())
console.log('body:', (await p.locator('body').innerText()).slice(0, 120).replace(/\n/g, ' | '))
await b.close()
