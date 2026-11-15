let accessToken: string | null = null
let restoreSession: (() => Promise<boolean>) | null = null
let recoverSession: (() => Promise<boolean>) | null = null
let invalidateSession: (() => void) | null = null
let boundary = 0
let recovery: { boundary: number; promise: Promise<boolean> } | null = null

export function getSessionBoundary() {
  return boundary
}

export function beginSessionBoundary() {
  return ++boundary
}

export function getAccessToken() {
  return accessToken
}

export function setAccessToken(value: string | null, preserveBoundary = false) {
  const next = value || null
  const changed = next !== accessToken
  if (changed && !preserveBoundary) beginSessionBoundary()
  accessToken = next
  if (changed && typeof window !== 'undefined') window.dispatchEvent(new Event('campusos:identity-session-changed'))
}

export function clearAccessToken() {
  setAccessToken(null)
}

export function registerSessionRestorer(restorer: () => Promise<boolean>) {
  restoreSession = restorer
}

export function registerSessionRecovery(recover: () => Promise<boolean>, invalidate: () => void) {
  recoverSession = recover
  invalidateSession = invalidate
}

export function recoverAccessToken() {
  if (recovery?.boundary === boundary) return recovery.promise
  const recover = recoverSession
  const pending = {
    boundary,
    promise: Promise.resolve()
      .then(() => recover?.() || false)
      .finally(() => {
        if (recovery === pending) recovery = null
      }),
  }
  recovery = pending
  return pending.promise
}

export function expireAccessSession(expectedBoundary = boundary) {
  if (boundary !== expectedBoundary) return
  if (invalidateSession) invalidateSession()
  else clearAccessToken()
  if (typeof window !== 'undefined') window.dispatchEvent(new Event('campusos:identity-session-expired'))
}

export async function ensureAccessToken() {
  return restoreSession ? restoreSession() : Boolean(accessToken)
}

export function csrfToken() {
  if (typeof document === 'undefined') return ''
  const encoded =
    document.cookie
      .split('; ')
      .find((item) => item.startsWith('campusos_csrf='))
      ?.split('=')
      .slice(1)
      .join('=') || ''
  try {
    return decodeURIComponent(encoded)
  } catch {
    return ''
  }
}
