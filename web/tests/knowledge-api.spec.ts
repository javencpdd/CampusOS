import { afterEach, describe, expect, it, vi } from 'vitest'
import { knowledgeApi, type KnowledgeCitation } from '../src/modules/knowledge/api'

const citation: KnowledgeCitation = {
  collection_id: 'kc_public',
  member_id: 'km_current',
  visibility_revision: 'a'.repeat(64),
  source: { kind: 'community.thread', id: '123', revision: 'b'.repeat(64) },
  generation: '1',
  chunk_index: 0,
  url: 'javascript:ignored',
}
afterEach(() => vi.unstubAllGlobals())
describe('anonymous Knowledge HTTP', () => {
  it('sends exact current citation proof and no identity or cache', async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValue({ ok: true, status: 200, json: async () => ({ code: 0, data: { current: true } }) })
    vi.stubGlobal('fetch', fetcher)
    const controller = new AbortController()
    await knowledgeApi.collections(controller.signal)
    await knowledgeApi.search('kc_public', '校园 图书馆', 2, controller.signal)
    await knowledgeApi.source(citation, controller.signal)
    expect(fetcher.mock.calls[0][0]).toBe('/api/v1/knowledge/collections')
    const search = new URL(fetcher.mock.calls[1][0], 'http://localhost')
    expect(Object.fromEntries(search.searchParams)).toEqual({ query: '校园 图书馆', page: '2', page_size: '20' })
    expect(fetcher.mock.calls[2][0]).toBe(
      `/api/v1/knowledge/collections/kc_public/sources/km_current?revision=${'a'.repeat(64)}`,
    )
    for (const [, options] of fetcher.mock.calls) {
      expect(options).toEqual({
        credentials: 'omit',
        cache: 'no-store',
        redirect: 'error',
        headers: { Accept: 'application/json' },
        signal: controller.signal,
      })
    }
  })
  it('retains machine error codes for page states and never returns a failed body', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: false,
        status: 503,
        json: async () => ({
          code: 1,
          error: { code: 'knowledge.disabled', message: 'disabled' },
          data: { stale: true },
        }),
      }),
    )
    await expect(knowledgeApi.collections()).rejects.toMatchObject({ status: 503, machineCode: 'knowledge.disabled' })
  })
})
