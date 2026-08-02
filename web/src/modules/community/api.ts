import api from '../../shared/api/client'

export const threadApi = {
  list: (params?: {
    page?: number
    page_size?: number
    category_id?: string
    content_format?: string
    thread_type?: 'discussion' | 'article' | 'mutual_aid' | 'secondhand'
    keyword?: string
  }) => api.get('/threads', { params }),
  get: (id: string) => api.get(`/threads/${id}`),
  getMine: (id: string) => api.get(`/threads/${id}/me`),
  create: (data: { title: string; content: string; category_id: string; tags?: string[]; is_private?: boolean }) =>
    api.post('/threads', data),
  update: (id: string, data: { title?: string; content?: string; tags?: string[]; status?: string }) =>
    api.put(`/threads/${id}`, data),
  delete: (id: string) => api.delete(`/threads/${id}`),
  submitForReview: (id: string) => api.post(`/threads/${id}/submit-review`),
  restoreTrash: (id: string) => api.post(`/threads/${id}/trash/restore`),
}

export const postApi = {
  list: (threadId: string, params?: { page?: number; page_size?: number }) =>
    api.get(`/threads/${threadId}/posts`, { params }),
  create: (threadId: string, data: { content: string; parent_id?: string }) =>
    api.post(`/threads/${threadId}/posts`, data),
  listMine: (threadId: string, params?: { page?: number; page_size?: number }) =>
    api.get(`/threads/${threadId}/posts/me`, { params }),
  update: (threadId: string, postId: string, data: { content: string }) =>
    api.put(`/threads/${threadId}/posts/${postId}`, data),
  delete: (threadId: string, postId: string) => api.delete(`/threads/${threadId}/posts/${postId}`),
}

export const moderationApi = {
  status: () => api.get('/moderation/status'),
  access: (threadId: string) => api.get('/moderation/me', { params: { thread_id: threadId } }),
  pin: (threadId: string) => api.post(`/moderation/threads/${threadId}/pin`),
  unpin: (threadId: string) => api.post(`/moderation/threads/${threadId}/unpin`),
  lock: (threadId: string) => api.post(`/moderation/threads/${threadId}/lock`),
  unlock: (threadId: string) => api.post(`/moderation/threads/${threadId}/unlock`),
  deletePost: (threadId: string, postId: string) => api.delete(`/moderation/threads/${threadId}/posts/${postId}`),
}

export const categoryApi = {
  list: () => api.get('/categories'),
  tree: () => api.get('/categories/tree'),
  get: (id: string) => api.get(`/categories/${id}`),
  threadTypes: (id: string) => api.get(`/categories/${id}/thread-types`),
}
