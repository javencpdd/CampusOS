import api from '../../shared/api/client'

// v0.8 keeps the historical /plugins endpoints as a compatibility projection.
// Built-in Feature screens use this adapter so they never invoke external
// plugin install, uninstall, reload, package or snapshot operations.
export const featureApi = {
  list: () => api.get('/plugins'),
  getCompatibility: (name: string) => api.get(`/plugins/${name}`),
  enableCompatibility: (name: string) => api.post(`/plugins/${name}/enable`),
  disableCompatibility: (name: string) => api.post(`/plugins/${name}/disable`),
  updateCompatibilityConfig: (name: string, config: Record<string, unknown>) =>
    api.put(`/plugins/${name}/config`, config),
}
