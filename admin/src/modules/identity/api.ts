import api from '../../shared/api/client'

export const authApi = {
  login: (data: { email: string; password: string }) => api.post('/auth/login', data),
  refresh: () => api.post('/auth/refresh'),
  logout: () => api.post('/auth/logout'),
  me: () => api.get('/auth/me'),
  roles: (userID: string) => api.get(`/users/${userID}/roles`),
}

export const userApi = {
  list: (params?: { page?: number; page_size?: number }) => api.get('/users', { params }),
  get: (id: string) => api.get(`/users/${id}`),
  suspend: (id: string) => api.post(`/users/${id}/suspend`),
  activate: (id: string) => api.post(`/users/${id}/activate`),
}

export const roleApi = {
  list: () => api.get('/roles'),
  getUserRoles: (userId: string) => api.get(`/users/${userId}/roles`),
  assign: (userId: string, roleId: number) => api.post(`/users/${userId}/roles`, { role_id: roleId }),
  revoke: (userId: string, roleId: number) => api.delete(`/users/${userId}/roles`, { data: { role_id: roleId } }),
  permissions: () => api.get('/permissions'),
  getPermissions: (roleId: number) => api.get(`/roles/${roleId}/permissions`),
  updatePermissions: (roleId: number, permissionCodes: string[]) =>
    api.put(`/roles/${roleId}/permissions`, { permission_codes: permissionCodes }),
  createCustom: (data: { name: string; description?: string; permission_codes: string[] }) => api.post('/roles', data),
  authorizationAudits: (limit = 100) => api.get('/authorization-audits', { params: { limit } }),
}

export const moderationApi = {
  list: () => api.get('/moderation/admin/moderators'),
  get: (userId: string) => api.get(`/moderation/admin/moderators/${userId}`),
  update: (userId: string, categoryIds: string[]) => api.put(`/moderation/admin/moderators/${userId}`, { category_ids: categoryIds }),
  status: () => api.get('/moderation/status'),
}

export const identityRecoveryApi = {
  cases: (limit = 100) => api.get('/identity/recovery-cases', { params: { limit } }),
  createCase: (data: { user_id: string; email: string; proof_reference: string }) => api.post('/identity/recovery-cases', data),
  cancelCase: (id: string) => api.post(`/identity/recovery-cases/${encodeURIComponent(id)}/cancel`),
  sessions: (userId: string) => api.get(`/identity/users/${encodeURIComponent(userId)}/sessions`),
  revokeAllSessions: (userId: string) => api.post(`/identity/users/${encodeURIComponent(userId)}/sessions/revoke-all`),
}

export type ChallengePolicy = {
  id: string
  email_window_minutes: number
  email_max_requests: number
  ip_window_minutes: number
  ip_max_requests: number
  version: number
  updated_by?: string
  updated_at: string
}

export const challengePolicyApi = {
  get: () => api.get('/identity/challenge-policy'),
  update: (data: {
    email_window_minutes: number
    email_max_requests: number
    ip_window_minutes: number
    ip_max_requests: number
    expected_version: number
  }) => api.put('/identity/challenge-policy', data),
}
