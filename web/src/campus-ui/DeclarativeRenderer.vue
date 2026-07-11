<template>
  <div v-if="node.component === 'stack'" class="schema-stack">
    <DeclarativeRenderer v-for="(child, i) in node.children || []" :key="i" :node="child" :plugin="plugin" />
  </div>
  <div v-else-if="node.component === 'grid'" class="schema-grid">
    <DeclarativeRenderer v-for="(child, i) in node.children || []" :key="i" :node="child" :plugin="plugin" />
  </div>
  <CampusCard v-else-if="node.component === 'card'"
    ><DeclarativeRenderer v-for="(child, i) in node.children || []" :key="i" :node="child" :plugin="plugin"
  /></CampusCard>
  <component :is="`h${Math.min(4, Math.max(1, node.level || 2))}`" v-else-if="node.component === 'heading'">{{
    node.text
  }}</component>
  <p v-else-if="node.component === 'text'">{{ node.text }}</p>
  <el-tag v-else-if="node.component === 'badge'" effect="plain">{{ node.text }}</el-tag>
  <el-alert v-else-if="node.component === 'alert'" :title="node.text || ''" :closable="false" />
  <CampusButton v-else-if="node.component === 'button'" :tone="node.tone" :loading="running" @click="invoke">{{
    node.text || action?.label || '执行'
  }}</CampusButton>
  <ul v-else-if="node.component === 'list'" class="schema-list">
    <li v-for="item in node.items || []" :key="String(item)">{{ item }}</li>
  </ul>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { uiRuntimeApi } from '@/api'
import { useUIRuntimeStore } from '@/stores/uiRuntime'
import type { UISchemaNode } from './contracts'
import CampusButton from './CampusButton.vue'
import CampusCard from './CampusCard.vue'

defineOptions({ name: 'DeclarativeRenderer' })
const props = defineProps<{ node: UISchemaNode; plugin: string }>()
const runtime = useUIRuntimeStore()
const running = ref(false)
const action = computed(() => (props.node.action_id ? runtime.action(props.node.action_id) : undefined))
const invoke = async () => {
  if (!action.value || action.value.plugin !== props.plugin || running.value) return
  if (action.value.confirm) await ElMessageBox.confirm(`确认${action.value.label}？`, '操作确认')
  running.value = true
  try {
    await uiRuntimeApi.extension(props.plugin, action.value)
    ElMessage.success(`${action.value.label}已完成`)
  } catch (error: any) {
    ElMessage.error(error?.error?.message || error?.msg || '操作失败')
  } finally {
    running.value = false
  }
}
</script>

<style scoped>
.schema-stack {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.schema-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 14px;
}
.schema-list {
  margin: 0;
  padding-left: 20px;
}
h1,
h2,
h3,
h4,
p {
  margin: 0;
  letter-spacing: 0;
}
</style>
