// @vitest-environment jsdom
import { flushPromises, shallowMount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const { api } = vi.hoisted(() => ({
  api: {
    summary: vi.fn(),
    emailDeliveryStatus: vi.fn(),
    events: vi.fn(),
    attempts: vi.fn(),
    workers: vi.fn(),
    operations: vi.fn(),
    commandAudits: vi.fn(),
    compatibility: vi.fn(),
    retentionPreview: vi.fn(),
    retentionRuns: vi.fn(),
    startRetentionPreview: vi.fn(),
    replay: vi.fn(),
  },
}))

vi.mock('../src/modules/operations/api', () => ({ reliabilityApi: api }))
vi.mock('element-plus', () => ({
  ElMessage: { error: vi.fn(), success: vi.fn() },
  ElMessageBox: { confirm: vi.fn() },
}))

import ReliabilityView from '../src/modules/operations/pages/ReliabilityView.vue'

const listResult = (items: any[] = []) => ({
  data: { items, total: items.length, pagination: { total: items.length } },
})

const stubs = {
  'el-alert': true,
  'el-button': true,
  'el-date-picker': true,
  'el-descriptions': true,
  'el-descriptions-item': true,
  'el-dialog': true,
  'el-icon': true,
  'el-input': true,
  'el-option': true,
  'el-pagination': true,
  'el-select': true,
  'el-tab-pane': true,
  'el-table': true,
  'el-table-column': true,
  'el-tabs': true,
  'el-tag': true,
  'el-tooltip': true,
}

describe('ReliabilityView diagnostics', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.summary.mockResolvedValue({ data: {} })
    api.emailDeliveryStatus.mockResolvedValue({ data: {} })
    api.events.mockResolvedValue(listResult())
    api.workers.mockResolvedValue(listResult())
    api.operations.mockResolvedValue(listResult())
    api.commandAudits.mockResolvedValue(listResult())
    api.compatibility.mockResolvedValue(listResult())
    api.retentionRuns.mockResolvedValue(listResult())
    api.attempts.mockResolvedValue(
      listResult([
        { consumer_name: 'consumer:first', attempt: 9, status: 'skipped' },
        { consumer_name: 'system:outbox-finalize', attempt: 9, status: 'failed' },
      ]),
    )
  })

  it('identifies attempt overflow, expired leases, and failed finalization', async () => {
    const wrapper = shallowMount(ReliabilityView, {
      global: { stubs, directives: { loading: {} } },
    })
    await flushPromises()
    const view = wrapper.vm as any
    const event = {
      id: 'event-1',
      status: 'processing',
      attempts: 9,
      max_attempts: 8,
      attempts_overflow: true,
      lease_expired: true,
      lease_generation: 9,
    }

    expect(view.eventDiagnostics(event).map((item: any) => item.label)).toEqual([
      '尝试次数越界',
      '处理租约已过期',
    ])
    await view.openAttempts(event)
    await flushPromises()
    expect(view.selectedEventWarnings).toEqual([
      '尝试次数越界',
      '处理租约已过期',
      '已记录消费者均完成，事件尚未最终化',
    ])
  })
})
