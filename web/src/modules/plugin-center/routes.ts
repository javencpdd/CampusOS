import type { RouteRecordRaw } from 'vue-router'

export const pluginCenterRoutes: RouteRecordRaw[] = [
  {
    path: '/plugin-config',
    name: 'PluginV5Config',
    component: () => import('./pages/PluginV5ConfigView.vue'),
    meta: { requiresAuth: true },
  },
  {
    path: '/plugins',
    name: 'PluginCenter',
    component: () => import('./pages/PluginV5CenterView.vue'),
    meta: { requiresAuth: true },
  },
]
