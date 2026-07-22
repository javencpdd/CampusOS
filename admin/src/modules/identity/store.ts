import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { authApi } from './api'
import {
  clearAdminSession,
  getAdminAccessToken,
  registerAdminSessionRestorer,
  setAdminAccessToken,
  setAdminRoleNames,
} from './session'

interface AdminRole {
  id: number
  name: string
  description: string
}

interface AdminUser {
  id: string
  username: string
  nickname: string
  email: string
  avatar: string
  status: string
  roles?: AdminRole[]
}

export const useAdminStore = defineStore('admin', () => {
  const user = ref<AdminUser | null>(null)
  const token = ref<string | null>(getAdminAccessToken())
  const isLoggedIn = computed(() => !!token.value)
  let restorePromise: Promise<boolean> | null = null

  function clearSession() {
    user.value = null
    token.value = null
    clearAdminSession()
  }

  function applySession(userData: AdminUser, accessToken: string) {
    user.value = userData
    token.value = accessToken
    setAdminAccessToken(accessToken)
    setAdminRoleNames(userData.roles?.map((role) => role.name) || [])
  }

  function applyLoginResponse(data: any) {
    const roles = data?.roles || []
    const hasAdminRole = roles.some((role: AdminRole) => role.name === 'admin' || role.name === 'super_admin')
    if (!data?.user || !data?.access_token || !hasAdminRole) {
      clearSession()
      return false
    }
    applySession({ ...data.user, roles }, data.access_token)
    return true
  }

  function updateAccessToken(accessToken: string) {
    if (!accessToken) return
    token.value = accessToken
    setAdminAccessToken(accessToken)
  }

  async function login(email: string, password: string) {
    const res: any = await authApi.login({ email, password })
    if (res.code === 0 && !res?.data?.mfa_required && !applyLoginResponse(res.data)) {
      return { code: 20004, msg: '管理后台仅允许管理员登录；版主请在用户端管理已分配板块' }
    }
    return res
  }

  async function completeMFA(ticket: string, code: string) {
    const res: any = await authApi.completeMFALogin({ mfa_ticket: ticket, code })
    if (res.code === 0 && !applyLoginResponse(res.data)) {
      return { code: 20004, msg: '管理后台仅允许管理员登录；版主请在用户端管理已分配板块' }
    }
    return res
  }

  async function restore() {
    if (token.value && user.value) return true
    if (restorePromise) return restorePromise
    restorePromise = (async () => {
      try {
        const refreshed: any = await authApi.refresh()
        if (refreshed?.code !== 0 || !refreshed?.data?.access_token) {
          clearSession()
          return false
        }
        setAdminAccessToken(refreshed.data.access_token)
        token.value = refreshed.data.access_token
        const me: any = await authApi.me()
        const currentUser = me?.data?.user || me?.data
        if (me?.code !== 0 || !currentUser?.id) {
          clearSession()
          return false
        }
        const rolesResponse: any = await authApi.roles(currentUser.id)
        const roles = rolesResponse?.code === 0 && Array.isArray(rolesResponse?.data) ? rolesResponse.data : []
        const hasAdminRole = roles.some((role: AdminRole) => role.name === 'admin' || role.name === 'super_admin')
        if (!hasAdminRole) {
          clearSession()
          return false
        }
        applySession({ ...currentUser, roles }, refreshed.data.access_token)
        return true
      } catch {
        clearSession()
        return false
      } finally {
        restorePromise = null
      }
    })()
    return restorePromise
  }

  async function logout() {
    try {
      if (token.value) await authApi.logout()
    } catch {
      // An already-expired server session still needs local cleanup.
    } finally {
      clearSession()
    }
  }

  const primaryRole = computed(() => user.value?.roles?.[0]?.name || 'member')
  const roleNames = computed(() => user.value?.roles?.map((role) => role.name) || [])
  const isAdmin = computed(() => roleNames.value.some((role) => role === 'admin' || role === 'super_admin'))

  registerAdminSessionRestorer(restore)
  if (typeof window !== 'undefined') window.addEventListener('campusos:admin-session-expired', clearSession)

  return { user, token, isLoggedIn, primaryRole, isAdmin, login, completeMFA, updateAccessToken, restore, logout }
})
