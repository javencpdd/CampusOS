<template>
  <section class="campus-card"><slot /></section>
</template>
<style scoped>
.campus-card {
  padding: 18px;
  background: var(--campus-surface-background, #fff);
  border: 1px solid var(--campus-border-color, #dfe3e8);
  border-radius: var(--campus-card-radius, 8px);
}
</style>
