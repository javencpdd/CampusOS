import type { RouteRecordRaw } from 'vue-router'

export const mutualAidRoutes: RouteRecordRaw[] = [
  { path: '/mutual-aid', name: 'MutualAidList', component: () => import('./pages/MutualAidListView.vue') },
  {
    path: '/mutual-aid/create',
    name: 'MutualAidCreate',
    component: () => import('./pages/MutualAidEditorView.vue'),
    meta: { requiresAuth: true },
  },
  { path: '/mutual-aid/:id', name: 'MutualAidDetail', component: () => import('./pages/MutualAidDetailView.vue') },
  {
    path: '/mutual-aid/:id/edit',
    name: 'MutualAidEdit',
    component: () => import('./pages/MutualAidEditorView.vue'),
    meta: { requiresAuth: true },
  },
]
