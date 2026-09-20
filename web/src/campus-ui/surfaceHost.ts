import { readonly, ref } from 'vue'
import router from '@/router'
import { useUIRuntimeStore } from '@/modules/plugin-runtime/store'

export type SurfacePresentation = 'modal' | 'drawer' | 'fullscreen' | 'new-tab'

export interface SurfaceOpenRequest {
  surfaceID: string
  invocationID: string
  presentation: SurfacePresentation
  pluginVersion?: string
}

const activeSurface = ref<SurfaceOpenRequest | null>(null)
let returnFocus: HTMLElement | null = null

const allowedPresentations = new Set<SurfacePresentation>(['modal', 'drawer', 'fullscreen', 'new-tab'])

// Only a trusted host module calls this function after an explicit user
// gesture. The plugin receives an opaque invocation ID, never a URL, path or
// credential.
export const openPluginSurface = (request: SurfaceOpenRequest) => {
  if (!request.surfaceID || !request.invocationID || !allowedPresentations.has(request.presentation)) {
    throw new Error('插件界面请求无效')
  }
  const surface = useUIRuntimeStore().surface(request.surfaceID)
  if (!surface || !surface.lifecycle.desired_enabled || surface.lifecycle.frontend_state !== 'loaded') {
    throw new Error('预览界面未启用或不可用，请使用附件的“下载”按钮。')
  }
  if (!surface.presentations?.includes(request.presentation)) {
    throw new Error('该插件界面不支持所选展示方式，请重新选择。')
  }
  if (request.presentation === 'new-tab') {
    const route = router.getRoutes().find((candidate) => candidate.meta.surfaceId === request.surfaceID)
    if (
      !route ||
      !/^\/extensions\/[A-Za-z0-9_./-]+$/.test(route.path) ||
      route.path.split('/').some((part) => part === '.' || part === '..')
    ) {
      throw new Error('该插件未注册可用的同源预览页面，请使用弹窗或下载附件。')
    }
    const target = `${route.path}?invocation=${encodeURIComponent(request.invocationID)}`
    window.open(target, '_blank', 'noopener,noreferrer')
    return
  }
  if (!activeSurface.value) returnFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
  activeSurface.value = { ...request, pluginVersion: surface.plugin_version }
}

export const closePluginSurface = () => {
  activeSurface.value = null
  const target = returnFocus
  returnFocus = null
  if (target?.isConnected) target.focus()
}

export const usePluginSurfaceHost = () => ({
  activeSurface: readonly(activeSurface),
  openPluginSurface,
  closePluginSurface,
})
