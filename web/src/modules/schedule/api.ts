import api from '../../shared/api/client'

export const scheduleApi = {
  status: () => api.get('/schedule/status'),
  me: (term?: { academic_term_id: string } | { term_year: number; semester: 'spring' | 'fall' }) =>
    api.get('/schedule/me', { params: term }),
  availableTerms: () => api.get('/schedule/terms'),
  terms: () => api.get('/schedule/me/terms'),
  activate: (data: { academic_term_id: string } | { term_year: number; semester: 'spring' | 'fall' }) =>
    api.post('/schedule/me/terms/activate', data),
  save: (data: {
    term_year: number
    semester: 'spring' | 'fall'
    expected_version: number
    first_week_start: string
    settings: Record<string, any>
    courses: any[]
    metadata?: Record<string, any>
  }) => api.put('/schedule/me', data),
  import: (
    file: File,
    term: { term_year: number; semester: 'spring' | 'fall'; expected_version: number },
    replace = false,
  ) => {
    const form = new FormData()
    form.append('file', file)
    form.append('term_year', String(term.term_year))
    form.append('semester', term.semester)
    form.append('expected_version', String(term.expected_version))
    form.append('replace', replace ? 'true' : 'false')
    return api.post('/schedule/me/import', form, { headers: { 'Content-Type': 'multipart/form-data' }, timeout: 60000 })
  },
}
