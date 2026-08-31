import { defineAsyncComponent, type Component } from 'vue'

type TrustedModuleLoader = () => Promise<{ default: Component }>

const trustedModuleLoaders: Record<string, TrustedModuleLoader> = {
  'core.schedule': () => import('@/modules/schedule/pages/ScheduleView.vue'),
  'core.personal-documents': () => import('@/modules/personal-documents/pages/PersonalDocumentsView.vue'),
  'core.personal-space': () => import('@/modules/space/pages/SpaceSettingsView.vue'),
  'core.richtext-editor': () => import('@/modules/community/pages/CreateThreadView.vue'),
  'core.appearance': () => import('@/modules/appearance/pages/AppearanceSettingsView.vue'),
}

export const trustedModules: Record<string, ReturnType<typeof defineAsyncComponent>> = Object.fromEntries(
  Object.entries(trustedModuleLoaders).map(([id, loader]) => [id, defineAsyncComponent(loader)]),
)

export const preloadTrustedModules = (moduleIDs?: Iterable<string>) => {
  const requested = moduleIDs ? new Set(moduleIDs) : null
  return Promise.allSettled(
    Object.entries(trustedModuleLoaders)
      .filter(([id]) => !requested || requested.has(id))
      .map(([, loader]) => loader()),
  )
}
