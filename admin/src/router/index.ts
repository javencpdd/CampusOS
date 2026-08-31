import { createRouter, createWebHistory } from 'vue-router'
import type { NavigationGuard } from 'vue-router'
import { appearanceRoutes } from '../modules/appearance/routes'
import { academicTermRoutes } from '../modules/academicterm/routes'
import { communityRoutes } from '../modules/community/routes'
import { featureRoutes } from '../modules/features/routes'
import { identityRoutes } from '../modules/identity/routes'
import { integrationRoutes } from '../modules/integrations/routes'
import { operationRoutes } from '../modules/operations/routes'
import { pluginRoutes } from '../modules/plugins/routes'
import { clearAdminSession, ensureAdminAccessToken, getAdminAccessToken, getAdminRoleNames } from '../modules/identity/session'

const legacyRedirects = [
  '/admin', '/admin/users', '/admin/moderators', '/admin/threads', '/admin/categories', '/admin/docs', '/admin/architecture', '/admin/academic-terms',
  '/admin/plugins', '/admin/plugin-center', '/admin/features', '/admin/appearance', '/admin/extensions', '/admin/permissions', '/admin/admin-admission', '/admin/integrations', '/admin/reviews', '/admin/events', '/admin/platform-logs', '/admin/challenge-policy',
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
        ...academicTermRoutes,
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

export const adminAuthGuard: NavigationGuard = async (to) => {
  if (to.meta.requiresAuth && !(await ensureAdminAccessToken())) {
    return { path: '/login', query: { redirect: to.fullPath } }
  }
  if (to.path === '/login' && getAdminAccessToken()) return { path: '/' }
  if (to.meta.adminOnly) {
    const roles = getAdminRoleNames()
    if (!roles.some((role) => role === 'admin' || role === 'super_admin')) {
      clearAdminSession()
      return { path: '/login', query: { redirect: to.fullPath } }
    }
  }
  return true
}

router.beforeEach(adminAuthGuard)

export default router
