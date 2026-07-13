import type { RouteRecordRaw } from 'vue-router'

export const operationRoutes: RouteRecordRaw[] = [
  { path: 'events', name: 'Events', component: () => import('./pages/EventLogView.vue'), meta: { title: '事件日志' } },
  { path: 'platform-logs', name: 'PlatformLogs', component: () => import('./pages/PlatformLogsView.vue'), meta: { title: '平台日志' } },
  { path: 'architecture', name: 'SystemArchitecture', component: () => import('@/modules/architecture/pages/SystemArchitectureView.vue'), meta: { title: '数据架构' } },
  { path: 'docs', name: 'DeveloperDocs', component: () => import('@/modules/docs/pages/DeveloperDocsView.vue'), meta: { title: '相关资料' } },
]
