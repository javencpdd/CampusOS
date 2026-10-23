// @vitest-environment jsdom
import { expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
const fixture = vi.hoisted(() => ({ request: vi.fn().mockResolvedValue({ values_json: '{}' }) }))
vi.mock('../src/campus-ui/DeclarativeRenderer.vue', () => ({ default: { template: '<div />' } }))
vi.mock('vue-router', () => ({ useRoute: () => ({ meta: { surfaceId: 'campusos.example.ui' }, query: {} }) }))
vi.mock('../src/modules/plugin-runtime/store', () => ({
  useUIRuntimeStore: () => ({
    surface: () => ({
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
    }),
  }),
}))
vi.mock('../src/campus-ui/useSurfaceResourceContext', () => ({ useSurfaceResourceContext: () => ({}) }))
vi.mock('../src/campus-ui/isolatedPluginHost', () => ({
  isolatedPluginFrameURL: (src: string) => src,
  handleIsolatedPluginRequest: fixture.request,
}))
import Page from '../src/campus-ui/PluginSurfacePage.vue'
it('opens a generic user UI without requiring a business resource and leaves resource authorization to its broker', async () => {
  const Frame = defineComponent({ props: ['identity', 'src', 'request'], template: '<div data-frame />' })
  const wrapper = mount(Page, { global: { stubs: { IsolatedPluginFrame: Frame, DeclarativeRenderer: true } } })
  const frame = wrapper.getComponent(Frame)
  expect(frame.props('src')).toBe('https://lease.plugins.test/ui/lease/dist/user/index.html')
  const request = { version: 'campusos.bridge/v1', request_id: 'one', method: 'config.read', params: {} }
  await frame.props('request')(request)
  expect(fixture.request).toHaveBeenCalledWith(expect.objectContaining({ plugin: 'campusos.example' }), '', request)
  wrapper.unmount()
})
