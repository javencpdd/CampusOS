import type { RouteRecordRaw } from 'vue-router'

export const pluginRoutes: RouteRecordRaw[] = [
  { path: 'extensions', name: 'Extensions', component: () => import('./pages/ExtensionHubView.vue'), meta: { title: '扩展与集成' } },
  { path: 'plugins', name: 'Plugins', component: () => import('./pages/PluginManageView.vue'), meta: { title: '插件管理' } },
  { path: 'plugin-center', name: 'PluginCenter', component: () => import('./pages/PluginCenterView.vue'), meta: { title: '插件中心' } },
]
