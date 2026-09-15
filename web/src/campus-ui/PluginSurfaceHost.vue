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
    <DeclarativeRenderer
      v-else-if="surface?.renderer === 'schema' && surface.schema"
      :node="surface.schema"
      :plugin="surface.plugin"
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
    <DeclarativeRenderer
      v-else-if="surface?.renderer === 'schema' && surface.schema"
      :node="surface.schema"
      :plugin="surface.plugin"
    />
  </el-drawer>
</template>

<script setup lang="ts">
import { usePluginSurfaceHost } from './surfaceHost'
import { trustedModules } from './trustedModules'
import { computed, watch } from 'vue'
import { useUIRuntimeStore } from '@/modules/plugin-runtime/store'
import DeclarativeRenderer from './DeclarativeRenderer.vue'

const { activeSurface, closePluginSurface } = usePluginSurfaceHost()
const runtime = useUIRuntimeStore()
const surface = computed(() => (activeSurface.value ? runtime.surface(activeSurface.value.surfaceID) : undefined))
const viewer = computed(() =>
  surface.value?.renderer === 'trusted-module' && surface.value.module_id
    ? trustedModules[surface.value.module_id]
    : undefined,
)
watch(surface, (current) => {
  if (
    activeSurface.value &&
    (!current || !current.lifecycle.desired_enabled || current.lifecycle.frontend_state !== 'loaded')
  ) {
    closePluginSurface()
  }
})
</script>
