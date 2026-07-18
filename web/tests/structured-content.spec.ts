// @vitest-environment jsdom
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import SafeContentEditor from '../src/modules/community/components/SafeContentEditor.vue'
import {
  contentApi,
  contentExcerpt,
  hasMeaningfulContent,
  htmlToPlainText,
  plainTextToHTML,
} from '../src/modules/community/content'

vi.mock('element-plus', () => ({ ElMessage: { error: vi.fn(), success: vi.fn() } }))

describe('structured image-text content helpers', () => {
  it('converts plain text without interpreting embedded markup', () => {
    expect(plainTextToHTML('<script>bad()</script>\nnext')).toBe('<p>&lt;script&gt;bad()&lt;/script&gt;<br>next</p>')
  })

  it('builds safe list excerpts from HTML content', () => {
    const html = '<h2>台灯实拍</h2><p>成色良好，图书馆附近交易。</p><img src="/asset.png">'
    expect(htmlToPlainText(html)).toContain('台灯实拍')
    expect(contentExcerpt(html, 'safe_html')).toBe('台灯实拍成色良好，图书馆附近交易。')
    expect(hasMeaningfulContent('<p><br></p>', 'safe_html')).toBe(false)
  })

  it('uploads an image, inserts it into HTML and exposes a visual preview', async () => {
    vi.spyOn(contentApi, 'uploadImage').mockResolvedValueOnce({
      data: {
        file_url: '/api/v1/content/assets/images/user-1/image.png',
        file_name: 'image.png',
        file_size: 120,
        mime_type: 'image/png',
      },
    } as never)
    const wrapper = mount(SafeContentEditor, {
      props: { modelValue: '<p>物品说明</p>', contentFormat: 'safe_html' },
      global: {
        stubs: {
          'el-segmented': true,
          'el-tooltip': { template: '<span><slot /></span>' },
          'el-button': { template: '<button @click="$emit(\'click\')"><slot /></button>' },
          'el-input': true,
          'el-image': true,
          'el-drawer': true,
        },
      },
    })
    const input = wrapper.get('input[type="file"]')
    Object.defineProperty(input.element, 'files', {
      value: [new File(['image'], 'image.png', { type: 'image/png' })],
      configurable: true,
    })
    await input.trigger('change')
    await flushPromises()

    expect(contentApi.uploadImage).toHaveBeenCalledOnce()
    expect(String(wrapper.emitted('update:modelValue')?.at(-1)?.[0])).toContain(
      '/api/v1/content/assets/images/user-1/image.png',
    )
    expect(wrapper.find('.image-preview-strip').exists()).toBe(true)
    expect(wrapper.get('el-image-stub').attributes('src')).toBe('/api/v1/content/assets/images/user-1/image.png')
  })
})
