import type { RouteRecordRaw } from 'vue-router'

export const secondhandRoutes: RouteRecordRaw[] = [
  { path: '/secondhand', name: 'SecondhandList', component: () => import('./pages/SecondhandListView.vue') },
  {
    path: '/secondhand/create',
    name: 'SecondhandCreate',
    component: () => import('./pages/SecondhandEditorView.vue'),
    meta: { requiresAuth: true },
  },
  { path: '/secondhand/:id', name: 'SecondhandDetail', component: () => import('./pages/SecondhandDetailView.vue') },
  {
    path: '/secondhand/:id/edit',
    name: 'SecondhandEdit',
    component: () => import('./pages/SecondhandEditorView.vue'),
    meta: { requiresAuth: true },
  },
]
