import api from '../../shared/api/client'

export const personalDocumentsApi = {
  list: (status = '') => api.get('/documents', { params: status ? { status } : undefined }),
  create: (data: { name: string; format: 'text' | 'markdown' | 'campusdoc'; content: string }) => api.post('/documents', data),
  upload: (file: File, name = '') => {
    const form = new FormData()
    form.append('file', file)
    if (name) form.append('name', name)
    return api.post('/documents/upload', form, { headers: { 'Content-Type': 'multipart/form-data' }, timeout: 60000 })
  },
  content: (id: string) => api.get(`/documents/${id}/content`),
  preview: (id: string) => api.get(`/documents/${id}/preview`),
  save: (id: string, data: { expected_version: number; name?: string; content: string }) => api.put(`/documents/${id}`, data),
  trash: (id: string, expected_version: number) => api.post(`/documents/${id}/trash`, { expected_version }),
  restore: (id: string, expected_version: number) => api.post(`/documents/${id}/restore`, { expected_version }),
  versions: (id: string) => api.get(`/documents/${id}/versions`),
  restoreVersion: (id: string, versionID: string, expected_version: number) => api.post(`/documents/${id}/versions/${versionID}/restore`, { expected_version }),
  downloadURL: (id: string) => `/api/v1/documents/${id}/download`,
}
