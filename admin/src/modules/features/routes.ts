import type { RouteRecordRaw } from 'vue-router'

export const featureRoutes: RouteRecordRaw[] = [
  {
    path: 'features',
    name: 'Features',
    component: () => import('./pages/FeatureManageView.vue'),
    meta: { title: '内置功能' },
  },
]
