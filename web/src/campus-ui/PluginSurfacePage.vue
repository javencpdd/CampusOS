<template>
  <component v-if="trustedComponent" :is="trustedComponent" />
  <IsolatedPluginFrame
    v-else-if="available && frameIdentity && frameURL"
    :identity="frameIdentity"
    :src="frameURL"
    :title="surface?.type === 'document-preview' ? 'PDF 预览' : '插件界面'"
    :request="requestFromFrame"
  />
  <DeclarativeRenderer
    v-else-if="available && surface?.renderer === 'schema' && surface.schema"
    :node="surface.schema"
    :plugin="surface.plugin"
    :resource-context="resourceContext"
  />
  <el-result
    v-else
    icon="warning"
    title="插件界面不兼容"
    sub-title="CampusOS 已阻止不受支持的渲染器，业务数据未受影响。"
  />
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useUIRuntimeStore } from '@/modules/plugin-runtime/store'
import DeclarativeRenderer from './DeclarativeRenderer.vue'
import { trustedModules } from './trustedModules'
import { useSurfaceResourceContext } from './useSurfaceResourceContext'
import IsolatedPluginFrame from './IsolatedPluginFrame.vue'
import { handleIsolatedPluginRequest, isolatedPluginFrameURL } from './isolatedPluginHost'
import type { PluginBridgeRequest, PluginFrameIdentity } from './isolatedPluginBridge'
const route = useRoute()
const runtime = useUIRuntimeStore()
const surface = computed(() => runtime.surface(String(route.meta.surfaceId || '')))
const versionChanged = ref(false)
watch(
  () => surface.value?.plugin_version,
  (current, previous) => {
    if (previous && current !== previous) versionChanged.value = true
  },
)
watch(
  () => route.query.invocation,
  () => {
    versionChanged.value = false
  },
)
const available = computed(
  () =>
    !versionChanged.value &&
    surface.value?.lifecycle.desired_enabled &&
    surface.value.lifecycle.frontend_state === 'loaded',
)
const resourceContext = useSurfaceResourceContext(
  computed(() => (surface.value?.renderer === 'schema' ? String(route.query.invocation || '') : '')),
  computed(() => surface.value?.id || ''),
  computed(() => surface.value?.plugin || ''),
)
const trustedComponent = computed(() =>
  available.value && surface.value?.renderer === 'trusted-module' && surface.value.module_id
    ? trustedModules[surface.value.module_id]
    : undefined,
)
const invocationID = computed(() => String(route.query.invocation || ''))
const frameIdentity = computed<PluginFrameIdentity | undefined>(() => {
  const frame = surface.value?.frame
  if (!frame || surface.value?.renderer !== 'isolated-iframe') return undefined
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
  return frame && invocationID.value ? isolatedPluginFrameURL(frame.src, invocationID.value) : ''
})
const requestFromFrame = (request: PluginBridgeRequest) => {
  if (!surface.value || !invocationID.value) return Promise.reject(new Error('预览上下文不可用。'))
  return handleIsolatedPluginRequest(surface.value, invocationID.value, request)
}
</script>
