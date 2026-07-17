let accessToken: string | null = null
let roleNames: string[] = []
let restoreSession: (() => Promise<boolean>) | null = null

export function getAdminAccessToken() {
  return accessToken
}

export function setAdminAccessToken(value: string | null) {
  accessToken = value || null
}

export function setAdminRoleNames(values: string[]) {
  roleNames = values.filter(Boolean)
}

export function getAdminRoleNames() {
  return roleNames
}

export function clearAdminSession() {
  accessToken = null
  roleNames = []
}

export function registerAdminSessionRestorer(restorer: () => Promise<boolean>) {
  restoreSession = restorer
}

export async function ensureAdminAccessToken() {
  if (accessToken) return true
  return (await restoreSession?.()) || false
}

export function adminCSRFToken() {
  if (typeof document === 'undefined') return ''
  const encoded = document.cookie.split('; ').find((item) => item.startsWith('campusos_csrf='))?.split('=').slice(1).join('=') || ''
  try {
    return decodeURIComponent(encoded)
  } catch {
    return ''
  }
}
