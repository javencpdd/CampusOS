import type { RouteRecordRaw } from 'vue-router'

export const academicTermRoutes: RouteRecordRaw[] = [
  { path: 'academic-terms', name: 'AcademicTerms', component: () => import('./pages/AcademicTermManageView.vue'), meta: { title: '学期治理' } },
]
