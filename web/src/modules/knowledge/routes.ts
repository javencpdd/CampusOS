import type { RouteRecordRaw } from 'vue-router'

export const knowledgeRoutes: RouteRecordRaw[] = [
  {
    path: '/knowledge',
    name: 'KnowledgeSearch',
    component: () => import('./pages/KnowledgeSearchView.vue'),
    meta: { title: '知识检索' },
  },
]
