<template>
  <el-button :type="elementType" :loading="loading"><slot /></el-button>
</template>
<script setup lang="ts">
import { computed } from 'vue'
const props = withDefaults(defineProps<{ tone?: string; loading?: boolean }>(), { tone: 'default', loading: false })
const elementType = computed(
  () =>
    (({
      primary: 'primary',
      success: 'success',
      warning: 'warning',
      danger: 'danger',
    })[props.tone] || 'default') as any,
)
</script>
