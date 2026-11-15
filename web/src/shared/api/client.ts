import axios from 'axios'
import {
  csrfToken,
  expireAccessSession,
  getAccessToken,
  getSessionBoundary,
  recoverAccessToken,
} from '../../modules/identity/session'
import { parseAPIError } from './error'

declare module 'axios' {
  interface AxiosRequestConfig {
    sessionRecovery?: boolean
    sessionProbeToken?: string
    sessionBoundary?: number
    sessionRetried?: boolean
  }
}

const api = axios.create({
  baseURL: '/api/v1',
  timeout: 10000,
  withCredentials: true,
  headers: { 'Content-Type': 'application/json' },
})

api.interceptors.request.use((config) => {
  if (config.sessionRetried && config.sessionBoundary !== getSessionBoundary())
    throw new axios.CanceledError('登录身份已改变，取消旧请求')
  const token = config.sessionProbeToken || getAccessToken()
  config.sessionBoundary ??= getSessionBoundary()
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  } else {
    config.headers.delete('Authorization')
  }
  const method = (config.method || 'get').toLowerCase()
  if (!['get', 'head', 'options'].includes(method)) {
    const csrf = csrfToken()
    if (csrf) config.headers['X-CSRF-Token'] = csrf
  }
  return config
})

api.interceptors.response.use(
  (response) => response.data,
  async (error) => {
    const config = error.config
    const requestPath = String(error.config?.url || '')
    const isBootstrapAuthRequest =
      requestPath.includes('/auth/login') ||
      requestPath.includes('/auth/mfa/login/complete') ||
      requestPath.includes('/auth/refresh') ||
      requestPath.startsWith('/auth/registration/') ||
      requestPath.startsWith('/auth/password-reset/')
    if (
      error.response?.status === 401 &&
      config &&
      config.sessionRecovery !== false &&
      !isBootstrapAuthRequest &&
      !config.sessionRetried &&
      config.sessionBoundary === getSessionBoundary() &&
      config.headers?.Authorization
    ) {
      const failedToken = String(config.headers.Authorization).replace(/^Bearer\s+/i, '')
      try {
        const recovered =
          (getAccessToken() !== failedToken && Boolean(getAccessToken())) || (await recoverAccessToken())
        if (recovered && config.sessionBoundary === getSessionBoundary()) {
          // Reads are safe to replay. Writes keep their original failure so a
          // user action, confirmation or idempotency contract controls retry.
          if (['get', 'head', 'options'].includes(String(config.method || 'get').toLowerCase())) {
            config.sessionRetried = true
            return api.request(config)
          }
        } else if (!recovered) expireAccessSession(config.sessionBoundary)
      } catch {
        // Network errors are not proof that a login was revoked. Keep the
        // in-memory identity and let a later request try recovery again.
      }
    }
    return Promise.reject(parseAPIError(error.response?.data || error, error.response?.status))
  },
)

export default api
