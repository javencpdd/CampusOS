// @vitest-environment jsdom
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ChallengePolicyView from '../src/modules/identity/pages/ChallengePolicyView.vue'

const { api } = vi.hoisted(() => ({
  api: {
    get: vi.fn(),
    update: vi.fn(),
  },
}))

vi.mock('../src/modules/identity/api', () => ({
  challengePolicyApi: api,
}))

vi.mock('element-plus', () => ({
  ElMessage: { error: vi.fn(), success: vi.fn() },
}))

const stubs = {
  'el-button': { template: '<button @click="$emit(\'click\')"><slot /></button>' },
  'el-form': { template: '<form><slot /></form>' },
  'el-form-item': { template: '<label><slot name="label" /><slot /></label>' },
  'el-input-number': true,
  'el-tooltip': { template: '<span><slot /></span>' },
  'el-icon': { template: '<span><slot /></span>' },
  QuestionFilled: true,
}

describe('ChallengePolicyView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.get.mockResolvedValue({
      data: {
        id: 'email_verification',
        email_window_minutes: 10,
        email_max_requests: 5,
        ip_window_minutes: 60,
        ip_max_requests: 10,
        version: 3,
        updated_at: '2026-07-18T08:00:00Z',
      },
    })
    api.update.mockImplementation(async ({ expected_version, ...values }) => ({
      data: {
        id: 'email_verification',
        ...values,
        version: expected_version + 1,
        updated_at: '2026-07-18T08:05:00Z',
      },
    }))
  })

  it('loads the current policy and submits a bounded hot update', async () => {
    const wrapper = mount(ChallengePolicyView, {
      global: { stubs, directives: { loading: {} } },
    })
    await flushPromises()

    expect(api.get).toHaveBeenCalledOnce()
    expect(wrapper.text()).toContain('验证码策略')
    expect(wrapper.text()).toContain('版本 3')

    const view = wrapper.vm as any
    view.form.email_window_minutes = 5
    view.form.email_max_requests = 2
    await view.save()
    await flushPromises()

    expect(api.update).toHaveBeenCalledWith({
      email_window_minutes: 5,
      email_max_requests: 2,
      ip_window_minutes: 60,
      ip_max_requests: 10,
      expected_version: 3,
    })
    expect(wrapper.text()).toContain('版本 4')
  })
})
