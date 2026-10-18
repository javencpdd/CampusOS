// @vitest-environment jsdom
import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, afterEach, it, expect, vi } from 'vitest'
const mock = vi.hoisted(() => ({
  list: vi.fn(),
  detail: vi.fn(),
  command: vi.fn(),
  user: { id: '1001' } as any,
  token: 'current',
}))
vi.mock('../src/modules/plugin-center/v5-api', () => ({ pluginV5Api: mock }))
vi.mock('../src/modules/identity/store', () => ({ useUserStore: () => mock }))
vi.mock('../src/modules/identity/session', () => ({ getAccessToken: () => mock.token }))
import View from '../src/modules/plugin-center/pages/PluginV5CenterView.vue'
const coordinate = { publisher: 'campusos', plugin_key: 'pdf-viewer', version: '3.0.0-dev.2' }
const entry = () => ({
  release: {
    ...coordinate,
    manifest: {
      consumes: [
        { capability: 'ui.surface.open', scope: { kind: 'self' }, purpose: 'Open selected resource', required: true },
      ],
    },
  },
  installation: {
    installed: true,
    version: coordinate.version,
    generation: '9007199254740993',
    desired: 'disabled',
    observed: 'pending',
  },
  grant: { approved: true, revision: '9007199254740994' },
  consent: { accepted: false, revision: '9007199254740995' },
  purpose_hash: 'immutable-purpose',
})
const wrappers: ReturnType<typeof mount>[] = []
beforeEach(() => {
  vi.clearAllMocks()
  mock.user = { id: '1001' }
  mock.token = 'current'
  mock.list.mockResolvedValue({ code: 0, data: { items: [entry()], next_cursor: '' } })
  mock.detail.mockResolvedValue({ code: 0, data: entry() })
  mock.command.mockResolvedValue({ code: 0, data: { id: 'operation' } })
})
afterEach(() => wrappers.splice(0).forEach((w) => w.unmount()))
async function opened() {
  const w = mount(View, { global: { stubs: { RouterLink: true } } })
  wrappers.push(w)
  await flushPromises()
  await w.get('[aria-label="可用插件"] button').trigger('click')
  await flushPromises()
  return w
}
it('does not install, consent or enable without explicit action and pins current purpose/revision', async () => {
  const w = await opened()
  expect(w.text()).toContain('Open selected resource')
  expect(mock.command).not.toHaveBeenCalled()
  await w.get('form').trigger('submit')
  expect(mock.command).not.toHaveBeenCalled()
  await w.get('form input[type="checkbox"]').setValue(true)
  await w.get('form').trigger('submit')
  await flushPromises()
  expect(mock.command).toHaveBeenCalledWith(expect.objectContaining(coordinate), {
    kind: 'consent',
    accepted: true,
    expected_revision: '9007199254740995',
    purpose_hash: 'immutable-purpose',
  })
  expect(mock.detail).toHaveBeenCalledTimes(2)
  expect(w.text()).not.toContain('已就绪')
})
it('uses exact generation for explicit lifecycle command and closes a stale conflict without retry', async () => {
  mock.command.mockRejectedValue({ status: 409 })
  const w = await opened()
  await w
    .findAll('button')
    .find((b) => b.text() === '启用')!
    .trigger('click')
  await flushPromises()
  expect(mock.command).toHaveBeenCalledOnce()
  expect(mock.command.mock.calls[0][1]).toEqual({ kind: 'enable', expected_generation: '9007199254740993' })
  expect(w.find('[aria-label="本人插件安装"]').exists()).toBe(false)
})
it('cannot submit cached consent after current user identity changes', async () => {
  const w = await opened()
  await w.get('form input[type="checkbox"]').setValue(true)
  mock.token = 'other'
  mock.user = { id: '1002' }
  await w.get('form').trigger('submit')
  expect(mock.command).not.toHaveBeenCalled()
  expect(w.find('[aria-label="本人插件安装"]').exists()).toBe(false)
  expect(w.text()).toContain('用户身份已变化')
})
