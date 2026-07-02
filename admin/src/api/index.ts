import axios from 'axios'

const api = axios.create({
  baseURL: '/api/v1',
  timeout: 10000,
  headers: { 'Content-Type': 'application/json' },
})

// 请求拦截器：自动携带 Token
api.interceptors.request.use((config) => {
  const token = localStorage.getItem('admin_token')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

// 响应拦截器
api.interceptors.response.use(
  (response) => response.data,
  (error) => {
    const isLoginRequest = error.config?.url?.includes('/auth/login')
    if (error.response?.status === 401 && !isLoginRequest) {
      localStorage.removeItem('admin_token')
      localStorage.removeItem('admin_user')
      window.location.href = '/login'
    }
    return Promise.reject(error.response?.data || error)
  },
)

// 认证 API
export const authApi = {
  login: (data: { email: string; password: string }) => api.post('/auth/login', data),
  me: () => api.get('/auth/me'),
}

// 用户管理 API
export const userApi = {
  list: (params?: { page?: number; page_size?: number }) => api.get('/users', { params }),
  get: (id: string) => api.get(`/users/${id}`),
  suspend: (id: string) => api.post(`/users/${id}/suspend`),
  activate: (id: string) => api.post(`/users/${id}/activate`),
}

// 角色管理 API
export const roleApi = {
  list: () => api.get('/roles'),
  getUserRoles: (userId: string) => api.get(`/users/${userId}/roles`),
  assign: (userId: string, roleId: number) => api.post(`/users/${userId}/roles`, { role_id: roleId }),
  revoke: (userId: string, roleId: number) =>
    api.delete(`/users/${userId}/roles`, { data: { role_id: roleId } }),
}

// 帖子管理 API
export const threadApi = {
  list: (params?: {
    page?: number
    page_size?: number
    category_id?: string
    keyword?: string
  }) => api.get('/threads', { params }),
  get: (id: string) => api.get(`/threads/${id}`),
  update: (id: string, data: { status?: string; title?: string; content?: string }) =>
    api.put(`/threads/${id}`, data),
  delete: (id: string) => api.delete(`/threads/${id}`),
  pin: (id: string) => api.post(`/threads/${id}/pin`),
  unpin: (id: string) => api.post(`/threads/${id}/unpin`),
  lock: (id: string) => api.post(`/threads/${id}/lock`),
  unlock: (id: string) => api.post(`/threads/${id}/unlock`),
}

// 版块管理 API
export const categoryApi = {
  list: () => api.get('/categories'),
  get: (id: string) => api.get(`/categories/${id}`),
  create: (data: {
    name: string
    slug?: string
    description?: string
    icon?: string
    color?: string
    sort_order?: number
    is_closed?: boolean
  }) =>
    api.post('/categories', data),
  update: (id: string, data: {
    name?: string
    slug?: string
    description?: string
    icon?: string
    color?: string
    sort_order?: number
    is_closed?: boolean
  }) =>
    api.put(`/categories/${id}`, data),
  delete: (id: string) => api.delete(`/categories/${id}`),
}

// 插件管理 API
export const pluginApi = {
  list: () => api.get('/plugins'),
  get: (name: string) => api.get(`/plugins/${name}`),
  logs: (name: string, params?: { limit?: number }) => api.get(`/plugins/${name}/logs`, { params }),
  exportPackage: (name: string) =>
    api.get(`/plugins/${name}/export`, { responseType: 'blob', timeout: 60000 }),
  importPackage: (file: File, replace = false) => {
    const form = new FormData()
    form.append('file', file)
    form.append('replace', replace ? 'true' : 'false')
    return api.post('/plugin-packages/import', form, {
      headers: { 'Content-Type': 'multipart/form-data' },
      timeout: 60000,
    })
  },
  enable: (name: string) => api.post(`/plugins/${name}/enable`),
  disable: (name: string) => api.post(`/plugins/${name}/disable`),
  uninstall: (name: string) => api.delete(`/plugins/${name}`),
}

// 事件日志 API
export const eventApi = {
  list: (params?: { limit?: number }) => api.get('/events', { params }),
}

// 健康检查
export const healthApi = {
  check: () => api.get('/health'),
}

export default api
