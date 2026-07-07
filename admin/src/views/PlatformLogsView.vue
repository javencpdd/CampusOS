<template>
  <div class="platform-logs">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>平台日志</span>
          <div class="toolbar">
            <el-select v-model="selectedSource" style="width: 180px" :disabled="connected">
              <el-option
                v-for="source in sources"
                :key="source.key"
                :label="source.label"
                :value="source.key"
              />
            </el-select>
            <el-input-number v-model="lineCount" :min="20" :max="1000" :step="20" controls-position="right" />
            <el-switch v-model="follow" active-text="实时" inactive-text="只读尾部" :disabled="connected" />
            <el-button type="primary" :loading="connecting" :disabled="connected" @click="startStream">
              <el-icon><VideoPlay /></el-icon>
              连接
            </el-button>
            <el-button :disabled="!connected" @click="stopStream">
              <el-icon><VideoPause /></el-icon>
              断开
            </el-button>
            <el-button @click="clearLines">
              <el-icon><Delete /></el-icon>
              清空
            </el-button>
            <el-button @click="loadSources" :loading="sourceLoading">
              <el-icon><Refresh /></el-icon>
              刷新源
            </el-button>
          </div>
        </div>
      </template>

      <el-table :data="sources" size="small" border class="source-table">
        <el-table-column prop="label" label="日志源" width="160" />
        <el-table-column prop="path" label="文件路径" min-width="280" show-overflow-tooltip />
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-tag :type="row.exists ? 'success' : 'info'" size="small">{{ row.exists ? '存在' : '未生成' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="大小" width="110">
          <template #default="{ row }">{{ formatBytes(row.size || 0) }}</template>
        </el-table-column>
        <el-table-column label="更新时间" width="190">
          <template #default="{ row }">{{ row.modified_at ? new Date(row.modified_at).toLocaleString() : '-' }}</template>
        </el-table-column>
      </el-table>

      <div ref="outputRef" class="log-output">
        <div v-if="lines.length === 0" class="empty-line">暂无日志输出</div>
        <div v-for="(line, index) in lines" :key="index" class="log-line">{{ line }}</div>
      </div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Delete, Refresh, VideoPause, VideoPlay } from '@element-plus/icons-vue'
import { platformLogApi } from '@/api'

interface LogSource {
  key: string
  label: string
  path: string
  exists: boolean
  size: number
  modified_at?: string
}

interface StreamLine {
  source: string
  line: string
}

const sources = ref<LogSource[]>([])
const selectedSource = ref('api')
const lineCount = ref(200)
const follow = ref(true)
const lines = ref<string[]>([])
const sourceLoading = ref(false)
const connecting = ref(false)
const connected = ref(false)
const outputRef = ref<HTMLElement>()

let controller: AbortController | null = null

const loadSources = async () => {
  sourceLoading.value = true
  try {
    const res = (await platformLogApi.sources()) as any
    sources.value = res?.data || []
    if (!sources.value.some((source) => source.key === selectedSource.value) && sources.value.length > 0) {
      selectedSource.value = sources.value[0].key
    }
  } catch (error: any) {
    ElMessage.error(error?.msg || '加载平台日志源失败')
  } finally {
    sourceLoading.value = false
  }
}

const startStream = async () => {
  stopStream()
  const token = localStorage.getItem('admin_token')
  if (!token) {
    ElMessage.error('管理员登录已失效')
    return
  }

  const activeController = new AbortController()
  controller = activeController
  connecting.value = true
  connected.value = true

  try {
    const response = await fetch(platformLogApi.streamUrl({
      source: selectedSource.value,
      lines: lineCount.value,
      follow: follow.value,
    }), {
      headers: { Authorization: `Bearer ${token}` },
      signal: activeController.signal,
    })
    if (!response.ok || !response.body) {
      throw new Error(`日志连接失败: HTTP ${response.status}`)
    }
    connecting.value = false
    const reader = response.body.getReader()
    const decoder = new TextDecoder()
    let buffer = ''

    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      buffer += decoder.decode(value, { stream: true })
      const events = buffer.split('\n\n')
      buffer = events.pop() || ''
      for (const event of events) {
        appendEvent(event)
      }
    }
  } catch (error: any) {
    if (!activeController.signal.aborted) {
      ElMessage.error(error?.message || '读取平台日志失败')
    }
  } finally {
    if (controller === activeController) {
      connected.value = false
      connecting.value = false
      controller = null
    }
  }
}

const stopStream = () => {
  if (controller) {
    controller.abort()
    controller = null
  }
  connected.value = false
  connecting.value = false
}

const appendEvent = (event: string) => {
  const data = event
    .split('\n')
    .filter((line) => line.startsWith('data:'))
    .map((line) => line.slice(5).trim())
    .join('\n')
  if (!data) return
  try {
    const payload = JSON.parse(data) as StreamLine
    lines.value.push(`[${payload.source}] ${payload.line}`)
  } catch {
    lines.value.push(data)
  }
  if (lines.value.length > 2000) {
    lines.value = lines.value.slice(lines.value.length - 2000)
  }
  nextTick(() => {
    if (outputRef.value) {
      outputRef.value.scrollTop = outputRef.value.scrollHeight
    }
  })
}

const clearLines = () => {
  lines.value = []
}

const formatBytes = (size: number) => {
  if (size < 1024) return `${size} B`
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`
  return `${(size / 1024 / 1024).toFixed(1)} MB`
}

onMounted(async () => {
  await loadSources()
})

onBeforeUnmount(stopStream)
</script>

<style scoped>
.platform-logs {
  max-width: 1280px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
}

.toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  justify-content: flex-end;
}

.source-table {
  margin-bottom: 16px;
}

.log-output {
  height: calc(100vh - 360px);
  min-height: 360px;
  overflow: auto;
  padding: 14px;
  border-radius: 6px;
  background: #111827;
  color: #e5e7eb;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-word;
}

.log-line {
  min-height: 19px;
}

.empty-line {
  color: #9ca3af;
}
</style>
