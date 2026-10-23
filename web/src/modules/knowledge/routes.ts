import type { RouteRecordRaw } from 'vue-router'

export const knowledgeRoutes: RouteRecordRaw[] = [
  {
    path: '/knowledge/private',
    name: 'PrivateKnowledge',
    component: () => import('./pages/PrivateKnowledgeView.vue'),
    meta: { title: '我的私人知识', requiresAuth: true },
  },
  {
    path: '/knowledge',
    name: 'KnowledgeSearch',
    component: () => import('./pages/KnowledgeSearchView.vue'),
    meta: { title: '知识检索' },
  },
]
