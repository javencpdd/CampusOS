// @vitest-environment jsdom
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ElPagination } from 'element-plus'
import ThreadListView from '../src/modules/community/pages/ThreadListView.vue'
import { categoryApi, threadApi } from '../src/modules/community/api'
import { resetCategoryCatalog } from '../src/modules/community/useCategoryCatalog'

vi.mock('vue-router', () => ({
  useRoute: () => ({ query: {} }),
  useRouter: () => ({ replace: vi.fn(), push: vi.fn() }),
}))

const layout = vi.hoisted(() => ({ compact: true }))
vi.mock('@/shared/layout/useLayoutCapability', async () => {
  const { ref } = await import('vue')
  return {
    useLayoutCapability: () => ({
      mode: ref(layout.compact ? 'compact-portrait' : 'wide'),
      isCompact: ref(layout.compact),
    }),
  }
})

const stubs = {
  RouterLink: { name: 'RouterLink', props: ['to'], template: '<a><slot /></a>' },
  ElTag: { template: '<span><slot /></span>' },
  ElInput: { template: '<div><slot name="append" /></div>' },
  ElButton: { template: '<button><slot /></button>' },
  ElIcon: { template: '<i><slot /></i>' },
  ElTabs: { template: '<div><slot /></div>' },
  ElTabPane: { template: '<div />' },
  ElEmpty: { template: '<div />' },
  ElPagination: { template: '<div />' },
  ElTable: { template: '<div />' },
  ElTableColumn: { template: '<div />' },
}

describe('thread list view taxonomy', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    resetCategoryCatalog()
    layout.compact = true
  })

  it('resolves board chips from one shared category tree without per-row requests', async () => {
    const treeSpy = vi.spyOn(categoryApi, 'tree').mockResolvedValue({
      data: [
        {
          id: 'group-1',
          name: '生活',
          node_kind: 'group',
          children: [{ id: 'board-2', name: '校园二手', icon: '♻️', node_kind: 'board' }],
        },
      ],
    } as never)
    const getSpy = vi.spyOn(categoryApi, 'get')
    vi.spyOn(threadApi, 'list').mockResolvedValue({
      code: 0,
      data: {
        items: [
          { id: 't1', title: '出闲置教材', category_id: 'board-2', tags: ['闲置', ' 教材 '] },
          { id: 't2', title: '也在这个板块', category_id: 'board-2', tags: [] },
          { id: 't3', title: '板块已归档的帖子', category_id: 'board-archived', tags: [] },
        ],
        pagination: { total: 3 },
      },
    } as never)

    const wrapper = mount(ThreadListView, {
      global: { stubs, directives: { loading: {} } },
    })
    await flushPromises()

    expect(treeSpy).toHaveBeenCalledTimes(1)
    expect(getSpy).not.toHaveBeenCalled()
    expect(wrapper.text().match(/♻️ 校园二手/g)).toHaveLength(2)
    expect(wrapper.text()).toContain('闲置')
    expect(wrapper.text()).toContain('教材')
    expect(wrapper.text()).toContain('板块已归档的帖子')
    const boardLink = wrapper
      .findAllComponents({ name: 'RouterLink' })
      .find((link) => (link.props('to') as any)?.query?.category_id === 'board-2')
    expect(boardLink).toBeTruthy()
    expect(boardLink!.props('to')).toEqual({ path: '/threads', query: { category_id: 'board-2' } })
    wrapper.unmount()
  })

  it.each([
    { compact: true, visiblePages: 5 },
    { compact: false, visiblePages: 7 },
  ])('keeps large-list pagination navigable with compact=$compact', async ({ compact, visiblePages }) => {
    layout.compact = compact
    vi.spyOn(categoryApi, 'tree').mockResolvedValue({ data: [] } as never)
    const list = vi.spyOn(threadApi, 'list').mockResolvedValue({
      code: 0,
      data: { items: [], pagination: { total: 10000 } },
    } as never)
    const wrapper = mount(ThreadListView, {
      global: { components: { ElPagination }, stubs: { ...stubs, ElPagination: false }, directives: { loading: {} } },
    })
    await flushPromises()
    expect(wrapper.findAll('.el-pager li.number')).toHaveLength(visiblePages)
    await wrapper.get('button.btn-next').trigger('click')
    await flushPromises()
    expect(list).toHaveBeenLastCalledWith(expect.objectContaining({ page: 2, page_size: 20 }))
    await wrapper.get('button.btn-prev').trigger('click')
    await flushPromises()
    expect(list).toHaveBeenLastCalledWith(expect.objectContaining({ page: 1, page_size: 20 }))
    wrapper.unmount()
  })
})
