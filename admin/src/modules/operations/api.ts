import api from '../../shared/api/client'

export const eventApi = {
  list: (params?: { limit?: number }) => api.get('/events', { params }),
}

export const platformLogApi = {
  sources: () => api.get('/platform/logs/sources'),
  streamUrl: (params: { source: string; lines?: number; follow?: boolean }) => {
    const search = new URLSearchParams()
    search.set('source', params.source)
    search.set('lines', String(params.lines || 200))
    search.set('follow', params.follow === false ? 'false' : 'true')
    return `/api/v1/platform/logs/stream?${search.toString()}`
  },
}

export const healthApi = {
  check: () => api.get('/health'),
}
