<template>
  <div
    class="campus-theme-root"
    data-campusos-theme-root
    data-campusos-web
    :data-web-theme="themeStore.activeName || 'default'"
    :data-layout-mode="layout.mode"
    :data-header-mode="layout.header_mode"
    :data-animation-preset="layout.animation_preset || 'none'"
    :style="rootStyle"
  >
    <slot />
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useWebThemeStore } from '@/modules/appearance/store'

const themeStore = useWebThemeStore()
const layout = computed(() => themeStore.effectiveLayout || { mode: 'contained' })
const rootStyle = computed(() => {
  const tokens = themeStore.effectiveTokens || {}
  const styles: Record<string, string> = Object.fromEntries(
    Object.entries(tokens).map(([key, value]) => [`--campus-${key.replace(/[._]/g, '-')}`, String(value)]),
  )
  const recipe = layout.value
  if (recipe.content_width) styles['--campus-content-width'] = recipe.content_width
  if (recipe.page_padding) styles['--campus-page-padding'] = recipe.page_padding
  if (recipe.left_sidebar_width) styles['--campus-left-sidebar-width'] = recipe.left_sidebar_width
  if (recipe.right_sidebar_width) styles['--campus-right-sidebar-width'] = recipe.right_sidebar_width
  if (recipe.background_asset && themeStore.activeName) {
    styles['--campus-theme-background-image'] =
      `url("/api/v1/web-themes/${encodeURIComponent(themeStore.activeName)}/assets/${recipe.background_asset}")`
  }
  if (recipe.overlay) styles['--campus-theme-overlay'] = recipe.overlay
  return styles
})
</script>

<style scoped>
.campus-theme-root {
  min-height: 100vh;
  color: var(--campus-text-color, #1f2937);
  background: var(--campus-page-background, #f4f6f8);
  background-image:
    linear-gradient(var(--campus-theme-overlay, transparent), var(--campus-theme-overlay, transparent)),
    var(--campus-theme-background-image, none);
  background-size: cover;
  background-position: center;
  background-attachment: fixed;
  font-family: var(--campus-font-family, Inter, 'Noto Sans SC', system-ui, sans-serif);
  letter-spacing: var(--campus-letter-spacing, 0);
}
.campus-theme-root[data-layout-mode='full'] :deep(.campus-shell-body) {
  width: 100%;
  max-width: none;
}
.campus-theme-root[data-header-mode='sticky'] :deep(.campus-shell-header) {
  position: sticky;
  top: 0;
  z-index: 25;
  backdrop-filter: blur(14px);
}
.campus-theme-root[data-header-mode='overlay'] :deep(.campus-shell-header) {
  position: absolute;
  inset: 0 0 auto;
  z-index: 25;
  background: transparent;
  border-bottom-color: transparent;
}
.campus-theme-root[data-animation-preset='reveal'] :deep(.campus-shell-main > *) {
  animation: campus-reveal 0.48s ease-out both;
}
@keyframes campus-reveal {
  from {
    opacity: 0;
    transform: translateY(14px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}
</style>
