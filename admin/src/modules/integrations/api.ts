import api from '../../shared/api/client'

export const integrationApi = {
  overview: () => api.get('/integrations/overview'),
  metrics: () => api.get('/metrics/summary'),
}

export const spaceAdminApi = {
  summary: () => api.get('/spaces/admin/summary'),
  disable: (userId: string, reason?: string) => api.post(`/spaces/${userId}/disable`, { reason }),
  enable: (userId: string) => api.post(`/spaces/${userId}/enable`),
}

export const webhookApi = {
  list: () => api.get('/webhooks'),
  summary: () => api.get('/webhooks/summary'),
  create: (data: { name: string; url: string; secret?: string; events?: string[]; enabled?: boolean; max_retries?: number; timeout_ms?: number }) =>
    api.post('/webhooks', data),
  test: (id: string) => api.post(`/webhooks/${id}/test`),
  enable: (id: string) => api.post(`/webhooks/${id}/enable`),
  disable: (id: string) => api.post(`/webhooks/${id}/disable`),
  deliveries: (id: string, params?: { limit?: number }) => api.get(`/webhooks/${id}/deliveries`, { params }),
}

export const mcpApi = {
  tools: () => api.get('/mcp/tools'),
  call: (name: string, args?: Record<string, any>) => api.post(`/mcp/tools/${name}/call`, { arguments: args || {} }),
  audit: (params?: { limit?: number }) => api.get('/mcp/audit', { params }),
  settings: () => api.get('/mcp/settings'),
  updateSettings: (enabled: boolean) => api.put('/mcp/settings', { enabled }),
}

export const messageApi = {
  adapters: () => api.get('/messages/adapters'),
  receiveLocal: (data: { conversation_id?: string; sender?: { id: string; display_name?: string; user_id?: string }; content: string; raw_payload?: Record<string, any> }) =>
    api.post('/messages/local/inbound', data),
  logs: (params?: { limit?: number }) => api.get('/messages/logs', { params }),
  summary: () => api.get('/messages/summary'),
}
