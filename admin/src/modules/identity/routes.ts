import type { RouteRecordRaw } from 'vue-router'

export const identityRoutes: RouteRecordRaw[] = [
  { path: 'users', name: 'Users', component: () => import('./pages/UserManageView.vue'), meta: { title: '用户管理' } },
  { path: 'moderators', name: 'Moderators', component: () => import('@/modules/moderation/pages/ModeratorManageView.vue'), meta: { title: '版主管理', adminOnly: true } },
  { path: 'permissions', name: 'Permissions', component: () => import('./pages/PermissionManageView.vue'), meta: { title: '角色与权限', adminOnly: true } },
  { path: 'admin-admission', name: 'AdminAdmission', component: () => import('./pages/AdminAdmissionView.vue'), meta: { title: '管理员准入', adminOnly: true } },
  { path: 'mfa-policy', name: 'MFAPolicy', component: () => import('./pages/MFAPolicyView.vue'), meta: { title: '管理员 MFA 策略', adminOnly: true } },
  { path: 'account-recovery', name: 'AccountRecovery', component: () => import('./pages/AccountRecoveryView.vue'), meta: { title: '账号恢复', adminOnly: true } },
  { path: 'challenge-policy', name: 'ChallengePolicy', component: () => import('./pages/ChallengePolicyView.vue'), meta: { title: '验证码策略', adminOnly: true } },
]
