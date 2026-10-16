// @vitest-environment jsdom
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ElMessage } from 'element-plus'
import SpaceSettingsView from '../src/modules/space/pages/SpaceSettingsView.vue'
import { spaceApi } from '../src/modules/space/api'

const state = vi.hoisted(() => ({ setAvatar: vi.fn() }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push: vi.fn() }) }))
vi.mock('@/modules/identity/store', () => ({ useUserStore: () => ({ setAvatar: state.setAvatar }) }))
vi.mock('element-plus', () => ({ ElMessage: { error: vi.fn(), success: vi.fn(), warning: vi.fn() } }))
vi.mock('@/campus-ui/surfaceHost', () => ({ openPluginSurface: vi.fn() }))
vi.mock('@/campus-ui/pluginConsent', () => ({ ensurePluginCapabilityConsent: vi.fn() }))
vi.mock('@/modules/richtext/api', () => ({
  richTextApi: { listUserAssets: vi.fn().mockResolvedValue({ data: { items: [], next_cursor: '' } }) },
}))
vi.mock('@/modules/space/api', () => ({
  spaceApi: {
    me: vi.fn(),
    updateMe: vi.fn(),
    selectAvatar: vi.fn(),
    uploadAvatar: vi.fn(),
    cleanupAvatars: vi.fn(),
    applyStyle: vi.fn(),
    rollbackStyle: vi.fn(),
    restoreDefaultStyle: vi.fn(),
    applyCustomHtml: vi.fn(),
    applyStylePack: vi.fn(),
    applySourceStylePack: vi.fn(),
    validateStyle: vi.fn(),
    previewStyle: vi.fn(),
    exportStyle: vi.fn(),
    validateCustomHtml: vi.fn(),
    customHtmlExample: vi.fn(),
    validateStylePack: vi.fn(),
    stylePackExampleZip: vi.fn(),
    storage: vi.fn().mockResolvedValue({
      data: {
        user_id: '1001',
        quota_bytes: 1024,
        used_bytes: 0,
        available_bytes: 1024,
        max_avatar_bytes: 1024,
        avatar_keep_limit: 3,
      },
    }),
    avatars: vi.fn().mockResolvedValue({ data: { items: [] } }),
    sourceStylePacks: vi.fn().mockResolvedValue({ data: { items: [] } }),
  },
}))
const container = { template: '<section><slot name="header" /><slot /><slot name="footer" /></section>' }
const stubs = {
  ElRow: container,
  ElCol: container,
  ElCard: container,
  ElForm: container,
  ElFormItem: container,
  ElTag: container,
  ElSelect: container,
  ElOption: true,
  ElSwitch: true,
  ElCheckbox: true,
  ElIcon: true,
  ElAvatar: true,
  ElEmpty: true,
  ElAlert: true,
  ElTable: true,
  ElTableColumn: true,
  ElButton: {
    emits: ['click'],
    props: ['loading'],
    template: '<button :disabled="loading" @click="$emit(\'click\')"><slot /></button>',
  },
  ElInput: {
    props: ['modelValue'],
    emits: ['update:modelValue'],
    template: '<input :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />',
  },
}
const payload = (title: string, avatar = '') => ({
  code: 0,
  data: {
    owner: { id: '1001', username: 'owner', nickname: 'Owner', avatar },
    space: {
      id: '2001',
      user_id: '1001',
      title,
      bio: 'Private profile',
      theme: 'default',
      layout: 'blog',
      visibility: 'private',
      sync_enabled: true,
      sync_categories: [],
      sync_tags: [],
    },
  },
})
let styleDownloadClick: { mockRestore: () => void } | undefined
afterEach(() => {
  styleDownloadClick?.mockRestore()
  styleDownloadClick = undefined
  vi.clearAllMocks()
  vi.unstubAllGlobals()
})
describe('checked style reads', () => {
  const pkg = {
    manifest: {
      schema_version: 'space-style.v1',
      name: 'checked-preview',
      version: '1.0.0',
      layout: 'blog',
      components: [],
    },
  }
  const cases = [
    ['校验', 'validateStyle', { data: { valid: true } }],
    [
      '预览',
      'previewStyle',
      { data: { validation: { valid: true }, manifest: pkg.manifest, layout: 'blog', components: [], tokens: {} } },
    ],
    [
      '导出风格',
      'exportStyle',
      { data: { package: pkg, filename: 'checked-style.json', validation: { valid: true } } },
    ],
    ['检测 HTML', 'validateCustomHtml', { data: { valid: true } }],
    [
      '生成示例',
      'customHtmlExample',
      { data: { html: '<section>Current example</section>', package: pkg, validation: { valid: true } } },
    ],
    [
      '筛查',
      'validateStylePack',
      {
        data: {
          validation: { valid: true },
          package: { manifest: { name: 'checked-pack', version: '1.0.0', target: 'personal-space' } },
        },
      },
    ],
    ['当前风格示例', 'stylePackExampleZip', new Blob(['checked archive'], { type: 'application/zip' })],
  ] as const
  for (const [label, method, result] of cases) {
    for (const accepted of [true, false]) {
      it(`${method} accepted=${accepted} consumes checked results and preserves the draft on denial`, async () => {
        vi.mocked(spaceApi.me).mockResolvedValue(payload('Current style reads') as never)
        vi.mocked(spaceApi.sourceStylePacks).mockResolvedValue({ data: { items: [] } } as never)
        if (accepted) vi.mocked(spaceApi[method]).mockResolvedValue(result as never)
        else vi.mocked(spaceApi[method]).mockRejectedValue({ msg: '当前会话或空间已变化' })
        const NativeURL = URL
        const create = vi.fn(() => 'blob:checked-style')
        const revoke = vi.fn()
        vi.stubGlobal(
          'URL',
          class extends NativeURL {
            static createObjectURL = create
            static revokeObjectURL = revoke
          },
        )
        const downloads: string[] = []
        styleDownloadClick = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (
          this: HTMLAnchorElement,
        ) {
          downloads.push(this.download)
        })
        const wrapper = mount(SpaceSettingsView, { global: { stubs, directives: { loading: {} } } })
        await flushPromises()
        await wrapper.find('input[placeholder="<section>...</section>"]').setValue('<section>Own draft</section>')
        if (method === 'validateStylePack') {
          const input = wrapper.find('input[accept=".zip,application/zip"]')
          Object.defineProperty(input.element, 'files', {
            value: [new File(['zip'], 'owned.zip', { type: 'application/zip' })],
            configurable: true,
          })
          await input.trigger('change')
        }
        await wrapper
          .findAll('button')
          .find((button) => button.text() === label)!
          .trigger('click')
        await flushPromises()
        expect(spaceApi[method]).toHaveBeenCalledOnce()
        expect(spaceApi.me).toHaveBeenCalledOnce()
        if (accepted) {
          expect(ElMessage.error).not.toHaveBeenCalled()
          if (method === 'previewStyle') expect(wrapper.find('.preview-header strong').text()).toBe('checked-preview')
          if (method === 'customHtmlExample')
            expect(
              (wrapper.find('input[placeholder="<section>...</section>"]').element as HTMLInputElement).value,
            ).toBe('<section>Current example</section>')
          if (method === 'exportStyle' || method === 'stylePackExampleZip') {
            expect(create).toHaveBeenCalledOnce()
            expect(revoke).toHaveBeenCalledOnce()
            expect(downloads).toHaveLength(1)
          }
        } else {
          expect(ElMessage.error).toHaveBeenCalledWith('当前会话或空间已变化')
          expect((wrapper.find('input[placeholder="<section>...</section>"]').element as HTMLInputElement).value).toBe(
            '<section>Own draft</section>',
          )
          expect(downloads).toHaveLength(0)
        }
        wrapper.unmount()
      })
    }
  }
  it('forgets a previously accepted preview when the current request is rejected', async () => {
    vi.mocked(spaceApi.me).mockResolvedValue(payload('Current style reads') as never)
    vi.mocked(spaceApi.previewStyle)
      .mockReset()
      .mockResolvedValueOnce({
        data: { validation: { valid: true }, manifest: pkg.manifest, components: [], tokens: {} },
      } as never)
      .mockRejectedValueOnce({ msg: '当前证明已失效' })
    const wrapper = mount(SpaceSettingsView, { global: { stubs, directives: { loading: {} } } })
    await flushPromises()
    const button = wrapper.findAll('button').find((button) => button.text() === '预览')!
    await button.trigger('click')
    await flushPromises()
    expect(wrapper.find('.preview-header strong').text()).toBe('checked-preview')
    await button.trigger('click')
    await flushPromises()
    expect(wrapper.find('.preview-header strong').text()).not.toBe('checked-preview')
    expect(ElMessage.error).toHaveBeenCalledWith('当前证明已失效')
    wrapper.unmount()
  })
})
describe('checked source style catalog', () => {
  const catalog = {
    data: {
      items: [
        {
          name: 'safe-source',
          display_name: '安全目录样式',
          version: '1.0.0',
          previews: { desktop: 'preview-desktop.png', mobile: 'preview-mobile.png' },
          validation: { valid: true },
        },
        { name: 'legacy-source', version: '1.0.0', validation: { valid: false, errors: ['需要补齐交付合同'] } },
      ],
    },
  }
  it('renders the path-free catalog and only enables valid source choices', async () => {
    vi.mocked(spaceApi.me).mockResolvedValue(payload('Catalog profile') as never)
    vi.mocked(spaceApi.sourceStylePacks).mockResolvedValue(catalog as never)
    const wrapper = mount(SpaceSettingsView, { global: { stubs, directives: { loading: {} } } })
    await flushPromises()
    expect(wrapper.find('[data-source-style-pack-name="safe-source"]').text()).toContain('安全目录样式')
    expect(wrapper.find('[data-source-style-pack-name="safe-source"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.find('[data-source-style-pack-name="legacy-source"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('.source-pack-preview').attributes('src')).toBe(
      '/api/v1/appearance/space-style-packs/safe-source/assets/preview-desktop.png',
    )
    expect(ElMessage.error).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('clears old choices and reports a rejected current catalog refresh', async () => {
    vi.mocked(spaceApi.me).mockResolvedValue(payload('Catalog profile') as never)
    vi.mocked(spaceApi.sourceStylePacks)
      .mockResolvedValueOnce(catalog as never)
      .mockRejectedValueOnce({ msg: '会话或源码目录已变化' })
    const wrapper = mount(SpaceSettingsView, { global: { stubs, directives: { loading: {} } } })
    await flushPromises()
    await wrapper.find('.source-pack-header button').trigger('click')
    await flushPromises()
    expect(spaceApi.sourceStylePacks).toHaveBeenCalledTimes(2)
    expect(wrapper.findAll('[data-source-style-pack-name]')).toHaveLength(0)
    expect(ElMessage.error).toHaveBeenCalledWith('会话或源码目录已变化')
    expect(spaceApi.applySourceStylePack).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
describe('current Space profile acknowledgement', () => {
  it('reloads the current profile after an ID-only acknowledgement', async () => {
    vi.mocked(spaceApi.me)
      .mockResolvedValueOnce(payload('Before save') as never)
      .mockResolvedValueOnce(payload('Current saved profile', '/owned/avatar.png') as never)
    vi.mocked(spaceApi.updateMe).mockResolvedValue({ code: 0, data: { space_id: '2001', owner_id: '1001' } } as never)
    const wrapper = mount(SpaceSettingsView, { global: { stubs, directives: { loading: {} } } })
    await flushPromises()
    await wrapper
      .findAll('button')
      .find((button) => button.text().includes('保存配置'))!
      .trigger('click')
    await flushPromises()
    expect(spaceApi.updateMe).toHaveBeenCalledOnce()
    expect(spaceApi.me).toHaveBeenCalledTimes(2)
    expect(
      wrapper.findAll('input').some((input) => (input.element as HTMLInputElement).value === 'Current saved profile'),
    ).toBe(true)
    expect(state.setAvatar).toHaveBeenCalledWith('/owned/avatar.png')
    expect(ElMessage.success).toHaveBeenCalledWith('已保存')
    expect(ElMessage.error).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('keeps the loaded profile when the current command is rejected', async () => {
    vi.mocked(spaceApi.me).mockResolvedValue(payload('Before save') as never)
    vi.mocked(spaceApi.updateMe).mockRejectedValue({ msg: '资料或权限已变化' })
    const wrapper = mount(SpaceSettingsView, { global: { stubs, directives: { loading: {} } } })
    await flushPromises()
    const button = wrapper.findAll('button').find((button) => button.text().includes('保存配置'))!
    await button.trigger('click')
    await flushPromises()
    expect(spaceApi.me).toHaveBeenCalledOnce()
    expect(wrapper.findAll('input').some((input) => (input.element as HTMLInputElement).value === 'Before save')).toBe(
      true,
    )
    expect(ElMessage.error).toHaveBeenCalledWith('资料或权限已变化')
    expect(ElMessage.success).not.toHaveBeenCalled()
    expect(button.attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })
})

describe('current retained avatar selection', () => {
  const history = (active: boolean) => ({
    data: {
      items: [
        {
          file_name: 'retained.png',
          url: '/owned/retained.png',
          size: 68,
          uploaded_at: '2026-10-14T12:00:00Z',
          active,
        },
      ],
    },
  })
  it('reloads current profile, storage and history after the ID-only acknowledgement', async () => {
    vi.mocked(spaceApi.me)
      .mockResolvedValueOnce(payload('Before selection') as never)
      .mockResolvedValueOnce(payload('Current selection', '/owned/retained.png') as never)
    vi.mocked(spaceApi.avatars)
      .mockResolvedValueOnce(history(false) as never)
      .mockResolvedValueOnce(history(true) as never)
    vi.mocked(spaceApi.selectAvatar).mockResolvedValue({
      code: 0,
      data: { space_id: '2001', owner_id: '1001' },
    } as never)
    const wrapper = mount(SpaceSettingsView, { global: { stubs, directives: { loading: {} } } })
    await flushPromises()
    await wrapper.find('button.avatar-history-item').trigger('click')
    await flushPromises()
    expect(spaceApi.selectAvatar).toHaveBeenCalledWith('retained.png')
    expect(spaceApi.me).toHaveBeenCalledTimes(2)
    expect(spaceApi.storage).toHaveBeenCalledTimes(2)
    expect(spaceApi.avatars).toHaveBeenCalledTimes(2)
    expect(wrapper.find('button.avatar-history-item').attributes('aria-pressed')).toBe('true')
    expect(state.setAvatar).toHaveBeenCalledWith('/owned/retained.png')
    expect(ElMessage.success).toHaveBeenCalledWith('头像已切换，保留文件顺序未改变')
    expect(ElMessage.error).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('keeps loaded history and profile when the current selection is rejected', async () => {
    vi.mocked(spaceApi.me).mockResolvedValue(payload('Before selection') as never)
    vi.mocked(spaceApi.avatars).mockResolvedValue(history(false) as never)
    vi.mocked(spaceApi.selectAvatar).mockRejectedValue({ msg: '头像或权限已变化' })
    const wrapper = mount(SpaceSettingsView, { global: { stubs, directives: { loading: {} } } })
    await flushPromises()
    await wrapper.find('button.avatar-history-item').trigger('click')
    await flushPromises()
    expect(spaceApi.me).toHaveBeenCalledOnce()
    expect(spaceApi.storage).toHaveBeenCalledOnce()
    expect(spaceApi.avatars).toHaveBeenCalledOnce()
    expect(wrapper.find('button.avatar-history-item').attributes('aria-pressed')).toBe('false')
    expect(ElMessage.error).toHaveBeenCalledWith('头像或权限已变化')
    expect(ElMessage.success).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})

describe('current avatar upload acknowledgement', () => {
  for (const pending of [false, true]) {
    it(`reloads current resources after upload with retention pending=${pending}`, async () => {
      vi.mocked(spaceApi.me)
        .mockResolvedValueOnce(payload('Before upload') as never)
        .mockResolvedValueOnce(payload('Uploaded current profile', '/owned/upload.png') as never)
      vi.mocked(spaceApi.avatars).mockResolvedValue({ data: { items: [] } } as never)
      vi.mocked(spaceApi.uploadAvatar).mockResolvedValue({
        code: 0,
        data: { space_id: '2001', owner_id: '1001', cleanup_pending: pending },
      } as never)
      const wrapper = mount(SpaceSettingsView, { global: { stubs, directives: { loading: {} } } })
      await flushPromises()
      const input = wrapper.find('input[type="file"][accept="image/png,image/jpeg,image/gif,image/webp"]')
      const file = new File(['x'], 'avatar.png', { type: 'image/png' })
      Object.defineProperty(input.element, 'files', { value: [file], configurable: true })
      await input.trigger('change')
      await flushPromises()
      expect(spaceApi.uploadAvatar).toHaveBeenCalledWith(file)
      expect(spaceApi.me).toHaveBeenCalledTimes(2)
      expect(spaceApi.storage).toHaveBeenCalledTimes(2)
      expect(spaceApi.avatars).toHaveBeenCalledTimes(2)
      expect(state.setAvatar).toHaveBeenCalledWith('/owned/upload.png')
      expect(ElMessage.success).toHaveBeenCalledWith('头像已更新')
      expect(ElMessage.warning).toHaveBeenCalledTimes(pending ? 1 : 0)
      expect(ElMessage.error).not.toHaveBeenCalled()
      wrapper.unmount()
    })
  }
  it('keeps the loaded profile and history when upload is rejected', async () => {
    vi.mocked(spaceApi.me).mockResolvedValue(payload('Before upload') as never)
    vi.mocked(spaceApi.avatars).mockResolvedValue({ data: { items: [] } } as never)
    vi.mocked(spaceApi.uploadAvatar).mockRejectedValue({ msg: '配额或权限已变化' })
    const wrapper = mount(SpaceSettingsView, { global: { stubs, directives: { loading: {} } } })
    await flushPromises()
    const input = wrapper.find('input[type="file"][accept="image/png,image/jpeg,image/gif,image/webp"]')
    Object.defineProperty(input.element, 'files', {
      value: [new File(['x'], 'avatar.png', { type: 'image/png' })],
      configurable: true,
    })
    await input.trigger('change')
    await flushPromises()
    expect(spaceApi.me).toHaveBeenCalledOnce()
    expect(spaceApi.storage).toHaveBeenCalledOnce()
    expect(ElMessage.error).toHaveBeenCalledWith('配额或权限已变化')
    expect(ElMessage.success).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})

describe('current avatar history cleanup', () => {
  for (const pending of [false, true]) {
    it(`reloads current resources after cleanup with pending=${pending}`, async () => {
      vi.mocked(spaceApi.me)
        .mockResolvedValueOnce(payload('Before cleanup') as never)
        .mockResolvedValueOnce(payload('Current after cleanup', '/owned/current.png') as never)
      vi.mocked(spaceApi.avatars).mockResolvedValue({ data: { items: [] } } as never)
      vi.mocked(spaceApi.cleanupAvatars).mockResolvedValue({
        code: 0,
        data: { owner_id: '1001', cleanup_pending: pending },
      } as never)
      const wrapper = mount(SpaceSettingsView, { global: { stubs, directives: { loading: {} } } })
      await flushPromises()
      await wrapper
        .findAll('button')
        .find((button) => button.text().includes('清理历史头像'))!
        .trigger('click')
      await flushPromises()
      expect(spaceApi.cleanupAvatars).toHaveBeenCalledWith()
      expect(spaceApi.me).toHaveBeenCalledTimes(2)
      expect(spaceApi.storage).toHaveBeenCalledTimes(2)
      expect(spaceApi.avatars).toHaveBeenCalledTimes(2)
      expect(state.setAvatar).toHaveBeenCalledWith('/owned/current.png')
      if (pending) expect(ElMessage.warning).toHaveBeenCalledWith('部分历史头像清理待处理，可稍后重试。')
      else expect(ElMessage.success).toHaveBeenCalledWith('已保留正在使用的头像和最新记录')
      expect(ElMessage.error).not.toHaveBeenCalled()
      wrapper.unmount()
    })
  }
  it('retains the loaded view and enables retry when current cleanup is denied', async () => {
    vi.mocked(spaceApi.me).mockResolvedValue(payload('Before cleanup') as never)
    vi.mocked(spaceApi.avatars).mockResolvedValue({ data: { items: [] } } as never)
    vi.mocked(spaceApi.cleanupAvatars).mockRejectedValue({ msg: '权限已变化，请重新登录' })
    const wrapper = mount(SpaceSettingsView, { global: { stubs, directives: { loading: {} } } })
    await flushPromises()
    const button = wrapper.findAll('button').find((item) => item.text().includes('清理历史头像'))!
    await button.trigger('click')
    await flushPromises()
    expect(spaceApi.me).toHaveBeenCalledOnce()
    expect(spaceApi.storage).toHaveBeenCalledOnce()
    expect(button.attributes('disabled')).toBeUndefined()
    expect(ElMessage.error).toHaveBeenCalledWith('权限已变化，请重新登录')
    expect(ElMessage.success).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})

describe('current Space style metadata commands', () => {
  const cases = [
    ['apply', '应用', 'applyStyle'],
    ['rollback', '回滚', 'rollbackStyle'],
    ['default', '默认', 'restoreDefaultStyle'],
  ] as const
  for (const [action, label, method] of cases) {
    it(`reloads the current profile after an ID-only ${action} acknowledgement`, async () => {
      vi.mocked(spaceApi.me)
        .mockResolvedValueOnce(payload('Before style change') as never)
        .mockResolvedValueOnce(payload(`Current ${action}`) as never)
      vi.mocked(spaceApi.avatars).mockResolvedValue({ data: { items: [] } } as never)
      vi.mocked(spaceApi[method]).mockResolvedValue({ code: 0, data: { space_id: '2001', owner_id: '1001' } } as never)
      const wrapper = mount(SpaceSettingsView, { global: { stubs, directives: { loading: {} } } })
      await flushPromises()
      if (action === 'apply') await wrapper.find('button.style-option').trigger('click')
      await wrapper
        .find('.style-actions')
        .findAll('button')
        .find((button) => button.text() === label)!
        .trigger('click')
      await flushPromises()
      expect(spaceApi[method]).toHaveBeenCalledOnce()
      expect(spaceApi.me).toHaveBeenCalledTimes(2)
      expect(
        wrapper.findAll('input').some((input) => (input.element as HTMLInputElement).value === `Current ${action}`),
      ).toBe(true)
      expect(ElMessage.success).toHaveBeenCalledOnce()
      expect(ElMessage.error).not.toHaveBeenCalled()
      wrapper.unmount()
    })
    it(`keeps the loaded configuration when current ${action} is rejected`, async () => {
      vi.mocked(spaceApi.me).mockResolvedValue(payload('Before style change') as never)
      vi.mocked(spaceApi.avatars).mockResolvedValue({ data: { items: [] } } as never)
      vi.mocked(spaceApi[method]).mockRejectedValue({ msg: '样式或权限已变化' })
      const wrapper = mount(SpaceSettingsView, { global: { stubs, directives: { loading: {} } } })
      await flushPromises()
      if (action === 'apply') await wrapper.find('button.style-option').trigger('click')
      const button = wrapper
        .find('.style-actions')
        .findAll('button')
        .find((item) => item.text() === label)!
      await button.trigger('click')
      await flushPromises()
      expect(spaceApi[method]).toHaveBeenCalledOnce()
      expect(spaceApi.me).toHaveBeenCalledOnce()
      expect(
        wrapper.findAll('input').some((input) => (input.element as HTMLInputElement).value === 'Before style change'),
      ).toBe(true)
      expect(ElMessage.error).toHaveBeenCalledWith('样式或权限已变化')
      expect(ElMessage.success).not.toHaveBeenCalled()
      expect(button.attributes('disabled')).toBeUndefined()
      wrapper.unmount()
    })
  }
})

describe('current HTML, ZIP and source style application', () => {
  const cases = [
    ['html', '应用 HTML', 'applyCustomHtml'],
    ['zip', '应用拓展包', 'applyStylePack'],
    ['source', '应用源码目录', 'applySourceStylePack'],
  ] as const
  for (const [variant, label, method] of cases) {
    for (const accepted of [true, false]) {
      it(`${variant} application accepted=${accepted} uses the current profile only after acknowledgement`, async () => {
        const me = vi.mocked(spaceApi.me).mockResolvedValue(payload('Before variant') as never)
        if (accepted)
          me.mockResolvedValueOnce(payload('Before variant') as never).mockResolvedValueOnce(
            payload(`Current ${variant}`) as never,
          )
        vi.mocked(spaceApi.avatars).mockResolvedValue({ data: { items: [] } } as never)
        vi.mocked(spaceApi.sourceStylePacks).mockResolvedValue({
          data: {
            items: [
              {
                name: 'variant-space',
                target: 'personal-space',
                version: '1.0.0',
                validation: { valid: true },
              },
            ],
          },
        } as never)
        if (accepted)
          vi.mocked(spaceApi[method]).mockResolvedValue({
            code: 0,
            data: { space_id: '2001', owner_id: '1001' },
          } as never)
        else vi.mocked(spaceApi[method]).mockRejectedValue({ msg: '风格包或权限已变化' })
        const wrapper = mount(SpaceSettingsView, { global: { stubs, directives: { loading: {} } } })
        await flushPromises()
        if (variant === 'html')
          await wrapper.find('input[placeholder="<section>...</section>"]').setValue('<section>Owned HTML</section>')
        if (variant === 'zip') {
          const input = wrapper.find('input[accept=".zip,application/zip"]')
          Object.defineProperty(input.element, 'files', {
            value: [new File(['zip'], 'style.zip', { type: 'application/zip' })],
            configurable: true,
          })
          await input.trigger('change')
        }
        if (variant === 'source') await wrapper.find('button.source-pack-option').trigger('click')
        const button = wrapper.findAll('button').find((item) => item.text() === label)!
        await button.trigger('click')
        await flushPromises()
        expect(spaceApi[method]).toHaveBeenCalledOnce()
        expect(spaceApi.me).toHaveBeenCalledTimes(accepted ? 2 : 1)
        if (variant === 'html') expect(spaceApi.applyCustomHtml).toHaveBeenCalledWith('<section>Owned HTML</section>')
        if (variant === 'source') expect(spaceApi.applySourceStylePack).toHaveBeenCalledWith('variant-space')
        expect(
          wrapper
            .findAll('input')
            .some(
              (input) =>
                (input.element as HTMLInputElement).value === (accepted ? `Current ${variant}` : 'Before variant'),
            ),
        ).toBe(true)
        if (accepted) {
          expect(ElMessage.success).toHaveBeenCalledOnce()
          expect(ElMessage.error).not.toHaveBeenCalled()
        } else {
          expect(ElMessage.error).toHaveBeenCalledWith('风格包或权限已变化')
          expect(ElMessage.success).not.toHaveBeenCalled()
        }
        expect(button.attributes('disabled')).toBeUndefined()
        wrapper.unmount()
      })
    }
  }
})
