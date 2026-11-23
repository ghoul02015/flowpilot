// 0.1.0.16 探针：新建实例对话框的镜像信任标注与后端口径一致性。
// 断言：未认证镜像（无 flowpilot.kind=comfyui 标签）在未放行时 option 禁用且标注「未认证」；
// 全部不可选时出现表单内指引；放行时未认证镜像可选但仍标注。
// 用法：node scripts/probe-image-trust.mjs [baseURL]
import { chromium } from 'playwright-core'

const base = process.argv[2] ?? process.env.FLOWPILOT_BASE ?? 'http://127.0.0.1:18080'
const username = process.env.FLOWPILOT_USER ?? 'admin'
const password = process.env.FLOWPILOT_PASSWORD ?? 'FlowPilot2026'

const failures = []
function check(name, ok, detail = '') {
  if (ok) console.log(`  ✓ ${name}`)
  else { console.log(`  ✗ ${name}${detail ? ` — ${detail}` : ''}`); failures.push(name) }
}

const browser = await chromium.launch({ channel: 'chrome', headless: true })
const page = await browser.newPage()

await page.goto(base + '/', { waitUntil: 'networkidle' })
await page.waitForTimeout(600)
if (await page.locator('[data-testid="login-page"]').count()) {
  await page.fill('#login-username', username)
  await page.fill('#login-password', password)
  await page.click('[data-testid="login-submit"]')
  await page.locator('[data-testid="login-page"]').waitFor({ state: 'detached', timeout: 25000 })
}
await page.getByRole('navigation', { name: '主导航' }).waitFor({ state: 'visible', timeout: 25000 })

// 页面内取真实口径：逃生阀状态与本机镜像标签。
const facts = await page.evaluate(async () => {
  const [instancesRes, imagesRes] = await Promise.all([fetch('/api/v1/instances'), fetch('/api/v1/images/local')])
  const instancesBody = await instancesRes.json()
  const imagesBody = await imagesRes.json()
  return { allowUnlabeled: instancesBody.defaults?.allowUnlabeledImages === true, images: imagesBody.images ?? [] }
})
console.log(`  · defaults.allowUnlabeledImages = ${facts.allowUnlabeled}，本机镜像 ${facts.images.length} 个`)

// 对话框候选口径与 isComfyImage 一致：标签命中或名称含 comfyui/comfy-ui。
const candidates = facts.images.flatMap((img) => img.RepoTags.map((tag) => ({ tag, trusted: img.Labels?.['flowpilot.kind'] === 'comfyui' })))
  .filter((item) => item.tag && item.tag !== '<none>:<none>' && (item.trusted || item.tag.toLowerCase().includes('comfyui') || item.tag.toLowerCase().includes('comfy-ui')))
console.log(`  · 对话框候选 ${candidates.length} 个：${candidates.map((c) => `${c.tag}(${c.trusted ? '可信' : '未认证'})`).join('、') || '无'}`)

// 打开新建实例对话框。
await page.goto(base + '/instances', { waitUntil: 'networkidle' })
await page.getByRole('button', { name: /新建实例/ }).first().click()
await page.getByText('新建 ComfyUI 实例').waitFor({ state: 'visible', timeout: 10000 })
await page.waitForTimeout(1200)

const select = page.locator('label:has-text("容器镜像（本机已有）") select')
await select.waitFor({ state: 'visible', timeout: 8000 })
const options = await select.locator('option').evaluateAll((nodes) => nodes.map((node) => ({ value: node.value, text: node.textContent ?? '', disabled: node.disabled })))
console.log(`  · 下拉选项 ${options.length} 个`)

check('候选数量与接口一致', options.length === candidates.length, `下拉 ${options.length} vs 接口 ${candidates.length}`)
for (const candidate of candidates) {
  const option = options.find((item) => item.value === candidate.tag)
  if (!option) { check(`选项存在 ${candidate.tag}`, false); continue }
  const expectDisabled = !facts.allowUnlabeled && !candidate.trusted
  check(`选项 ${candidate.tag} 禁用状态`, option.disabled === expectDisabled, `disabled=${option.disabled}，期望 ${expectDisabled}`)
  if (candidate.trusted) {
    check(`选项 ${candidate.tag} 无未认证标注`, !option.text.includes('未认证'), option.text)
  } else {
    check(`选项 ${candidate.tag} 未认证标注`, option.text.includes('未认证'), option.text)
  }
}

const enabledCount = candidates.filter((c) => facts.allowUnlabeled || c.trusted).length
const bodyText = await page.locator('[role="dialog"], .fixed.inset-0').last().innerText().catch(() => '')
if (candidates.length > 0 && enabledCount === 0) {
  check('全不可选指引可见', bodyText.includes('未通过可信认证') && bodyText.includes('FLOWPILOT_IMAGE_ALLOW_UNLABELED'), bodyText.slice(0, 200))
} else if (enabledCount > 0) {
  check('存在可选项时下拉可用', !(await select.isDisabled()))
  const selected = await select.inputValue()
  check('自动选中项在可选集合内', candidates.some((c) => c.tag === selected && (facts.allowUnlabeled || c.trusted)), `当前选中 ${selected}`)
}

await browser.close()
if (failures.length > 0) {
  console.error(`\n探针失败 ${failures.length} 项：${failures.join('；')}`)
  process.exit(1)
}
console.log('\n探针全部通过')
