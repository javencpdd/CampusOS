import api from '../../shared/api/client'

export const pluginCenterApi = {
  catalog: () => api.get('/plugin-market'),
  myGrants: () => api.get('/plugin-market/me'),
  myUsage: () => api.get('/plugin-market/me/usage'),
  enable: (name: string, permissions: string[]) =>
    api.post(`/plugin-market/${encodeURIComponent(name)}/enable`, { permissions }),
  revoke: (name: string) => api.post(`/plugin-market/${encodeURIComponent(name)}/revoke`),
  marketplaceSources: () => api.get('/plugin-market/sources'),
  searchMarketplace: (sourceID: string, query = '') =>
    api.get(`/plugin-market/sources/${encodeURIComponent(sourceID)}/plugins`, { params: { q: query } }),
  requestMarketplace: (sourceID: string, pluginID: string, message = '') =>
    api.post('/plugin-market/requests', { source_id: sourceID, plugin_id: pluginID, message }),
  exportData: (name: string) => api.get(`/plugin-market/${encodeURIComponent(name)}/export`),
  deleteData: (name: string) => api.delete(`/plugin-market/${encodeURIComponent(name)}/data`),
  search: (plugin: string, collection: string, query: string) =>
    api.get('/plugin-market/search', { params: { plugin, collection, q: query } }),
  getRecord: (name: string, collection: string, key: string) =>
    api.get(
      `/plugin-market/${encodeURIComponent(name)}/records/${encodeURIComponent(collection)}/${encodeURIComponent(key)}`,
    ),
  createRecord: (name: string, collection: string, data: { record_key: string; data: Record<string, any> }) =>
    api.post(`/plugin-market/${encodeURIComponent(name)}/records/${encodeURIComponent(collection)}`, data),
  updateRecord: (name: string, collection: string, key: string, data: { version: number; data: Record<string, any> }) =>
    api.put(
      `/plugin-market/${encodeURIComponent(name)}/records/${encodeURIComponent(collection)}/${encodeURIComponent(key)}`,
      data,
    ),
  authorization: (name: string) => api.get(`/plugin-authorizations/${encodeURIComponent(name)}`),
  setConsent: (
    name: string,
    versionId: string,
    capability: string,
    status: 'granted' | 'revoked',
    scope: Record<string, any>,
  ) =>
    api.put(
      `/plugin-authorizations/${encodeURIComponent(name)}/versions/${versionId}/consents/${encodeURIComponent(capability)}`,
      { status, scope },
    ),
  issueDelegation: (name: string, versionId: string, capabilities: string[], ttlSeconds = 900) =>
    api.post(`/plugin-authorizations/${encodeURIComponent(name)}/versions/${versionId}/delegations`, {
      capabilities,
      scope: { scope: 'self' },
      ttl_seconds: ttlSeconds,
    }),
  revokeDelegation: (name: string, delegationId: string) =>
    api.delete(`/plugin-authorizations/${encodeURIComponent(name)}/delegations/${delegationId}`),
  secrets: (name: string) => api.get(`/plugin-authorizations/${encodeURIComponent(name)}/secrets`),
  setSecret: (name: string, secret: string, value: string) =>
    api.put(`/plugin-authorizations/${encodeURIComponent(name)}/secrets/${encodeURIComponent(secret)}`, { value }),
  revokeSecret: (name: string, secret: string) =>
    api.delete(`/plugin-authorizations/${encodeURIComponent(name)}/secrets/${encodeURIComponent(secret)}`),
}
