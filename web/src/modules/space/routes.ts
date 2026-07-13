import type { RouteRecordRaw } from 'vue-router'

export const spaceRoutes: RouteRecordRaw[] = [
  { path: '/u/:username', name: 'PublicSpace', component: () => import('./pages/PublicSpaceView.vue') },
]
