import axios from 'axios'
import { adminCSRFToken, clearAdminSession, getAdminAccessToken } from '../../modules/identity/session'

const api = axios.create({
  baseURL: '/api/v1',
  timeout: 10000,
  withCredentials: true,
  headers: { 'Content-Type': 'application/json' },
})

api.interceptors.request.use((config) => {
  const token = getAdminAccessToken()
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  const method = (config.method || 'get').toLowerCase()
  if (!['get', 'head', 'options'].includes(method)) {
    const csrf = adminCSRFToken()
    if (csrf) config.headers['X-CSRF-Token'] = csrf
  }
  return config
})

api.interceptors.response.use(
  (response) => response.data,
  (error) => {
    const requestPath = String(error.config?.url || '')
    const isBootstrapAuthRequest =
      requestPath.includes('/auth/login') ||
      requestPath.includes('/auth/admin/login') ||
      requestPath.includes('/auth/refresh')
    if (error.response?.status === 401 && !isBootstrapAuthRequest) {
      clearAdminSession()
      window.dispatchEvent(new Event('campusos:admin-session-expired'))
      if (window.location.pathname !== '/login') window.location.assign('/login')
    }
    return Promise.reject(error.response?.data || error)
  },
)

export default api
