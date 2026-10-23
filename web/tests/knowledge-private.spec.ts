// @vitest-environment jsdom
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, expect, it, vi } from 'vitest'
import Page from '../src/modules/knowledge/pages/PrivateKnowledgeView.vue'
const api = vi.hoisted(() => ({
  status: vi.fn(),
  list: vi.fn(),
  preview: vi.fn(),
  select: vi.fn(),
  withdraw: vi.fn(),
  retry: vi.fn(),
  search: vi.fn(),
  source: vi.fn(),
  choices: vi.fn(),
}))
vi.mock('../src/modules/knowledge/private-api', () => ({ privateKnowledgeApi: api }))
const source = {
  kind: 'personal_document',
  source_id: '201',
  revision: 'a'.repeat(64),
  format: 'text',
  title: '本人来源.txt',
}
beforeEach(() => {
  vi.resetAllMocks()
  api.status.mockResolvedValue({ data: { can_index: true, default_inclusion: false, outbound_authorized: false } })
  api.list.mockResolvedValue({ data: { items: [] } })
  api.choices.mockResolvedValue({ data: { items: [{ id: '201', name: '本人来源.txt' }] } })
  api.preview.mockResolvedValue({ data: { source, can_index: true } })
  api.select.mockResolvedValue({ data: {} })
  api.search.mockResolvedValue({ data: { items: [], total: 0 } })
})
const start = async () => {
  const w = mount(Page, { global: { stubs: { RouterLink: true } } })
  await flushPromises()
  return w
}
it('defaults to no selection and does not grant consent on loading or preview', async () => {
  const w = await start()
  expect(w.text()).toContain('默认不入库')
  expect(api.select).not.toHaveBeenCalled()
  await w
    .findAll('button')
    .find((b) => b.text() === '加载本人来源')!
    .trigger('click')
  await flushPromises()
  await w.findAll('select')[1]!.setValue('201')
  await w
    .findAll('button')
    .find((b) => b.text() === '核对当前来源')!
    .trigger('click')
  await flushPromises()
  const select = w.findAll('button').find((b) => b.text() === '确认选入这一项')!
  expect(select.attributes('disabled')).toBeDefined()
  expect(api.select).not.toHaveBeenCalled()
  await w.find('input[type=checkbox]').setValue(true)
  await select.trigger('click')
  await flushPromises()
  expect(api.select).toHaveBeenCalledTimes(1)
  expect(api.select.mock.calls[0]![0]).toEqual(source)
  w.unmount()
})
it('keeps protected storage unavailable and unsupported formats closed', async () => {
  api.status.mockResolvedValue({ data: { can_index: false } })
  const w = await start()
  expect(w.text()).toContain('未开启受保护')
  expect(w.find('input[type=checkbox]').exists()).toBe(false)
  expect(api.preview).not.toHaveBeenCalled()
  w.unmount()
})
it('clears stale and failed output without replaying a mutation', async () => {
  const w = await start()
  api.search.mockRejectedValue(new Error('来源已改变'))
  await w.find('input[autocomplete=off]').setValue('校园')
  await w.find('form').trigger('submit')
  await flushPromises()
  expect(w.find('[role=alert]').text()).toContain('来源已改变')
  expect(w.find('section[aria-label=私人引用]').exists()).toBe(false)
  expect(api.select).not.toHaveBeenCalled()
  w.unmount()
})
it('rejects a late search response after query changes even when transport ignores abort', async () => {
  let release!: (v: unknown) => void
  api.search.mockReturnValue(
    new Promise((r) => {
      release = r
    }),
  )
  const w = await start()
  await w.find('input[autocomplete=off]').setValue('校园')
  await w.find('form').trigger('submit')
  await w.find('input[autocomplete=off]').setValue('新词')
  release({ data: { items: [{ id: 'x', title: 'PRIVATE_LATE', snippet: 'PRIVATE_LATE_BODY' }] } })
  await flushPromises()
  expect(w.text()).not.toContain('PRIVATE_LATE')
  w.unmount()
})
it('renders private snippet and source as text and clears them on unmount', async () => {
  const hit = {
    id: 'kp_' + '1'.repeat(32),
    kind: 'personal_document',
    source_id: '201',
    revision: source.revision,
    title: '<img src=x onerror=alert(1)>',
    snippet: '<script>PRIVATE_TEXT</script>',
    score: 1,
  }
  api.search.mockResolvedValue({ data: { items: [hit] } })
  api.source.mockResolvedValue({ data: { ...source, title: hit.title, text: hit.snippet } })
  const w = await start()
  await w.find('input[autocomplete=off]').setValue('校园')
  await w.find('form').trigger('submit')
  await flushPromises()
  expect(w.find('img').exists()).toBe(false)
  expect(w.text()).toContain('<script>PRIVATE_TEXT')
  await w
    .findAll('button')
    .find((b) => b.text() === '读取当前引用')!
    .trigger('click')
  await flushPromises()
  expect(w.find('pre').text()).toBe(hit.snippet)
  expect(w.find('script').exists()).toBe(false)
  w.unmount()
})
