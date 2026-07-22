import api from '../../shared/api/client'

export const authApi = {
  login: (data: { email: string; password: string }) => api.post('/auth/admin/login', data),
  completeMFALogin: (data: { mfa_ticket: string; code: string }) => api.post('/auth/mfa/login/complete', data),
  stepUpMFA: (data: { code: string }) => api.post('/auth/mfa/step-up', data),
  refresh: () => api.post('/auth/refresh'),
  logout: () => api.post('/auth/logout'),
  me: () => api.get('/auth/me'),
  roles: (userID: string) => api.get(`/users/${userID}/roles`),
}

export type MFAAdminPolicy = {
  id: string
  mode: 'off' | 'enrollment_grace' | 'required'
  grace_ends_at?: string
  version: number
  updated_by?: string
  updated_at: string
}

export type MFAAdminPolicyStatus = {
  policy: MFAAdminPolicy
  coverage: {
    active_administrators: number
    mfa_enrolled_administrators: number
    local_recovery_available: boolean
  }
  available: boolean
}

export const mfaPolicyApi = {
  get: () => api.get('/identity/mfa-policy'),
  update: (data: { mode: MFAAdminPolicy['mode']; grace_ends_at?: string; expected_version: number }) =>
    api.put('/identity/mfa-policy', data),
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

export type AdminAdmissionAccount = {
  id: string
  user_id: string
  credential_account_id: string
  status: 'active' | 'suspended' | 'revoked'
  activation_source: string
  activated_at?: string
  revoked_at?: string
  last_authenticated_at?: string
  status_reason?: string
  status_changed_by?: string
  status_changed_at?: string
  version: number
  created_at: string
  updated_at: string
}

export type AdminAdmissionRecord = {
  account: AdminAdmissionAccount
  username?: string
  nickname?: string
  email?: string
  user_status?: string
}

export const adminAdmissionApi = {
  list: (params?: { status?: string; page?: number; page_size?: number }) => api.get('/identity/admin-accounts', { params }),
  get: (userId: string) => api.get(`/identity/admin-accounts/${encodeURIComponent(userId)}`),
  suspend: (userId: string, data: { expected_version: number; reason: string }) =>
    api.post(`/identity/admin-accounts/${encodeURIComponent(userId)}/suspend`, data),
  restore: (userId: string, data: { expected_version: number; reason: string }) =>
    api.post(`/identity/admin-accounts/${encodeURIComponent(userId)}/restore`, data),
  audits: (limit = 100) => api.get('/identity/admin-accounts/audits', { params: { limit } }),
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
