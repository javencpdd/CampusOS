<template>
  <div class="admin-events">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>事件记录</span>
          <div class="header-actions">
            <el-select v-model="filterType" placeholder="筛选事件类型" clearable style="width: 180px" @change="load">
              <el-option label="全部类型" value="" />
              <el-option v-for="t in eventTypes" :key="t" :label="t" :value="t" />
            </el-select>
            <el-button @click="load" :loading="loading">
              <el-icon><Refresh /></el-icon>
              刷新
            </el-button>
          </div>
        </div>
      </template>

      <el-table :data="events" v-loading="loading" stripe border style="width: 100%">
        <el-table-column prop="id" label="ID" width="80" align="center" />
        <el-table-column prop="type" label="事件类型" width="180">
          <template #default="{ row }">
            <el-tag
              :type="eventTypeTag(row.type)"
              size="small"
              effect="plain"
            >
              {{ row.type }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="source" label="来源" width="150" />
        <el-table-column prop="time" label="发生时间" min-width="220" />
      </el-table>

      <el-empty v-if="!loading && events.length === 0" description="暂无事件记录" />
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import { eventApi, type EventMetadata } from '@/modules/operations/api'

const events = ref<EventMetadata[]>([])
const loading = ref(false)
const filterType = ref('')
let loadSequence = 0

const eventTypes = [
  'user.created', 'user.updated',
  'thread.created', 'thread.updated', 'thread.deleted',
  'post.created', 'post.deleted',
  'category.created', 'category.updated', 'category.moved',
  'category.archived', 'category.restored', 'other',
]

const eventTypeTag = (type: string) => {
  if (type.startsWith('user.')) return 'primary'
  if (type.startsWith('thread.')) return 'success'
  if (type.startsWith('post.')) return 'warning'
  if (type.startsWith('category.')) return 'info'
  return ''
}

const load = async () => {
  const sequence = ++loadSequence
  loading.value = true
  try {
    const r = (await eventApi.list({ limit: 100, type: filterType.value || undefined })) as any
    if (sequence === loadSequence) {
      events.value = Array.isArray(r?.data?.items) ? r.data.items : []
    }
  } catch {
    if (sequence === loadSequence) {
      events.value = []
      ElMessage.error('加载事件记录失败，请确认当前管理权限')
    }
  } finally {
    if (sequence === loadSequence) loading.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.admin-events {
  max-width: 1400px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.header-actions {
  display: flex;
  gap: 12px;
}
</style>
