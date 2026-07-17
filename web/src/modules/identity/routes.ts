import type { RouteRecordRaw } from 'vue-router'

export const identityRoutes: RouteRecordRaw[] = [
  { path: '/login', name: 'Login', component: () => import('./pages/LoginView.vue') },
  { path: '/register', name: 'Register', component: () => import('./pages/RegisterView.vue') },
  { path: '/reset-password', name: 'PasswordReset', component: () => import('./pages/PasswordResetView.vue') },
  {
    path: '/account/security',
    name: 'AccountSecurity',
    component: () => import('./pages/AccountSecurityView.vue'),
    meta: { requiresAuth: true },
  },
]
