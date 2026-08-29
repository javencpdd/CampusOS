import api from '../../shared/api/client'
import { htmlToPlainText, isSafeHTML, plainTextToHTML, type StructuredContentFormat } from '../content-editor/safe-html'

export { htmlToPlainText, isSafeHTML, plainTextToHTML }
export type { StructuredContentFormat }

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

export const contentExcerpt = (content: string, format?: string, limit = 220) => {
  const text = isSafeHTML(format) ? htmlToPlainText(content) : content.trim()
  return text.length > limit ? `${text.slice(0, limit)}...` : text
}

export const hasMeaningfulContent = (content: string, format?: string) =>
  (isSafeHTML(format) ? htmlToPlainText(content) : content.trim()).length > 0
