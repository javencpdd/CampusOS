// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, reactive } from 'vue'
const fixture = vi.hoisted(() => ({
  request: vi.fn().mockResolvedValue({ values_json: '{}' }),
  push: vi.fn().mockResolvedValue(undefined),
  route: null as any,
  surfaces: null as any,
}))
vi.mock('../src/campus-ui/DeclarativeRenderer.vue', () => ({ default: { template: '<div />' } }))
vi.mock('vue-router', () => ({
  useRouter: () => ({ push: fixture.push }),
  useRoute: () => fixture.route,
}))
vi.mock('../src/modules/plugin-runtime/store', () => ({
  useUIRuntimeStore: () => ({
    surface: (id: string) => fixture.surfaces[id],
  }),
}))
vi.mock('../src/campus-ui/trustedModules', () => ({
  trustedModules: { 'core.appearance': { template: '<div data-appearance>界面风格</div>' } },
}))
const initialSurface = () => ({
  id: 'campusos.example.ui',
  plugin: 'campusos.example',
  plugin_version: '1.0.0',
  renderer: 'isolated-iframe',
  frame: {
    src: 'https://lease.plugins.test/ui/lease/dist/user/index.html',
    origin: 'https://lease.plugins.test',
    audience: 'user',
  },
  lifecycle: { desired_enabled: true, frontend_state: 'loaded' },
})
vi.mock('../src/campus-ui/useSurfaceResourceContext', () => ({ useSurfaceResourceContext: () => ({}) }))
vi.mock('../src/campus-ui/isolatedPluginHost', () => ({
  isolatedPluginFrameURL: (src: string) => src,
  handleIsolatedPluginRequest: fixture.request,
}))
import Page from '../src/campus-ui/PluginSurfacePage.vue'
const Frame = defineComponent({ props: ['identity', 'src', 'request'], template: '<div data-frame />' })
const wrappers: ReturnType<typeof mount>[] = []
function opened() {
  const wrapper = mount(Page, {
    global: {
      stubs: {
        IsolatedPluginFrame: Frame,
        DeclarativeRenderer: true,
        ElResult: { props: ['title'], template: '<div data-result>{{ title }}</div>' },
      },
    },
  })
  wrappers.push(wrapper)
  return wrapper
}
beforeEach(() => {
  vi.clearAllMocks()
  fixture.route = reactive({ meta: { surfaceId: 'campusos.example.ui' }, query: {} })
  fixture.surfaces = reactive({
    'campusos.example.ui': initialSurface(),
    'module.appearance.page.main': {
      id: 'module.appearance.page.main',
      plugin: 'appearance',
      plugin_version: '0.11.0',
      renderer: 'trusted-module',
      module_id: 'core.appearance',
      lifecycle: { desired_enabled: true, frontend_state: 'loaded' },
    },
  })
})
afterEach(() => wrappers.splice(0).forEach((wrapper) => wrapper.unmount()))
it('opens a generic user UI without requiring a business resource and leaves resource authorization to its broker', async () => {
  const wrapper = opened()
  const frame = wrapper.getComponent(Frame)
  expect(frame.props('src')).toBe('https://lease.plugins.test/ui/lease/dist/user/index.html')
  const request = { version: 'campusos.bridge/v1', request_id: 'one', method: 'config.read', params: {} }
  let portActive = true
  await frame.props('request')(request, () => portActive)
  expect(fixture.request).toHaveBeenCalledWith(
    expect.objectContaining({ plugin: 'campusos.example' }),
    '',
    request,
    expect.any(Function),
    expect.any(Function),
  )
  const [, , , open, active] = fixture.request.mock.calls.at(-1)!
  expect(active()).toBe(true)
  portActive = false
  expect(active()).toBe(false)
  await expect(open('closed-port-import')).rejects.toThrow()
  portActive = true
  wrapper.unmount()
  expect(active()).toBe(false)
  await expect(open('stale-import')).rejects.toThrow()
  expect(fixture.push).not.toHaveBeenCalled()
})

it('navigates between an isolated plugin and appearance without treating their different versions as an upgrade', async () => {
  const wrapper = opened()
  expect(wrapper.find('[data-frame]').exists()).toBe(true)
  fixture.route.meta.surfaceId = 'module.appearance.page.main'
  await flushPromises()
  expect(wrapper.find('[data-appearance]').exists()).toBe(true)
  expect(wrapper.find('[data-result]').exists()).toBe(false)
  fixture.route.meta.surfaceId = 'campusos.example.ui'
  await flushPromises()
  expect(wrapper.find('[data-frame]').exists()).toBe(true)
  fixture.route.meta.surfaceId = 'module.appearance.page.main'
  await flushPromises()
  expect(wrapper.find('[data-appearance]').exists()).toBe(true)
})

it('still blocks a version change in the same page until a new invocation opens', async () => {
  const wrapper = opened()
  fixture.surfaces['campusos.example.ui'].plugin_version = '1.0.1'
  await flushPromises()
  expect(wrapper.find('[data-frame]').exists()).toBe(false)
  expect(wrapper.find('[data-result]').exists()).toBe(true)
  fixture.surfaces['campusos.example.ui'].plugin_version = '1.0.2'
  await flushPromises()
  expect(wrapper.find('[data-frame]').exists()).toBe(false)
  fixture.route.query.invocation = 'new-resource-invocation'
  await flushPromises()
  expect(wrapper.find('[data-frame]').exists()).toBe(true)
})

it('resets a prior version block when navigating to another page even if its version number is identical', async () => {
  const wrapper = opened()
  fixture.surfaces['campusos.example.ui'].plugin_version = '1.0.1'
  await flushPromises()
  expect(wrapper.find('[data-frame]').exists()).toBe(false)
  fixture.surfaces['module.appearance.page.main'].plugin_version = '1.0.1'
  fixture.route.meta.surfaceId = 'module.appearance.page.main'
  await flushPromises()
  expect(wrapper.find('[data-appearance]').exists()).toBe(true)
})

it('keeps missing and unsupported contexts blocked instead of bypassing renderer checks', async () => {
  const wrapper = opened()
  delete fixture.surfaces['campusos.example.ui']
  await flushPromises()
  fixture.surfaces['campusos.example.ui'] = initialSurface()
  await flushPromises()
  expect(wrapper.find('[data-frame]').exists()).toBe(false)
  fixture.route.meta.surfaceId = 'module.appearance.page.main'
  fixture.surfaces['module.appearance.page.main'].module_id = 'unknown.module'
  await flushPromises()
  expect(wrapper.find('[data-result]').exists()).toBe(true)
  fixture.surfaces['module.appearance.page.main'].module_id = 'core.appearance'
  fixture.surfaces['module.appearance.page.main'].lifecycle.desired_enabled = false
  await flushPromises()
  expect(wrapper.find('[data-appearance]').exists()).toBe(false)
})
