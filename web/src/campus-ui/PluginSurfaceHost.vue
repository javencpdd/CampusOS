<template>
  <el-dialog
    v-if="activeSurface?.presentation !== 'drawer'"
    :model-value="Boolean(activeSurface)"
    :fullscreen="activeSurface?.presentation === 'fullscreen'"
    :width="activeSurface?.presentation === 'fullscreen' ? undefined : 'min(1120px, 96vw)'"
    :title="surface?.type === 'document-preview' ? 'PDF 预览' : '插件界面'"
    append-to-body
    destroy-on-close
    @close="closePluginSurface"
  >
    <component :is="viewer" v-if="viewer && activeSurface" :invocation-id="activeSurface.invocationID" />
    <IsolatedPluginFrame
      v-else-if="frameIdentity && frameURL && activeSurface"
      :identity="frameIdentity"
      :src="frameURL"
      :title="surface?.type === 'document-preview' ? 'PDF 预览' : '插件界面'"
      :request="requestFromFrame"
    />
    <DeclarativeRenderer
      v-else-if="surface?.renderer === 'schema' && surface.schema"
      :node="surface.schema"
      :plugin="surface.plugin"
      :resource-context="resourceContext"
    />
  </el-dialog>
  <el-drawer
    v-else
    :model-value="Boolean(activeSurface)"
    :title="surface?.type === 'document-preview' ? 'PDF 预览' : '插件界面'"
    size="min(92vw, 920px)"
    append-to-body
    destroy-on-close
    @close="closePluginSurface"
  >
    <component :is="viewer" v-if="viewer" :invocation-id="activeSurface.invocationID" />
    <IsolatedPluginFrame
      v-else-if="frameIdentity && frameURL && activeSurface"
      :identity="frameIdentity"
      :src="frameURL"
      :title="surface?.type === 'document-preview' ? 'PDF 预览' : '插件界面'"
      :request="requestFromFrame"
    />
    <DeclarativeRenderer
      v-else-if="surface?.renderer === 'schema' && surface.schema"
      :node="surface.schema"
      :plugin="surface.plugin"
      :resource-context="resourceContext"
    />
  </el-drawer>
</template>

<script setup lang="ts">
import { usePluginSurfaceHost } from './surfaceHost'
import { trustedModules } from './trustedModules'
import { computed, watch } from 'vue'
import { useUIRuntimeStore } from '@/modules/plugin-runtime/store'
import DeclarativeRenderer from './DeclarativeRenderer.vue'
import { useSurfaceResourceContext } from './useSurfaceResourceContext'
import IsolatedPluginFrame from './IsolatedPluginFrame.vue'
import { handleIsolatedPluginRequest, isolatedPluginFrameURL } from './isolatedPluginHost'
import type { PluginBridgeRequest, PluginFrameIdentity } from './isolatedPluginBridge'

const { activeSurface, closePluginSurface } = usePluginSurfaceHost()
const runtime = useUIRuntimeStore()
const surface = computed(() => (activeSurface.value ? runtime.surface(activeSurface.value.surfaceID) : undefined))
const resourceContext = useSurfaceResourceContext(
  computed(() => (surface.value?.renderer === 'schema' ? activeSurface.value?.invocationID || '' : '')),
  computed(() => surface.value?.id || ''),
  computed(() => surface.value?.plugin || ''),
)
const viewer = computed(() =>
  surface.value?.renderer === 'trusted-module' && surface.value.module_id
    ? trustedModules[surface.value.module_id]
    : undefined,
)
const frameIdentity = computed<PluginFrameIdentity | undefined>(() => {
  const frame = surface.value?.frame
  const active = activeSurface.value
  if (!frame || !active || surface.value?.renderer !== 'isolated-iframe') return undefined
  return {
    pluginKey: surface.value.plugin,
    pluginVersion: surface.value.plugin_version || '',
    surfaceID: surface.value.id.replace(`${surface.value.plugin}.`, ''),
    audience: frame.audience,
    origin: frame.origin,
  }
})
const frameURL = computed(() => {
  const frame = surface.value?.frame
  const active = activeSurface.value
  return frame && active ? isolatedPluginFrameURL(frame.src, active.invocationID) : ''
})
const requestFromFrame = (request: PluginBridgeRequest) => {
  if (!surface.value || !activeSurface.value) return Promise.reject(new Error('预览已关闭。'))
  return handleIsolatedPluginRequest(surface.value, activeSurface.value.invocationID, request)
}
watch(surface, (current) => {
  if (
    activeSurface.value &&
    (!current ||
      !current.lifecycle.desired_enabled ||
      current.lifecycle.frontend_state !== 'loaded' ||
      current.plugin_version !== activeSurface.value.pluginVersion)
  ) {
    closePluginSurface()
  }
})
</script>
