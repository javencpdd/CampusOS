import type { RouteRecordRaw } from 'vue-router'

export const integrationRoutes: RouteRecordRaw[] = [
	{ path: 'integrations/principals', name: 'IntegrationPrincipals', component: () => import('./pages/IntegrationPrincipalsView.vue'), meta: { title: '机器集成凭据' } },
  { path: 'integrations', name: 'Integrations', component: () => import('./pages/IntegrationCenterView.vue'), meta: { title: '集成中心' } },
]
