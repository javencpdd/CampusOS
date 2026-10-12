let accessToken: string | null = null
let restoreSession: (() => Promise<boolean>) | null = null

export function getAccessToken() {
  return accessToken
}

export function setAccessToken(value: string | null) {
  const next = value || null
  const changed = next !== accessToken
  accessToken = next
  if (changed && typeof window !== 'undefined') window.dispatchEvent(new Event('campusos:identity-session-changed'))
}

export function clearAccessToken() {
  setAccessToken(null)
}

export function registerSessionRestorer(restorer: () => Promise<boolean>) {
  restoreSession = restorer
}

export async function ensureAccessToken() {
  if (accessToken) return true
  return (await restoreSession?.()) || false
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
