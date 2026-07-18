// @vitest-environment jsdom
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { describe, expect, it, vi } from 'vitest'

vi.mock('element-plus', () => ({
  ElMessage: { warning: vi.fn(), success: vi.fn(), error: vi.fn() },
}))
vi.mock('vue-router', () => ({
  useRouter: () => ({ replace: vi.fn() }),
}))
vi.mock('../src/modules/identity/api', () => ({
  authApi: {},
}))

import PasswordResetView from '../src/modules/identity/pages/PasswordResetView.vue'

describe('Password reset interaction', () => {
  it('keeps the protocol challenge id hidden for ordinary recovery', async () => {
    const wrapper = mount(PasswordResetView, {
      global: {
        stubs: {
          ElSegmented: true,
          ElStep: true,
          ElSteps: { template: '<div><slot /></div>' },
          ElInput: true,
          ElFormItem: { props: ['label'], template: '<div :data-label="label"><slot /></div>' },
          ElButton: { template: '<button><slot /></button>' },
          ElForm: { template: '<form><slot /></form>' },
          RouterLink: { template: '<a><slot /></a>' },
        },
      },
    })

    expect(wrapper.find('[data-label="请求编号"]').exists()).toBe(false)

    const view = wrapper.vm as any
    view.mode = 'assisted'
    await nextTick()

    expect(wrapper.find('[data-label="请求编号"]').exists()).toBe(true)
  })
})
