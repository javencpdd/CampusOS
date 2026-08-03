// @vitest-environment jsdom
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import ThreadBoardChips from '../src/modules/community/components/ThreadBoardChips.vue'

const stubs = {
  RouterLink: { name: 'RouterLink', props: ['to'], template: '<a><slot /></a>' },
  ElTag: { template: '<span><slot /></span>' },
}

describe('thread board chips', () => {
  it('renders the board as a filter link and normalized tags', () => {
    const wrapper = mount(ThreadBoardChips, {
      props: {
        category: { id: 'board-2', name: '校园二手', description: '校内闲置交易', icon: '♻️' },
        tags: ['闲置', ' 教材 ', '闲置'],
      },
      global: { stubs },
    })

    expect(wrapper.text()).toContain('♻️ 校园二手')
    expect(wrapper.text().match(/闲置/g)).toHaveLength(1)
    expect(wrapper.text()).toContain('教材')
    expect(wrapper.getComponent({ name: 'RouterLink' }).props('to')).toEqual({
      path: '/threads',
      query: { category_id: 'board-2' },
    })
  })

  it('renders tags without a board when the category cannot be resolved', () => {
    const wrapper = mount(ThreadBoardChips, {
      props: { category: null, tags: ['求助'] },
      global: { stubs },
    })

    expect(wrapper.findComponent({ name: 'RouterLink' }).exists()).toBe(false)
    expect(wrapper.text()).toContain('求助')
  })

  it('renders nothing when both board and tags are missing', () => {
    const wrapper = mount(ThreadBoardChips, {
      props: { category: null, tags: [] },
      global: { stubs },
    })

    expect(wrapper.find('.thread-board-chips').exists()).toBe(false)
    expect(wrapper.text()).toBe('')
  })
})
