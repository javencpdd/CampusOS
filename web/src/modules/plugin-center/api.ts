import api from '../../shared/api/client'

export const pluginCenterApi = {
  catalog: () => api.get('/plugin-market'),
  myGrants: () => api.get('/plugin-market/me'),
  myUsage: () => api.get('/plugin-market/me/usage'),
  enable: (name: string, permissions: string[]) =>
    api.post(`/plugin-market/${encodeURIComponent(name)}/enable`, { permissions }),
  revoke: (name: string) => api.post(`/plugin-market/${encodeURIComponent(name)}/revoke`),
  request: (name: string, message = '') => api.post(`/plugin-market/${encodeURIComponent(name)}/request`, { message }),
  exportData: (name: string) => api.get(`/plugin-market/${encodeURIComponent(name)}/export`),
  deleteData: (name: string) => api.delete(`/plugin-market/${encodeURIComponent(name)}/data`),
  search: (plugin: string, collection: string, query: string) =>
    api.get('/plugin-market/search', { params: { plugin, collection, q: query } }),
}
