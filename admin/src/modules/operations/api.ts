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

export const reliabilityApi = {
  summary: () => api.get('/platform/reliability/summary'),
  events: (params?: { status?: string; type?: string; limit?: number }) => api.get('/platform/reliability/events', { params }),
  attempts: (params?: { event_id?: string; limit?: number }) => api.get('/platform/reliability/attempts', { params }),
  workers: (params?: { limit?: number }) => api.get('/platform/reliability/workers', { params }),
  operations: (params?: { kind?: string; limit?: number }) => api.get('/platform/reliability/operations', { params }),
  commandAudits: (params?: { limit?: number }) => api.get('/platform/reliability/command-audits', { params }),
  compatibility: (params?: { limit?: number }) => api.get('/platform/reliability/compatibility', { params }),
  retentionPreview: (params?: { target?: string; before?: string }) => api.get('/platform/reliability/retention-preview', { params }),
  retentionRuns: (params?: { limit?: number }) => api.get('/platform/reliability/retention-runs', { params }),
  startRetentionPreview: (params?: { target?: string; before?: string }) => api.post('/platform/reliability/retention-runs/preview', null, { params }),
  replay: (id: string, idempotencyKey: string) => api.post(`/platform/reliability/events/${encodeURIComponent(id)}/replay`, null, { headers: { 'Idempotency-Key': idempotencyKey } }),
}
