// @vitest-environment jsdom
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ElMessage } from 'element-plus'
import ThreadDetailView from '../src/modules/community/pages/ThreadDetailView.vue'
import { moderationApi, postApi, threadApi } from '../src/modules/community/api'

vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { id: '190101' }, query: {} }),
  useRouter: () => ({ push: vi.fn() }),
}))
vi.mock('@/modules/identity/store', () => ({
  useUserStore: () => ({ isLoggedIn: true, user: { id: '190104' } }),
}))
vi.mock('element-plus', () => ({
  ElMessage: { error: vi.fn(), success: vi.fn(), warning: vi.fn() },
}))
vi.mock('@/campus-ui/surfaceHost', () => ({ openPluginSurface: vi.fn() }))
vi.mock('@/campus-ui/pluginConsent', () => ({ ensurePluginCapabilityConsent: vi.fn() }))
vi.mock('@/modules/richtext/api', () => ({
  richTextApi: {
    getMine: vi.fn().mockRejectedValue(new Error('No article')),
    getPublished: vi.fn().mockResolvedValue({ code: 404 }),
  },
}))
const stubs = {
  RouterLink: { template: '<a><slot /></a>' },
  ElCard: { template: '<section><header><slot name="header" /></header><slot /></section>' },
  ElTag: { template: '<span><slot /></span>' },
  ElTooltip: { template: '<span><slot /></span>' },
  ElButton: { emits: ['click'], template: '<button @click="$emit(\'click\')"><slot /></button>' },
  ElIcon: { template: '<i><slot /></i>' },
  ElAvatar: true,
  ElInput: true,
  ElEmpty: true,
  ElPagination: true,
  ElPopconfirm: true,
  ElAlert: true,
  ElDropdown: true,
  ElDropdownItem: true,
  ElDropdownMenu: true,
  ThreadTaxonomy: true,
  CheckedImage: true,
  CheckedHTML: true,
}
afterEach(() => {
  vi.restoreAllMocks()
  vi.clearAllMocks()
})

async function renderThread(pinned = false, locked = false, replies: any[] = []) {
  vi.spyOn(threadApi, 'getMine').mockResolvedValue({
    code: 0,
    data: {
      id: '190101',
      title: 'Existing thread title',
      content: 'Existing thread body',
      content_format: 'plain',
      author_id: '190102',
      author_name: 'Existing author',
      category_id: '190103',
      thread_type: 'discussion',
      publication_status: 'published',
      moderation_status: 'clear',
      deletion_status: 'active',
      is_pinned: pinned,
      is_locked: locked,
      tags: ['existing tag'],
      reply_count: 7,
    },
  } as never)
  const page = { code: 0, data: { items: replies, pagination: { total: replies.length } } }
  vi.spyOn(postApi, 'list').mockResolvedValue(page as never)
  vi.spyOn(postApi, 'listMine').mockResolvedValue(page as never)
  vi.spyOn(moderationApi, 'preview').mockImplementation(
    async (_id, ids) =>
      ({
        code: 0,
        data: {
          thread_id: '190101',
          pin: true,
          lock: true,
          posts: ids.map((id) => ({ id, delete: id === 'reply.one' })),
        },
      }) as never,
  )
  const wrapper = mount(ThreadDetailView, { global: { stubs, directives: { loading: {} } } })
  await flushPromises()
  return wrapper
}

describe('current User thread flag acknowledgement', () => {
  for (const operation of ['pin', 'unpin', 'lock', 'unlock'] as const) {
    it('applies ' + operation + ' flags while retaining the loaded thread', async () => {
      const wrapper = await renderThread(operation === 'unpin', operation === 'unlock')
      const api = vi.spyOn(moderationApi, operation).mockResolvedValue({
        code: 0,
        data: { id: '190101', is_pinned: operation === 'pin', is_locked: operation === 'lock' },
      } as never)
      const label = { pin: '置顶主题', unpin: '取消置顶', lock: '锁定主题', unlock: '解锁主题' }[operation]
      await wrapper.get('button[aria-label="' + label + '"]').trigger('click')
      await flushPromises()
      expect(api).toHaveBeenCalledWith('190101')
      expect(api).toHaveBeenCalledOnce()
      expect(wrapper.text()).toContain('Existing thread title')
      expect(wrapper.text()).toContain('Existing thread body')
      expect(wrapper.text()).toContain('Existing author')
      expect(ElMessage.success).toHaveBeenCalledOnce()
      expect(threadApi.getMine).toHaveBeenCalledOnce()
      wrapper.unmount()
    })
  }

  for (const data of [
    { id: 'other-thread', is_pinned: true, is_locked: false },
    { id: '190101', is_pinned: 'true', is_locked: false },
  ]) {
    it('rejects a mismatched or malformed acknowledgement without replacing content', async () => {
      const wrapper = await renderThread()
      vi.spyOn(moderationApi, 'pin').mockResolvedValue({ code: 0, data } as never)
      await wrapper.get('button[aria-label="置顶主题"]').trigger('click')
      await flushPromises()
      expect(wrapper.text()).toContain('Existing thread title')
      expect(wrapper.text()).toContain('Existing thread body')
      expect(wrapper.find('button[aria-label="置顶主题"]').exists()).toBe(true)
      expect(ElMessage.error).toHaveBeenCalledOnce()
      expect(ElMessage.success).not.toHaveBeenCalled()
      wrapper.unmount()
    })
  }

  it('previews actual current page IDs and offers deletion only for allowed replies', async () => {
    const wrapper = await renderThread(false, false, [
      { id: 'reply.one', content: 'First reply', author_name: 'One', floor_number: 1 },
      { id: 'reply.two', content: 'Second reply', author_name: 'Two', floor_number: 2 },
    ])
    expect(moderationApi.preview).toHaveBeenLastCalledWith('190101', ['reply.one', 'reply.two'])
    const confirmations = wrapper.findAllComponents({ name: 'ElPopconfirm' })
    expect(confirmations).toHaveLength(1)
    expect(wrapper.text()).toContain('First reply')
    expect(wrapper.text()).toContain('Second reply')
    wrapper.unmount()
  })

  it('clears the permission controls when preview is unavailable', async () => {
    vi.spyOn(moderationApi, 'preview').mockRejectedValue(new Error('Unavailable'))
    // The fixture installs its current API. Fail the next explicit request
    // through the exposed page refresh instead of returning a role label.
    const wrapper = await renderThread()
    vi.mocked(moderationApi.preview).mockRejectedValue(new Error('Unavailable'))
    await (wrapper.vm as any).loadModerationAccess()
    await flushPromises()
    expect(wrapper.find('button[aria-label="置顶主题"]').exists()).toBe(false)
    expect(wrapper.findAllComponents({ name: 'ElPopconfirm' })).toHaveLength(0)
    wrapper.unmount()
  })

  it('ignores a late preview response after a newer unavailable request', async () => {
    const wrapper = await renderThread()
    let resolveOlder!: (data: any) => void
    vi.mocked(moderationApi.preview).mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveOlder = resolve
        }) as never,
    )
    const older = (wrapper.vm as any).loadModerationAccess()
    vi.mocked(moderationApi.preview).mockRejectedValueOnce(new Error('Unavailable'))
    await (wrapper.vm as any).loadModerationAccess()
    resolveOlder({ code: 0, data: { thread_id: '190101', pin: true, lock: true, posts: [] } })
    await older
    await flushPromises()
    expect(wrapper.find('button[aria-label="置顶主题"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
