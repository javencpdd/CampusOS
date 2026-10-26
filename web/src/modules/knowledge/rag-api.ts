import api from '../../shared/api/client'

export interface RAGModel {
  profile_id: string
  profile_revision: string
  binding_revision: string
  definition: string
  model: string
  endpoint: string
  system_prompt: string
  max_tokens: number
}
export interface RAGRequest {
  selection_id?: string
  revision?: string
  collection_id?: string
  question: string
  run_id: string
  binding_revision: string
  step: 'chat'
}
export interface RAGReview {
  status: string
  reason?: string
  approval_id: string
  digest: string
  expires_at_ms: number
  request: RAGRequest
  model: RAGModel
  content: string
}
export interface RAGAnswer {
  status: string
  reason?: string
  answer: string
  run_id?: string
  citations: {
    id: string
    title: string
    excerpt: string
    selection_id?: string
    revision?: string
    citation?: import('./api').KnowledgeCitation
  }[]
}
const options = (signal?: AbortSignal) => ({ signal, timeout: 75000, headers: { 'Cache-Control': 'no-store' } })
export const ragApi = {
  status: (signal?: AbortSignal) => api.get('/knowledge/rag/status', options(signal)),
  publicAnswer: (body: RAGRequest, signal?: AbortSignal) => api.post('/knowledge/rag/public', body, options(signal)),
  prepare: (body: RAGRequest, signal?: AbortSignal) =>
    api.post('/knowledge/private/rag/prepare', body, options(signal)),
  answer: (review: RAGReview, signal?: AbortSignal) =>
    api.post(
      '/knowledge/private/rag/answer',
      {
        ...review.request,
        step: 'chat',
        approval_id: review.approval_id,
        review_digest: review.digest,
        consented: true,
      },
      options(signal),
    ),
  revoke: (approval_id: string) => api.post('/knowledge/private/rag/revoke', { approval_id }, options()),
}
