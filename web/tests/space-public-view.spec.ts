// @vitest-environment jsdom
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { reactive } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import PublicSpaceView from '../src/modules/space/pages/PublicSpaceView.vue'
import { spaceApi } from '../src/modules/space/api'
import { scheduleApi } from '../src/modules/schedule/api'

const state = vi.hoisted(() => ({ route: null as any, user: null as any }))
vi.mock('vue-router', () => ({ useRoute: () => state.route }))
vi.mock('@/modules/identity/store', () => ({ useUserStore: () => state.user }))
vi.mock('@/modules/space/api', () => ({
  spaceApi: { publicByUsername: vi.fn(), publicContentsByUsername: vi.fn(), myContents: vi.fn() },
}))
vi.mock('@/modules/schedule/api', () => ({ scheduleApi: { me: vi.fn() } }))
vi.mock('@/components/StyleEffectLayer.vue', () => ({
  default: { name: 'StyleEffectLayer', props: ['resolveQuery', 'script', 'capabilities'], template: '<aside />' },
}))
const deferred = <T>() => {
  let resolve!: (value: T) => void
  let reject!: (error: unknown) => void
  const promise = new Promise<T>((a, b) => {
    resolve = a
    reject = b
  })
  return { promise, resolve, reject }
}
const profile = (name: string, id = name) => ({
  data: {
    owner: { id, username: name, nickname: name },
    space: {
      title: `${name} profile`,
      layout: 'blog',
      sync_enabled: true,
      style_manifest: {
        custom_html_enabled: true,
        custom_html: '<section>appearance</section>',
        custom_css: '.public-space[data-campusos-space] { color: red; }',
      },
    },
  },
})
const rows = (title: string) => ({ data: { items: [{ id: title, thread_id: title, title, excerpt: title }] } })
const wrappers: VueWrapper[] = []
const start = () => {
  const slot = { template: '<div><slot /></div>' }
  const wrapper = mount(PublicSpaceView, {
    global: {
      stubs: {
        ElTag: slot,
        ElAvatar: slot,
        ElEmpty: true,
        RouterLink: slot,
        ElAlert: { props: ['title'], template: '<div>{{ title }}</div>' },
      },
      directives: { loading: () => {} },
    },
  })
  wrappers.push(wrapper)
  return wrapper
}
const query = (wrapper: VueWrapper) =>
  wrapper.findComponent({ name: 'StyleEffectLayer' }).props('resolveQuery') as (
    method: string,
    params: Record<string, unknown>,
  ) => Promise<any>
beforeEach(() => {
  vi.resetAllMocks()
  state.route = reactive({ params: { username: 'alice' } })
  state.user = reactive({ user: { id: 'visitor' }, isLoggedIn: true })
  vi.mocked(spaceApi.publicByUsername).mockImplementation(async (name) => profile(name) as any)
  vi.mocked(spaceApi.publicContentsByUsername).mockImplementation(async (name) => rows(`${name} public`) as any)
  vi.mocked(spaceApi.myContents).mockResolvedValue(rows('alice private') as any)
})
afterEach(() => {
  wrappers.splice(0).forEach((wrapper) => wrapper.unmount())
})

describe('current public space context', () => {
  it('uses the captured owner identity and current public projection', async () => {
    const wrapper = start()
    await flushPromises()
    expect(wrapper.text()).toContain('alice public')
    expect(spaceApi.myContents).not.toHaveBeenCalled()
    expect((await query(wrapper)('space.posts.read', { limit: 1 })).items[0].title).toBe('alice public')
    state.user.user.id = 'alice'
    await flushPromises()
    expect(wrapper.text()).toContain('alice private')
    expect(wrapper.text()).toContain('这是你的主页预览')
  })

  it('discards late profiles and errors after the route changes', async () => {
    const first = deferred<any>()
    vi.mocked(spaceApi.publicByUsername).mockImplementationOnce(() => first.promise)
    const wrapper = start()
    state.route.params.username = 'bob'
    await flushPromises()
    first.reject({ msg: 'stale alice error' })
    await flushPromises()
    expect(wrapper.text()).toContain('bob profile')
    expect(wrapper.text()).not.toContain('stale alice error')
    expect(spaceApi.publicContentsByUsername).toHaveBeenCalledTimes(1)
    expect(spaceApi.publicContentsByUsername).toHaveBeenCalledWith('bob', { page: 1, page_size: 20 })
  })

  it('rejects a pending private response after a user switch', async () => {
    state.user.user.id = 'alice'
    const privateRows = deferred<any>()
    vi.mocked(spaceApi.myContents).mockReturnValueOnce(privateRows.promise)
    const wrapper = start()
    await flushPromises()
    state.user.user.id = 'bob'
    await flushPromises()
    privateRows.resolve(rows('ALICE PRIVATE BODY'))
    await flushPromises()
    expect(wrapper.text()).toContain('alice public')
    expect(wrapper.text()).not.toContain('ALICE PRIVATE BODY')
    expect((await query(wrapper)('space.posts.read', {})).items[0].title).toBe('alice public')
  })

  it('clears cached content, style and bridge data immediately, including failed reloads', async () => {
    const wrapper = start()
    await flushPromises()
    expect(document.head.querySelector('[data-campusos-style-pack]')).not.toBeNull()
    const pending = deferred<any>()
    vi.mocked(spaceApi.publicByUsername).mockReturnValueOnce(pending.promise)
    state.route.params.username = 'bob'
    await expect(query(wrapper)('space.profile.read', {})).rejects.toThrow('主页数据已更新')
    await expect(query(wrapper)('space.posts.read', {})).rejects.toThrow('主页数据已更新')
    await flushPromises()
    expect(wrapper.text()).not.toContain('alice public')
    expect(document.head.querySelector('[data-campusos-style-pack]')).toBeNull()
    pending.reject({ msg: '当前主页不可访问' })
    await flushPromises()
    expect(wrapper.text()).toContain('当前主页不可访问')
    await expect(query(wrapper)('space.posts.read', {})).rejects.toThrow('主页数据已更新')
  })

  it('rechecks owner and route after schedule I/O before returning to the style bridge', async () => {
    state.user.user.id = 'alice'
    const wrapper = start()
    await flushPromises()
    const pending = deferred<any>()
    vi.mocked(scheduleApi.me).mockReturnValueOnce(pending.promise)
    const result = query(wrapper)('schedule.me.read', {})
    const assertion = expect(result).rejects.toThrow('主页数据已更新')
    state.user.user.id = 'bob'
    await flushPromises()
    pending.resolve({ data: { PRIVATE_SCHEDULE: true } })
    await assertion
    await expect(query(wrapper)('schedule.me.read', {})).rejects.toThrow('课表只允许')
  })
})
