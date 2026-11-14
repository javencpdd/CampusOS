// @vitest-environment jsdom
import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, afterEach, it, expect, vi } from 'vitest'
const mock = vi.hoisted(() => ({
  list: vi.fn(),
  detail: vi.fn(),
  command: vi.fn(),
  router: { push: vi.fn() },
  runtime: { sync: vi.fn(), surfaces: new Map(), navigation: [] as any[] },
  user: { id: '1001' } as any,
  token: 'current',
}))
vi.mock('../src/modules/plugin-center/v5-api', () => ({ pluginV5Api: mock }))
vi.mock('../src/modules/identity/store', () => ({ useUserStore: () => mock }))
vi.mock('../src/modules/identity/session', () => ({ getAccessToken: () => mock.token }))
vi.mock('vue-router', () => ({ useRouter: () => mock.router }))
vi.mock('../src/modules/plugin-runtime/store', () => ({ useUIRuntimeStore: () => mock.runtime }))
import View from '../src/modules/plugin-center/pages/PluginV5CenterView.vue'
const coordinate = { publisher: 'campusos', plugin_key: 'pdf-viewer', version: '3.0.0-dev.2' }
const entry = () => ({
  release: {
    ...coordinate,
    manifest: {
      runtime: 'none',
      ui: [{ audience: 'user', entrypoint: 'dist/user/index.html', digest: 'sha256:' + 'a'.repeat(64) }],
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
  grant: {
    approved: true,
    revision: '9007199254740994',
    consumes: [
      { capability: 'ui.surface.open', scope: { kind: 'self' }, purpose: 'Open selected resource', required: true },
    ],
  },
  consent: {
    accepted: false,
    revision: '9007199254740995',
    purpose_hash: 'immutable-purpose',
    grant_revision: '9007199254740994',
  },
  purpose_hash: 'immutable-purpose',
  site_state: { installed: true, enabled: true, ready: true, observed: 'stopped' },
})
const consentedEntry = () => ({ ...entry(), consent: { ...entry().consent, accepted: true } })
const wrappers: ReturnType<typeof mount>[] = []
beforeEach(() => {
  vi.clearAllMocks()
  mock.user = { id: '1001' }
  mock.token = 'current'
  mock.list.mockResolvedValue({ code: 0, data: { items: [entry()], next_cursor: '' } })
  mock.detail.mockResolvedValue({ code: 0, data: entry() })
  mock.command.mockResolvedValue({ code: 0, data: { id: 'operation' } })
  mock.runtime.sync.mockResolvedValue(undefined)
  mock.runtime.surfaces = new Map()
  mock.runtime.navigation = []
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
  expect(w.text()).toContain('尚未提交')
  mock.detail.mockResolvedValue({ code: 0, data: consentedEntry() })
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
it('uses exact generation and retains the selected plugin after a conflict without retry', async () => {
  mock.detail.mockResolvedValue({ code: 0, data: consentedEntry() })
  mock.command.mockRejectedValue({ status: 409 })
  const w = await opened()
  await w
    .findAll('button')
    .find((b) => b.text() === '启用')!
    .trigger('click')
  await flushPromises()
  expect(mock.command).toHaveBeenCalledOnce()
  expect(mock.command.mock.calls[0][1]).toEqual({ kind: 'enable', expected_generation: '9007199254740993' })
  expect(w.find('[aria-label="本人插件安装"]').exists()).toBe(true)
  expect(w.text()).toContain('未自动重试')
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

it('keeps a stopped staged version inactive until an explicit enable command', async () => {
  const staged = {
    ...consentedEntry(),
    release: { ...entry().release, version: '8.2.0' },
    installation: { ...entry().installation, desired_version: '8.2.0' },
  }
  mock.list.mockResolvedValue({ code: 0, data: { items: [staged], next_cursor: '' } })
  mock.detail.mockResolvedValue({ code: 0, data: staged })
  const w = await opened()
  expect(w.text()).toContain('当前版本仍为')
  expect(mock.command).not.toHaveBeenCalled()
  await w
    .findAll('button')
    .find((b) => b.text() === '启用此版本')!
    .trigger('click')
  await flushPromises()
  expect(mock.command).toHaveBeenCalledOnce()
  expect(mock.command.mock.calls[0][0].version).toBe('8.2.0')
  expect(mock.command.mock.calls[0][1]).toEqual({ kind: 'enable', expected_generation: '9007199254740993' })
})

it.each(['disabled', 'executor'])('explains the site-side %s block without sending a personal enable', async (kind) => {
  const item: any = consentedEntry()
  item.site_state =
    kind === 'disabled'
      ? { installed: true, enabled: false, ready: false, observed: 'stopped' }
      : { installed: true, enabled: true, ready: false, observed: 'pending', error_code: 'plugin.runtime_unavailable' }
  mock.detail.mockResolvedValue({ code: 0, data: item })
  const w = await opened()
  expect(w.get('[aria-label="下一步"]').text()).toContain(
    kind === 'disabled' ? '联系管理员' : '管理员需要配置插件执行器',
  )
  const enable = w.findAll('button').find((b) => b.text() === '启用')!
  expect(enable.attributes('disabled')).toBeDefined()
  await enable.trigger('click')
  expect(mock.command).not.toHaveBeenCalled()
})

it('requires fresh consent when the saved consent belongs to a previous grant revision', async () => {
  const item = consentedEntry()
  item.consent.grant_revision = '9007199254740990'
  mock.detail.mockResolvedValue({ code: 0, data: item })
  const w = await opened()
  expect(w.text()).toContain('需要重新确认用途')
  expect(
    w
      .findAll('button')
      .find((b) => b.text() === '启用')!
      .attributes('disabled'),
  ).toBeDefined()
  await w.get('form input[type="checkbox"]').setValue(true)
  expect(w.text()).toContain('尚未提交')
  expect(mock.command).not.toHaveBeenCalled()
})

it('does not treat incomplete consent metadata as a saved current consent', async () => {
  const item: any = consentedEntry()
  delete item.purpose_hash
  delete item.consent.purpose_hash
  mock.detail.mockResolvedValue({ code: 0, data: item })
  const w = await opened()
  expect(w.text()).toContain('需要重新确认用途')
  expect(
    w
      .findAll('button')
      .find((b) => b.text() === '启用')!
      .attributes('disabled'),
  ).toBeDefined()
  expect(mock.command).not.toHaveBeenCalled()
})

it('resets the explicit purge choice after a successful uninstall instead of carrying it into later installs', async () => {
  mock.detail.mockResolvedValue({ code: 0, data: consentedEntry() })
  const w = await opened()
  const dangerous = w.findAll('details').find((d) => d.find('summary').text() === '撤回同意与卸载')!
  await dangerous.get('input[type=checkbox]').setValue(true)
  mock.detail.mockResolvedValue({
    code: 0,
    data: { ...consentedEntry(), installation: { ...entry().installation, installed: false } },
  })
  await dangerous
    .findAll('button')
    .find((b) => b.text() === '卸载')!
    .trigger('click')
  await flushPromises()
  expect(mock.command.mock.calls[0][1]).toEqual(expect.objectContaining({ kind: 'uninstall', purge_data: true }))
  mock.detail.mockResolvedValue({ code: 0, data: consentedEntry() })
  await w.get('[aria-label="可用插件"] button').trigger('click')
  await flushPromises()
  const fresh = w.findAll('details').find((d) => d.find('summary').text() === '撤回同意与卸载')!
  expect((fresh.get('input[type=checkbox]').element as HTMLInputElement).checked).toBe(false)
})

it('opens only the current host-registered navigation after explicit enable has completed', async () => {
  const item = {
    ...consentedEntry(),
    installation: { ...entry().installation, desired: 'enabled', observed: 'stopped' },
  }
  mock.detail.mockResolvedValue({ code: 0, data: item })
  mock.runtime.surfaces.set('campusos.pdf-viewer.ui', {
    id: 'campusos.pdf-viewer.ui',
    plugin: 'campusos.pdf-viewer',
    plugin_version: coordinate.version,
    type: 'isolated-plugin',
  })
  mock.runtime.navigation = [
    {
      plugin: 'campusos.pdf-viewer',
      route_id: 'campusos.pdf-viewer.ui.route',
      path: '/extensions/campusos.pdf-viewer/ui',
    },
  ]
  const w = await opened()
  expect(w.get('[aria-label="插件当前状态"]').text()).toContain('已启用')
  await w
    .findAll('button')
    .find((b) => b.text() === '打开插件')!
    .trigger('click')
  await flushPromises()
  expect(mock.router.push).toHaveBeenCalledWith('/extensions/campusos.pdf-viewer/ui')
  expect(mock.command).not.toHaveBeenCalled()
})

it('does not navigate after the user identity changes during plugin UI synchronization', async () => {
  mock.detail.mockResolvedValue({
    code: 0,
    data: { ...consentedEntry(), installation: { ...entry().installation, desired: 'enabled', observed: 'stopped' } },
  })
  let finish!: () => void
  mock.runtime.sync.mockImplementationOnce(
    () =>
      new Promise<void>((resolve) => {
        finish = resolve
      }),
  )
  const w = await opened()
  await w
    .findAll('button')
    .find((b) => b.text() === '打开插件')!
    .trigger('click')
  await flushPromises()
  mock.token = 'another-user-session'
  finish()
  await flushPromises()
  expect(mock.router.push).not.toHaveBeenCalled()
  expect(w.find('[aria-label="本人插件安装"]').exists()).toBe(false)
})

it('automatically reads a pending start until ready and then stops without writing', async () => {
  vi.useFakeTimers()
  let w: ReturnType<typeof mount> | undefined
  try {
    mock.detail.mockResolvedValue({
      code: 0,
      data: { ...consentedEntry(), installation: { ...entry().installation, desired: 'enabled', observed: 'pending' } },
    })
    w = await opened()
    mock.detail.mockResolvedValue({
      code: 0,
      data: { ...consentedEntry(), installation: { ...entry().installation, desired: 'enabled', observed: 'stopped' } },
    })
    await vi.advanceTimersByTimeAsync(1300)
    await flushPromises()
    expect(w.get('[aria-label="插件当前状态"]').text()).toContain('已启用')
    expect(mock.detail).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(5000)
    expect(mock.detail).toHaveBeenCalledTimes(2)
    expect(mock.command).not.toHaveBeenCalled()
  } finally {
    w?.unmount()
    vi.useRealTimers()
  }
})

it('cancels pending status reads when the user identity changes', async () => {
  vi.useFakeTimers()
  let w: ReturnType<typeof mount> | undefined
  try {
    mock.detail.mockResolvedValue({
      code: 0,
      data: { ...consentedEntry(), installation: { ...entry().installation, desired: 'enabled', observed: 'pending' } },
    })
    w = await opened()
    mock.token = 'changed-owner'
    await vi.advanceTimersByTimeAsync(1400)
    expect(mock.detail).toHaveBeenCalledOnce()
    expect(w.find('[aria-label="本人插件安装"]').exists()).toBe(false)
  } finally {
    w?.unmount()
    vi.useRealTimers()
  }
})

it('keeps the selected plugin after a failed personal command and exposes technical details only on demand', async () => {
  mock.detail.mockResolvedValue({ code: 0, data: consentedEntry() })
  mock.command.mockRejectedValue({ status: 403, message: '用途或授权已变化' })
  const w = await opened()
  expect(w.get('details[aria-label="技术详情"]').attributes('open')).toBeUndefined()
  await w
    .findAll('button')
    .find((b) => b.text() === '启用')!
    .trigger('click')
  await flushPromises()
  expect(w.find('[aria-label="本人插件安装"]').exists()).toBe(true)
  expect(w.text()).toContain('未自动重试')
  expect(mock.command).toHaveBeenCalledOnce()
})
