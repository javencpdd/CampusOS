import api from '../../shared/api/client'

export const threadApi = {
  list: (params?: { page?: number; page_size?: number; category_id?: string; status?: string; content_format?: string; keyword?: string }) =>
    api.get('/admin/threads', { params }),
  get: (id: string) => api.get(`/threads/${id}`),
  update: (id: string, data: { status?: string; title?: string; content?: string }) => api.put(`/threads/${id}`, data),
  delete: (id: string) => api.delete(`/threads/${id}`),
  adminDelete: (id: string) => api.delete(`/admin/threads/${id}`),
  pin: (id: string) => api.post(`/threads/${id}/pin`),
  unpin: (id: string) => api.post(`/threads/${id}/unpin`),
  lock: (id: string) => api.post(`/threads/${id}/lock`),
  unlock: (id: string) => api.post(`/threads/${id}/unlock`),
}

export const richTextAdminApi = {
  offline: (id: string) => api.post(`/richtext/articles/${id}/admin/offline`),
  restore: (id: string) => api.post(`/richtext/articles/${id}/admin/restore`),
  delete: (id: string) => api.delete(`/richtext/articles/${id}/admin`),
}

export const categoryApi = {
  list: () => api.get('/categories'),
  get: (id: string) => api.get(`/categories/${id}`),
  create: (data: { name: string; slug?: string; description?: string; icon?: string; color?: string; default_tags?: string[]; sort_order?: number; is_closed?: boolean }) =>
    api.post('/categories', data),
  update: (id: string, data: { name?: string; slug?: string; description?: string; icon?: string; color?: string; default_tags?: string[]; sort_order?: number; is_closed?: boolean }) =>
    api.put(`/categories/${id}`, data),
  delete: (id: string) => api.delete(`/categories/${id}`),
}
