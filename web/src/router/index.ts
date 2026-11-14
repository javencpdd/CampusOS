import { createRouter, createWebHistory } from 'vue-router'
import type { NavigationGuard } from 'vue-router'
import { communityRoutes } from '../modules/community/routes'
import { identityRoutes } from '../modules/identity/routes'
import { pluginCenterRoutes } from '../modules/plugin-center/routes'
import { spaceRoutes } from '../modules/space/routes'
import { mutualAidRoutes } from '../modules/mutual-aid/routes'
import { secondhandRoutes } from '../modules/secondhand/routes'
import { knowledgeRoutes } from '../modules/knowledge/routes'
import { ensureAccessToken } from '../modules/identity/session'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    ...communityRoutes,
    ...identityRoutes,
    ...spaceRoutes,
    ...pluginCenterRoutes,
    ...mutualAidRoutes,
    ...secondhandRoutes,
    ...knowledgeRoutes,
    {
      path: '/extensions/:plugin/ui',
      name: 'PluginUnavailable',
      component: () => import('../campus-ui/PluginUnavailableView.vue'),
      meta: { requiresAuth: true, pluginUnavailable: true },
    },
  ],
})

export const webAuthGuard: NavigationGuard = async (to) => {
  if (to.meta.requiresAuth && !(await ensureAccessToken())) {
    return { path: '/login', query: { redirect: to.fullPath } }
  }
  if (to.path === '/login' && (await ensureAccessToken())) return { path: '/', replace: true }
  return true
}

router.beforeEach(webAuthGuard)

window.addEventListener('campusos:identity-session-expired', () => {
  const current = router.currentRoute.value
  if (current.meta.requiresAuth) void router.replace({ path: '/login', query: { redirect: current.fullPath } })
})

export default router
