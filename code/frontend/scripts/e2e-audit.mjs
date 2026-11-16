// 深度 UI 审计：真实浏览各页面、打开主要弹窗、检查深链接/窄屏/键盘/请求重复与脚本错误。
// 只做只读与可取消操作：不提交构建、不停止实例、不删除数据。
// 用法：node scripts/e2e-audit.mjs [baseURL] [screenshotDir]
import { chromium } from 'playwright-core'
import { mkdirSync } from 'node:fs'

const base = process.argv[2] ?? process.env.FLOWPILOT_BASE ?? 'http://127.0.0.1:18080'
const shotDir = process.argv[3] ?? '../../.tmp/audit'
const username = process.env.FLOWPILOT_USER ?? 'admin'
const password = process.env.FLOWPILOT_PASSWORD ?? 'FlowPilot2026'
mkdirSync(shotDir, { recursive: true })

const failures = []
const notes = []
const consoleErrors = []
const httpErrors = []
const requests = []

function check(name, ok, detail = '') {
  if (ok) console.log(`  ✓ ${name}`)
  else {
    console.log(`  ✗ ${name}${detail ? ` — ${detail}` : ''}`)
    failures.push(`${name}${detail ? ` — ${detail}` : ''}`)
  }
}

async function shot(page, name) {
  await page.screenshot({ path: `${shotDir}/${name}.png`, fullPage: false })
}

async function completeSetup(page) {
  if (!(await page.getByRole('button', { name: /完成初始化/ }).count())) return
  const token = process.env.FLOWPILOT_INIT_TOKEN ?? 'flowpilot-demo-init-2026'
  const inputs = page.locator('input')
  await inputs.nth(0).fill(token)
  await inputs.nth(1).fill(password)
  await inputs.nth(2).fill(password)
  await page.getByRole('button', { name: /完成初始化/ }).click()
  await page.waitForTimeout(2000)
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
  try {
    await page.getByRole('heading', { name: text, exact: false }).first().waitFor({ state: 'visible', timeout: 15000 })
    return true
  } catch {
    return false
  }
}

async function openNav(page, label) {
  await page.getByRole('link', { name: new RegExp(label) }).first().click()
  await page.waitForTimeout(500)
}

async function overflow(page) {
  return page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
}

