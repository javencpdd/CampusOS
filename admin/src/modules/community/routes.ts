import type { RouteRecordRaw } from 'vue-router'

export const communityRoutes: RouteRecordRaw[] = [
  { path: 'threads', name: 'Threads', component: () => import('./pages/ThreadManageView.vue'), meta: { title: '帖子管理' } },
  { path: 'categories', name: 'Categories', component: () => import('./pages/CategoryManageView.vue'), meta: { title: '版块管理' } },
  { path: 'reviews', name: 'Reviews', component: () => import('./pages/AdminReviews.vue'), meta: { title: '帖子审核' } },
]
