import { randomRequestID } from '../../../sdk/typescript/src/random-request-id'
import { getAccessToken } from '@/modules/identity/session'

export type ImportedArticle = {
  title: string
  summary: string
  content_html: string
  cover_url: string
  warnings: string[]
  source: string
}
const pending = new Map<string, { token: string; expires: number; article: ImportedArticle }>()
export function stageImportedArticle(article: ImportedArticle): string {
  const token = getAccessToken()
  if (!token || article.content_html.length > 100000 || !article.title) throw new Error('导入内容或当前用户不可用。')
  pending.clear()
  const id = randomRequestID()
  pending.set(id, { token, expires: Date.now() + 120000, article })
  return id
}
export function takeImportedArticle(id: string): ImportedArticle | undefined {
  const value = pending.get(id)
  pending.delete(id)
  if (!value || value.token !== getAccessToken() || Date.now() >= value.expires) return undefined
  return value.article
}
