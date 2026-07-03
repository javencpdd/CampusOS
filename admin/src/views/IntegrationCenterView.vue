<template>
  <div class="integration-center">
    <div class="page-header">
      <div>
        <h2>集成中心</h2>
        <p>统一查看插件、AI、个人主页、Webhook、MCP、Message 和基础观测状态。</p>
      </div>
      <el-button :loading="loading" @click="loadAll">
        <el-icon><Refresh /></el-icon>
        刷新
      </el-button>
    </div>

    <el-row :gutter="16" class="overview-grid">
      <el-col v-for="card in overview" :key="card.key" :xs="24" :sm="12" :lg="6">
        <el-card class="overview-card" shadow="never">
          <div class="card-title">
            <span>{{ card.title }}</span>
            <el-tag :type="statusTag(card.status)" size="small">{{ statusLabel(card.status) }}</el-tag>
          </div>
          <div class="card-summary">{{ card.summary || '-' }}</div>
          <div class="metrics">
            <span v-for="(value, key) in card.metrics || {}" :key="key">
              {{ key }}: {{ value }}
            </span>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <el-tabs v-model="activeTab" class="ops-tabs">
      <el-tab-pane label="Webhook" name="webhook">
        <el-card shadow="never">
          <template #header>
            <div class="section-header">
              <span>Webhook Endpoint</span>
              <el-button size="small" @click="loadWebhooks">
                <el-icon><Refresh /></el-icon>
                刷新
              </el-button>
            </div>
          </template>
          <el-form :inline="true" :model="webhookForm" class="compact-form">
            <el-form-item label="名称">
              <el-input v-model="webhookForm.name" placeholder="mock endpoint" />
            </el-form-item>
            <el-form-item label="URL">
              <el-input v-model="webhookForm.url" placeholder="http://localhost:9000/webhook" class="url-input" />
            </el-form-item>
            <el-form-item label="事件">
              <el-input v-model="webhookForm.events" placeholder="thread.created,user.created" class="events-input" />
            </el-form-item>
            <el-form-item>
              <el-button type="primary" :loading="webhookCreating" @click="createWebhook">
                <el-icon><Plus /></el-icon>
                创建
              </el-button>
            </el-form-item>
          </el-form>
          <el-table :data="webhooks" border stripe size="small">
            <el-table-column prop="name" label="名称" width="160" />
            <el-table-column prop="url" label="URL" min-width="260" show-overflow-tooltip />
            <el-table-column prop="events" label="事件" min-width="180">
              <template #default="{ row }">
                <el-tag v-for="event in row.events || []" :key="event" size="small" effect="plain">{{ event }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="enabled" label="状态" width="90">
              <template #default="{ row }">
                <el-tag :type="row.enabled ? 'success' : 'info'" size="small">{{ row.enabled ? '启用' : '禁用' }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="操作" width="220" fixed="right">
              <template #default="{ row }">
                <el-button size="small" @click="testWebhook(row.id)">测试</el-button>
                <el-button size="small" :type="row.enabled ? 'warning' : 'success'" @click="toggleWebhook(row)">
                  {{ row.enabled ? '禁用' : '启用' }}
                </el-button>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-tab-pane>

      <el-tab-pane label="MCP 只读工具" name="mcp">
        <el-card shadow="never">
          <template #header>
            <div class="section-header">
              <span>MCP 工具</span>
              <el-switch v-model="mcpEnabled" active-text="启用" inactive-text="禁用" @change="updateMcpEnabled" />
            </div>
          </template>
          <el-table :data="mcpTools" border stripe size="small">
            <el-table-column prop="name" label="工具" width="180" />
            <el-table-column prop="description" label="说明" min-width="260" />
            <el-table-column prop="read_only" label="权限" width="100">
              <template #default="{ row }">
                <el-tag :type="row.read_only ? 'success' : 'danger'" size="small">
                  {{ row.read_only ? '只读' : '写入' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="操作" width="110">
              <template #default="{ row }">
                <el-button size="small" @click="callMcp(row.name)">调用</el-button>
              </template>
            </el-table-column>
          </el-table>
          <pre v-if="mcpResult" class="result-pre">{{ mcpResult }}</pre>
        </el-card>
      </el-tab-pane>

      <el-tab-pane label="Message Local" name="message">
        <el-card shadow="never">
          <template #header>
            <div class="section-header">
              <span>本地消息适配器</span>
              <el-button size="small" @click="loadMessages">
                <el-icon><Refresh /></el-icon>
                刷新日志
              </el-button>
            </div>
          </template>
          <el-form :inline="true" :model="messageForm" class="compact-form">
            <el-form-item label="会话">
              <el-input v-model="messageForm.conversation_id" placeholder="local-room" />
            </el-form-item>
            <el-form-item label="发送者">
              <el-input v-model="messageForm.sender_id" placeholder="tester" />
            </el-form-item>
            <el-form-item label="内容">
              <el-input v-model="messageForm.content" placeholder="ping" />
            </el-form-item>
            <el-form-item>
              <el-button type="primary" @click="sendLocalMessage">
                <el-icon><Promotion /></el-icon>
                发送
              </el-button>
            </el-form-item>
          </el-form>
          <el-table :data="messages" border stripe size="small">
            <el-table-column prop="created_at" label="时间" width="170">
              <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
            </el-table-column>
            <el-table-column prop="platform" label="平台" width="90" />
            <el-table-column prop="direction" label="方向" width="90" />
            <el-table-column prop="conversation_id" label="会话" width="140" />
            <el-table-column prop="sender.id" label="发送者" width="140" />
            <el-table-column prop="content" label="内容" min-width="240" show-overflow-tooltip />
          </el-table>
        </el-card>
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { integrationApi, mcpApi, messageApi, webhookApi } from '@/api'
import { Plus, Promotion, Refresh } from '@element-plus/icons-vue'

const loading = ref(false)
const activeTab = ref('webhook')
const overview = ref<any[]>([])
const webhooks = ref<any[]>([])
const webhookCreating = ref(false)
const webhookForm = ref({ name: '', url: '', events: 'thread.created,user.created' })
const mcpTools = ref<any[]>([])
const mcpEnabled = ref(true)
const mcpResult = ref('')
const messages = ref<any[]>([])
const messageForm = ref({ conversation_id: 'local-room', sender_id: 'tester', content: 'ping' })

const itemsOf = (payload: any): any[] => {
  const candidates = [payload?.data?.items, payload?.items, payload?.data, payload]
  for (const candidate of candidates) {
    if (Array.isArray(candidate)) return candidate
  }
  return []
}

const dataOf = (payload: any): any => payload?.data || payload

const loadOverview = async () => {
  const res = await integrationApi.overview()
  overview.value = itemsOf(res)
}

const loadWebhooks = async () => {
  const res = await webhookApi.list()
  webhooks.value = itemsOf(res)
}

const loadMcp = async () => {
  const res = await mcpApi.tools()
  const data = dataOf(res)
  mcpEnabled.value = Boolean(data?.enabled)
  mcpTools.value = itemsOf(data)
}

const loadMessages = async () => {
  const res = await messageApi.logs({ limit: 50 })
  messages.value = itemsOf(res)
}

const loadAll = async () => {
  loading.value = true
  try {
    await Promise.all([loadOverview(), loadWebhooks(), loadMcp(), loadMessages()])
  } catch (err: any) {
    ElMessage.error(err?.msg || '加载集成中心失败')
  } finally {
    loading.value = false
  }
}

const createWebhook = async () => {
  if (!webhookForm.value.name || !webhookForm.value.url) {
    ElMessage.warning('请填写名称和 URL')
    return
  }
  webhookCreating.value = true
  try {
    await webhookApi.create({
      name: webhookForm.value.name,
      url: webhookForm.value.url,
      events: webhookForm.value.events.split(',').map((item) => item.trim()).filter(Boolean),
      enabled: true,
    })
    ElMessage.success('Webhook 已创建')
    webhookForm.value.name = ''
    await Promise.all([loadWebhooks(), loadOverview()])
  } catch (err: any) {
    ElMessage.error(err?.msg || '创建 Webhook 失败')
  } finally {
    webhookCreating.value = false
  }
}

const testWebhook = async (id: string) => {
  try {
    const res = await webhookApi.test(id)
    const data = dataOf(res)
    ElMessage.success(`测试完成：${data?.status || 'unknown'}`)
    await loadOverview()
  } catch (err: any) {
    ElMessage.error(err?.msg || 'Webhook 测试失败')
  }
}

const toggleWebhook = async (row: any) => {
  try {
    if (row.enabled) {
      await webhookApi.disable(row.id)
    } else {
      await webhookApi.enable(row.id)
    }
    await Promise.all([loadWebhooks(), loadOverview()])
  } catch (err: any) {
    ElMessage.error(err?.msg || '切换 Webhook 状态失败')
  }
}

const updateMcpEnabled = async (enabled: boolean | string | number) => {
  try {
    await mcpApi.updateSettings(Boolean(enabled))
    await loadOverview()
  } catch (err: any) {
    ElMessage.error(err?.msg || '更新 MCP 状态失败')
    await loadMcp()
  }
}

const callMcp = async (name: string) => {
  try {
    const args = name === 'threads.list' ? { page: 1, page_size: 5 } : {}
    const res = await mcpApi.call(name, args)
    mcpResult.value = JSON.stringify(dataOf(res)?.result || dataOf(res), null, 2)
    await loadOverview()
  } catch (err: any) {
    ElMessage.error(err?.msg || '调用 MCP 工具失败')
  }
}

const sendLocalMessage = async () => {
  try {
    await messageApi.receiveLocal({
      conversation_id: messageForm.value.conversation_id,
      sender: { id: messageForm.value.sender_id, display_name: messageForm.value.sender_id },
      content: messageForm.value.content,
    })
    ElMessage.success('本地消息已发送')
    await Promise.all([loadMessages(), loadOverview()])
  } catch (err: any) {
    ElMessage.error(err?.msg || '发送本地消息失败')
  }
}

const statusTag = (status: string) => {
  if (status === 'ok') return 'success'
  if (status === 'warning') return 'warning'
  if (status === 'error') return 'danger'
  return 'info'
}

const statusLabel = (status: string) => {
  if (status === 'ok') return '正常'
  if (status === 'warning') return '注意'
  if (status === 'error') return '异常'
  if (status === 'disabled') return '未启用'
  if (status === 'memory') return '内存模式'
  return status || '未知'
}

const formatTime = (value: string) => {
  if (!value) return ''
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}

onMounted(loadAll)
</script>

<style scoped>
.integration-center {
  max-width: 1440px;
}

.page-header,
.section-header,
.card-title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.page-header {
  margin-bottom: 16px;
}

.page-header h2 {
  margin: 0 0 4px;
  font-size: 22px;
}

.page-header p {
  margin: 0;
  color: #606266;
}

.overview-grid {
  margin-bottom: 16px;
}

.overview-card {
  min-height: 160px;
  margin-bottom: 16px;
}

.card-title {
  font-weight: 600;
  margin-bottom: 10px;
}

.card-summary {
  min-height: 42px;
  color: #606266;
  line-height: 1.5;
}

.metrics {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 10px;
  color: #909399;
  font-size: 12px;
}

.compact-form {
  margin-bottom: 12px;
}

.url-input {
  width: 360px;
}

.events-input {
  width: 260px;
}

.ops-tabs {
  background: #fff;
  padding: 12px;
}

.result-pre {
  max-height: 320px;
  overflow: auto;
  margin: 12px 0 0;
  padding: 12px;
  background: #f6f8fa;
  border: 1px solid #e4e7ed;
  border-radius: 6px;
}
</style>
