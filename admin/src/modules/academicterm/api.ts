import api from '@/shared/api/client'

export type AcademicTerm = {
  id: string
  year: number
  semester: 'spring' | 'fall'
  display_name: string
  first_week_start: string
  status: 'open' | 'closed'
  is_default: boolean
  version: number
  created_at: string
  updated_at: string
  closed_at?: string
  schedule_reference_count?: number
}

export const academicTermApi = {
  list: () => api.get('/admin/academic-terms'),
  create: (data: Record<string, unknown>) => api.post('/admin/academic-terms', data),
  updateFirstWeek: (id: string, data: Record<string, unknown>) => api.put(`/admin/academic-terms/${encodeURIComponent(id)}`, data),
  open: (id: string, data: Record<string, unknown>) => api.post(`/admin/academic-terms/${encodeURIComponent(id)}/open`, data),
  close: (id: string, data: Record<string, unknown>) => api.post(`/admin/academic-terms/${encodeURIComponent(id)}/close`, data),
  setDefault: (id: string, data: Record<string, unknown>) => api.post(`/admin/academic-terms/${encodeURIComponent(id)}/default`, data),
  remove: (id: string, data: Record<string, unknown>) => api.delete(`/admin/academic-terms/${encodeURIComponent(id)}`, { data }),
}
