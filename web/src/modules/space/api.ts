import api from '../../shared/api/client'

export const spaceApi = {
  me: () => api.get('/spaces/me'),
  myContents: (params?: { page?: number; page_size?: number }) => api.get('/spaces/me/contents', { params }),
  updateMe: (data: {
    title?: string
    bio?: string
    avatar?: string
    cover_image?: string
    theme?: string
    layout?: string
    visibility?: string
    sync_enabled?: boolean
    sync_categories?: string[]
    sync_tags?: string[]
  }) => api.put('/spaces/me', data),
  validateStyle: (data: any) => api.post('/spaces/me/styles/validate', data),
  previewStyle: (data: any) => api.post('/spaces/me/styles/preview', data),
  exportStyle: (data?: { name?: string; version?: string; description?: string }) =>
    api.post('/spaces/me/styles/export', data || {}),
  applyStyle: (data: any) => api.post('/spaces/me/styles/apply', data),
  validateCustomHtml: (html: string) => api.post('/spaces/me/styles/html/validate', { html }),
  customHtmlExample: () => api.get('/spaces/me/styles/html-example'),
  applyCustomHtml: (html: string) => api.post('/spaces/me/styles/html/apply', { html }),
  validateStylePack: (file: File) => withFile('/spaces/me/styles/packs/validate', file),
  stylePackExample: () => api.get('/spaces/me/styles/packs/example'),
  stylePackExampleZip: () =>
    api.get('/spaces/me/styles/packs/example.zip', { responseType: 'blob', timeout: 60000 }) as Promise<Blob>,
  sourceStylePacks: () => api.get('/spaces/me/styles/packs/sources'),
  applyStylePack: (file: File) => withFile('/spaces/me/styles/packs/apply', file),
  applySourceStylePack: (name: string) => api.post('/spaces/me/styles/packs/apply-source', { name }),
  rollbackStyle: () => api.post('/spaces/me/styles/rollback'),
  restoreDefaultStyle: () => api.post('/spaces/me/styles/default'),
  syncStatus: () => api.get('/spaces/me/sync-status'),
  storage: () => api.get('/spaces/me/storage'),
  uploadAvatar: (file: File) => withFile('/spaces/me/avatar', file),
  publicByUsername: (username: string) => api.get(`/u/${username}`),
  publicContentsByUsername: (username: string, params?: { page?: number; page_size?: number }) =>
    api.get(`/u/${username}/contents`, { params }),
}

function withFile(path: string, file: File) {
  const form = new FormData()
  form.append('file', file)
  return api.post(path, form, { headers: { 'Content-Type': 'multipart/form-data' } })
}
