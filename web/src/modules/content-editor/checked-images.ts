import { getAccessToken } from '../identity/session'

export type ImageContext = { threadId?: string; article?: boolean }

// Only allowlisted platform image endpoints receive a bearer token. Stored query
// parameters cannot select a different resource or carry a credential.
export function checkedImageRequest(source: string, context: ImageContext = {}) {
  const parsed = new URL(source, window.location.origin)
  if (parsed.origin !== window.location.origin) return null
  if (/^\/api\/v1\/spaces\/files\/[0-9]+\/avatars\/[A-Za-z0-9_.-]+$/.test(parsed.pathname)) return parsed.pathname
  const match = parsed.pathname.match(
    /^\/api\/v1\/(content\/assets\/images|richtext\/assets)\/([A-Za-z0-9_-]+)\/([A-Za-z0-9_.-]+)$/,
  )
  if (!match) return null
  let path = parsed.pathname
  const query = new URLSearchParams()
  if (context.threadId) {
    query.set('thread_id', context.threadId)
    if (context.article && match[1] === 'content/assets/images') {
      path = `/api/v1/richtext/assets/${match[2]}/${match[3]}`
      query.set('store', 'content')
    }
  }
  return path + (query.size ? `?${query}` : '')
}

export function checkedImageHTML(html: string) {
  const doc = new DOMParser().parseFromString(html, 'text/html')
  doc.body
    .querySelectorAll('[data-checked-image-source]')
    .forEach((node) => node.removeAttribute('data-checked-image-source'))
  doc.body.querySelectorAll('img[src]').forEach((node) => {
    const src = node.getAttribute('src') || ''
    if (checkedImageRequest(src) !== null) {
      node.removeAttribute('src')
      node.setAttribute('data-checked-image-source', src)
    }
  })
  return doc.body.innerHTML
}

export async function loadCheckedImage(source: string, context: ImageContext, signal: AbortSignal) {
  const path = checkedImageRequest(source, context)
  if (path === null) return { url: source, owned: false }
  const token = getAccessToken()
  const headers = token ? { Authorization: `Bearer ${token}` } : undefined
  const response = await fetch(path, { headers, signal, cache: 'no-store', credentials: 'same-origin' })
  if (!response.ok || !/^image\/(png|jpeg|gif|webp)(;|$)/i.test(response.headers.get('Content-Type') || '')) {
    throw new Error('图片不存在或无权访问。')
  }
  const blob = await response.blob()
  // Credentials may change while the response is in flight.
  if (signal.aborted || token !== getAccessToken()) throw new Error('图片访问会话已变化。')
  return { url: URL.createObjectURL(blob), owned: true }
}
