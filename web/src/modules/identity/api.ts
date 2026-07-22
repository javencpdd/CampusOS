import api from '../../shared/api/client'

export type MFARequirement = {
  mfa_required: boolean
  mfa_ticket?: string
  mfa_expires_at?: string
  mfa_enrollment_due?: boolean
}

export type MFAStatus = {
  enabled: boolean
  pending_enrollment: boolean
  recovery_codes_remaining: number
  mfa_available: boolean
  policy_mode: 'off' | 'enrollment_grace' | 'required'
  grace_ends_at?: string
  step_up_required_after_seconds: number
}

export type MFAEnrollment = {
  manual_key: string
  otpauth_uri: string
  expires_at: string
}

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
  completeMFALogin: (data: { mfa_ticket: string; code: string }) => api.post('/auth/mfa/login/complete', data),
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
  mfaStatus: () => api.get('/auth/mfa'),
  startMFAEnrollment: (data: { password: string }) => api.post('/auth/mfa/totp/enrollment', data),
  confirmMFAEnrollment: (data: { code: string }) => api.post('/auth/mfa/totp/confirm', data),
  rotateMFARecoveryCodes: (data: { code?: string; recovery_code?: string }) =>
    api.post('/auth/mfa/recovery-codes/rotate', data),
  disableMFA: (data: { password: string; code?: string; recovery_code?: string }) =>
    api.delete('/auth/mfa/totp', { data }),
  stepUpMFA: (data: { code: string }) => api.post('/auth/mfa/step-up', data),
  me: () => api.get('/auth/me'),
}

export const userApi = {
  list: (params?: { page?: number; page_size?: number }) => api.get('/users', { params }),
  get: (id: string) => api.get(`/users/${id}`),
  update: (id: string, data: { nickname?: string; bio?: string }) => api.put(`/users/${id}`, data),
}
