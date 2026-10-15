// @vitest-environment jsdom
import { flushPromises, shallowMount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { h } from 'vue'

const { list, error } = vi.hoisted(() => ({ list: vi.fn(), error: vi.fn() }))
vi.mock('../src/modules/operations/api', () => ({ eventApi: { list } }))
vi.mock('element-plus', () => ({ ElMessage: { error } }))

import EventLogView from '../src/modules/operations/pages/EventLogView.vue'

const metadata = { id: 'event-1', type: 'thread.created', source: 'community', time: '2026-10-13T00:00:00Z' }
const mount = () => shallowMount(EventLogView, {
  global: {
    directives: { loading: {} },
    renderStubDefaultSlot: true,
    stubs: {
      'el-card': true, 'el-select': true, 'el-option': true, 'el-button': true,
      'el-icon': true, 'el-table': true,
      'el-table-column': { render: () => h('el-table-column-stub') },
      'el-tag': true, 'el-empty': true,
    },
  },
})

describe('event metadata history', () => {
  beforeEach(() => vi.clearAllMocks())

  it('has only metadata columns and no payload or subject viewer', async () => {
    list.mockResolvedValue({ data: { items: [{ ...metadata, data: { body: 'private-body' }, subject: 'private-subject' }], total: 1 } })
    const wrapper = mount()
    await flushPromises()
    expect(wrapper.findAll('el-table-column-stub').map(column => column.attributes('prop'))).toEqual(['id', 'type', 'source', 'time'])
    expect(wrapper.text()).not.toContain('private-body')
    expect(wrapper.text()).not.toContain('private-subject')
    expect(wrapper.text()).not.toContain('查看详情')
    expect(wrapper.find('el-dialog-stub').exists()).toBe(false)
    wrapper.unmount()
  })

  it('sends the selected type to the server before pagination and clears on revoked access', async () => {
    list.mockResolvedValueOnce({ data: { items: [metadata], total: 1 } })
    const wrapper = mount()
    await flushPromises()
    const view = wrapper.vm as any
    expect(view.events).toHaveLength(1)
    list.mockRejectedValueOnce(new Error('access revoked'))
    view.filterType = 'post.deleted'
    await view.load()
    expect(list).toHaveBeenLastCalledWith({ limit: 100, type: 'post.deleted' })
    expect(view.events).toEqual([])
    expect(error).toHaveBeenCalledOnce()
    wrapper.unmount()
  })

  it('keeps the latest filter result when requests finish out of order', async () => {
    let resolveOld!: (value: unknown) => void
    list.mockReturnValueOnce(new Promise(resolve => { resolveOld = resolve }))
    const wrapper = mount()
    const view = wrapper.vm as any
    view.filterType = 'post.deleted'
    const latest = { ...metadata, id: 'event-2', type: 'post.deleted' }
    list.mockResolvedValueOnce({ data: { items: [latest], total: 1 } })
    await view.load()
    resolveOld({ data: { items: [metadata], total: 1 } })
    await flushPromises()
    expect(view.events).toEqual([latest])
    expect(view.loading).toBe(false)
    wrapper.unmount()
  })
})
