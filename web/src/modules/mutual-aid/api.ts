import api from '../../shared/api/client'

export type AidType = 'request' | 'offer' | 'volunteer' | 'resource_share'
export type AidStatus = 'open' | 'in_progress' | 'resolved' | 'closed'
export type ContactMode = 'comment' | 'in_app' | 'email' | 'other'

export type MutualAidDetail = {
  thread_id: string
  aid_type: AidType
  aid_status: AidStatus
  deadline?: string
  location_scope?: string
  contact_mode: ContactMode
  version: number
  created_by: string
  created_at: string
  updated_at: string
}

export type MutualAidResult = {
  thread: Record<string, any>
  detail: MutualAidDetail
}

export type MutualAidRequest = {
  title: string
  content: string
  content_format: 'markdown' | 'safe_html'
  category_id?: string
  tags?: string[]
  aid_type: AidType
  deadline?: string | null
  location_scope?: string
  contact_mode: ContactMode
  version?: number
}

export const mutualAidApi = {
  status: () => api.get('/mutual-aid/status'),
  list: (params?: { page?: number; page_size?: number; category_id?: string; keyword?: string; tag?: string }) =>
    api.get('/mutual-aid/threads', { params }),
  get: (id: string) => api.get(`/mutual-aid/threads/${id}`),
  getMine: (id: string) => api.get(`/mutual-aid/threads/${id}/me`),
  create: (data: MutualAidRequest) => api.post('/mutual-aid/threads', data),
  update: (id: string, data: MutualAidRequest) => api.put(`/mutual-aid/threads/${id}`, data),
  updateStatus: (id: string, data: { aid_status: AidStatus; version: number }) =>
    api.post(`/mutual-aid/threads/${id}/status`, data),
}
