import type { RouteRecordRaw } from 'vue-router'

export const pluginRoutes: RouteRecordRaw[] = [
  { path: 'plugins', name: 'Plugins', component: () => import('./pages/PluginManageView.vue'), meta: { title: '插件管理' } },
  { path: 'plugin-center', name: 'PluginCenter', component: () => import('./pages/PluginCenterView.vue'), meta: { title: '插件中心' } },
]
