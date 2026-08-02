// @vitest-environment jsdom
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ThreadTaxonomy from '../src/modules/community/components/ThreadTaxonomy.vue'
import { categoryApi } from '../src/modules/community/api'

describe('thread taxonomy', () => {
  afterEach(() => vi.restoreAllMocks())

  it('shows the board as a filter link and renders normalized tags', async () => {
    vi.spyOn(categoryApi, 'get').mockResolvedValueOnce({
      data: { id: 'board-2', name: '校园二手', description: '校内闲置交易', icon: '♻️' },
    } as never)

    const wrapper = mount(ThreadTaxonomy, {
      props: { categoryId: 'board-2', tags: ['闲置', ' 教材 ', '闲置'] },
      global: {
        stubs: {
          RouterLink: { name: 'RouterLink', props: ['to'], template: '<a><slot /></a>' },
          ElTag: { template: '<span><slot /></span>' },
        },
      },
    })
    await flushPromises()

    expect(categoryApi.get).toHaveBeenCalledWith('board-2')
    expect(wrapper.text()).toContain('板块')
    expect(wrapper.text()).toContain('♻️ 校园二手')
    expect(wrapper.text()).toContain('标签')
    expect(wrapper.text().match(/闲置/g)).toHaveLength(1)
    expect(wrapper.text()).toContain('教材')
    expect(wrapper.getComponent({ name: 'RouterLink' }).props('to')).toEqual({
      path: '/threads',
      query: { category_id: 'board-2' },
    })
  })

  it('keeps the information block actionable when metadata cannot be loaded', async () => {
    vi.spyOn(categoryApi, 'get').mockRejectedValueOnce(new Error('offline'))
    const wrapper = mount(ThreadTaxonomy, {
      props: { categoryId: 'board-missing', tags: [] },
      global: {
        stubs: {
          RouterLink: { name: 'RouterLink', props: ['to'], template: '<a><slot /></a>' },
          ElTag: { template: '<span><slot /></span>' },
        },
      },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('板块信息暂不可用')
    expect(wrapper.text()).toContain('暂无标签')
  })
})
