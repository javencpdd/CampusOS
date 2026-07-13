import api from '../../shared/api/client'

export const uiRuntimeApi = {
  manifest: () => api.get('/ui/runtime-manifest'),
  extension: (plugin: string, action: { method: string; path: string; body?: Record<string, unknown> }) =>
    api.request({
      method: action.method,
      url: `/extensions/${encodeURIComponent(plugin)}${action.path}`,
      data: action.body || {},
    }),
}
