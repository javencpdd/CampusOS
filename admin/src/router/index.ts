import { createRouter, createWebHistory } from 'vue-router'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/login',
      name: 'Login',
      component: () => import('@/views/LoginView.vue'),
    },
    { path: '/admin', redirect: '/' },
    { path: '/admin/users', redirect: '/users' },
    { path: '/admin/moderators', redirect: '/moderators' },
    { path: '/admin/threads', redirect: '/threads' },
    { path: '/admin/categories', redirect: '/categories' },
    { path: '/admin/docs', redirect: '/docs' },
    { path: '/admin/architecture', redirect: '/architecture' },
    { path: '/admin/plugins', redirect: '/plugins' },
    { path: '/admin/integrations', redirect: '/integrations' },
    { path: '/admin/reviews', redirect: '/reviews' },
    { path: '/admin/events', redirect: '/events' },
    { path: '/admin/platform-logs', redirect: '/platform-logs' },
    {
      path: '/',
      component: () => import('@/components/AdminLayout.vue'),
      meta: { requiresAuth: true, adminOnly: true },
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
          path: 'moderators',
          name: 'Moderators',
          component: () => import('@/views/ModeratorManageView.vue'),
          meta: { title: '版主管理', adminOnly: true },
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
          path: 'docs',
          name: 'DeveloperDocs',
          component: () => import('@/views/DeveloperDocsView.vue'),
          meta: { title: '相关资料' },
        },
        {
          path: 'architecture',
          name: 'SystemArchitecture',
          component: () => import('@/views/SystemArchitectureView.vue'),
          meta: { title: '数据架构' },
        },
        {
          path: 'plugins',
          name: 'Plugins',
          component: () => import('@/views/PluginManageView.vue'),
          meta: { title: '插件管理' },
        },
        {
          path: 'integrations',
          name: 'Integrations',
          component: () => import('@/views/IntegrationCenterView.vue'),
          meta: { title: '集成中心' },
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
        {
          path: 'platform-logs',
          name: 'PlatformLogs',
          component: () => import('@/views/PlatformLogsView.vue'),
          meta: { title: '平台日志' },
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

  if (to.meta.adminOnly && user) {
    const roles = user.roles?.map((item: { name?: string }) => item.name).filter(Boolean) || []
    if (!roles.some((role: string) => role === 'admin' || role === 'super_admin')) {
      localStorage.removeItem('admin_user')
      localStorage.removeItem('admin_token')
      next({ path: '/login', query: { redirect: to.fullPath } })
      return
    }
  }

  next()
})

export default router
