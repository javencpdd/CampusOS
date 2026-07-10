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
    content_format?: string
    keyword?: string
  }) => api.get('/threads', { params }),
  get: (id: string) => api.get(`/threads/${id}`),
  getMine: (id: string) => api.get(`/threads/${id}/me`),
  create: (data: { title: string; content: string; category_id: string; tags?: string[]; is_private?: boolean }) =>
    api.post('/threads', data),
  update: (id: string, data: { title?: string; content?: string; tags?: string[]; status?: string }) =>
    api.put(`/threads/${id}`, data),
  delete: (id: string) => api.delete(`/threads/${id}`),
}

// 受控富文本图文文章 API
export const richTextApi = {
  status: () => api.get('/richtext/status'),
  createDraft: (data: {
    title: string
    summary?: string
    cover_url?: string
    category_id: string
    tags?: string[]
    content_html: string
    content_json?: any
  }) => api.post('/richtext/articles', data),
  getPublished: (threadId: string) => api.get(`/richtext/articles/${threadId}`),
  getMine: (threadId: string) => api.get(`/richtext/articles/${threadId}/me`),
  updateDraft: (threadId: string, data: {
    title: string
    summary?: string
    cover_url?: string
    category_id?: string
    tags?: string[]
    content_html: string
    content_json?: any
  }) => api.put(`/richtext/articles/${threadId}`, data),
  preview: (content_html: string) => api.post('/richtext/preview', { content_html }),
  publish: (threadId: string) => api.post(`/richtext/articles/${threadId}/publish`),
  offline: (threadId: string) => api.post(`/richtext/articles/${threadId}/offline`),
  delete: (threadId: string) => api.delete(`/richtext/articles/${threadId}`),
  uploadAsset: (file: File, data?: { thread_id?: string; article_content_id?: string }) => {
    const form = new FormData()
    form.append('file', file)
    if (data?.thread_id) form.append('thread_id', data.thread_id)
    if (data?.article_content_id) form.append('article_content_id', data.article_content_id)
    return api.post('/richtext/assets', form, { headers: { 'Content-Type': 'multipart/form-data' } })
  },
}

// 回复 API
export const postApi = {
  list: (threadId: string, params?: { page?: number; page_size?: number }) =>
    api.get(`/threads/${threadId}/posts`, { params }),
  create: (threadId: string, data: { content: string; parent_id?: string }) =>
    api.post(`/threads/${threadId}/posts`, data),
  listMine: (threadId: string, params?: { page?: number; page_size?: number }) =>
    api.get(`/threads/${threadId}/posts/me`, { params }),
  update: (threadId: string, postId: string, data: { content: string }) =>
    api.put(`/threads/${threadId}/posts/${postId}`, data),
  delete: (threadId: string, postId: string) => api.delete(`/threads/${threadId}/posts/${postId}`),
}

// 个人课表 API
export const scheduleApi = {
  status: () => api.get('/schedule/status'),
  me: () => api.get('/schedule/me'),
  save: (data: {
    term_year: number
    semester: 'spring' | 'fall'
    first_week_start: string
    settings: Record<string, any>
    courses: any[]
    metadata?: Record<string, any>
  }) => api.put('/schedule/me', data),
  import: (file: File, replace = false) => {
    const form = new FormData()
    form.append('file', file)
    form.append('replace', replace ? 'true' : 'false')
    return api.post('/schedule/me/import', form, { headers: { 'Content-Type': 'multipart/form-data' }, timeout: 60000 })
  },
}

// 版块 API
export const categoryApi = {
  list: () => api.get('/categories'),
}

// 首页配置 API
export const homeApi = {
  config: () => api.get('/home/config'),
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
  validateCustomHtml: (html: string) => api.post('/spaces/me/styles/html/validate', { html }),
  customHtmlExample: () => api.get('/spaces/me/styles/html-example'),
  applyCustomHtml: (html: string) => api.post('/spaces/me/styles/html/apply', { html }),
  validateStylePack: (file: File) => {
    const form = new FormData()
    form.append('file', file)
    return api.post('/spaces/me/styles/packs/validate', form, { headers: { 'Content-Type': 'multipart/form-data' } })
  },
  stylePackExample: () => api.get('/spaces/me/styles/packs/example'),
  stylePackExampleZip: () => api.get('/spaces/me/styles/packs/example.zip', { responseType: 'blob', timeout: 60000 }) as Promise<Blob>,
  sourceStylePacks: () => api.get('/spaces/me/styles/packs/sources'),
  applyStylePack: (file: File) => {
    const form = new FormData()
    form.append('file', file)
    return api.post('/spaces/me/styles/packs/apply', form, { headers: { 'Content-Type': 'multipart/form-data' } })
  },
  applySourceStylePack: (name: string) => api.post('/spaces/me/styles/packs/apply-source', { name }),
  rollbackStyle: () => api.post('/spaces/me/styles/rollback'),
  restoreDefaultStyle: () => api.post('/spaces/me/styles/default'),
  syncStatus: () => api.get('/spaces/me/sync-status'),
  storage: () => api.get('/spaces/me/storage'),
  uploadAvatar: (file: File) => {
    const form = new FormData()
    form.append('file', file)
    return api.post('/spaces/me/avatar', form, { headers: { 'Content-Type': 'multipart/form-data' } })
  },
  publicByUsername: (username: string) => api.get(`/u/${username}`),
  publicContentsByUsername: (username: string, params?: { page?: number; page_size?: number }) =>
    api.get(`/u/${username}/contents`, { params }),
}

// 健康检查
export const healthApi = {
  check: () => api.get('/health'),
}

export default api
