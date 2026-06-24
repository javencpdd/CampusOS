<template>
  <div class="admin-events">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>事件日志</span>
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

      <el-table :data="filteredEvents" v-loading="loading" stripe border style="width: 100%">
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
        <el-table-column prop="subject" label="主题" min-width="250" show-overflow-tooltip />
        <el-table-column prop="source" label="来源" width="150" show-overflow-tooltip />
        <el-table-column label="数据" width="100" align="center">
          <template #default="{ row }">
            <el-button
              v-if="row.data"
              type="primary"
              size="small"
              text
              @click="showData(row)"
            >
              查看详情
            </el-button>
            <span v-else style="color: #c0c4cc">无</span>
          </template>
        </el-table-column>
      </el-table>

      <el-empty v-if="!loading && filteredEvents.length === 0" description="暂无事件记录" />
    </el-card>

    <!-- 事件数据详情对话框 -->
    <el-dialog v-model="dataDialogVisible" title="事件数据详情" width="600px">
      <el-descriptions :column="1" border>
        <el-descriptions-item label="事件类型">
          <el-tag :type="eventTypeTag(selectedEvent?.type || '')" size="small">
            {{ selectedEvent?.type }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="主题">{{ selectedEvent?.subject }}</el-descriptions-item>
        <el-descriptions-item label="来源">{{ selectedEvent?.source }}</el-descriptions-item>
      </el-descriptions>
      <div style="margin-top: 16px">
        <h4 style="margin-bottom: 8px; color: #606266">数据内容：</h4>
        <pre class="event-data-pre">{{ formatData(selectedEvent?.data) }}</pre>
      </div>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import { eventApi } from '@/api'

const events = ref<any[]>([])
const loading = ref(false)
const filterType = ref('')
const dataDialogVisible = ref(false)
const selectedEvent = ref<any>(null)

// 已知的事件类型列表
const eventTypes = [
  'user.created',
  'user.login',
  'thread.created',
  'thread.updated',
  'thread.deleted',
  'post.created',
  'category.created',
  'category.updated',
  'category.deleted',
]

const filteredEvents = computed(() => {
  if (!filterType.value) return events.value
  return events.value.filter((e) => e.type === filterType.value)
})

const eventTypeTag = (type: string) => {
  if (type.startsWith('user.')) return 'primary'
  if (type.startsWith('thread.')) return 'success'
  if (type.startsWith('post.')) return 'warning'
  if (type.startsWith('category.')) return 'info'
  return ''
}

const load = async () => {
  loading.value = true
  try {
    const r = (await eventApi.list({ limit: 100 })) as any
    events.value = r?.data?.items || r?.data || []
  } catch {
    ElMessage.error('加载事件日志失败')
  }
  loading.value = false
}

const showData = (row: any) => {
  selectedEvent.value = row
  dataDialogVisible.value = true
}

const formatData = (data: any) => {
  if (!data) return '无数据'
  if (typeof data === 'string') {
    try {
      return JSON.stringify(JSON.parse(data), null, 2)
    } catch {
      return data
    }
  }
  return JSON.stringify(data, null, 2)
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

.event-data-pre {
  background: #f5f7fa;
  border: 1px solid #ebeef5;
  border-radius: 4px;
  padding: 12px;
  font-size: 13px;
  line-height: 1.6;
  max-height: 300px;
  overflow-y: auto;
  white-space: pre-wrap;
  word-break: break-all;
}
</style>