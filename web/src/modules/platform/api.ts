import api from '../../shared/api/client'

export const healthApi = {
  check: () => api.get('/health'),
}
