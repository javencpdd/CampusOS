<template>
  <component v-if="trustedComponent" :is="trustedComponent" />
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
</script>
