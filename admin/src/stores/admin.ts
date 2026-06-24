import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { authApi } from '@/api'

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
      // 将角色信息注入到用户对象中
      const userData = { ...res.data.user, roles: res.data.roles || [] }
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

  // 获取用户主角色（第一个角色）
  const primaryRole = computed(() => {
    return user.value?.roles?.[0]?.name || 'member'
  })

  // 判断是否为管理员
  const isAdmin = computed(() => {
    const role = primaryRole.value
    return role === 'admin' || role === 'super_admin'
  })

  // 判断是否为版主或管理员
  const isModerator = computed(() => {
    const role = primaryRole.value
    return role === 'admin' || role === 'super_admin' || role === 'moderator'
  })

  return { user, token, isLoggedIn, primaryRole, isAdmin, isModerator, login, logout }
})