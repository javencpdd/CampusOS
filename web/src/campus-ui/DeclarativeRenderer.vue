<template>
  <div v-if="node.component === 'stack'" class="schema-stack">
    <DeclarativeRenderer
      v-for="(child, i) in node.children || []"
      :key="i"
      :node="child"
      :plugin="plugin"
      :resource-context="resourceContext"
    />
  </div>
  <div v-else-if="node.component === 'grid'" class="schema-grid">
    <DeclarativeRenderer
      v-for="(child, i) in node.children || []"
      :key="i"
      :node="child"
      :plugin="plugin"
      :resource-context="resourceContext"
    />
  </div>
  <CampusCard v-else-if="node.component === 'card'"
    ><DeclarativeRenderer
      v-for="(child, i) in node.children || []"
      :key="i"
      :node="child"
      :plugin="plugin"
      :resource-context="resourceContext"
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
import { uiRuntimeApi } from '@/modules/plugin-runtime/api'
import { useUIRuntimeStore } from '@/modules/plugin-runtime/store'
import type { UISchemaNode, UIResourceContext } from './contracts'
import { openPluginSurface } from './surfaceHost'
import CampusButton from './CampusButton.vue'
import CampusCard from './CampusCard.vue'

defineOptions({ name: 'DeclarativeRenderer' })
const props = defineProps<{ node: UISchemaNode; plugin: string; resourceContext?: UIResourceContext }>()
const runtime = useUIRuntimeStore()
const running = ref(false)
const action = computed(() => (props.node.action_id ? runtime.action(props.node.action_id) : undefined))
const invoke = async () => {
  if (!action.value || action.value.plugin !== props.plugin || running.value) return
  if (action.value.confirm) {
    try {
      await ElMessageBox.confirm(`确认${action.value.label}？`, '操作确认')
    } catch {
      return
    }
  }
  running.value = true
  try {
    if (action.value.kind === 'open-surface') {
      if (!props.resourceContext || !action.value.surface_id || !action.value.presentation) {
        throw new Error('请先在文章附件或个人空间中选择要预览的文件，再打开插件界面。')
      }
      const response: any = await uiRuntimeApi.openSurface(props.plugin, action.value, props.resourceContext)
      const invocation = response?.data || response
      openPluginSurface({
        surfaceID: action.value.surface_id,
        invocationID: invocation.id,
        presentation: action.value.presentation,
      })
    } else {
      await uiRuntimeApi.extension(props.plugin, action.value)
    }
    ElMessage.success(`${action.value.label}已完成`)
  } catch (error: any) {
    ElMessage.error(error?.error?.message || error?.msg || error?.message || '操作失败')
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
