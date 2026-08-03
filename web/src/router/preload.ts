const coreViewLoaders = [
  () => import('@/modules/community/pages/HomeView.vue'),
  () => import('@/modules/community/pages/ThreadListView.vue'),
  () => import('@/modules/community/pages/ThreadDetailView.vue'),
  () => import('@/modules/community/pages/CreateThreadView.vue'),
  () => import('@/modules/mutual-aid/pages/MutualAidListView.vue'),
  () => import('@/modules/mutual-aid/pages/MutualAidDetailView.vue'),
  () => import('@/modules/mutual-aid/pages/MutualAidEditorView.vue'),
  () => import('@/modules/secondhand/pages/SecondhandListView.vue'),
  () => import('@/modules/secondhand/pages/SecondhandDetailView.vue'),
  () => import('@/modules/secondhand/pages/SecondhandEditorView.vue'),
  () => import('@/modules/space/pages/PublicSpaceView.vue'),
  () => import('@/modules/plugin-center/pages/PluginCenterView.vue'),
  () => import('@/modules/identity/pages/AccountSecurityView.vue'),
]

let preloadPromise: Promise<PromiseSettledResult<unknown>[]> | null = null

// Keep route-level code splitting, but warm the core page chunks once the shell is
// interactive. A first visit then behaves like an ordinary client-side route
// change instead of briefly looking like a full-page refresh.
export const preloadCoreViews = () => {
  preloadPromise ||= Promise.allSettled(coreViewLoaders.map((loader) => loader()))
  return preloadPromise
}
