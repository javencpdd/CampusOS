import type { RouteRecordRaw } from 'vue-router'

export const appearanceRoutes: RouteRecordRaw[] = [
  {
    path: 'appearance',
    name: 'Appearance',
    component: () => import('./pages/AppearanceManageView.vue'),
    meta: { title: '外观与风格包' },
  },
]
