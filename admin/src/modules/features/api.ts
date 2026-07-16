import api from '../../shared/api/client'

// Core modules and Built-in Features use an independent management plane.
// The external /plugins lifecycle never receives these requests.
export const featureApi = {
  list: () => api.get('/features'),
  get: (id: string) => api.get(`/features/${id}`),
  enable: (id: string) => api.post(`/features/${id}/enable`),
  disable: (id: string) => api.post(`/features/${id}/disable`),
  updateConfig: (id: string, config: Record<string, unknown>) =>
    api.put(`/features/${id}/config`, config),
}
