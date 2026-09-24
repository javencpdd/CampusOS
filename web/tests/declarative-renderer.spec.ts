// @vitest-environment jsdom
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import DeclarativeRenderer from '../src/campus-ui/DeclarativeRenderer.vue'
import { useUIRuntimeStore } from '../src/modules/plugin-runtime/store'

const { extension, openSurface, openHost } = vi.hoisted(() => ({
  extension: vi.fn(),
  openSurface: vi.fn(),
  openHost: vi.fn(),
}))

vi.mock('../src/modules/plugin-runtime/api', () => ({ uiRuntimeApi: { extension, openSurface } }))
vi.mock('../src/campus-ui/surfaceHost', () => ({ openPluginSurface: openHost }))
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

  it('opens a declared surface only with a trusted selected resource context', async () => {
    const runtime = useUIRuntimeStore()
    const action = {
      id: 'reader.open',
      label: '预览',
      plugin: 'reader',
      kind: 'open-surface' as const,
      surface_id: 'reader.preview',
      presentation: 'modal' as const,
    }
    runtime.actions = new Map([[action.id, action]])
    openSurface.mockResolvedValue({ data: { id: 'opaque' } })
    const wrapper = mount(DeclarativeRenderer, {
      props: {
        plugin: 'reader',
        node: { component: 'stack', children: [{ component: 'button', action_id: action.id }] },
      },
      global: { stubs },
    })
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(openSurface).not.toHaveBeenCalled()
    const resourceContext = { resource_type: 'personal_asset' as const, resource_id: '123' }
    await wrapper.setProps({ resourceContext })
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(openSurface).toHaveBeenCalledWith('reader', action, resourceContext)
    expect(extension).not.toHaveBeenCalled()
    expect(openHost).toHaveBeenCalledWith({
      surfaceID: 'reader.preview',
      invocationID: 'opaque',
      presentation: 'modal',
    })
    wrapper.unmount()
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
