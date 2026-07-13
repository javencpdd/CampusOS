import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { authApi } from './api'

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
  const user = ref<AdminUser | null>(JSON.parse(localStorage.getItem('admin_user') || 'null'))
  const token = ref<string | null>(localStorage.getItem('admin_token'))
  const isLoggedIn = computed(() => !!token.value)

  async function login(email: string, password: string) {
    const res: any = await authApi.login({ email, password })
    if (res.code === 0) {
      const roles = res.data.roles || []
      const hasAdminRole = roles.some((role: AdminRole) => role.name === 'admin' || role.name === 'super_admin')
      if (!hasAdminRole) {
        logout()
        return { code: 20004, msg: '管理后台仅允许管理员登录；版主请在用户端管理已分配板块' }
      }
      const userData = { ...res.data.user, roles }
      user.value = userData
      token.value = res.data.access_token
      localStorage.setItem('admin_user', JSON.stringify(userData))
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

  const primaryRole = computed(() => user.value?.roles?.[0]?.name || 'member')
  const roleNames = computed(() => user.value?.roles?.map((role) => role.name) || [])
  const isAdmin = computed(() => roleNames.value.some((role) => role === 'admin' || role === 'super_admin'))

  return { user, token, isLoggedIn, primaryRole, isAdmin, login, logout }
})
