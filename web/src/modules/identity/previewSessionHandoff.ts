import { getAccessToken } from './session'

const channelName = 'campusos.user.preview-handoff/v1'
const nonce = () => {
  const bytes = crypto.getRandomValues(new Uint8Array(24))
  return Array.from(bytes, (x) => x.toString(16).padStart(2, '0')).join('')
}
const validNonce = (value: unknown): value is string => typeof value === 'string' && /^[0-9a-f]{48}$/.test(value)

// A handoff is one-use and memory-only. It is invalidated by any local token
// change; no bearer token is placed in a URL, cookie or browser storage.
export function memoryPreviewHandoffs(current: () => string | null, now = Date.now, random = nonce) {
  const tickets = new Map<string, { token: string; expires: number }>()
  return {
    issue() {
      for (const [key, item] of tickets) if (item.expires <= now() || item.token !== current()) tickets.delete(key)
      const token = current()
      if (!token || tickets.size >= 32) throw new Error('当前用户连接不可用，请重新打开预览。')
      const key = random()
      tickets.set(key, { token, expires: now() + 10000 })
      return key
    },
    claim(key: string) {
      const item = tickets.get(key)
      tickets.delete(key)
      return item && item.expires > now() && item.token === current() ? item.token : undefined
    },
  }
}
const handoffs = memoryPreviewHandoffs(getAccessToken)
let sender: BroadcastChannel | undefined
export function issuePreviewSessionHandoff(): string {
  if (typeof window.BroadcastChannel !== 'function')
    throw new Error('此浏览器暂不支持安全的新标签预览，请使用弹窗或下载。')
  if (!sender) {
    sender = new BroadcastChannel(channelName)
    sender.onmessage = (event) => {
      const value = event.data
      if (value?.type !== 'request' || !validNonce(value.ticket) || !validNonce(value.request)) return
      const token = handoffs.claim(value.ticket)
      if (token) sender?.postMessage({ type: 'response', ticket: value.ticket, request: value.request, token })
    }
  }
  return handoffs.issue()
}

// BroadcastChannel is origin-bound: the separate plugin origin cannot join.
// The receiving host still authenticates this token with the current /auth/me
// endpoint before rendering. It does not rotate the shared refresh cookie and
// revoke the parent page's session merely to open a preview tab.
export async function receivePreviewSessionHandoff(): Promise<string | undefined> {
  const url = new URL(window.location.href),
    ticket = url.searchParams.get('preview_handoff')
  if (!ticket) return
  url.searchParams.delete('preview_handoff')
  window.history.replaceState(window.history.state, '', url.pathname + url.search + url.hash)
  if (!validNonce(ticket) || typeof window.BroadcastChannel !== 'function') return
  const channel = new BroadcastChannel(channelName),
    request = nonce()
  return new Promise((resolve) => {
    const done = (token?: string) => {
      clearTimeout(timer)
      channel.close()
      resolve(token)
    }
    const timer = setTimeout(() => done(), 1000)
    channel.onmessage = (event) => {
      const value = event.data
      if (
        value?.type === 'response' &&
        value.ticket === ticket &&
        value.request === request &&
        typeof value.token === 'string' &&
        value.token.length <= 8192 &&
        /^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$/.test(value.token)
      )
        done(value.token)
    }
    channel.postMessage({ type: 'request', ticket, request })
  })
}
