import api from '../../shared/api/client'

export const authApi = {
  requestRegistrationChallenge: (data: { email: string }) => api.post('/auth/registration/challenge', data),
  verifyRegistrationChallenge: (data: { challenge_id: string; code: string }) =>
    api.post('/auth/registration/verify', data),
  register: (data: {
    username: string
    nickname: string
    email: string
    password: string
    challenge_id: string
    ticket: string
  }) => api.post('/auth/register', data),
  login: (data: { email: string; password: string }) => api.post('/auth/login', data),
  refresh: () => api.post('/auth/refresh'),
  logout: () => api.post('/auth/logout'),
  logoutAll: () => api.post('/auth/logout-all'),
  sessions: () => api.get('/auth/sessions'),
  revokeSession: (id: string) => api.delete(`/auth/sessions/${id}`),
  requestPasswordReset: (data: { email: string }) => api.post('/auth/password-reset/challenge', data),
  verifyPasswordReset: (data: { challenge_id: string; code: string }) => api.post('/auth/password-reset/verify', data),
  completePasswordReset: (data: { email: string; challenge_id: string; ticket: string; password: string }) =>
    api.post('/auth/password-reset/complete', data),
  completeAdminRecovery: (data: { challenge_id: string; ticket: string; password: string }) =>
    api.post('/auth/recovery/complete', data),
  requestEmailBinding: (data: { email: string }) => api.post('/auth/email-binding/challenge', data),
  verifyEmailBinding: (data: { challenge_id: string; code: string }) => api.post('/auth/email-binding/verify', data),
  completeEmailBinding: (data: { email: string; challenge_id: string; ticket: string }) =>
    api.post('/auth/email-binding/complete', data),
  me: () => api.get('/auth/me'),
}

export const userApi = {
  list: (params?: { page?: number; page_size?: number }) => api.get('/users', { params }),
  get: (id: string) => api.get(`/users/${id}`),
  update: (id: string, data: { nickname?: string; bio?: string }) => api.put(`/users/${id}`, data),
}
