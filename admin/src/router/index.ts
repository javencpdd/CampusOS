import { createRouter, createWebHistory } from 'vue-router'
import type { NavigationGuard } from 'vue-router'
import { appearanceRoutes } from '../modules/appearance/routes'
import { communityRoutes } from '../modules/community/routes'
import { featureRoutes } from '../modules/features/routes'
import { identityRoutes } from '../modules/identity/routes'
import { integrationRoutes } from '../modules/integrations/routes'
import { operationRoutes } from '../modules/operations/routes'
import { pluginRoutes } from '../modules/plugins/routes'

const legacyRedirects = [
  '/admin', '/admin/users', '/admin/moderators', '/admin/threads', '/admin/categories', '/admin/docs', '/admin/architecture',
  '/admin/plugins', '/admin/plugin-center', '/admin/features', '/admin/appearance', '/admin/extensions', '/admin/permissions', '/admin/integrations', '/admin/reviews', '/admin/events', '/admin/platform-logs',
]

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', name: 'Login', component: () => import('@/modules/identity/pages/LoginView.vue') },
    ...legacyRedirects.map((path) => ({ path, redirect: path.replace('/admin', '') || '/' })),
    {
      path: '/',
      component: () => import('@/components/AdminLayout.vue'),
      meta: { requiresAuth: true, adminOnly: true },
      children: [
        { path: '', name: 'Dashboard', component: () => import('@/modules/dashboard/pages/DashboardView.vue'), meta: { title: '仪表盘' } },
        ...identityRoutes,
        ...communityRoutes,
        ...featureRoutes,
        ...appearanceRoutes,
        ...pluginRoutes,
        ...integrationRoutes,
        ...operationRoutes,
      ],
    },
  ],
})

export const adminAuthGuard: NavigationGuard = (to, _from, next) => {
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
}

router.beforeEach(adminAuthGuard)

export default router
