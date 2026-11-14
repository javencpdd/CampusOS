// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
const fixture = vi.hoisted(() => ({ token: 'owned-session', channels: [] as any[] }))
vi.mock('../src/modules/identity/session', () => ({ getAccessToken: () => fixture.token }))
import Frame from '../src/campus-ui/IsolatedPluginFrame.vue'
import { PLUGIN_BRIDGE_VERSION } from '../src/campus-ui/isolatedPluginBridge'

const identity = {
  pluginKey: 'campusos.rss-publisher',
  pluginVersion: '1.0.0',
  surfaceID: 'ui',
  audience: 'user' as const,
  origin: 'https://lease.plugins.test',
}
const src = identity.origin + '/ui/owned/dist/user/index.html'
const wrappers: ReturnType<typeof mount>[] = []
function opened() {
  const wrapper = mount(Frame, {
    attachTo: document.body,
    props: { identity, src, title: '插件界面', request: vi.fn().mockResolvedValue({}) },
    global: {
      stubs: {
        ElAlert: { props: ['title'], template: '<p role="alert">{{ title }}</p>' },
        ElButton: { template: '<button><slot /></button>' },
      },
    },
  })
  wrappers.push(wrapper)
  return wrapper
}
function ready(wrapper: ReturnType<typeof mount>, origin = identity.origin) {
  const iframe = wrapper.get('iframe').element as HTMLIFrameElement
  const post = vi.spyOn(iframe.contentWindow!, 'postMessage').mockImplementation(() => {})
  window.dispatchEvent(
    new MessageEvent('message', {
      source: iframe.contentWindow,
      origin,
      data: {
        type: 'campusos.bridge.ready',
        version: PLUGIN_BRIDGE_VERSION,
        plugin_key: identity.pluginKey,
        plugin_version: identity.pluginVersion,
        surface_id: identity.surfaceID,
        audience: identity.audience,
      },
    }),
  )
  return post.mock.calls[0]?.[0]?.challenge
}
beforeEach(() => {
  vi.useFakeTimers()
  fixture.token = 'owned-session'
  fixture.channels = []
  const port = () => ({ start: vi.fn(), close: vi.fn(), postMessage: vi.fn(), onmessage: null })
  vi.stubGlobal(
    'MessageChannel',
    class {
      port1 = port()
      port2 = port()
      constructor() {
        fixture.channels.push(this)
      }
    },
  )
})
afterEach(() => {
  wrappers.splice(0).forEach((wrapper) => wrapper.unmount())
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  vi.useRealTimers()
})
it('ends a stalled iframe navigation instead of leaving the page loading forever', async () => {
  const wrapper = opened()
  await vi.advanceTimersByTimeAsync(10000)
  expect(wrapper.text()).toContain('加载超时')
  expect(wrapper.text()).toContain(identity.origin)
  expect(wrapper.find('iframe').exists()).toBe(false)
  expect(wrapper.get('button').text()).toBe('重新加载插件界面')
})
it('also times out when the document loads but the plugin never announces readiness', async () => {
  const wrapper = opened()
  await wrapper.get('iframe').trigger('load')
  expect(wrapper.text()).toContain('正在建立')
  await vi.advanceTimersByTimeAsync(10000)
  expect(wrapper.text()).toContain('加载超时')
  expect(wrapper.get('button').exists()).toBe(true)
})
it('shows a recoverable failure when the iframe raises a network error', async () => {
  const wrapper = opened()
  await wrapper.get('iframe').trigger('error')
  expect(wrapper.text()).toContain('未能加载')
  expect(wrapper.find('iframe').exists()).toBe(false)
  expect(wrapper.get('button').exists()).toBe(true)
})
it('retries with a fresh frame and still requires a matching secure handshake', async () => {
  const wrapper = opened()
  const old = wrapper.get('iframe').element
  await vi.advanceTimersByTimeAsync(10000)
  await wrapper.get('button').trigger('click')
  expect(wrapper.get('iframe').element).not.toBe(old)
  expect(wrapper.text()).toContain('正在加载')
  const challenge = ready(wrapper)
  expect(challenge).toBeTruthy()
  fixture.channels[0].port1.onmessage({
    data: { type: 'campusos.bridge.handshake', version: PLUGIN_BRIDGE_VERSION, challenge },
  })
  await flushPromises()
  expect(wrapper.text()).not.toContain('正在加载')
  await vi.advanceTimersByTimeAsync(10000)
  expect(wrapper.find('button').exists()).toBe(false)
  expect(wrapper.find('iframe').exists()).toBe(true)
})
it('keeps forged handshakes blocked and does not offer a security bypass', async () => {
  const wrapper = opened()
  ready(wrapper)
  fixture.channels[0].port1.onmessage({
    data: { type: 'campusos.bridge.handshake', version: PLUGIN_BRIDGE_VERSION, challenge: 'wrong' },
  })
  await flushPromises()
  expect(wrapper.text()).toContain('已阻止加载')
  expect(wrapper.find('button').exists()).toBe(false)
  expect(fixture.channels[0].port1.close).toHaveBeenCalled()
})
it('does not accept a readiness message from a different origin', async () => {
  const wrapper = opened()
  ready(wrapper, 'https://untrusted.plugins.test')
  expect(fixture.channels).toHaveLength(0)
  await vi.advanceTimersByTimeAsync(10000)
  expect(wrapper.text()).toContain('加载超时')
})
it('does not let a retry reauthorize a changed user session', async () => {
  const wrapper = opened()
  await vi.advanceTimersByTimeAsync(10000)
  fixture.token = 'different-session'
  await wrapper.get('button').trigger('click')
  expect(wrapper.text()).toContain('已阻止加载')
  expect(wrapper.find('iframe').exists()).toBe(false)
})
it('cancels the old deadline when a new authorized source replaces the frame', async () => {
  const wrapper = opened()
  await vi.advanceTimersByTimeAsync(9000)
  await wrapper.setProps({ src: identity.origin + '/ui/replacement/dist/user/index.html' })
  await vi.advanceTimersByTimeAsync(1000)
  expect(wrapper.text()).toContain('正在加载')
  await vi.advanceTimersByTimeAsync(9000)
  expect(wrapper.text()).toContain('加载超时')
})
it('closes the connection and removes timers when the page unmounts', async () => {
  const wrapper = opened()
  ready(wrapper)
  wrapper.unmount()
  expect(fixture.channels[0].port1.close).toHaveBeenCalled()
  expect(vi.getTimerCount()).toBe(0)
})
