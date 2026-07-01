import axios from 'axios'

const api = axios.create({
  baseURL: '/api/v1',
  timeout: 10000,
  headers: { 'Content-Type': 'application/json' },
})

// 请求拦截器：自动携带 Token
api.interceptors.request.use((config) => {
  const token = localStorage.getItem('access_token')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

// 响应拦截器
api.interceptors.response.use(
  (response) => response.data,
  (error) => {
    if (error.response?.status === 401) {
      localStorage.removeItem('access_token')
      localStorage.removeItem('user')
      window.location.href = '/login'
    }
    return Promise.reject(error.response?.data || error)
  },
)

// 认证 API
export const authApi = {
  register: (data: { username: string; nickname: string; email: string; password: string }) =>
    api.post('/auth/register', data),
  login: (data: { email: string; password: string }) => api.post('/auth/login', data),
  me: () => api.get('/auth/me'),
}

// 用户 API
export const userApi = {
  list: (params?: { page?: number; page_size?: number }) => api.get('/users', { params }),
  get: (id: string) => api.get(`/users/${id}`),
  update: (id: string, data: { nickname?: string; bio?: string }) =>
    api.put(`/users/${id}`, data),
}

// 帖子 API
export const threadApi = {
  list: (params?: {
    page?: number
    page_size?: number
    category_id?: string
    keyword?: string
  }) => api.get('/threads', { params }),
  get: (id: string) => api.get(`/threads/${id}`),
  create: (data: { title: string; content: string; category_id: string; tags?: string[] }) =>
    api.post('/threads', data),
  update: (id: string, data: { title?: string; content?: string; tags?: string[] }) =>
    api.put(`/threads/${id}`, data),
  delete: (id: string) => api.delete(`/threads/${id}`),
}

// 版块 API
export const categoryApi = {
  list: () => api.get('/categories'),
}

// 个人主页 Space API
export const spaceApi = {
  me: () => api.get('/spaces/me'),
  updateMe: (data: {
    title?: string
    bio?: string
    avatar?: string
    cover_image?: string
    theme?: string
    layout?: string
    visibility?: string
    sync_enabled?: boolean
    sync_categories?: string[]
    sync_tags?: string[]
  }) => api.put('/spaces/me', data),
  validateStyle: (data: any) => api.post('/spaces/me/styles/validate', data),
  previewStyle: (data: any) => api.post('/spaces/me/styles/preview', data),
  exportStyle: (data?: { name?: string; version?: string; description?: string }) =>
    api.post('/spaces/me/styles/export', data || {}),
  applyStyle: (data: any) => api.post('/spaces/me/styles/apply', data),
  publicByUsername: (username: string) => api.get(`/u/${username}`),
  publicContentsByUsername: (username: string, params?: { page?: number; page_size?: number }) =>
    api.get(`/u/${username}/contents`, { params }),
}

// 健康检查
export const healthApi = {
  check: () => api.get('/health'),
}

export default api
