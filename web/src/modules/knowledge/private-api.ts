import api from '../../shared/api/client'
export interface PrivateSource {
  kind: string
  source_id: string
  revision: string
  format: string
  title?: string
  text?: string
}
export interface PrivateSelection {
  title?: string
  id: string
  kind: string
  source_id: string
  revision: string
  format: string
  state: string
  task_state: string
  attempts: number
  error_code: string
}
export interface PrivateHit {
  id: string
  kind: string
  source_id: string
  revision: string
  title: string
  snippet: string
  score: number
}
export interface PrivateStatus {
  can_index: boolean
  reason: string
  default_inclusion: false
  outbound_authorized: false
}
const base = '/knowledge/private'
// Private queries and references live in POST bodies; never URLs or storage.
export const privateKnowledgeApi = {
  status: (signal?: AbortSignal) => api.get(base + '/status', { signal, headers: { 'Cache-Control': 'no-store' } }),
  list: (signal?: AbortSignal) => api.get(base + '/selections', { signal }),
  preview: (kind: string, source_id: string, signal?: AbortSignal) =>
    api.post(base + '/preview', { kind, source_id }, { signal }),
  select: (source: PrivateSource, signal?: AbortSignal) =>
    api.post(
      base + '/selections',
      { kind: source.kind, source_id: source.source_id, revision: source.revision, consent: true },
      { signal },
    ),
  withdraw: (id: string, signal?: AbortSignal) =>
    api.delete(base + '/selections/' + encodeURIComponent(id), { signal }),
  retry: (id: string, signal?: AbortSignal) =>
    api.post(base + '/selections/' + encodeURIComponent(id) + '/retry', undefined, { signal }),
  search: (query: string, signal?: AbortSignal) => api.post(base + '/search', { query }, { signal }),
  source: (hit: PrivateHit, signal?: AbortSignal) =>
    api.post(base + '/selections/' + encodeURIComponent(hit.id) + '/source', { revision: hit.revision }, { signal }),
  choices: (kind: string, signal?: AbortSignal) =>
    api.get(kind === 'personal_document' ? '/documents' : '/me/threads', {
      signal,
      params: kind === 'personal_document' ? undefined : { page: 1, page_size: 50 },
    }),
}
