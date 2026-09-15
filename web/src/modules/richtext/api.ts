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
  listAttachments: (threadId: string) => api.get(`/richtext/articles/${threadId}/attachments`),
  uploadAttachment: (threadId: string, file: File) => {
    const form = new FormData()
    form.append('file', file)
    return api.post(`/richtext/articles/${threadId}/attachments/upload`, form, {
      headers: { 'Content-Type': 'multipart/form-data' },
    })
  },
  bindAttachment: (threadId: string, data: { asset_id: string; display_name?: string }) =>
    api.post(`/richtext/articles/${threadId}/attachments`, data),
  reorderAttachments: (threadId: string, attachmentIDs: string[]) =>
    api.patch(`/richtext/articles/${threadId}/attachments/order`, { attachment_ids: attachmentIDs }),
  renameAttachment: (threadId: string, attachmentId: string, displayName: string) =>
    api.patch(`/richtext/articles/${threadId}/attachments/${attachmentId}`, { display_name: displayName }),
  removeAttachment: (threadId: string, attachmentId: string) =>
    api.delete(`/richtext/articles/${threadId}/attachments/${attachmentId}`),
  downloadAttachment: (threadId: string, attachmentId: string) =>
    api.get(
      `/richtext/articles/${encodeURIComponent(threadId)}/attachments/${encodeURIComponent(attachmentId)}/download`,
      {
        responseType: 'blob',
      },
    ),
  listUserAssets: (status: 'active' | 'trashed' = 'active') => api.get('/assets', { params: { status } }),
  downloadUserAsset: (assetId: string) =>
    api.get(`/assets/${encodeURIComponent(assetId)}/download`, { responseType: 'blob' }),
  trashUserAsset: (assetId: string) => api.post(`/assets/${assetId}/trash`),
  restoreUserAsset: (assetId: string) => api.post(`/assets/${assetId}/restore`),
  createPDFInvocation: (
    threadId: string,
    attachmentId: string,
    presentation: 'modal' | 'drawer' | 'fullscreen' | 'new-tab',
  ) =>
    api.post(`/plugin-ui/invocations?thread_id=${encodeURIComponent(threadId)}`, {
      attachment_id: attachmentId,
      presentation,
    }),
  createPersonalAssetPDFInvocation: (assetId: string, presentation: 'modal' | 'drawer' | 'fullscreen' | 'new-tab') =>
    api.post(`/assets/${encodeURIComponent(assetId)}/pdf-invocations`, { presentation }),
  getPDFInvocation: (invocationId: string) => api.get(`/plugin-ui/invocations/${encodeURIComponent(invocationId)}`),
  getPDFInvocationContent: (invocationId: string) =>
    api.get(`/plugin-ui/invocations/${encodeURIComponent(invocationId)}/content`, { responseType: 'blob' }),
}
