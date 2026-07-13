// @vitest-environment jsdom
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import PluginCenterView from '../src/modules/plugin-center/pages/PluginCenterView.vue'

const { api } = vi.hoisted(() => ({
  api: {
    catalog: vi.fn(),
    myGrants: vi.fn(),
    myUsage: vi.fn(),
    enable: vi.fn(),
    revoke: vi.fn(),
    request: vi.fn(),
    exportData: vi.fn(),
    deleteData: vi.fn(),
  },
}))

vi.mock('../src/modules/plugin-center/api', () => ({ pluginCenterApi: api }))
vi.mock('element-plus', () => ({ ElMessage: { error: vi.fn(), success: vi.fn() } }))

const stubs = {
  'el-icon': true,
  'el-alert': true,
  'el-tag': { template: '<span><slot /></span>' },
  'el-empty': true,
  'el-skeleton': true,
  'el-dialog': {
    template: '<section v-if="modelValue"><slot /><slot name="footer" /></section>',
    props: ['modelValue'],
  },
  'el-popconfirm': { template: '<span><slot name="reference" /></span>' },
  'el-checkbox-group': { template: '<div><slot /></div>' },
  'el-checkbox': { template: '<label><slot /></label>', props: ['label'] },
  'el-input': { template: '<input />', props: ['modelValue'] },
  'el-button': { template: '<button @click="$emit(\'click\')"><slot /></button>' },
}

describe('PluginCenterView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.catalog.mockResolvedValue({
      data: {
        items: [
          {
            plugin_name: 'notes-v2',
            display_name: '课堂笔记',
            description: '保存个人课堂笔记',
            version: '1.0.0',
            runtime: 'wasm',
            data_capabilities: ['managed-data', 'user-consent'],
            user_permissions: [
              {
                resource: 'managed_data',
                actions: ['read', 'write'],
                purpose: '保存课堂笔记',
                risk: 'low',
                revocable: true,
              },
            ],
          },
        ],
      },
    })
    api.myGrants.mockResolvedValue({
      data: {
        items: [
          { plugin_name: 'notes-v2', status: 'enabled', permissions: ['managed_data:read', 'managed_data:write'] },
        ],
      },
    })
    api.myUsage.mockResolvedValue({
      data: {
        items: [{ plugin_name: 'notes-v2', record_count: 8, file_count: 2, file_bytes: 2048, search_enabled: true }],
      },
    })
  })

  it('renders the published catalog and clearly marks an existing user grant', async () => {
    const wrapper = mount(PluginCenterView, { global: { stubs } })
    await flushPromises()

    expect(api.catalog).toHaveBeenCalledOnce()
    expect(api.myGrants).toHaveBeenCalledOnce()
    expect(api.myUsage).toHaveBeenCalledOnce()
    expect(wrapper.text()).toContain('课堂笔记')
    expect(wrapper.text()).toContain('已授权')
    expect(wrapper.text()).toContain('保存课堂笔记')
    expect(wrapper.text()).toContain('8 条')
    expect(wrapper.text()).toContain('2.0 KB')
    expect(wrapper.text()).toContain('推荐或申请安装插件')
  })
})
