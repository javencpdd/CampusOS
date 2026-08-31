export type StructuredContentFormat = 'markdown' | 'safe_html'

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
