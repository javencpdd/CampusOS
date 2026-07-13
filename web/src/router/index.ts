import { createRouter, createWebHistory } from 'vue-router'
import type { NavigationGuard } from 'vue-router'
import { communityRoutes } from '../modules/community/routes'
import { identityRoutes } from '../modules/identity/routes'
import { pluginCenterRoutes } from '../modules/plugin-center/routes'
import { spaceRoutes } from '../modules/space/routes'

const router = createRouter({
  history: createWebHistory(),
  routes: [...communityRoutes, ...identityRoutes, ...spaceRoutes, ...pluginCenterRoutes],
})

export const webAuthGuard: NavigationGuard = (to, _from, next) => {
  const token = localStorage.getItem('access_token')
  if (to.meta.requiresAuth && !token) {
    next({ path: '/login', query: { redirect: to.fullPath } })
    return
  }
  next()
}

router.beforeEach(webAuthGuard)

export default router
