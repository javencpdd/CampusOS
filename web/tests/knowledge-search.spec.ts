// @vitest-environment jsdom
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import KnowledgeSearchView from '../src/modules/knowledge/pages/KnowledgeSearchView.vue'
import {
  knowledgeApi,
  type KnowledgeCitation,
  type KnowledgeSearchPage,
  type KnowledgeSource,
} from '../src/modules/knowledge/api'

vi.mock('../src/modules/knowledge/api', () => ({
  knowledgeApi: { collections: vi.fn(), search: vi.fn(), source: vi.fn() },
}))
const collections = [
  { id: 'kc_one', title: '校园公开资料', status: 'active', version: '1' },
  { id: 'kc_two', title: '另一集合', status: 'active', version: '1' },
]
const citation: KnowledgeCitation = {
  collection_id: 'kc_one',
  member_id: 'km_one',
  visibility_revision: 'a'.repeat(64),
  source: { kind: 'community.thread', id: '123', revision: 'b'.repeat(64) },
  generation: '1',
  chunk_index: 0,
  url: 'javascript:ignored',
}
const page = (number = 1): KnowledgeSearchPage => ({
  items: [
    {
      title: '<img src="/not-fetched">校园资料',
      snippet: '<script>unsafe()</script>公开片段',
      score: 1,
      explanation: { terms: [], title_terms: [], chunk_terms: [], title_weight: 4, chunk_weight: 1 },
      citation,
    },
  ],
  total: 21,
  page: number,
  page_size: 20,
  provider: 'builtin.lexical/v1',
  generation: '1',
})
const source: KnowledgeSource = {
  collection_id: 'kc_one',
  member_id: 'km_one',
  revision: citation.visibility_revision,
  source: {
    reference: citation.source,
    category_id: '1',
    title: '当前原文',
    content: '<img src="/must-not-fetch" onerror="unsafe()">CURRENT_PUBLIC_TEXT',
    format: 'markdown',
    url: 'javascript:unsafe()',
  },
}
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: unknown) => void
  const promise = new Promise<T>((r, j) => {
    resolve = r
    reject = j
  })
  return { promise, resolve, reject }
}
const wrappers: VueWrapper[] = []
const render = () => {
  const w = mount(KnowledgeSearchView, {
    attachTo: document.body,
    global: { stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' } } },
  })
  wrappers.push(w)
  return w
}
async function prepare() {
  const w = render()
  await flushPromises()
  await w.get('select').setValue('kc_one')
  await w.get('input').setValue('校园')
  return w
}
async function submit(w: VueWrapper) {
  await w.get('form').trigger('submit')
  await flushPromises()
}
const button = (w: VueWrapper, label: string) => w.findAll('button').find((b) => b.text() === label)!

beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(knowledgeApi.collections).mockResolvedValue(collections)
  vi.mocked(knowledgeApi.search).mockResolvedValue(page())
  vi.mocked(knowledgeApi.source).mockResolvedValue(source)
})
afterEach(() => {
  wrappers.splice(0).forEach((w) => w.unmount())
  vi.restoreAllMocks()
})

