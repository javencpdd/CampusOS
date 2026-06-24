import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { authApi } from '@/api'

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
  const user = ref<User | null>(JSON.parse(localStorage.getItem('user') || 'null'))
  const token = ref<string | null>(localStorage.getItem('access_token'))

  const isLoggedIn = computed(() => !!token.value)

  async function login(email: string, password: string) {
    const res: any = await authApi.login({ email, password })
    if (res.code === 0) {
      user.value = res.data.user
      token.value = res.data.access_token
      localStorage.setItem('user', JSON.stringify(res.data.user))
      localStorage.setItem('access_token', res.data.access_token)
    }
    return res
  }

  async function register(data: { username: string; nickname: string; email: string; password: string }) {
    const res: any = await authApi.register(data)
    return res
  }

  function logout() {
    user.value = null
    token.value = null
    localStorage.removeItem('user')
    localStorage.removeItem('access_token')
  }

  return { user, token, isLoggedIn, login, register, logout }
})