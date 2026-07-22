<template>
  <div class="campus-app-shell" :data-health="health">
    <header class="campus-shell-header app-header" data-campusos-required-region="header">
      <slot name="header" />
    </header>
    <nav class="campus-shell-primary-nav">
      <slot name="primary-navigation" />
    </nav>
    <nav v-if="$slots['secondary-navigation']" class="campus-shell-secondary-nav">
      <slot name="secondary-navigation" />
    </nav>
    <section v-if="$slots.hero" class="campus-shell-hero">
      <slot name="hero" />
    </section>
    <div class="campus-shell-body">
      <aside v-if="$slots['left-sidebar']" class="campus-shell-left">
        <slot name="left-sidebar" />
      </aside>
      <main class="campus-shell-main app-main" data-campusos-required-region="page-outlet">
        <slot name="page-outlet" />
      </main>
      <aside v-if="$slots['right-sidebar']" class="campus-shell-right">
        <slot name="right-sidebar" />
      </aside>
    </div>
    <div v-if="$slots['floating-action']" class="campus-shell-floating">
      <slot name="floating-action" />
    </div>
    <footer class="campus-shell-footer app-footer">
      <slot name="footer" />
    </footer>
    <div class="campus-shell-safety" data-campusos-required-region="safety">
      <slot name="safety" />
    </div>
  </div>
</template>

<script setup lang="ts">
withDefaults(defineProps<{ health?: string }>(), { health: 'healthy' })
</script>

<style scoped>
.campus-app-shell {
  min-height: 100vh;
  display: flex;
  flex-direction: column;
  position: relative;
  z-index: 1;
  isolation: isolate;
}
.campus-shell-header {
  min-height: 64px;
  display: flex;
  align-items: center;
  background: var(--campus-header-background, rgba(255, 255, 255, 0.94));
  border-bottom: 1px solid var(--campus-border-color, #dfe3e8);
}
.campus-shell-primary-nav,
.campus-shell-secondary-nav {
  background: var(--campus-navigation-background, #fff);
}
.campus-shell-body {
  width: min(var(--campus-content-width, 1200px), 100%);
  margin: 0 auto;
  padding: var(--campus-page-padding, 24px);
  display: grid;
  grid-template-columns:
    minmax(0, var(--campus-left-sidebar-width, 0px))
    minmax(0, 1fr) minmax(0, var(--campus-right-sidebar-width, 0px));
  gap: var(--campus-layout-gap, 20px);
  flex: 1;
  box-sizing: border-box;
}
.campus-shell-main {
  min-width: 0;
  grid-column: 2;
}
.campus-shell-left {
  grid-column: 1;
}
.campus-shell-right {
  grid-column: 3;
}
.campus-shell-floating {
  position: fixed;
  right: 24px;
  bottom: 24px;
  z-index: 20;
}
.campus-shell-footer {
  text-align: center;
  color: var(--campus-muted-color, #6b7280);
  padding: 24px;
}
.campus-shell-safety {
  position: fixed;
  left: 16px;
  bottom: 16px;
  z-index: 30;
  max-width: min(440px, calc(100vw - 32px));
}
@media (max-width: 900px) {
  .campus-shell-body {
    grid-template-columns: minmax(0, 1fr);
    padding: 16px;
  }
  .campus-shell-main {
    grid-column: 1;
  }
  .campus-shell-left,
  .campus-shell-right {
    display: none;
  }
}
@media (max-width: 760px), (max-height: 540px) and (max-width: 1000px) {
  .campus-shell-primary-nav {
    min-height: 48px;
    border-bottom: 1px solid var(--campus-border-color, #dfe3e8);
  }
}
</style>
