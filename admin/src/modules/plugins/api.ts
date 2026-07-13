import api from '../../shared/api/client'

export const pluginApi = {
  list: () => api.get('/plugins'),
  get: (name: string) => api.get(`/plugins/${name}`),
  logs: (name: string, params?: { limit?: number }) => api.get(`/plugins/${name}/logs`, { params }),
  exportPackage: (name: string) => api.get(`/plugins/${name}/export`, { responseType: 'blob', timeout: 60000 }),
  importPackage: (file: File, replace = false) => withFile('/plugin-packages/import', file, replace),
  precheckPackage: (file: File) => withFile('/plugin-packages/precheck', file),
  enable: (name: string) => api.post(`/plugins/${name}/enable`),
  disable: (name: string) => api.post(`/plugins/${name}/disable`),
  reload: (name: string) => api.post(`/plugins/${name}/reload`),
  snapshots: (name: string) => api.get(`/plugins/${name}/snapshots`),
  rollback: (name: string, snapshotId: string) => api.post(`/plugins/${name}/rollback`, { snapshot_id: snapshotId }),
  updateConfig: (name: string, config: Record<string, any>) => api.put(`/plugins/${name}/config`, config),
  uninstall: (name: string) => api.delete(`/plugins/${name}`),
}

function withFile(path: string, file: File, replace?: boolean) {
  const form = new FormData()
  form.append('file', file)
  if (replace !== undefined) form.append('replace', replace ? 'true' : 'false')
  return api.post(path, form, { headers: { 'Content-Type': 'multipart/form-data' }, timeout: 60000 })
}
