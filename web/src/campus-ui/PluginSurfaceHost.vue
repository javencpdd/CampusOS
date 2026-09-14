<template>
  <el-dialog
    v-if="activeSurface?.presentation !== 'drawer'"
    :model-value="Boolean(activeSurface)"
    :fullscreen="activeSurface?.presentation === 'fullscreen'"
    :width="activeSurface?.presentation === 'fullscreen' ? undefined : 'min(1120px, 96vw)'"
    :title="activeSurface?.presentation === 'fullscreen' ? 'PDF 全屏预览' : 'PDF 预览'"
    append-to-body
    destroy-on-close
    @close="closePluginSurface"
  >
    <component
      :is="pdfViewer"
      v-if="activeSurface?.surfaceID === pdfSurfaceID"
      :invocation-id="activeSurface.invocationID"
    />
  </el-dialog>
  <el-drawer
    v-else
    :model-value="Boolean(activeSurface)"
    title="PDF 预览"
    size="min(92vw, 920px)"
    append-to-body
    destroy-on-close
    @close="closePluginSurface"
  >
    <component
      :is="pdfViewer"
      v-if="activeSurface.surfaceID === pdfSurfaceID"
      :invocation-id="activeSurface.invocationID"
    />
  </el-drawer>
</template>

<script setup lang="ts">
import { usePluginSurfaceHost } from './surfaceHost'
import { trustedModules } from './trustedModules'

const pdfSurfaceID = 'builtin.pdf-viewer.preview'
const pdfViewer = trustedModules['core.pdf-viewer']
const { activeSurface, closePluginSurface } = usePluginSurfaceHost()
</script>
