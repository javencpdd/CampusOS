<template>
  <component v-if="trustedComponent" :is="trustedComponent" />
  <DeclarativeRenderer
    v-else-if="surface?.renderer === 'schema' && surface.schema"
    :node="surface.schema"
    :plugin="surface.plugin"
  />
  <el-result
    v-else
    icon="warning"
    title="插件界面不兼容"
    sub-title="CampusOS 已阻止不受支持的渲染器，业务数据未受影响。"
  />
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { useUIRuntimeStore } from '@/modules/plugin-runtime/store'
import DeclarativeRenderer from './DeclarativeRenderer.vue'
import { trustedModules } from './trustedModules'
const route = useRoute()
const runtime = useUIRuntimeStore()
const surface = computed(() => runtime.surface(String(route.meta.surfaceId || '')))
const trustedComponent = computed(() =>
  surface.value?.renderer === 'trusted-module' && surface.value.module_id
    ? trustedModules[surface.value.module_id]
    : undefined,
)
</script>
