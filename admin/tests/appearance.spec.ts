// @vitest-environment jsdom
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import AppearanceManageView from '../src/modules/appearance/pages/AppearanceManageView.vue'

const { api } = vi.hoisted(() => ({
  api: {
    catalog: vi.fn(),
    sources: vi.fn(),
    validate: vi.fn(),
    apply: vi.fn(),
    example: vi.fn(),
    exampleZip: vi.fn(),
    applySource: vi.fn(),
    rollback: vi.fn(),
    homeConfig: vi.fn(),
    uploadLogo: vi.fn(),
    resetLogo: vi.fn(),
  },
}))

vi.mock('../src/modules/appearance/api', () => ({
  webThemeCatalogApi: { catalog: api.catalog },
  homeStylePackApi: {
    sources: api.sources,
    validate: api.validate,
    apply: api.apply,
    example: api.example,
    exampleZip: api.exampleZip,
    applySource: api.applySource,
    rollback: api.rollback,
  },
  homeBrandingApi: {
    config: api.homeConfig,
    uploadLogo: api.uploadLogo,
    resetLogo: api.resetLogo,
  },
}))

vi.mock('element-plus', () => ({
  ElMessage: { error: vi.fn(), success: vi.fn() },
  ElMessageBox: { confirm: vi.fn() },
}))

const stubs = {
  'el-alert': true,
  'el-tag': { template: '<span><slot /></span>' },
  'el-empty': true,
  'el-button': { template: '<button @click="$emit(\'click\')"><slot /></button>' },
  'el-select': true,
  'el-option': true,
}

describe('AppearanceManageView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.catalog.mockResolvedValue({
      data: {
        enabled: true,
        allow_user_switch: true,
        default_style_pack: 'campus-canvas',
        items: [
          {
            name: 'campus-canvas',
            display_name: 'Campus Canvas',
            version: '2.0.0',
            description: 'Responsive system theme.',
          },
        ],
      },
    })
    api.sources.mockResolvedValue({
      data: {
        items: [
          {
            name: 'campus-hero',
            display_name: 'Campus Hero',
            version: '1.0.0',
            validation: { valid: true },
          },
        ],
      },
    })
    api.homeConfig.mockResolvedValue({
      data: {
        logo: {
          url: '/api/v1/home/logo?v=default',
          custom: false,
          mime_type: 'image/png',
          size_bytes: 104765,
          width: 720,
          height: 223,
          max_bytes: 2 * 1024 * 1024,
        },
      },
    })
  })

  it('shows the three appearance ownership boundaries and live catalogs', async () => {
    const wrapper = mount(AppearanceManageView, {
      global: { stubs, directives: { loading: {} } },
    })
    await flushPromises()

    expect(api.catalog).toHaveBeenCalledOnce()
    expect(api.sources).toHaveBeenCalledOnce()
    expect(api.homeConfig).toHaveBeenCalledOnce()
    expect(wrapper.text()).toContain('系统 Logo')
    expect(wrapper.text()).toContain('上传后立即生效，无需重启 Docker')
    expect(wrapper.text()).toContain('首页风格包')
    expect(wrapper.text()).toContain('管理员统一切换')
    expect(wrapper.text()).toContain('系统主题目录')
    expect(wrapper.text()).toContain('Campus Canvas')
    expect(wrapper.text()).toContain('主页所有者选择')
    expect(wrapper.text()).toContain('Admin 不代替用户修改个人主页')
  })
})
