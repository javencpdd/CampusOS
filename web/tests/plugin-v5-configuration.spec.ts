// @vitest-environment jsdom
import { mount, flushPromises } from '@vue/test-utils'
import { expect, it, beforeEach, afterEach, vi } from 'vitest'
import { parseExactJSON, stringifyExactJSON } from '../../sdk/typescript/src/exact-json'
const mock = vi.hoisted(() => ({
  list: vi.fn(),
  detail: vi.fn(),
  options: vi.fn(),
  configure: vi.fn(),
  configureSecrets: vi.fn(),
  user: { id: '1001' } as any,
  token: 'current',
}))
vi.mock('../src/modules/plugin-center/v5-api', () => ({ pluginV5Api: mock }))
vi.mock('../src/modules/identity/store', () => ({ useUserStore: () => mock }))
vi.mock('../src/modules/identity/session', () => ({ getAccessToken: () => mock.token }))
import View from '../src/modules/plugin-center/pages/PluginV5ConfigView.vue'
function entry() {
  return parseExactJSON(
    `{"release":{"publisher":"campusos","plugin_key":"example","version":"1.0.0","manifest":{"api_version":"campusos.plugin/v5","data":{"config_scope":"user"},"configuration":{"contract":"campusos.plugin-config/v1","version":"v1","schema":{"type":"object","additionalProperties":false,"properties":{"count":{"type":"integer","title":"数量","minimum":9007199254740992,"maximum":9007199254740994}},"required":["count"]},"defaults":{},"selectors":[],"secret_names":["api_key"]}}},"installation":{"installed":true,"observed":"pending","desired":"disabled"},"configuration":{"owner_user_id":"1001","revision":"9007199254740993","values":{"count":9007199254740993},"secret_refs":{"api_key":"secret-ref:current"}}}`,
  )
}
const wrappers: ReturnType<typeof mount>[] = []
beforeEach(() => {
  vi.clearAllMocks()
  mock.token = 'current'
  mock.user = { id: '1001' }
  mock.list.mockResolvedValue({ code: 0, data: { items: [entry()], next_cursor: '' } })
  mock.detail.mockResolvedValue({ code: 0, data: entry() })
  mock.options.mockResolvedValue({
    code: 0,
    data: {
      profiles: [],
      public_collections: [],
      secret_refs: {
        api_key: [
          { id: 'secret-ref:current', title: '当前受管密钥' },
          { id: 'secret-ref:replacement', title: '替换密钥' },
        ],
      },
    },
  })
  mock.configure.mockResolvedValue({ code: 0, data: { id: 'operation' } })
  mock.configureSecrets.mockResolvedValue({ code: 0, data: { id: 'operation' } })
})
afterEach(() => wrappers.splice(0).forEach((w) => w.unmount()))
async function opened() {
  const w = mount(View)
  wrappers.push(w)
  await flushPromises()
  await w.get('[aria-label="插件版本列表"] button').trigger('click')
  await flushPromises()
  return w
}
it('keeps exact user numbers, ownership and CAS while ordinary save carries no Secret refs', async () => {
  const w = await opened()
  expect((w.get('[aria-label="数量"]').element as HTMLInputElement).value).toBe('9007199254740993')
  await w.get('[aria-label="数量"]').setValue('9007199254740994')
  await w.get('[aria-label="普通配置"]').trigger('submit')
  await flushPromises()
  expect(mock.configure).toHaveBeenCalledOnce()
  expect(mock.configure.mock.calls[0][1]).toBe('9007199254740993')
  expect(stringifyExactJSON(mock.configure.mock.calls[0][2])).toBe('{"count":9007199254740994}')
  expect(mock.configureSecrets).not.toHaveBeenCalled()
  expect(w.find('input[type="password"]').exists()).toBe(false)
})
it('updates only the Secret binding and preserves normal values in a separate request', async () => {
  const w = await opened()
  await w.get('[aria-label="密钥引用 api_key"]').setValue('secret-ref:replacement')
  await w.get('[aria-label="密钥引用"]').trigger('submit')
  await flushPromises()
  expect(mock.configureSecrets).toHaveBeenCalledWith(
    expect.objectContaining({ publisher: 'campusos', plugin_key: 'example', version: '1.0.0' }),
    '9007199254740993',
    { api_key: 'secret-ref:replacement' },
  )
  expect(mock.configure).not.toHaveBeenCalled()
  expect(w.text()).toContain('密钥引用已更新')
})
it('rejects a response belonging to another owner and a late response after identity changes', async () => {
  const e = entry()
  e.configuration.owner_user_id = '1002'
  mock.detail.mockResolvedValueOnce({ code: 0, data: e })
  const w = await opened()
  expect(w.find('form').exists()).toBe(false)
  expect(w.text()).toContain('不属于当前用户')
  let release!: (r: unknown) => void
  mock.detail.mockImplementationOnce(
    () =>
      new Promise((r) => {
        release = r
      }),
  )
  await w.get('[aria-label="插件版本列表"] button').trigger('click')
  mock.token = 'other'
  mock.user = { id: '1002' }
  release({ code: 0, data: entry() })
  await flushPromises()
  expect(w.find('form').exists()).toBe(false)
  expect(w.text()).toContain('身份已变化')
  expect(mock.configure).not.toHaveBeenCalled()
})
it('rejects exact numeric overflow before any network write', async () => {
  const w = await opened()
  await w.get('[aria-label="数量"]').setValue('9007199254740995')
  await w.get('[aria-label="普通配置"]').trigger('submit')
  expect(mock.configure).not.toHaveBeenCalled()
  expect(w.text()).toContain('超过最大值')
})
