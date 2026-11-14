// @vitest-environment jsdom
import { beforeEach, afterEach, expect, it, vi } from 'vitest'
const fixture = vi.hoisted(() => ({
  action: vi.fn(),
  configure: vi.fn(),
  preview: vi.fn(),
  upload: vi.fn(),
  confirm: vi.fn(),
  token: 'alice',
}))
vi.mock('../src/modules/plugin-center/v5-api', () => ({
  pluginV5Api: { uiAction: fixture.action, configure: fixture.configure },
}))
vi.mock('../src/modules/richtext/api', () => ({
  richTextApi: { preview: fixture.preview, uploadAsset: fixture.upload },
}))
vi.mock('../src/modules/plugin-runtime/store', () => ({ useUIRuntimeStore: () => ({ sync: vi.fn() }) }))
vi.mock('../src/modules/identity/session', () => ({ getAccessToken: () => fixture.token }))
vi.mock('element-plus', () => ({ ElMessageBox: { confirm: fixture.confirm } }))
import { handlePluginHostAction } from '../src/campus-ui/pluginHostActions'
import { takeImportedArticle } from '../src/campus-ui/importedArticle'
import type { RuntimeSurface } from '../src/campus-ui/contracts'
import { PLUGIN_BRIDGE_VERSION, type PluginBridgeRequest } from '../src/campus-ui/isolatedPluginBridge'

const surface = {
  plugin: 'example.news-reader',
  plugin_version: '2.0.0',
  context_proof: 'current',
  frame: { audience: 'user' },
} as RuntimeSurface
const request = (method: PluginBridgeRequest['method'], params: Record<string, unknown>): PluginBridgeRequest => ({
  version: PLUGIN_BRIDGE_VERSION,
  request_id: 'owned-1',
  method,
  params,
})
const article = () =>
  request('editor.article.import', {
    title: '插件文章',
    summary: '摘要',
    content_html: '<p>正文</p><img src="https://public.example/photo.png">',
    source: 'https://public.example/article',
  })
beforeEach(() => {
  vi.useFakeTimers()
  vi.clearAllMocks()
  fixture.token = 'alice'
  fixture.confirm.mockResolvedValue(true)
  fixture.action.mockImplementation(async (_c, q) => ({
    code: 0,
    data: q.action === 'web.read' ? { bytes: btoa('owned-image'), mime: 'image/png' } : { authorized: true },
  }))
  fixture.preview.mockImplementation(async (html) => ({ data: { sanitized_html: html } }))
  fixture.upload.mockResolvedValue({ data: { file_url: '/api/v1/richtext/assets/123/content' } })
})
afterEach(() => vi.useRealTimers())
it('uses a generic public reader for a publisher unknown to the host', async () => {
  const result: any = await handlePluginHostAction(
    surface,
    request('web.read', { url: 'https://public.example/page', kind: 'document' }),
    vi.fn().mockResolvedValue(undefined),
  )
  expect(new TextDecoder().decode(result.bytes)).toBe('owned-image')
  expect(fixture.action.mock.calls[0]).toEqual([
    { publisher: 'example', plugin_key: 'news-reader', version: '2.0.0' },
    { proof: 'current', action: 'web.read', url: 'https://public.example/page', kind: 'document' },
  ])
  for (const url of ['file:///etc/passwd', 'https://name:password@example.test/private'])
    await expect(
      handlePluginHostAction(surface, request('web.read', { url, kind: 'document' }), vi.fn()),
    ).rejects.toThrow()
  expect(fixture.action).toHaveBeenCalledTimes(1)
})
it('rejects server proxy selectors and endpoints from a user plugin', async () => {
  for (const extra of [{ profile: 'clash' }, { proxy: 'http://127.0.0.1:7890' }]) {
    await expect(
      handlePluginHostAction(
        surface,
        request('web.read', { url: 'https://public.example/document', kind: 'document', ...extra }),
        vi.fn(),
      ),
    ).rejects.toThrow()
  }
  const imported = article()
  imported.params.media_profile = 'clash'
  await expect(handlePluginHostAction(surface, imported, vi.fn(), vi.fn())).rejects.toThrow()
  expect(fixture.action).not.toHaveBeenCalled()
})
it('saves arbitrary declared configuration without a feed-field convention', async () => {
  fixture.configure.mockResolvedValue({ code: 0 })
  await handlePluginHostAction(
    surface,
    request('config.update', { revision: '3', values_json: '{"layout":"compact"}' }),
    vi.fn().mockResolvedValue(undefined),
  )
  expect(fixture.configure).toHaveBeenCalledWith(
    { publisher: 'example', plugin_key: 'news-reader', version: '2.0.0' },
    '3',
    { layout: 'compact' },
    'current',
  )
})
it('creates only an owner-bound handoff after confirmation, image copy and sanitization', async () => {
  const open = vi.fn().mockResolvedValue(undefined)
  await handlePluginHostAction(surface, article(), vi.fn().mockResolvedValue(undefined), open)
  expect(open).not.toHaveBeenCalled()
  expect(fixture.confirm).toHaveBeenCalledTimes(1)
  expect(fixture.upload).toHaveBeenCalledTimes(1)
  await vi.advanceTimersByTimeAsync(100)
  const imported = takeImportedArticle(open.mock.calls[0][0])!
  expect(imported.content_html).toContain('/api/v1/richtext/assets/123/content')
  expect(imported.content_html).not.toContain('src="https://')
  expect(imported.content_html).toContain('来源：')
  expect(imported.title).toBe('插件文章')
  expect(takeImportedArticle(open.mock.calls[0][0])).toBeUndefined()
})
it('performs no upload or navigation after cancellation or authority loss', async () => {
  const open = vi.fn()
  fixture.confirm.mockRejectedValue('cancel')
  await expect(handlePluginHostAction(surface, article(), vi.fn().mockResolvedValue(undefined), open)).rejects.toBe(
    'cancel',
  )
  expect(fixture.upload).not.toHaveBeenCalled()
  expect(open).not.toHaveBeenCalled()
  fixture.confirm.mockResolvedValue(true)
  const current = vi.fn().mockRejectedValue(new Error('old context'))
  await expect(handlePluginHostAction(surface, article(), current, open)).rejects.toThrow('old context')
  expect(fixture.upload).not.toHaveBeenCalled()
})
it('rejects a late handoff after switching users and leaves failed images as text', async () => {
  fixture.upload.mockRejectedValue(new Error('unavailable'))
  const open = vi.fn().mockResolvedValue(undefined)
  await handlePluginHostAction(surface, article(), vi.fn().mockResolvedValue(undefined), open)
  await vi.advanceTimersByTimeAsync(100)
  const imported = takeImportedArticle(open.mock.calls[0][0])!
  expect(imported.content_html).toContain('图片待补充')
  expect(imported.warnings).toHaveLength(1)
  await handlePluginHostAction(surface, article(), vi.fn().mockResolvedValue(undefined), open)
  await vi.advanceTimersByTimeAsync(100)
  fixture.token = 'bob'
  expect(takeImportedArticle(open.mock.calls[1][0])).toBeUndefined()
})
