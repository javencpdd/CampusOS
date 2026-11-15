import { computed, onScopeDispose, ref } from 'vue'
import { defineStore } from 'pinia'
import { authApi } from './api'
import {
  beginSessionBoundary,
  csrfToken,
  expireAccessSession,
  getAccessToken,
  getSessionBoundary,
  registerSessionRecovery,
  registerSessionRestorer,
  setAccessToken,
} from './session'
import { announceSessionChange, peerAccessToken, shareCurrentSession, withSessionLock } from './sessionCoordination'

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

  let generation = 0
  let signedOut = false
  let signedOutCookie = ''
  let boundCSRF = ''
  let recoveryPromise: Promise<boolean> | null = null

  const invalidCredential = (error: any) => error?.status === 401 || error?.status === 403
  const profile = (value: any): User | null => (value?.code === 0 && value.data ? value.data.user || value.data : null)

  function clearSession() {
    generation++
    beginSessionBoundary()
    signedOut = true
    signedOutCookie = csrfToken()
    boundCSRF = ''
    user.value = null
    token.value = null
    setAccessToken(null, true)
  }

  function applyLoginResponse(data: any) {
    if (!data?.user || !data?.access_token) return false
    user.value = data.user
    token.value = data.access_token
    signedOut = false
    boundCSRF = csrfToken()
    setAccessToken(data.access_token, true)
    return true
  }

  function updateAccessToken(accessToken: string) {
    if (!accessToken) return
    generation++
    beginSessionBoundary()
    signedOut = false
    user.value = null
    token.value = accessToken
    boundCSRF = ''
    setAccessToken(accessToken, true)
  }

  async function login(email: string, password: string) {
    const current = ++generation
    beginSessionBoundary()
    signedOut = false
    return withSessionLock(async () => {
      const res: any = await authApi.login({ email, password })
      if (current === generation && res.code === 0 && !res?.data?.mfa_required && applyLoginResponse(res.data))
        announceSessionChange()
      return res
    })
  }

  async function completeMFA(ticket: string, code: string) {
    const current = ++generation
    beginSessionBoundary()
    return withSessionLock(async () => {
      const res: any = await authApi.completeMFALogin({ mfa_ticket: ticket, code })
      if (current === generation && res.code === 0 && applyLoginResponse(res.data)) announceSessionChange()
      return res
    })
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

  async function recoverWithinLock(current: number, expectedOwner?: string) {
    if (current !== generation) return false
    const peer = await peerAccessToken()
    if (peer) {
      try {
        const candidate = profile(await authApi.me(peer))
        if (current !== generation) return false
        if (candidate && (!expectedOwner || String(candidate.id) === expectedOwner))
          return applyLoginResponse({ user: candidate, access_token: peer })
        if (candidate && expectedOwner) {
          expireAccessSession()
          return false
        }
      } catch (error) {
        if (!invalidCredential(error)) throw error
      }
    }
    if (current !== generation) return false
    const refreshed: any = await authApi.refresh()
    if (current !== generation) return false
    if (refreshed?.code !== 0 || !refreshed?.data?.access_token) throw new Error('登录恢复返回异常')
    const candidate = profile(await authApi.me(refreshed.data.access_token))
    if (current !== generation) return false
    if (!candidate) throw new Error('无法读取当前登录身份')
    if (expectedOwner && String(candidate.id) !== expectedOwner) {
      expireAccessSession()
      return false
    }
    return applyLoginResponse({ user: candidate, access_token: refreshed.data.access_token })
  }

  async function recover() {
    if (recoveryPromise) return recoveryPromise
    const current = generation
    const expectedOwner = user.value?.id ? String(user.value.id) : undefined
    const boundary = getSessionBoundary()
    recoveryPromise = withSessionLock(async () => {
      if (current !== generation) return false
      try {
        return await recoverWithinLock(current, expectedOwner)
      } catch (error) {
        if (current === generation && invalidCredential(error)) expireAccessSession(boundary)
        throw error
      }
    }).finally(() => {
      recoveryPromise = null
    })
    return recoveryPromise
  }

  async function restore() {
    if (signedOut && signedOutCookie === csrfToken()) return false
    if (token.value && user.value) return true
    if (restorePromise) return restorePromise
    const current = generation
    restorePromise = (async () => {
      try {
        if (token.value) {
          const checkingToken = token.value
          try {
            const candidate = profile(await authApi.me(checkingToken))
            if (current !== generation || checkingToken !== getAccessToken()) return false
            if (candidate) return applyLoginResponse({ user: candidate, access_token: checkingToken })
          } catch (error) {
            if (!invalidCredential(error)) throw error
          }
        }
        return current === generation && (await recover())
      } catch {
        return false
      } finally {
        restorePromise = null
      }
    })()
    return restorePromise
  }

  async function logout() {
    const current = ++generation
    beginSessionBoundary()
    const expectedOwner = user.value?.id ? String(user.value.id) : undefined
    let revoked = false
    try {
      await withSessionLock(async () => {
        if (current !== generation || !token.value) return
        try {
          await authApi.logout()
          revoked = true
        } catch (error) {
          if (!invalidCredential(error)) throw error
          if (await recoverWithinLock(current, expectedOwner)) {
            await authApi.logout()
            revoked = true
          }
        }
      })
    } catch {
      // Local sign-out remains effective even if the service is unreachable.
    } finally {
      if (current === generation) clearSession()
      if (revoked) announceSessionChange()
    }
  }

  function setAvatar(avatar: string) {
    if (!user.value) return
    user.value = { ...user.value, avatar }
  }

  registerSessionRestorer(restore)
  registerSessionRecovery(recover, clearSession)
  const stopSharing = shareCurrentSession(
    () => (user.value && token.value && boundCSRF ? { token: token.value, csrf: boundCSRF } : null),
    () => {
      const boundary = getSessionBoundary()
      expireAccessSession(boundary)
      if (csrfToken()) {
        signedOut = false
        void restore()
      }
    },
  )
  onScopeDispose(stopSharing)

  return {
    user,
    token,
    isLoggedIn,
    login,
    completeMFA,
    updateAccessToken,
    restore,
    requestRegistrationChallenge,
    verifyRegistrationChallenge,
    register,
    logout,
    setAvatar,
  }
})
