import api from '../../shared/api/client'

export const authApi = {
  login: (data: { email: string; password: string }) => api.post('/auth/login', data),
  me: () => api.get('/auth/me'),
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
