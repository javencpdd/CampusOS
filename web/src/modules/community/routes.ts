import type { RouteRecordRaw } from 'vue-router'

export const communityRoutes: RouteRecordRaw[] = [
  { path: '/', name: 'Home', component: () => import('./pages/HomeView.vue') },
  { path: '/threads', name: 'ThreadList', component: () => import('./pages/ThreadListView.vue') },
  { path: '/threads/:id', name: 'ThreadDetail', component: () => import('./pages/ThreadDetailView.vue') },
  {
    path: '/threads/create',
    name: 'CreateThread',
    component: () => import('./pages/CreateThreadView.vue'),
    meta: { requiresAuth: true },
  },
  {
    path: '/threads/:id/edit',
    name: 'EditThread',
    component: () => import('./pages/CreateThreadView.vue'),
    meta: { requiresAuth: true },
  },
]
