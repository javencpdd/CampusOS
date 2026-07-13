import api from '../../shared/api/client'

export const homeApi = {
  config: () => api.get('/home/config'),
}

export const webThemeApi = {
  catalog: () => api.get('/web-themes'),
  package: (name: string) => api.get(`/web-themes/${encodeURIComponent(name)}`),
}
