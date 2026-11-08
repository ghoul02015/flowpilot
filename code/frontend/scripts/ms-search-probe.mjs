import { chromium } from 'playwright-core'
const executablePath = process.env.CHROME_PATH ?? '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'
const browser = await chromium.launch({ executablePath, headless: true })
const page = await browser.newPage()
page.on('request', (req) => {
  const url = req.url()
  if (url.includes('/api/') && !url.includes('alicdn') && !url.includes('aliyun')) {
    console.log('[REQ]', req.method(), url.slice(0, 200))
    const post = req.postData()
    if (post) console.log('[BODY]', post.slice(0, 300))
  }
})
page.on('response', async (res) => {
  const url = res.url()
  if (url.includes('/api/') && !url.includes('alicdn') && res.status() === 200) {
    const ct = res.headers()['content-type'] ?? ''
    if (ct.includes('json')) {
      const text = await res.text().catch(() => '')
      if (text.toLowerCase().includes('minimax')) console.log('[RESP]', url.slice(0, 150), '=>', text.slice(0, 400))
    }
  }
})
await page.goto('https://modelscope.cn/models?name=minimax', { waitUntil: 'networkidle', timeout: 60000 }).catch((e) => console.log('goto:', e.message))
await page.waitForTimeout(5000)
await browser.close()
