import { csrfToken } from './session'

type Snapshot = { token: string; csrf: string }
let source: (() => Snapshot | null) | null = null
let changed: (() => void) | null = null
let channel: BroadcastChannel | null = null
const replies = new Map<string, (token: string | null) => void>()
let sequence = 0
let localQueue: Promise<unknown> = Promise.resolve()

function tabID() {
  const bytes = new Uint32Array(4)
  crypto.getRandomValues(bytes)
  return Array.from(bytes, (value) => value.toString(16)).join('-')
}

function connection() {
  if (channel) return channel
  if (typeof window === 'undefined' || typeof window.BroadcastChannel !== 'function') return null
  channel = new window.BroadcastChannel('campusos-user-session/v1')
  channel.onmessage = ({ data }) => {
    if (!data || typeof data !== 'object' || data.csrf !== csrfToken()) return
    if (data.type === 'request' && typeof data.id === 'string' && data.id.length < 160) {
      const current = source?.()
      if (current?.csrf && current.csrf === data.csrf)
        channel?.postMessage({ type: 'reply', id: data.id, csrf: current.csrf, token: current.token })
    } else if (data.type === 'reply' && typeof data.token === 'string' && data.token.length <= 16384) {
      replies.get(data.id)?.(data.token)
    } else if (data.type === 'changed') {
      const current = source?.()
      if (!current || current.csrf !== data.csrf) changed?.()
    }
  }
  return channel
}

export function shareCurrentSession(snapshot: () => Snapshot | null, onChanged: () => void) {
  source = snapshot
  changed = onChanged
  connection()
  return () => {
    if (source !== snapshot) return
    source = null
    changed = null
    channel?.close()
    channel = null
    for (const done of replies.values()) done(null)
    replies.clear()
  }
}

// Tokens cross only the current origin's in-memory channel. The caller must
// verify /auth/me before applying any reply. No token is stored in IndexedDB.
export function peerAccessToken(): Promise<string | null> {
  const bus = connection()
  const csrf = csrfToken()
  if (!bus || !csrf) return Promise.resolve(null)
  const id = `${tabID()}-${++sequence}`
  return new Promise((resolve) => {
    const timer = setTimeout(() => finish(null), 150)
    const finish = (token: string | null) => {
      clearTimeout(timer)
      replies.delete(id)
      resolve(token)
    }
    replies.set(id, finish)
    bus.postMessage({ type: 'request', id, csrf })
  })
}

export function announceSessionChange() {
  connection()?.postMessage({ type: 'changed', csrf: csrfToken() })
}

async function database(): Promise<IDBDatabase | null> {
  if (typeof indexedDB === 'undefined') return null
  return new Promise((resolve) => {
    const request = indexedDB.open('campusos-user-auth-coordination', 1)
    request.onupgradeneeded = () => request.result.createObjectStore('leases')
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => resolve(null)
    request.onblocked = () => resolve(null)
  })
}

async function withDatabaseLock<T>(action: () => Promise<T>): Promise<T> {
  const db = await database()
  if (!db) return action()
  const owner = tabID()
  const claim = () =>
    new Promise<boolean>((resolve, reject) => {
      const tx = db.transaction('leases', 'readwrite')
      const store = tx.objectStore('leases')
      let acquired = false
      const request = store.get('user-auth')
      request.onsuccess = () => {
        if (!request.result || request.result.expires <= Date.now()) {
          store.put({ owner, expires: Date.now() + 60000 }, 'user-auth')
          acquired = true
        }
      }
      tx.oncomplete = () => resolve(acquired)
      tx.onerror = () => reject(tx.error)
      tx.onabort = () => reject(tx.error)
    })
  try {
    while (!(await claim())) await new Promise((resolve) => setTimeout(resolve, 100))
    return await action()
  } finally {
    await new Promise<void>((resolve) => {
      const tx = db.transaction('leases', 'readwrite')
      const store = tx.objectStore('leases')
      const request = store.get('user-auth')
      request.onsuccess = () => {
        if (request.result?.owner === owner) store.delete('user-auth')
      }
      tx.oncomplete = tx.onerror = tx.onabort = () => resolve()
    })
    db.close()
  }
}

// Cookie rotation is single-use. Serialize all operations that write the User
// refresh cookie, including across HTTP LAN tabs where Web Locks is absent.
export function withSessionLock<T>(action: () => Promise<T>): Promise<T> {
  const run = async (): Promise<T> => {
    if (typeof navigator !== 'undefined' && navigator.locks)
      return await navigator.locks.request('campusos-user-auth', action)
    return withDatabaseLock(action)
  }
  const next = localQueue.then(run, run)
  localQueue = next.catch(() => {})
  return next
}
