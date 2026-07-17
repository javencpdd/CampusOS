import { createRouter, createWebHistory } from 'vue-router'
import type { NavigationGuard } from 'vue-router'
import { communityRoutes } from '../modules/community/routes'
import { identityRoutes } from '../modules/identity/routes'
import { pluginCenterRoutes } from '../modules/plugin-center/routes'
import { spaceRoutes } from '../modules/space/routes'
import { mutualAidRoutes } from '../modules/mutual-aid/routes'
import { secondhandRoutes } from '../modules/secondhand/routes'
import { ensureAccessToken, getAccessToken } from '../modules/identity/session'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    ...communityRoutes,
    ...identityRoutes,
    ...spaceRoutes,
    ...pluginCenterRoutes,
    ...mutualAidRoutes,
    ...secondhandRoutes,
  ],
})

export const webAuthGuard: NavigationGuard = async (to) => {
  if (to.meta.requiresAuth && !(await ensureAccessToken())) {
    return { path: '/login', query: { redirect: to.fullPath } }
  }
  if (to.path === '/login' && getAccessToken()) return { path: '/' }
  return true
}

router.beforeEach(webAuthGuard)

export default router
