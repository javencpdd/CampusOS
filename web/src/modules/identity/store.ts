import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { authApi } from './api'
import { clearAccessToken, getAccessToken, registerSessionRestorer, setAccessToken } from './session'

interface User {
  id: string
  username: string
  nickname: string
  email: string
  avatar: string
  bio: string
  status: string
}

export const useUserStore = defineStore('user', () => {
  const user = ref<User | null>(null)
  const token = ref<string | null>(getAccessToken())
  const isLoggedIn = computed(() => !!token.value)
  let restorePromise: Promise<boolean> | null = null

  function clearSession() {
    user.value = null
    token.value = null
    clearAccessToken()
  }

  async function login(email: string, password: string) {
    const res: any = await authApi.login({ email, password })
    if (res.code === 0) {
      user.value = res.data.user
      token.value = res.data.access_token
      setAccessToken(res.data.access_token)
    }
    return res
  }

  async function requestRegistrationChallenge(email: string) {
    return authApi.requestRegistrationChallenge({ email })
  }

  async function verifyRegistrationChallenge(challengeID: string, code: string) {
    return authApi.verifyRegistrationChallenge({ challenge_id: challengeID, code })
  }

  async function register(data: {
    username: string
    nickname: string
    email: string
    password: string
    challenge_id: string
    ticket: string
  }) {
    return authApi.register(data)
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
        setAccessToken(refreshed.data.access_token)
        token.value = refreshed.data.access_token
        const me: any = await authApi.me()
        if (me?.code !== 0 || !me?.data) {
          clearSession()
          return false
        }
        user.value = me.data.user || me.data
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
      // Local memory must be cleared even when an expired session is already gone.
    } finally {
      clearSession()
    }
  }

  function setAvatar(avatar: string) {
    if (!user.value) return
    user.value = { ...user.value, avatar }
  }

  registerSessionRestorer(restore)
  if (typeof window !== 'undefined') window.addEventListener('campusos:identity-session-expired', clearSession)

  return {
    user,
    token,
    isLoggedIn,
    login,
    restore,
    requestRegistrationChallenge,
    verifyRegistrationChallenge,
    register,
    logout,
    setAvatar,
  }
})
