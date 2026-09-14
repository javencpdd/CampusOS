import { readonly, ref } from 'vue'

export type SurfacePresentation = 'modal' | 'drawer' | 'fullscreen' | 'new-tab'

export interface SurfaceOpenRequest {
  surfaceID: string
  invocationID: string
  presentation: SurfacePresentation
}

const activeSurface = ref<SurfaceOpenRequest | null>(null)

const allowedPresentations = new Set<SurfacePresentation>(['modal', 'drawer', 'fullscreen', 'new-tab'])

// Only a trusted host module calls this function after an explicit user
// gesture. The plugin receives an opaque invocation ID, never a URL, path or
// credential.
export const openPluginSurface = (request: SurfaceOpenRequest) => {
  if (!request.surfaceID || !request.invocationID || !allowedPresentations.has(request.presentation)) {
    throw new Error('插件界面请求无效')
  }
  if (request.presentation === 'new-tab') {
    const target = `/extensions/builtin.pdf-viewer/preview?invocation=${encodeURIComponent(request.invocationID)}`
    window.open(target, '_blank', 'noopener,noreferrer')
    return
  }
  activeSurface.value = { ...request }
}

export const closePluginSurface = () => {
  activeSurface.value = null
}

export const usePluginSurfaceHost = () => ({
  activeSurface: readonly(activeSurface),
  openPluginSurface,
  closePluginSurface,
})
