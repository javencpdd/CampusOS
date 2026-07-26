// @vitest-environment jsdom
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import NotificationCenter from '../src/modules/notifications/components/NotificationCenter.vue'
import { notificationApi } from '../src/modules/notifications/api'

vi.mock('../src/modules/notifications/api', () => ({
  notificationApi: {
    list: vi.fn(),
    markRead: vi.fn(),
    markAllRead: vi.fn(),
  },
}))
vi.mock('element-plus', () => ({
  ElMessage: { error: vi.fn(), success: vi.fn() },
  ElNotification: vi.fn(),
}))

describe('notification center', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(notificationApi.list).mockResolvedValue({
      data: {
        items: [
          {
            id: 'notification-1',
            user_id: 'user-1',
            type: 'community.thread.trashed',
            title: '帖子已被管理员移入回收站',
            content: '您的帖子《测试内容》已被管理员移入回收站。',
            action_url: '/threads/thread-1',
            is_read: false,
            metadata: { thread_id: 'thread-1' },
            created_at: '2026-07-26T10:00:00Z',
            updated_at: '2026-07-26T10:00:00Z',
          },
        ],
        unread_count: 1,
        pagination: { page: 1, page_size: 20, total: 1, total_pages: 1 },
      },
    } as never)
    vi.mocked(notificationApi.markRead).mockResolvedValue({} as never)
  })

  it('renders an unread governance message, marks it read and opens its internal target', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/', component: { template: '<div />' } },
        { path: '/threads/:id', component: { template: '<div />' } },
      ],
    })
    await router.push('/')
    await router.isReady()

    const wrapper = mount(NotificationCenter, {
      global: {
        plugins: [router],
        directives: { loading: () => undefined },
        stubs: {
          'el-tooltip': { template: '<span><slot /></span>' },
          'el-badge': { template: '<span><slot /></span>' },
          'el-button': { template: '<button @click="$emit(\'click\')"><slot /></button>' },
          'el-icon': { template: '<span><slot /></span>' },
          'el-drawer': { template: '<section><slot /></section>' },
          'el-empty': true,
          Bell: true,
          Check: true,
          ArrowRight: true,
        },
      },
    })
    await flushPromises()

    expect(notificationApi.list).toHaveBeenCalledWith({ page: 1, page_size: 20 })
    expect(wrapper.text()).toContain('帖子已被管理员移入回收站')
    expect(wrapper.text()).toContain('1 条未读')

    await wrapper.get('.notification-item').trigger('click')
    await flushPromises()

    expect(notificationApi.markRead).toHaveBeenCalledWith('notification-1')
    expect(router.currentRoute.value.fullPath).toBe('/threads/thread-1')
    wrapper.unmount()
  })
})
