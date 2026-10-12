// @vitest-environment jsdom
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { checkedImageHTML, checkedImageRequest, loadCheckedImage } from '../src/modules/content-editor/checked-images'
import { clearAccessToken, setAccessToken } from '../src/modules/identity/session'
import CheckedHTML from '../src/modules/content-editor/CheckedHTML.vue'
import CheckedImage from '../src/modules/content-editor/CheckedImage.vue'

const source = '/api/v1/content/assets/images/1001/abc.png'
const response = () => ({
  ok: true,
  headers: new Headers({ 'Content-Type': 'image/png' }),
  blob: async () => new Blob(['png']),
})

describe('checked platform images', () => {
  beforeEach(() => {
    clearAccessToken()
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response()))
    URL.createObjectURL = vi.fn().mockReturnValue('blob:checked-image')
    URL.revokeObjectURL = vi.fn()
  })
  afterEach(() => vi.unstubAllGlobals())

  it('binds public article images to the actual parent and ignores stored selectors', () => {
    expect(checkedImageRequest(source + '?thread_id=other&token=secret', { threadId: '1234', article: true })).toBe(
      '/api/v1/richtext/assets/1001/abc.png?thread_id=1234&store=content',
    )
    expect(checkedImageRequest('/api/v1/spaces/files/1001/avatars/avatar.png?owner_id=other')).toBe(
      '/api/v1/spaces/files/1001/avatars/avatar.png',
    )
    expect(checkedImageRequest('https://external.test' + source)).toBeNull()
    expect(checkedImageRequest('/api/v1/documents/1001/download')).toBeNull()
    expect(checkedImageRequest('/api/v1/content/assets/images/1001/../../admin.png')).toBeNull()
  })

  it('sends credentials only to a checked same-origin endpoint', async () => {
    setAccessToken('current-session-token')
    await loadCheckedImage(source, {}, new AbortController().signal)
    expect(fetch).toHaveBeenCalledWith(
      source,
      expect.objectContaining({ headers: { Authorization: 'Bearer current-session-token' }, cache: 'no-store' }),
    )
    vi.mocked(fetch).mockClear()
    expect(await loadCheckedImage('https://external.test/image.png', {}, new AbortController().signal)).toEqual({
      url: 'https://external.test/image.png',
      owned: false,
    })
    expect(fetch).not.toHaveBeenCalled()
  })

  it('never renders the raw private URL before authorization and releases bytes on logout', async () => {
    setAccessToken('current')
    const wrapper = mount(CheckedHTML, { props: { html: `<p>body</p><img src="${source}">`, threadId: '1234' } })
    expect(wrapper.get('img').attributes('src')).toBeUndefined()
    await flushPromises()
    expect(wrapper.get('img').attributes('src')).toBe('blob:checked-image')
    vi.mocked(fetch).mockResolvedValue({ ...response(), ok: false } as Response)
    clearAccessToken()
    await flushPromises()
    expect(wrapper.get('img').attributes('src')).toBeUndefined()
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:checked-image')
    wrapper.unmount()
  })

  it('rejects a successful in-flight response when its current session changes', async () => {
    setAccessToken('before')
    let complete: ((response: Response) => void) | undefined
    vi.mocked(fetch).mockReturnValue(
      new Promise<Response>((resolve) => {
        complete = resolve
      }),
    )
    const result = loadCheckedImage(source, {}, new AbortController().signal)
    setAccessToken('after')
    complete?.(response() as Response)
    await expect(result).rejects.toThrow('会话已变化')
    expect(URL.createObjectURL).not.toHaveBeenCalled()
  })

  it('keeps denied images empty and aborts old work when the component changes', async () => {
    vi.mocked(fetch).mockResolvedValue({ ...response(), ok: false } as Response)
    const wrapper = mount(CheckedImage, { props: { src: source } })
    await flushPromises()
    expect(wrapper.attributes('src')).toBeUndefined()
    const signal = vi.mocked(fetch).mock.calls[0][1]?.signal
    await wrapper.setProps({ src: source.replace('abc', 'other') })
    expect(signal?.aborted).toBe(true)
    wrapper.unmount()
  })

  it('retains external images without relaying tokens and ignores forged loader attributes', () => {
    const html = checkedImageHTML(
      `<img src="${source}" data-checked-image-source="evil"><img src="https://external.test/x.png"><p data-checked-image-source="evil">body</p>`,
    )
    expect(html).toContain(`data-checked-image-source="${source}"`)
    expect(html).not.toContain(`src="${source}"`)
    expect(html).not.toContain('evil')
    expect(html).toContain('src="https://external.test/x.png"')
  })
})
