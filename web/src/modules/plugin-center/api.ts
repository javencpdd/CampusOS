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
  authorization: (name: string) => api.get(`/plugin-authorizations/${encodeURIComponent(name)}`),
  setConsent: (
    name: string,
    versionId: number,
    capability: string,
    status: 'granted' | 'revoked',
    scope: Record<string, any>,
  ) =>
    api.put(
      `/plugin-authorizations/${encodeURIComponent(name)}/versions/${versionId}/consents/${encodeURIComponent(capability)}`,
      { status, scope },
    ),
  issueDelegation: (name: string, versionId: number, capabilities: string[], ttlSeconds = 900) =>
    api.post(`/plugin-authorizations/${encodeURIComponent(name)}/versions/${versionId}/delegations`, {
      capabilities,
      scope: { scope: 'self' },
      ttl_seconds: ttlSeconds,
    }),
  revokeDelegation: (name: string, delegationId: number) =>
    api.delete(`/plugin-authorizations/${encodeURIComponent(name)}/delegations/${delegationId}`),
  secrets: (name: string) => api.get(`/plugin-authorizations/${encodeURIComponent(name)}/secrets`),
  setSecret: (name: string, secret: string, value: string) =>
    api.put(`/plugin-authorizations/${encodeURIComponent(name)}/secrets/${encodeURIComponent(secret)}`, { value }),
  revokeSecret: (name: string, secret: string) =>
    api.delete(`/plugin-authorizations/${encodeURIComponent(name)}/secrets/${encodeURIComponent(secret)}`),
}
