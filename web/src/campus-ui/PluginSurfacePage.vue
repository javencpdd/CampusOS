<template>
  <component v-if="trustedComponent" :is="trustedComponent" />
  <template v-else-if="available && frameIdentity && frameURL">
    <IsolatedPluginFrame
      :identity="frameIdentity"
      :src="frameURL"
      :title="surface?.type === 'document-preview' ? 'PDF 预览' : '插件界面'"
      :request="requestFromFrame"
    />
  </template>
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
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useUIRuntimeStore } from '@/modules/plugin-runtime/store'
import DeclarativeRenderer from './DeclarativeRenderer.vue'
import { trustedModules } from './trustedModules'
import { useSurfaceResourceContext } from './useSurfaceResourceContext'
import IsolatedPluginFrame from './IsolatedPluginFrame.vue'
import { handleIsolatedPluginRequest, isolatedPluginFrameURL } from './isolatedPluginHost'
import type { PluginBridgeRequest, PluginFrameIdentity } from './isolatedPluginBridge'
const route = useRoute()
const router = useRouter()
let disposed = false
onBeforeUnmount(() => {
  disposed = true
})
const runtime = useUIRuntimeStore()
const surface = computed(() => runtime.surface(String(route.meta.surfaceId || '')))
const versionChanged = ref(false)
watch(
  () => [route.meta.surfaceId, route.query.invocation, surface.value?.plugin_version] as const,
  ([surfaceID, invocation, version], [previousSurfaceID, previousInvocation, previousVersion]) => {
    // The router reuses this component across built-in and plugin pages.
    // Fence version changes only within the current surface and invocation.
    if (surfaceID !== previousSurfaceID || invocation !== previousInvocation) {
      versionChanged.value = false
    } else if (previousVersion && version !== previousVersion) {
      versionChanged.value = true
    }
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
    ...(frame.opaque ? { opaque: true } : {}),
  }
})
const frameURL = computed(() => {
  const frame = surface.value?.frame
  return frame ? isolatedPluginFrameURL(frame.src, invocationID.value) : ''
})
const requestFromFrame = (request: PluginBridgeRequest, portActive: () => boolean = () => true) => {
  if (!surface.value) return Promise.reject(new Error('插件上下文不可用。'))
  return handleIsolatedPluginRequest(
    surface.value,
    invocationID.value,
    request,
    (id) =>
      disposed || !portActive()
        ? Promise.reject(new Error('插件界面已关闭'))
        : router.push({ path: '/threads/create', query: { plugin_import: id } }),
    () => !disposed && portActive(),
  )
}
</script>
