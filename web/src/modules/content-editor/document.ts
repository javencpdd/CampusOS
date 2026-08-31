export type DocumentFormat = 'text' | 'markdown' | 'campusdoc'

export const editableDocumentFormats: Array<{ label: string; value: DocumentFormat }> = [
  { label: '文本', value: 'text' },
  { label: 'Markdown', value: 'markdown' },
  { label: 'CampusDoc', value: 'campusdoc' },
]

export const isEditableDocumentFormat = (value?: string): value is DocumentFormat =>
  value === 'text' || value === 'markdown' || value === 'campusdoc'

export const defaultDocumentContent = (format: DocumentFormat) =>
  format === 'campusdoc' ? '{\n  "version": 1,\n  "blocks": [\n    { "type": "paragraph", "text": "" }\n  ]\n}' : ''

export const documentNameForFormat = (name: string, format: DocumentFormat) => {
  const suffix = format === 'text' ? '.txt' : format === 'markdown' ? '.md' : '.campusdoc'
  const base = name.trim().replace(/\.(txt|md|markdown|campusdoc|json)$/i, '') || '未命名文档'
  return `${base}${suffix}`
}
