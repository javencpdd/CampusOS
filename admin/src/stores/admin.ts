import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { authApi } from '@/api'

interface AdminUser {
  id: string
  username: string
  nickname: string
  email: string
  avatar: string
  status: string
}

export const useAdminStore = defineStore('admin', () => {
  const user = ref<AdminUser | null>(JSON.parse(localStorage.getItem('admin_user') || 'null'))
  const token = ref<string | null>(localStorage.getItem('admin_token'))

  const isLoggedIn = computed(() => !!token.value)

  async function login(email: string, password: string) {
    const res: any = await authApi.login({ email, password })
    if (res.code === 0) {
      user.value = res.data.user
      token.value = res.data.access_token
      localStorage.setItem('admin_user', JSON.stringify(res.data.user))
      localStorage.setItem('admin_token', res.data.access_token)
    }
    return res
  }

  function logout() {
    user.value = null
    token.value = null
    localStorage.removeItem('admin_user')
    localStorage.removeItem('admin_token')
  }

  return { user, token, isLoggedIn, login, logout }
})