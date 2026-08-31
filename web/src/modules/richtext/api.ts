import api from '../../shared/api/client'

export const richTextApi = {
  status: () => api.get('/richtext/status'),
  createDraft: (data: {
    title: string
    summary?: string
    cover_url?: string
    category_id: string
    tags?: string[]
    content_html: string
    content_json?: any
  }) => api.post('/richtext/articles', data),
  getPublished: (threadId: string) => api.get(`/richtext/articles/${threadId}`),
  getMine: (threadId: string) => api.get(`/richtext/articles/${threadId}/me`),
  updateDraft: (
    threadId: string,
    data: {
      title: string
      summary?: string
      cover_url?: string
      category_id?: string
      tags?: string[]
      content_html: string
      content_json?: any
    },
  ) => api.put(`/richtext/articles/${threadId}`, data),
  preview: (content_html: string) => api.post('/richtext/preview', { content_html }),
  publish: (threadId: string) => api.post(`/richtext/articles/${threadId}/publish`),
  offline: (threadId: string) => api.post(`/richtext/articles/${threadId}/offline`),
  delete: (threadId: string) => api.delete(`/richtext/articles/${threadId}`),
  listMyAssets: () => api.get('/richtext/assets/me'),
  uploadAsset: (file: File, data?: { thread_id?: string; article_content_id?: string }) => {
    const form = new FormData()
    form.append('file', file)
    if (data?.thread_id) form.append('thread_id', data.thread_id)
    if (data?.article_content_id) form.append('article_content_id', data.article_content_id)
    return api.post('/richtext/assets', form, { headers: { 'Content-Type': 'multipart/form-data' } })
  },
}
