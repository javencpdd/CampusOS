import { createRouter, createWebHistory } from 'vue-router'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/login',
      name: 'Login',
      component: () => import('@/views/LoginView.vue'),
    },
    {
      path: '/',
      component: () => import('@/components/AdminLayout.vue'),
      meta: { requiresAuth: true },
      children: [
        {
          path: '',
          name: 'Dashboard',
          component: () => import('@/views/DashboardView.vue'),
          meta: { title: '仪表盘' },
        },
        {
          path: 'users',
          name: 'Users',
          component: () => import('@/views/UserManageView.vue'),
          meta: { title: '用户管理' },
        },
        {
          path: 'threads',
          name: 'Threads',
          component: () => import('@/views/ThreadManageView.vue'),
          meta: { title: '帖子管理' },
        },
        {
          path: 'categories',
          name: 'Categories',
          component: () => import('@/views/CategoryManageView.vue'),
          meta: { title: '版块管理' },
        },
        {
          path: 'plugins',
          name: 'Plugins',
          component: () => import('@/views/PluginManageView.vue'),
          meta: { title: '插件管理' },
        },
        {
          path: 'reviews',
          name: 'Reviews',
          component: () => import('@/views/AdminReviews.vue'),
          meta: { title: '帖子审核' },
        },
        {
          path: 'events',
          name: 'Events',
          component: () => import('@/views/EventLogView.vue'),
          meta: { title: '事件日志' },
        },
      ],
    },
  ],
})

// 路由守卫
router.beforeEach((to, _from, next) => {
  const token = localStorage.getItem('admin_token')
  const user = JSON.parse(localStorage.getItem('admin_user') || 'null')

  if (to.meta.requiresAuth && !token) {
    next({ path: '/login', query: { redirect: to.fullPath } })
    return
  }

  if (to.path === '/login' && token) {
    next({ path: '/' })
    return
  }

  // 角色验证：已登录但非 admin/moderator 时显示警告
  if (to.meta.requiresAuth && user) {
    const role = user.roles?.[0]?.name || user.role || 'member'
    const allowed = ['admin', 'super_admin', 'moderator']
    if (!allowed.includes(role)) {
      // 前端提示权限不足，后端 API 会做真正的权限校验
      console.warn(`用户 ${user.username} 角色 ${role} 无权访问管理后台`)
    }
  }

  next()
})

export default router