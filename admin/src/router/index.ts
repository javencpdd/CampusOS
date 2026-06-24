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

  if (to.meta.requiresAuth && !token) {
    next({ path: '/login', query: { redirect: to.fullPath } })
    return
  }

  if (to.path === '/login' && token) {
    next({ path: '/' })
    return
  }

  next()
})

export default router