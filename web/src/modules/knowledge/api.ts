import { parseAPIError } from '../../shared/api/error'
import type {
  KnowledgeCitation,
  KnowledgeSearchPage,
  KnowledgeSource,
} from '../../../../sdk/typescript/src/knowledge-public'

export type { KnowledgeCitation, KnowledgeSearchPage, KnowledgeSource }
export interface PublicCollection {
  id: string
  title: string
  version: string
  status: string
}

// These endpoints always use anonymous-public authority, independently of the
// surrounding signed-in shell. Never attach identity, cookies or cached bodies.
async function readPublic<T>(path: string, signal?: AbortSignal): Promise<T> {
  const response = await fetch(`/api/v1/knowledge${path}`, {
    credentials: 'omit',
    cache: 'no-store',
    redirect: 'error',
    headers: { Accept: 'application/json' },
    signal,
  })
  const payload = await response.json()
  if (!response.ok || payload?.code !== 0 || payload.data === undefined) {
    throw parseAPIError(payload, response.status, '公开知识暂不可用')
  }
  return payload.data as T
}

export const knowledgeApi = {
  collections: (signal?: AbortSignal) => readPublic<PublicCollection[]>('/collections', signal),
  search: (collection: string, query: string, page: number, signal?: AbortSignal) => {
    const params = new URLSearchParams({ query, page: String(page), page_size: '20' })
    return readPublic<KnowledgeSearchPage>(`/collections/${encodeURIComponent(collection)}/search?${params}`, signal)
  },
  source: (citation: KnowledgeCitation, signal?: AbortSignal) =>
    readPublic<KnowledgeSource>(
      `/collections/${encodeURIComponent(citation.collection_id)}/sources/${encodeURIComponent(citation.member_id)}?revision=${encodeURIComponent(citation.visibility_revision)}`,
      signal,
    ),
}
