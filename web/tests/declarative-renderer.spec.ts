// @vitest-environment jsdom
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import DeclarativeRenderer from '../src/campus-ui/DeclarativeRenderer.vue'
import { useUIRuntimeStore } from '../src/modules/plugin-runtime/store'

const { extension } = vi.hoisted(() => ({ extension: vi.fn() }))

vi.mock('../src/modules/plugin-runtime/api', () => ({ uiRuntimeApi: { extension } }))
vi.mock('element-plus', () => ({
  ElMessage: { error: vi.fn(), success: vi.fn() },
  ElMessageBox: { confirm: vi.fn() },
}))

const stubs = {
  'el-button': { template: '<button @click="$emit(\'click\')"><slot /></button>' },
  'el-tag': { template: '<span><slot /></span>' },
  'el-alert': { template: '<span>{{ title }}</span>', props: ['title'] },
}

describe('DeclarativeRenderer', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setActivePinia(createPinia())
  })

  it('renders a safe schema and invokes only the owning plugin action', async () => {
    const runtime = useUIRuntimeStore()
    runtime.actions = new Map([
      [
        'plugin.notes.refresh',
        {
          id: 'plugin.notes.refresh',
          label: '刷新笔记',
          method: 'POST',
          path: '/refresh',
          plugin: 'notes-v2',
        },
      ],
    ])
    extension.mockResolvedValue({ data: { ok: true } })

    const wrapper = mount(DeclarativeRenderer, {
      props: {
        plugin: 'notes-v2',
        node: {
          component: 'stack',
          children: [
            { component: 'heading', level: 2, text: '课堂笔记' },
            { component: 'button', text: '刷新', action_id: 'plugin.notes.refresh' },
          ],
        },
      },
      global: { stubs },
    })
    await wrapper.get('button').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('课堂笔记')
    expect(extension).toHaveBeenCalledOnce()
    expect(extension).toHaveBeenCalledWith('notes-v2', expect.objectContaining({ id: 'plugin.notes.refresh' }))
  })
})
