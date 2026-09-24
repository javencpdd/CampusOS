import { ref, watch, type Ref } from 'vue'
import type { UIResourceContext } from './contracts'
import { richTextApi } from '@/modules/richtext/api'

// Context comes only from the authenticated host summary. Schema/action body
// fields cannot supply a path, owner, token or replacement resource reference.
export function useSurfaceResourceContext(id: Ref<string>, surfaceID: Ref<string>, plugin: Ref<string>) {
  const resourceContext = ref<UIResourceContext>()
  watch(
    [id, surfaceID, plugin],
    async ([currentID, currentSurface, currentPlugin], _, onCleanup) => {
      resourceContext.value = undefined
      let stale = false
      onCleanup(() => {
        stale = true
      })
      if (!currentID || !currentSurface || !currentPlugin) return
      try {
        const response: any = await richTextApi.getPDFInvocation(currentID)
        const data = response?.data || response
        if (!stale && data.invocation?.surface_id === currentSurface && data.invocation?.plugin_key === currentPlugin) {
          resourceContext.value = data.resource_context
        }
      } catch {
        // The Viewer owns its error UI. A schema action without context fails
        // closed with an actionable selection/reopen message.
      }
    },
    { immediate: true },
  )
  return resourceContext
}
