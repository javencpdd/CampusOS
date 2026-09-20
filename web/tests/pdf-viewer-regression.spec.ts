// @vitest-environment jsdom
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import PdfViewer from '../src/modules/pdf-viewer/pages/PdfViewerView.vue'

const mocks = vi.hoisted(() => ({
  getDocument: vi.fn(),
  summary: vi.fn(),
  getRecord: vi.fn(),
  createRecord: vi.fn(),
  updateRecord: vi.fn(),
  ensureConsent: vi.fn(),
  download: vi.fn(),
}))
vi.mock('pdfjs-dist', () => ({ getDocument: mocks.getDocument, GlobalWorkerOptions: {} }))
vi.mock('pdfjs-dist/build/pdf.worker.min.mjs?url', () => ({ default: '/worker.mjs' }))
vi.mock('../src/modules/richtext/api', () => ({
  richTextApi: { getPDFInvocation: mocks.summary, downloadPDFInvocation: mocks.download },
}))
vi.mock('../src/modules/identity/session', () => ({ getAccessToken: () => 'test-only' }))
vi.mock('../src/modules/plugin-center/api', () => ({
  pluginCenterApi: { getRecord: mocks.getRecord, createRecord: mocks.createRecord, updateRecord: mocks.updateRecord },
}))
vi.mock('../src/modules/pdf-viewer/authorization', () => ({ ensurePDFViewerConsent: mocks.ensureConsent }))
vi.mock('element-plus', () => ({ ElMessage: { info: vi.fn(), warning: vi.fn(), success: vi.fn() } }))

const render = vi.fn(() => ({ promise: Promise.resolve(), cancel: vi.fn() }))
class PrivatePDF {
  #pages = 2
  get numPages() {
    return this.#pages
  }
  async getOutline() {
    return []
  }
  async getPage() {
    return { getViewport: () => ({ width: 100, height: 100 }), render }
  }
  async destroy() {}
}
const wrappers: ReturnType<typeof mount>[] = []
const open = () => {
  const wrapper = mount(PdfViewer, {
    props: { invocationId: 'one' },
    global: {
      directives: { loading: () => undefined },
      stubs: {
        ElButton: { template: '<button><slot /></button>' },
        ElAlert: { props: ['title'], template: '<p>{{ title }}</p>' },
        ElInputNumber: true,
        ElInput: true,
        ElEmpty: true,
        ElDropdown: true,
        ElDropdownItem: true,
        ElDropdownMenu: true,
      },
    },
  })
  wrappers.push(wrapper)
  return wrapper
}

describe('PDF lifecycle regression', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue({} as never)
    mocks.summary.mockResolvedValue({
      data: {
        invocation: { context_kind: 'article_attachment' },
        attachment: { id: 'asset', display_name: '测试.pdf', asset: { storage_object_id: 'object-1' } },
      },
    })
    mocks.getDocument.mockImplementation(() => ({ promise: Promise.resolve(new PrivatePDF()), destroy: vi.fn() }))
    mocks.ensureConsent.mockResolvedValue(undefined)
    mocks.getRecord.mockRejectedValue({ response: { status: 404 } })
  })
  afterEach(() => {
    wrappers.splice(0).forEach((wrapper) => wrapper.unmount())
    vi.restoreAllMocks()
  })
  it('keeps private PDF.js fields usable and renders the first canvas', async () => {
    const wrapper = open()
    await flushPromises()
    expect(wrapper.find('canvas').exists()).toBe(true)
    expect(render).toHaveBeenCalledTimes(1)
    expect(mocks.getDocument).toHaveBeenCalledWith(expect.objectContaining({ isEvalSupported: false }))
  })
  it('continues reading when browser storage is blocked', async () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('blocked')
    })
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('blocked')
    })
    open()
    await flushPromises()
    expect(render).toHaveBeenCalledTimes(1)
  })
  it('uses the approved plugin-record namespace instead of browser persistent storage', async () => {
    const getItem = vi.spyOn(Storage.prototype, 'getItem')
    open()
    await flushPromises()
    expect(mocks.ensureConsent).toHaveBeenCalledWith('plugin_record.self.read')
    expect(mocks.ensureConsent).toHaveBeenCalledWith('plugin_record.self.write')
    expect(mocks.getRecord).toHaveBeenCalledWith('builtin.pdf-viewer', 'reading_positions', 'pdf-a-object-1')
    expect(getItem).not.toHaveBeenCalled()
  })
  it('ignores a stale summary after a different invocation is selected', async () => {
    let resolveOld!: (value: unknown) => void
    mocks.summary.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveOld = resolve
        }),
    )
    const wrapper = open()
    await flushPromises()
    await wrapper.setProps({ invocationId: 'two' })
    await flushPromises()
    resolveOld({ data: { attachment: { display_name: '旧文件.pdf' } } })
    await flushPromises()
    expect(wrapper.text()).not.toContain('旧文件.pdf')
    expect(mocks.getDocument).toHaveBeenCalledTimes(1)
  })
  it('destroys an unfinished loading task on unmount', async () => {
    const destroy = vi.fn()
    mocks.getDocument.mockReturnValue({ promise: new Promise(() => undefined), destroy })
    const wrapper = open()
    await flushPromises()
    wrapper.unmount()
    wrappers.splice(wrappers.indexOf(wrapper), 1)
    expect(destroy).toHaveBeenCalledTimes(1)
  })
  it('uses host authenticated download even when the preview summary expires', async () => {
    mocks.summary.mockRejectedValueOnce({ msg: '预览已过期' })
    mocks.download.mockRejectedValueOnce({ msg: '测试不产生真实下载' })
    const wrapper = open()
    await flushPromises()
    await wrapper
      .findAll('button')
      .find((button) => button.text() === '下载')!
      .trigger('click')
    await flushPromises()
    expect(mocks.download).toHaveBeenCalledWith('one')
    expect(mocks.getDocument).not.toHaveBeenCalled()
  })
})
