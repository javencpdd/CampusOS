import type { RouteRecordRaw } from 'vue-router'

export const identityRoutes: RouteRecordRaw[] = [
  { path: 'users', name: 'Users', component: () => import('./pages/UserManageView.vue'), meta: { title: '用户管理' } },
  { path: 'moderators', name: 'Moderators', component: () => import('@/modules/moderation/pages/ModeratorManageView.vue'), meta: { title: '版主管理', adminOnly: true } },
]
