import type { RouteRecordRaw } from 'vue-router'

export const pluginCenterRoutes: RouteRecordRaw[] = [
  {
    path: '/plugins',
    name: 'PluginCenter',
    component: () => import('./pages/PluginCenterView.vue'),
    meta: { requiresAuth: true },
  },
]
