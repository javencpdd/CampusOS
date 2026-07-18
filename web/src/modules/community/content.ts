import api from '../../shared/api/client'

export type StructuredContentFormat = 'markdown' | 'safe_html'

export type ContentImage = {
  file_url: string
  file_name: string
  file_size: number
  mime_type: string
  width?: number
  height?: number
}

export const contentApi = {
  preview: (content: string) => api.post('/content/preview', { content }),
  uploadImage: (file: File) => {
    const form = new FormData()
    form.append('file', file)
    return api.post('/content/assets/images', form, { headers: { 'Content-Type': 'multipart/form-data' } })
  },
}

export const isSafeHTML = (format?: string) => format === 'safe_html'

export const htmlToPlainText = (content: string) => {
  if (typeof document === 'undefined')
    return content
      .replace(/<[^>]*>/g, ' ')
      .replace(/\s+/g, ' ')
      .trim()
  const container = document.createElement('div')
  container.innerHTML = content
  return (container.textContent || '').replace(/\s+/g, ' ').trim()
}

const escapeHTML = (value: string) =>
  value.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')

export const plainTextToHTML = (content: string) =>
  content
    .split(/\n{2,}/)
    .map((paragraph) => `<p>${escapeHTML(paragraph.trim()).replace(/\n/g, '<br>')}</p>`)
    .join('\n')

export const contentExcerpt = (content: string, format?: string, limit = 220) => {
  const text = isSafeHTML(format) ? htmlToPlainText(content) : content.trim()
  return text.length > limit ? `${text.slice(0, limit)}...` : text
}

export const hasMeaningfulContent = (content: string, format?: string) =>
  (isSafeHTML(format) ? htmlToPlainText(content) : content.trim()).length > 0
