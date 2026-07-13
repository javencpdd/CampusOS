import type { RouteRecordRaw } from 'vue-router'

export const integrationRoutes: RouteRecordRaw[] = [
  { path: 'integrations', name: 'Integrations', component: () => import('./pages/IntegrationCenterView.vue'), meta: { title: '集成中心' } },
]
