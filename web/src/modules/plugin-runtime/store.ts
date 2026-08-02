import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import router from '@/router'
import { RuntimeRegistry } from '@/campus-ui/runtimeRegistry'
import { preloadTrustedModules } from '@/campus-ui/trustedModules'
import type { RuntimeNavigation, RuntimeSurface, UIAction, UIRuntimeManifest } from '@/campus-ui/contracts'
import { uiRuntimeApi } from './api'

const registry = new RuntimeRegistry(router)

export const useUIRuntimeStore = defineStore('ui-runtime', () => {
  const revision = ref(0)
  const navigation = ref<RuntimeNavigation[]>([])
  const surfaces = ref<Map<string, RuntimeSurface>>(new Map())
  const actions = ref<Map<string, UIAction & { plugin: string }>>(new Map())
  const slots = ref<Map<string, RuntimeSurface[]>>(new Map())
  const loading = ref(false)
  const error = ref('')
  let events: EventSource | null = null
  let retryTimer: number | undefined
  let syncPromise: Promise<void> | null = null
  let syncQueued = false

  const degradedPlugins = computed(() =>
    Array.from(surfaces.value.values()).filter((surface) => surface.lifecycle.health !== 'healthy'),
  )

  const syncOnce = async () => {
    try {
      const envelope = (await uiRuntimeApi.manifest()) as any
      const manifest = envelope.data as UIRuntimeManifest
      if (manifest.revision < revision.value) return
      const snapshot = registry.replace(manifest)
      revision.value = snapshot.revision
      navigation.value = snapshot.navigation
      surfaces.value = new Map(snapshot.surfaces)
      actions.value = new Map(snapshot.actions)
      slots.value = new Map(snapshot.slots)
      void preloadTrustedModules(
        Array.from(snapshot.surfaces.values())
          .map((surface) => surface.module_id)
          .filter((moduleID): moduleID is string => Boolean(moduleID)),
      )
      error.value = ''
    } catch (cause: any) {
      error.value = cause?.error?.message || cause?.msg || cause?.message || '插件界面运行时同步失败'
    }
  }

  const sync = () => {
    syncQueued = true
    if (syncPromise) return syncPromise
    loading.value = true
    syncPromise = (async () => {
      while (syncQueued) {
        syncQueued = false
        await syncOnce()
      }
    })().finally(() => {
      loading.value = false
      syncPromise = null
    })
    return syncPromise
  }

  const connect = () => {
    events?.close()
    events = new EventSource('/api/v1/ui/events')
    events.addEventListener('revision', (event) => {
      const next = Number((event as MessageEvent).data)
      if (Number.isFinite(next) && next > revision.value) void sync()
    })
    events.onerror = () => {
      events?.close()
      window.clearTimeout(retryTimer)
      retryTimer = window.setTimeout(connect, 3000)
    }
  }

  const initialize = async () => {
    await sync()
    connect()
  }
  const surface = (id: string) => surfaces.value.get(id)
  const action = (id: string) => actions.value.get(id)

  return {
    revision,
    navigation,
    surfaces,
    actions,
    slots,
    loading,
    error,
    degradedPlugins,
    initialize,
    sync,
    surface,
    action,
  }
})