async function main() {
  const browser = await chromium.launch({ channel: 'chrome', headless: true })
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 } })
  const page = await context.newPage()
  page.on('console', (message) => {
    if (message.type() !== 'error') return
    const text = message.text()
    if (text.includes('401 (Unauthorized)')) return
    consoleErrors.push(text)
  })
  page.on('pageerror', (error) => consoleErrors.push(`pageerror: ${error.message}`))
  page.on('response', (response) => {
    if (!response.url().includes('/api/v1/')) return
    if (response.status() === 401 && response.url().includes('/api/v1/auth/session')) return
    if (response.status() >= 400) httpErrors.push(`${response.status()} ${response.url()}`)
  })
  page.on('request', (request) => {
    if (request.url().includes('/api/v1/')) requests.push(request.url())
  })

  // 从接口读取当前环境真实数据，断言不依赖特定种子。
  async function apiJson(path) {
    try {
      const response = await page.request.get(base + path)
      if (!response.ok()) return null
      return await response.json()
    } catch {
      return null
    }
  }

  console.log(`\n[1] 登录与认证跳转（${base}）`)
  await login(page)
  const instancesJson = await apiJson('/api/v1/instances')
  const instanceName = instancesJson?.instances?.[0]?.name ?? ''
  const modelsJson = await apiJson('/api/v1/models')
  const modelKeyword = modelsJson?.files?.find((file) => file.origin === 'local')?.name ?? ''
  const tasksJson = await apiJson('/api/v1/tasks?limit=50')
  const taskTypes = new Set((tasksJson?.tasks ?? []).map((task) => task.type))
  const hasImport = taskTypes.has('import')
  const hasFailed = (tasksJson?.tasks ?? []).some((task) => task.status === 'failed')
  notes.push(`环境数据：实例 ${instanceName || '无'}，模型 ${modelsJson?.files?.length ?? 0} 个，任务 ${tasksJson?.total ?? 0} 条`)
  check('登录进入管理台', (await page.locator('[data-testid="login-page"]').count()) === 0)
  await page.click('[data-testid="logout-button"]')
  await page.locator('[data-testid="login-page"]').waitFor({ state: 'visible', timeout: 15000 })
  check('退出登录回到登录页', page.url().includes('/login'))

  await page.goto(base + '/models?tab=online', { waitUntil: 'networkidle' })
  await page.waitForTimeout(800)
  check('未登录访问深链接跳登录并带 next', page.url().includes('/login') && page.url().includes('next='), page.url())
  const nextParam = new URL(page.url()).searchParams.get('next')
  check('next 保留原始目标', nextParam === '/models?tab=online', String(nextParam))
  await page.fill('#login-username', username)
  await page.fill('#login-password', password)
  await page.click('[data-testid="login-submit"]')
  await page.locator('[data-testid="login-page"]').waitFor({ state: 'detached', timeout: 25000 })
  await page.waitForTimeout(800)
  check('登录后回到原始深链接', page.url().includes('/models') && page.url().includes('tab=online'), page.url())

  console.log('\n[2] 首页看板')
  await page.goto(base + '/dashboard', { waitUntil: 'networkidle' })
  await page.waitForTimeout(1500)
  const dash = await page.locator('main').innerText()
  check('展示问候与概览', dash.includes('管理员') && dash.includes('OVERVIEW'))
  check('卡片：运行实例／就绪模型／进行中任务', dash.includes('运行实例') && dash.includes('就绪模型') && dash.includes('进行中任务'))
  check('快捷操作入口存在', dash.includes('部署 ComfyUI') && dash.includes('添加模型') && dash.includes('查看任务'))
  await shot(page, '01-dashboard')

  // 首页实例名 → 详情深链接
  const instanceLink = instanceName ? page.locator('main button', { hasText: instanceName }).first() : null
  if (instanceLink && await instanceLink.count()) {
    await instanceLink.click()
    await page.waitForTimeout(1200)
    check('首页实例名跳转详情', page.url().endsWith(`/instances/${encodeURIComponent(instanceName)}`), page.url())
  } else {
    notes.push('首页未出现实例行（实例为空或不在概览中）')
  }

  console.log('\n[3] 实例列表与详情')
  await page.goto(base + '/instances', { waitUntil: 'networkidle' })
  check('实例列表可达', await heading(page, '实例管理'))
  await shot(page, '02-instances')
  const row = instanceName ? page.locator(`[data-testid="instance-row-${instanceName}"]`) : null
  check('实例行展示', Boolean(row && (await row.count()) > 0), instanceName || '无实例')
  if (row && await row.count()) {
    await row.locator('button', { hasText: instanceName }).first().click()
    await page.waitForTimeout(1500)
    check('点击实例名进入详情', page.url().endsWith(`/instances/${encodeURIComponent(instanceName)}`))
    check('详情页标题为实例名', await heading(page, instanceName))
    check('应用健康区块存在', (await page.locator('[data-testid="detail-app-health"]').count()) > 0)
    check('事件时间线存在', (await page.locator('[data-testid="detail-events"]').count()) > 0)
    await shot(page, '03-instance-detail')

    // 日志弹窗
    await page.getByRole('button', { name: '实例日志' }).click()
    await page.waitForTimeout(1200)
    const logsDialog = page.getByRole('dialog')
    check('实例日志弹窗打开', (await logsDialog.count()) > 0)
    await shot(page, '04-instance-logs')
    await page.keyboard.press('Escape')
    await page.waitForTimeout(500)
    check('Esc 关闭日志弹窗', (await page.getByRole('dialog').count()) === 0)

    // 模型关联弹窗
    await page.getByRole('button', { name: '模型关联' }).click()
    await page.waitForTimeout(1200)
    check('模型关联弹窗打开', (await page.getByRole('dialog').count()) > 0)
    await page.keyboard.press('Escape')
    await page.waitForTimeout(400)

    // 删除弹窗只打开不确认
    await page.getByRole('button', { name: '删除实例' }).click()
    await page.waitForTimeout(600)
    check('删除确认弹窗打开', (await page.getByRole('dialog').count()) > 0)
    check('删除按钮在未输入名称时禁用', await page.getByRole('button', { name: '确认删除' }).isDisabled())
    await page.getByRole('button', { name: '取消' }).click()
    await page.waitForTimeout(400)
    check('取消删除回到详情', (await page.getByRole('dialog').count()) === 0 && page.url().includes(`/instances/${encodeURIComponent(instanceName)}`))
    // 返回列表
    await page.getByRole('button', { name: '返回列表' }).click()
    await page.waitForTimeout(800)
    check('返回实例列表', page.url().endsWith('/instances'), page.url())
  }

  console.log('\n[4] 镜像中心')
  await page.goto(base + '/images', { waitUntil: 'networkidle' })
  await page.waitForTimeout(1200)
  check('镜像中心可达', await heading(page, '镜像中心'))
  await shot(page, '05-images')
  const imageSearch = page.getByPlaceholder('搜索镜像名、标签或镜像 ID')
  check('镜像搜索框存在', (await imageSearch.count()) > 0)
  if (await imageSearch.count()) {
    await imageSearch.fill('flowpilot-web')
    await page.waitForTimeout(600)
    const filtered = await page.locator('main').innerText()
    check('搜索过滤生效', filtered.includes('flowpilot-web') && !filtered.includes('regress:0.1.0.7'), filtered.slice(0, 60))
    await imageSearch.fill('')
    await page.waitForTimeout(400)
  }
  const otherToggle = page.getByRole('button', { name: /展开|收起/ }).last()
  if (await otherToggle.count()) {
    const before = await page.getByText('机器其他镜像').count()
    await otherToggle.click()
    await page.waitForTimeout(400)
    check('其他镜像可展开／收起', before > 0)
  }
  // 构建弹窗（只看不提交）
  await page.getByRole('button', { name: /一键构建 ComfyUI 镜像/ }).click()
  await page.waitForTimeout(1500)
  check('构建弹窗打开', (await page.getByRole('dialog').count()) > 0)
  const buildDialog = await page.getByRole('dialog').innerText().catch(() => '')
  check('构建弹窗包含版本选择', buildDialog.includes('ComfyUI 版本'), buildDialog.slice(0, 80))
  await page.keyboard.press('Escape')
  await page.waitForTimeout(400)
  check('Esc 关闭构建弹窗', (await page.getByRole('dialog').count()) === 0)
  // 拉取弹窗
  await page.getByRole('button', { name: /拉取在线容器镜像/ }).click()
  await page.waitForTimeout(600)
  check('拉取弹窗打开', (await page.getByRole('dialog').count()) > 0)
  await page.keyboard.press('Escape')
  await page.waitForTimeout(300)

  console.log('\n[5] 模型中心三个页签')
  await page.goto(base + '/models?tab=local', { waitUntil: 'networkidle' })
  await page.waitForTimeout(1500)
  check('本地仓库页签', await page.getByRole('tab', { name: '本地仓库', selected: true }).count() > 0)
  const modelsText = await page.locator('main').innerText()
  check('展示本地仓库统计', modelsText.includes('本地仓库') && modelsText.includes('数据来源'))
  check('模型分组可见（LoRA 等）', /LoRA|Checkpoints|VAE/.test(modelsText), modelsText.slice(0, 80))
  await shot(page, '06-models-local')
  // 搜索与筛选
  if (modelKeyword) {
    const keyword = modelKeyword.replace(/\.(safetensors|ckpt|pt|bin)$/i, '')
    await page.getByPlaceholder('搜索模型文件').fill(keyword)
    await page.waitForTimeout(600)
    const filteredModels = await page.locator('main').innerText()
    check('模型搜索生效', filteredModels.includes(keyword), keyword)
  } else {
    notes.push('没有本地模型文件，跳过模型搜索检查')
  }
  await page.getByPlaceholder('搜索模型文件').fill('')
  await page.waitForTimeout(300)
  // 折叠分组
  const groupToggle = page.locator('main button[aria-expanded]').first()
  if (await groupToggle.count()) {
    const before = await groupToggle.getAttribute('aria-expanded')
    await groupToggle.click()
    await page.waitForTimeout(300)
    const after = await groupToggle.getAttribute('aria-expanded')
    check('模型分组可折叠', before !== after, `${before}→${after}`)
    await groupToggle.click()
  }
  // 导出清单与下载任务操作
  const [csv] = await Promise.all([
    page.waitForEvent('download', { timeout: 10000 }).catch(() => null),
    page.getByRole('button', { name: '导出清单' }).click(),
  ])
  check('导出模型清单 CSV', Boolean(csv && csv.suggestedFilename().endsWith('.csv')), csv ? csv.suggestedFilename() : 'no download')
  await page.goto(base + '/models?tab=local', { waitUntil: 'networkidle' })
  // 数据行渲染依赖多次接口往返，固定短等待在重载机器上不稳定。
  await page.getByRole('row').first().waitFor({ timeout: 15000 }).catch(() => {})
  await page.waitForTimeout(500)
  const retryButtons = await page.getByRole('button', { name: /重试/ }).count()
  check('未完成下载提供重试入口', retryButtons > 0, `retry=${retryButtons}`)
  const copyButtons = await page.getByRole('button', { name: /复制 .* 路径/ }).count()
  check('就绪模型提供复制路径', copyButtons > 0, `copy=${copyButtons}`)
  // 添加模型弹窗（0.1.0.14 起瘦身：仅 URL 下载／本地导入，模型市场在「在线获取」页签）
  await page.getByRole('button', { name: '添加模型' }).click()
  await page.waitForTimeout(1500)
  check('添加模型弹窗打开', (await page.getByRole('dialog').count()) > 0)
  const addDialog = await page.getByRole('dialog').innerText().catch(() => '')
  check('弹窗提供 URL 下载与本地导入', addDialog.includes('URL 下载') && addDialog.includes('本地导入'), addDialog.slice(0, 60))
  await page.getByRole('button', { name: 'URL 下载' }).click()
  await page.waitForTimeout(400)
  await page.getByRole('button', { name: '开始下载' }).click()
  await page.waitForTimeout(500)
  const urlError = await page.getByRole('alert').innerText().catch(() => '')
  check('URL 下载校验非法地址', urlError.includes('http'), urlError)
  await page.keyboard.press('Escape')
  await page.waitForTimeout(400)
  check('Esc 关闭添加模型弹窗', (await page.getByRole('dialog').count()) === 0)

  // 在线获取
  await page.goto(base + '/models?tab=online', { waitUntil: 'networkidle' })
  // 在线目录需实时拉取 ModelScope，网络波动时首屏超过 1.5 秒。
  await page.locator('[data-testid^="market-entry-"]').first().waitFor({ timeout: 45000 }).catch(() => {})
  await page.waitForTimeout(500)
  check('在线获取页签', await page.getByRole('tab', { name: '在线获取', selected: true }).count() > 0)
  check('在线面板有搜索输入', (await page.getByPlaceholder(/搜索模型名称|搜索模型仓库/).count()) > 0)
  const entryTops = await page.locator('[data-testid^="market-entry-"]').evaluateAll((nodes) => nodes.slice(0, 2).map((node) => Math.round(node.getBoundingClientRect().top)))
  check('大屏在线目录双列展示', entryTops.length === 2 && Math.abs(entryTops[0] - entryTops[1]) < 4, JSON.stringify(entryTops))
  const listHeight = await page.locator('#model-panel-online .overflow-y-auto').first().evaluate((node) => node.clientHeight).catch(() => 0)
  check('在线列表可视高度不小于 400px', listHeight >= 400, `height=${listHeight}`)
  await shot(page, '07-models-online')
  // 组合套装
  await page.goto(base + '/models?tab=bundles', { waitUntil: 'networkidle' })
  await page.waitForTimeout(1500)
  check('组合套装页签', await page.getByRole('tab', { name: '组合套装', selected: true }).count() > 0)
  check('套装清单渲染', (await page.locator('main').innerText()).includes('套装'))
  await shot(page, '08-models-bundles')

  console.log('\n[6] 任务中心与筛选')
  await page.goto(base + '/tasks', { waitUntil: 'networkidle' })
  await page.waitForTimeout(1500)
  check('任务列表可达', await heading(page, '任务中心'))
  const taskRows = await page.locator('[data-testid="unified-task-list"] tbody tr').count()
  check('任务表格有数据', taskRows > 0, `rows=${taskRows}`)
  await shot(page, '09-tasks')
  const filterType = hasImport ? 'import' : (taskTypes.values().next().value ?? '')
  if (filterType) {
    await page.locator('[data-testid="task-type-filter"]').selectOption(filterType)
    await page.waitForTimeout(900)
    check('类型筛选写入 URL', page.url().includes(`type=${filterType}`), page.url())
    const typeBadges = await page.locator(`[data-testid="task-type-badge-${filterType}"]`).count()
    check('筛选结果与类型一致', typeBadges > 0, filterType)
  } else {
    notes.push('没有任务可用于类型筛选检查')
  }
  if (hasFailed) {
    await page.goto(base + '/tasks?status=failed', { waitUntil: 'networkidle' })
    await page.waitForTimeout(1200)
    const failedText = await page.locator('[data-testid="unified-task-list"]').innerText()
    check('状态筛选展示失败任务', failedText.includes('失败'))
  } else {
    notes.push('没有失败任务，跳过失败筛选检查')
  }
  // 任务详情
  const listSearch = new URL(page.url()).search
  const detailButton = page.getByRole('button', { name: '详情' }).first()
  if (await detailButton.count()) {
    await detailButton.click()
    await page.waitForURL(/\/tasks\/[^/]+$/, { timeout: 15000 }).catch(() => {})
    await page.getByText('任务信息').first().waitFor({ timeout: 15000 }).catch(() => {})
    check('任务详情路由打开', /\/tasks\/.+/.test(page.url()), page.url())
    const detailText = await page.locator('main').innerText()
    check('详情含任务字段与失败原因/信息', detailText.includes('任务信息') && /失败原因|最新信息/.test(detailText))
    await shot(page, '10-task-detail')
    await page.getByRole('link', { name: '返回任务中心' }).click()
    await page.waitForTimeout(800)
    check('返回任务中心保留筛选', page.url().endsWith(`/tasks${listSearch}`), `${page.url()} vs /tasks${listSearch}`)
  }
  // 分页
  await page.goto(base + '/tasks', { waitUntil: 'networkidle' })
  await page.waitForTimeout(1200)
  const nextPage = page.getByRole('button', { name: '下一页' })
  if (await nextPage.count() && !(await nextPage.isDisabled())) {
    await nextPage.click()
    await page.waitForTimeout(900)
    check('下一页写入 page 参数', page.url().includes('page=2'), page.url())
    await page.getByRole('button', { name: '上一页' }).click()
    await page.waitForTimeout(900)
    check('上一页回到第一页', !page.url().includes('page=2'), page.url())
  } else {
    notes.push('任务不足一页，跳过翻页检查')
  }
  // 清理弹窗只打开
  await page.getByRole('button', { name: '清理已结束' }).click()
  await page.waitForTimeout(500)
  check('清理确认弹窗打开', (await page.getByRole('dialog').count()) > 0)
  await page.getByRole('button', { name: '取消' }).click()
  await page.waitForTimeout(300)

  console.log('\n[7] 硬件管理')
  await page.goto(base + '/hardware', { waitUntil: 'networkidle' })
  await page.waitForTimeout(1500)
  check('硬件页可达', await heading(page, '硬件管理'))
  const hardwareText = await page.locator('main').innerText()
  check('CPU/内存/存储区块', hardwareText.includes('处理器') || hardwareText.includes('CPU'))
  check('兼容性区块', hardwareText.includes('兼容性'))
  await shot(page, '11-hardware')

  console.log('\n[8] 系统管理八个分区')
  const sections = [
    ['environment', '环境检测'],
    ['storage', '网络存储'],
    ['database', '数据底座'],
    ['migration', '数据迁移'],
    ['backup', '存储与备份'],
    ['security', '安全与会话'],
    ['audit', '操作审计'],
    ['about', '诊断与关于'],
  ]
  for (const [slug, label] of sections) {
    await page.goto(base + `/system/${slug}`, { waitUntil: 'networkidle' })
    await page.waitForTimeout(1200)
    const ok = await heading(page, '系统管理')
    const text = await page.locator('main').innerText()
    check(`分区 ${label} 渲染`, ok && text.length > 120, `chars=${text.length}`)
    const subTab = await page.getByRole('tab', { name: label, selected: true }).count()
    const groupTab = await page.getByRole('tab', { selected: true }).first().innerText().catch(() => '')
    check(`分区 ${label} 高亮`, subTab > 0 || groupTab.length > 0, `sub=${subTab} group=${groupTab}`)
  }
  await shot(page, '12-system-security')
  await page.goto(base + '/system/security', { waitUntil: 'networkidle' })
  await page.waitForTimeout(1200)
  const securityText = await page.locator('main').innerText()
  check('安全页展示会话/密码能力', securityText.includes('密码') || securityText.includes('会话'))
  await page.goto(base + '/system/audit', { waitUntil: 'networkidle' })
  // 审计记录较多时首屏渲染偏慢，等待表格出现再计数。
  await page.locator('main table').first().waitFor({ timeout: 15000 }).catch(() => {})
  check('审计页有记录表格', (await page.locator('main table').count()) > 0)

  console.log('\n[9] 404 与非法参数')
  await page.goto(base + '/models?tab=bogus', { waitUntil: 'networkidle' })
  await page.waitForTimeout(1000)
  check('非法模型页签回退本地仓库', await page.getByRole('tab', { name: '本地仓库', selected: true }).count() > 0)
  await page.goto(base + '/system/bogus', { waitUntil: 'networkidle' })
  await page.waitForTimeout(1000)
  check('非法系统分区显示兜底提示', (await page.locator('main').innerText()).includes('未识别的系统分区'))
  await page.goto(base + '/no-such-page', { waitUntil: 'networkidle' })
  await page.waitForTimeout(800)
  check('未知路径 404 页面', (await page.locator('main').innerText()).includes('页面不存在'))
  await page.getByRole('button', { name: '返回首页看板' }).click()
  await page.waitForTimeout(800)
  check('404 返回看板', page.url().endsWith('/dashboard'))

  console.log('\n[10] 键盘可用性')
  await page.goto(base + '/dashboard', { waitUntil: 'networkidle' })
  await page.waitForTimeout(800)
  await page.keyboard.press('Tab')
  const firstFocus = await page.evaluate(() => document.activeElement?.tagName + ':' + (document.activeElement?.textContent || '').trim().slice(0, 12))
  notes.push(`首个 Tab 焦点：${firstFocus}`)
  const navLink = page.getByRole('link', { name: /模型中心/ }).first()
  await navLink.focus()
  await page.keyboard.press('Enter')
  await page.waitForTimeout(900)
  check('键盘 Enter 可进入导航', page.url().endsWith('/models'), page.url())

  console.log('\n[11] 375px 窄屏与弹窗')
  await page.setViewportSize({ width: 375, height: 812 })
  for (const [path, name] of [
    ['/dashboard', '13-mobile-dashboard'],
    ['/instances', '14-mobile-instances'],
    ...(instanceName ? [[`/instances/${encodeURIComponent(instanceName)}`, '15-mobile-instance-detail']] : []),
    ['/images', '16-mobile-images'],
    ['/models?tab=local', '17-mobile-models'],
    ['/models?tab=online', '18-mobile-online'],
    ['/models?tab=bundles', '19-mobile-bundles'],
    ['/tasks', '20-mobile-tasks'],
    ['/tasks?status=failed', '21-mobile-tasks-failed'],
    ['/hardware', '22-mobile-hardware'],
    ['/system/security', '23-mobile-system'],
  ]) {
    await page.goto(base + path, { waitUntil: 'networkidle' })
    await page.waitForTimeout(1200)
    const px = await overflow(page)
    check(`${path} 无横向溢出`, px <= 2, `overflow=${px}px`)
    await shot(page, name)
  }
  // 窄屏打开添加模型弹窗
  await page.goto(base + '/models?tab=local', { waitUntil: 'networkidle' })
  await page.waitForTimeout(800)
  await page.getByRole('button', { name: '添加模型' }).click()
  await page.waitForTimeout(1200)
  check('窄屏添加模型弹窗打开', (await page.getByRole('dialog').count()) > 0)
  const dialogOverflow = await page.evaluate(() => {
    const dialog = document.querySelector('[role="dialog"]')
    return dialog ? Math.round(dialog.scrollWidth - dialog.clientWidth) : -1
  })
  check('窄屏弹窗内部无横向溢出', dialogOverflow <= 2, `overflow=${dialogOverflow}px`)
  await shot(page, '24-mobile-add-model')
  await page.keyboard.press('Escape')

  console.log('\n[12] 请求重复与错误')
  await page.setViewportSize({ width: 1440, height: 900 })
  requests.length = 0
  await page.goto(base + '/dashboard', { waitUntil: 'networkidle' })
  await page.waitForTimeout(12000)
  const counts = new Map()
  for (const url of requests) {
    const key = url.split('?')[0].replace(base, '')
    counts.set(key, (counts.get(key) ?? 0) + 1)
  }
  const top = [...counts.entries()].sort((a, b) => b[1] - a[1]).slice(0, 6)
  notes.push(`看板 12 秒请求：${top.map(([k, v]) => `${k}×${v}`).join('，')}`)
  const modelsCalls = counts.get('/api/v1/models') ?? 0
  check('模型扫描未被高频轮询（≤1 次）', modelsCalls <= 1, `models×${modelsCalls}`)
  const summaryCalls = counts.get('/api/v1/tasks/summary') ?? 0
  check('任务摘要周期合理（12s ≤5 次）', summaryCalls <= 5, `summary×${summaryCalls}`)

  // 面包屑与失败原因
  if (instanceName) {
    await page.goto(base + `/instances/${encodeURIComponent(instanceName)}`, { waitUntil: 'networkidle' })
    await page.locator('header').first().waitFor({ timeout: 15000 })
    await page.waitForTimeout(500)
    const crumb = await page.locator('header').innerText()
    check('详情面包屑含对象名', crumb.includes(instanceName), crumb.replace(/\n/g, ' '))
  } else {
    notes.push('没有实例，跳过面包屑检查')
  }
  await page.goto(base + '/tasks?status=failed', { waitUntil: 'networkidle' })
  await page.waitForTimeout(1200)
  const failDetailButton = page.getByRole('button', { name: '详情' }).first()
  if (await failDetailButton.count()) {
    await failDetailButton.click()
    await page.waitForURL(/\/tasks\/[^/]+$/, { timeout: 15000 }).catch(() => {})
    await page.getByText('任务信息').first().waitFor({ timeout: 15000 }).catch(() => {})
    const detailFail = await page.locator('main').innerText()
    check('失败任务展示失败原因区块', detailFail.includes('失败原因'), detailFail.slice(0, 80))
    const gridClass = await page.evaluate(() => document.querySelector('main > div.grid')?.className ?? '')
    check('失败任务无日志时面板占满宽度', !gridClass.includes('lg:grid-cols-3'), gridClass)
  } else {
    notes.push('没有失败任务可用于详情检查')
  }

  // 窄屏模型市场条目名称可读（不被压成 1 个字符）
  await page.setViewportSize({ width: 375, height: 812 })
  await page.goto(base + '/models?tab=online', { waitUntil: 'networkidle' })
  await page.waitForTimeout(1500)
  const firstName = await page.locator('#model-panel-online').getByText('Stable Diffusion 1.5').first().boundingBox()
  check('窄屏在线条目名称宽度足够', Boolean(firstName && firstName.width > 120), `width=${firstName ? Math.round(firstName.width) : 'n/a'}`)
  await shot(page, '25-mobile-online-fixed')
  await page.setViewportSize({ width: 1440, height: 900 })

  const scriptErrors = consoleErrors.filter((text) => !text.includes('Failed to load resource'))
  check('无页面脚本错误', scriptErrors.length === 0, scriptErrors.slice(0, 2).join(' | '))
  check('无 4xx/5xx（登录 401 除外）', httpErrors.length === 0, httpErrors.slice(0, 3).join(' | '))

  await browser.close()

  console.log('\n===== 审计结果 =====')
  for (const note of notes) console.log('· ' + note)
  if (failures.length === 0) {
    console.log('全部检查通过')
    process.exit(0)
  }
  console.log(`失败 ${failures.length} 项：`)
  for (const item of failures) console.log(' - ' + item)
  if (scriptErrors.length) console.log('脚本错误：', scriptErrors.slice(0, 5))
  if (httpErrors.length) console.log('HTTP 错误：', httpErrors.slice(0, 5))
  process.exit(1)
}

main().catch((error) => {
  console.error(error)
  process.exit(2)
})
