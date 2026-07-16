import api from '../../shared/api/client'

export const homeStylePackApi = {
  validate: (file: File) => withFile('/home/style-packs/validate', file),
  apply: (file: File) => withFile('/home/style-packs/apply', file),
  example: () => api.get('/home/style-packs/example'),
  exampleZip: () => api.get('/home/style-packs/example.zip', { responseType: 'blob', timeout: 60000 }) as Promise<Blob>,
  sources: () => api.get('/home/style-packs/sources'),
  applySource: (name: string) => api.post('/home/style-packs/apply-source', { name }),
  rollback: () => api.post('/home/style-packs/rollback'),
}

export const webThemeCatalogApi = {
  catalog: () => api.get('/web-themes'),
}

function withFile(path: string, file: File) {
  const form = new FormData()
  form.append('file', file)
  return api.post(path, form, { headers: { 'Content-Type': 'multipart/form-data' }, timeout: 60000 })
}
