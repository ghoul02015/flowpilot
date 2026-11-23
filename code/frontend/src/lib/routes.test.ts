// 路由契约单元测试：路径映射、查询参数白名单与登录回跳安全。
import { describe, expect, it } from 'vitest'
import { documentTitle, isModelTab, isSystemSection, pageFromPath, pagePath, safeNextPath } from './routes'

describe('pagePath', () => {
  it('一级页面映射固定路径', () => {
    expect(pagePath('dashboard')).toBe('/dashboard')
    expect(pagePath('instances')).toBe('/instances')
    expect(pagePath('tasks')).toBe('/tasks')
    expect(pagePath('system')).toBe('/system')
  })

  it('实例定位进入路径参数并转义', () => {
    expect(pagePath('instances', { focus: 'demo' })).toBe('/instances/demo')
    expect(pagePath('instances', { focus: '实例 01' })).toBe('/instances/%E5%AE%9E%E4%BE%8B%2001')
  })

  it('任务定位进入详情路由', () => {
    expect(pagePath('tasks', { focus: 'dl-42' })).toBe('/tasks/dl-42')
  })

  it('模型页 focus 仅接受合法页签', () => {
    expect(pagePath('models', { focus: 'online' })).toBe('/models?tab=online')
    expect(pagePath('models', { focus: 'bad-tab' })).toBe('/models?tab=local')
  })
})

describe('pageFromPath', () => {
  it('从深层路径回推一级页面', () => {
    expect(pageFromPath('/')).toBe('dashboard')
    expect(pageFromPath('/instances/demo')).toBe('instances')
    expect(pageFromPath('/tasks/dl-1')).toBe('tasks')
    expect(pageFromPath('/system/security')).toBe('system')
  })

  it('未知路径回退看板，不抛错', () => {
    expect(pageFromPath('/unknown/deep')).toBe('dashboard')
  })
})

describe('safeNextPath', () => {
  it('接受站内路径', () => {
    expect(safeNextPath('/instances/demo?x=1')).toBe('/instances/demo?x=1')
    expect(safeNextPath(encodeURIComponent('/models?tab=online'))).toBe('/models?tab=online')
  })

  it('拒绝协议、协议相对地址与反斜杠', () => {
    expect(safeNextPath('https://evil.example')).toBeNull()
    expect(safeNextPath('//evil.example/path')).toBeNull()
    expect(safeNextPath('/\\evil.example')).toBeNull()
    expect(safeNextPath('\\\\evil')).toBeNull()
    expect(safeNextPath(null)).toBeNull()
  })

  it('拒绝登录与初始化页自身，避免循环', () => {
    expect(safeNextPath('/login')).toBeNull()
    expect(safeNextPath('/setup')).toBeNull()
  })
})

describe('参数白名单', () => {
  it('模型页签仅允许 local／online／bundles', () => {
    expect(isModelTab('local')).toBe(true)
    expect(isModelTab('bundles')).toBe(true)
    expect(isModelTab('market')).toBe(false)
    expect(isModelTab(null)).toBe(false)
  })

  it('系统分区仅允许已定义值', () => {
    expect(isSystemSection('environment')).toBe(true)
    expect(isSystemSection('backup')).toBe(true)
    expect(isSystemSection('root')).toBe(false)
    expect(isSystemSection(undefined)).toBe(false)
  })
})

describe('documentTitle', () => {
  it('页面标题包含产品名', () => {
    expect(documentTitle('models')).toContain('模型中心')
    expect(documentTitle(undefined, 'demo')).toContain('demo')
  })
})