describe('public Knowledge search vertical slice', () => {
  it('distinguishes loading, unconfigured and disabled collections with a working reload', async () => {
    const pending = deferred<typeof collections>()
    vi.mocked(knowledgeApi.collections).mockReturnValueOnce(pending.promise)
    const w = render()
    expect(w.text()).toContain('正在加载公开集合')
    pending.resolve([])
    await flushPromises()
    expect(w.text()).toContain('尚未配置公开知识集合')
    expect(w.find('form').exists()).toBe(false)
    vi.mocked(knowledgeApi.collections).mockRejectedValueOnce({ machineCode: 'knowledge.disabled' })
    await button(w, '重新加载集合').trigger('click')
    await flushPromises()
    expect(w.text()).toContain('知识检索暂未启用')
    await button(w, '重新加载集合').trigger('click')
    await flushPromises()
    expect(w.find('form').exists()).toBe(true)
    expect(w.get('select').element.value).toBe('')
    expect(knowledgeApi.search).not.toHaveBeenCalled()
  })
  it('retries a collection transport failure without disclosing internal error text', async () => {
    vi.mocked(knowledgeApi.collections).mockRejectedValueOnce(Error('INTERNAL_SECRET'))
    const w = render()
    await flushPromises()
    expect(w.text()).toContain('公开集合暂不可用')
    expect(w.text()).not.toContain('INTERNAL_SECRET')
    await button(w, '重新加载集合').trigger('click')
    await flushPromises()
    expect(w.find('select').exists()).toBe(true)
  })
  it('searches only an explicit selection, pages correctly and never renders source markup', async () => {
    const w = await prepare()
    await submit(w)
    expect(knowledgeApi.search).toHaveBeenLastCalledWith('kc_one', '校园', 1, expect.any(AbortSignal))
    expect(w.text()).toContain('找到 21 条公开资料')
    expect(w.find('img').exists()).toBe(false)
    expect(w.find('script').exists()).toBe(false)
    await button(w, '查看当前引用').trigger('click')
    await flushPromises()
    expect(knowledgeApi.source).toHaveBeenCalledWith(citation, expect.any(AbortSignal))
    expect(w.get('pre').text()).toContain('CURRENT_PUBLIC_TEXT')
    expect(document.activeElement?.getAttribute('aria-labelledby')).toBe('current-source-title')
    expect(w.get('a').attributes('href')).toBe('/threads/123')
    expect(w.find('img').exists()).toBe(false)
    vi.mocked(knowledgeApi.search).mockResolvedValueOnce(page(2))
    await button(w, '下一页').trigger('click')
    await flushPromises()
    expect(knowledgeApi.search).toHaveBeenLastCalledWith('kc_one', '校园', 2, expect.any(AbortSignal))
    expect(w.find('pre').exists()).toBe(false)
    expect(button(w, '下一页').attributes('disabled')).toBeDefined()
  })
  it('drops previous results immediately, shows a real loading state, and retries a failed search', async () => {
    const w = await prepare()
    await submit(w)
    await button(w, '查看当前引用').trigger('click')
    await flushPromises()
    const pending = deferred<KnowledgeSearchPage>()
    vi.mocked(knowledgeApi.search).mockReturnValueOnce(pending.promise)
    await w.get('form').trigger('submit')
    expect(w.text()).toContain('正在检索')
    expect(w.find('pre').exists()).toBe(false)
    expect(w.find('.knowledge-hit').exists()).toBe(false)
    pending.reject(Error('PRIVATE_INTERNAL'))
    await flushPromises()
    expect(w.text()).toContain('检索暂不可用')
    expect(w.text()).not.toContain('PRIVATE_INTERNAL')
    await button(w, '重试检索').trigger('click')
    await flushPromises()
    expect(w.text()).toContain('找到 21 条公开资料')
  })
  it('ignores a late search result even if its transport ignores cancellation', async () => {
    const w = await prepare()
    const pending = deferred<KnowledgeSearchPage>()
    vi.mocked(knowledgeApi.search).mockReturnValueOnce(pending.promise)
    await w.get('form').trigger('submit')
    const signal = vi.mocked(knowledgeApi.search).mock.calls[0][3]!
    await w.get('select').setValue('kc_two')
    expect(signal.aborted).toBe(true)
    pending.resolve(page())
    await flushPromises()
    expect(w.find('.knowledge-hit').exists()).toBe(false)
    expect(w.text()).not.toContain('找到 21')
  })
  it('ignores late source bytes after query/collection changes or unmount', async () => {
    const w = await prepare()
    await submit(w)
    const pending = deferred<KnowledgeSource>()
    vi.mocked(knowledgeApi.source).mockReturnValueOnce(pending.promise)
    await button(w, '查看当前引用').trigger('click')
    const signal = vi.mocked(knowledgeApi.source).mock.calls[0][1]!
    await w.get('input').setValue('其他关键词')
    expect(signal.aborted).toBe(true)
    pending.resolve(source)
    await flushPromises()
    expect(w.find('pre').exists()).toBe(false)
    expect(w.text()).not.toContain('CURRENT_PUBLIC_TEXT')
    await submit(w)
    const second = deferred<KnowledgeSource>()
    vi.mocked(knowledgeApi.source).mockReturnValueOnce(second.promise)
    await button(w, '查看当前引用').trigger('click')
    const lastSignal = vi.mocked(knowledgeApi.source).mock.calls.at(-1)![1]!
    w.unmount()
    expect(lastSignal.aborted).toBe(true)
    second.resolve(source)
    await flushPromises()
  })
  it.each([404, 409])('clears source and snippets on rejected current citation (%s)', async (status) => {
    const w = await prepare()
    await submit(w)
    await button(w, '查看当前引用').trigger('click')
    await flushPromises()
    vi.mocked(knowledgeApi.source).mockRejectedValueOnce({ status })
    await button(w, '查看当前引用').trigger('click')
    await flushPromises()
    expect(w.find('pre').exists()).toBe(false)
    expect(w.find('.knowledge-hit').exists()).toBe(false)
    expect(w.text()).not.toContain('CURRENT_PUBLIC_TEXT')
    expect(w.text()).toContain('重新搜索')
    await button(w, '重新搜索').trigger('click')
    await flushPromises()
    expect(knowledgeApi.search).toHaveBeenCalledTimes(2)
  })
  it('never accepts a mismatched citation response and retries the exact proof', async () => {
    const w = await prepare()
    await submit(w)
    vi.mocked(knowledgeApi.source).mockResolvedValueOnce({ ...source, revision: 'different' })
    await button(w, '查看当前引用').trigger('click')
    await flushPromises()
    expect(w.find('pre').exists()).toBe(false)
    await button(w, '重试引用').trigger('click')
    await flushPromises()
    expect(w.get('pre').text()).toContain('CURRENT_PUBLIC_TEXT')
    expect(
      vi.mocked(knowledgeApi.source).mock.calls.every(([q]) => q.visibility_revision === citation.visibility_revision),
    ).toBe(true)
  })
  it('clears all public buffers on hot disable and on hiding the page', async () => {
    const w = await prepare()
    await submit(w)
    await button(w, '查看当前引用').trigger('click')
    await flushPromises()
    vi.spyOn(document, 'hidden', 'get').mockReturnValue(true)
    document.dispatchEvent(new Event('visibilitychange'))
    await flushPromises()
    expect(w.find('pre').exists()).toBe(false)
    expect(w.find('.knowledge-hit').exists()).toBe(false)
    await submit(w)
    vi.mocked(knowledgeApi.source).mockRejectedValueOnce({ machineCode: 'knowledge.disabled' })
    await button(w, '查看当前引用').trigger('click')
    await flushPromises()
    expect(w.text()).toContain('知识检索暂未启用')
    expect(w.find('form').exists()).toBe(false)
  })
  it('shows empty results and blocks oversized input before any HTTP request', async () => {
    const w = await prepare()
    vi.mocked(knowledgeApi.search).mockResolvedValueOnce({ ...page(), items: [], total: 0 })
    await submit(w)
    expect(w.text()).toContain('没有匹配的公开资料')
    await w.get('input').setValue('校'.repeat(400))
    await submit(w)
    expect(w.text()).toContain('搜索词过长')
    expect(knowledgeApi.search).toHaveBeenCalledTimes(1)
  })
})
