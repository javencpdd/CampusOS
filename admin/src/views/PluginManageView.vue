<template>
  <div class="admin-plugins">
    <el-card>
      <template #header>
        <div class="card-header">
          <div class="header-title">
            <span>插件管理</span>
            <el-tag type="info" size="small">已安装 {{ plugins.length }} 个插件</el-tag>
          </div>
          <div class="header-actions">
            <el-switch
              v-model="replaceOnImport"
              inline-prompt
              active-text="覆盖"
              inactive-text="保留"
            />
            <el-upload
              :auto-upload="false"
              :show-file-list="false"
              :disabled="importing"
              accept=".tar.gz,.campusos-plugin.tar.gz,application/gzip"
              :on-change="handleImportChange"
            >
              <el-button type="primary" size="small" :loading="importing">
                <el-icon><Upload /></el-icon>
                导入
              </el-button>
            </el-upload>
            <el-button size="small" @click="load" :loading="loading">
              <el-icon><Refresh /></el-icon>
              刷新
            </el-button>
          </div>
        </div>
      </template>

      <el-table :data="plugins" v-loading="loading" stripe border style="width: 100%">
        <el-table-column prop="name" label="插件名称" width="180">
          <template #default="{ row }">
            <div class="plugin-name">
              <el-icon style="margin-right: 4px"><Connection /></el-icon>
              <strong>{{ row.name }}</strong>
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="display_name" label="显示名称" width="150" />
        <el-table-column prop="version" label="版本" width="100" align="center">
          <template #default="{ row }">
            <el-tag size="small" effect="plain">v{{ row.version }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="description" label="描述" min-width="250" show-overflow-tooltip />
        <el-table-column prop="runtime" label="运行时" width="100" align="center">
          <template #default="{ row }">
            <el-tag :type="row.runtime === 'grpc' ? 'success' : 'warning'" size="small">
              {{ row.runtime?.toUpperCase() || 'N/A' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="checksum" label="Checksum" width="180" show-overflow-tooltip />
        <el-table-column prop="status" label="状态" width="100" align="center">
          <template #default="{ row }">
            <el-tag :type="statusTag(row.status)" size="small">
              {{ statusLabel(row.status) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="430" align="center" fixed="right">
          <template #default="{ row }">
            <el-button type="primary" size="small" plain @click="showLogs(row.name)">
              <el-icon><Document /></el-icon>
              日志
            </el-button>
            <el-button size="small" plain @click="openConfig(row)">
              <el-icon><Setting /></el-icon>
              配置
            </el-button>
            <el-button type="success" size="small" plain @click="doExport(row)">
              <el-icon><Download /></el-icon>
              导出
            </el-button>
            <el-switch
              :model-value="isPluginEnabled(row.status)"
              active-text="启用"
              inactive-text="禁用"
              inline-prompt
              style="margin-right: 8px"
              @change="onTogglePlugin(row, $event)"
            />
            <el-popconfirm
              title="确定要卸载该插件吗？此操作不可恢复。"
              confirm-button-text="卸载"
              cancel-button-text="取消"
              confirm-button-type="danger"
              @confirm="doUninstall(row.name)"
            >
              <template #reference>
                <el-button type="danger" size="small" plain>卸载</el-button>
              </template>
            </el-popconfirm>
          </template>
        </el-table-column>
      </el-table>

      <el-empty v-if="!loading && plugins.length === 0" description="暂无已安装的插件" />
    </el-card>

    <el-dialog v-model="logDialogVisible" :title="`${selectedPluginName} 运行日志`" width="860px">
      <div class="log-toolbar">
        <el-button size="small" @click="loadLogs" :loading="logsLoading">
          <el-icon><Refresh /></el-icon>
          刷新
        </el-button>
      </div>
      <el-table :data="pluginLogs" v-loading="logsLoading" stripe border style="width: 100%">
        <el-table-column prop="created_at" label="时间" width="170">
          <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
        </el-table-column>
        <el-table-column prop="level" label="级别" width="90" align="center">
          <template #default="{ row }">
            <el-tag :type="logLevelTag(row.level)" size="small">{{ row.level || 'info' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="message" label="消息" min-width="210" show-overflow-tooltip />
        <el-table-column prop="event_type" label="事件" width="150" show-overflow-tooltip />
        <el-table-column label="元数据" width="100" align="center">
          <template #default="{ row }">
            <el-popover v-if="row.metadata" placement="left" width="420" trigger="click">
              <pre class="metadata-pre">{{ formatMetadata(row.metadata) }}</pre>
              <template #reference>
                <el-button type="primary" size="small" text>查看</el-button>
              </template>
            </el-popover>
            <span v-else class="empty-text">无</span>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!logsLoading && pluginLogs.length === 0" description="暂无插件日志" />
    </el-dialog>

    <el-dialog v-model="configDialogVisible" :title="`${selectedPluginName} 插件配置`" width="720px">
      <el-alert
        v-if="configDescription"
        :title="configDescription"
        type="info"
        show-icon
        :closable="false"
        class="config-alert"
      />
      <el-form label-position="top" v-loading="configLoading">
        <el-form-item
          v-for="field in configFields"
          :key="field.key"
          :label="field.label || field.key"
        >
          <el-switch
            v-if="field.type === 'boolean'"
            v-model="configForm[field.key]"
            inline-prompt
            active-text="开"
            inactive-text="关"
          />
          <el-input-number
            v-else-if="field.type === 'number'"
            v-model="configForm[field.key]"
            :min="0"
            :max="1000000"
            controls-position="right"
          />
          <el-select v-else-if="field.type === 'select'" v-model="configForm[field.key]" style="width: 100%">
            <el-option
              v-for="option in field.options || []"
              :key="String(option.value)"
              :label="option.label"
              :value="option.value"
            />
          </el-select>
          <el-input
            v-else-if="field.type === 'text' || field.type === 'json'"
            v-model="configForm[field.key]"
            type="textarea"
            :rows="field.type === 'json' ? 6 : 3"
          />
          <el-input v-else v-model="configForm[field.key]" />
          <p v-if="field.description" class="field-description">{{ field.description }}</p>
        </el-form-item>
      </el-form>
      <el-empty v-if="!configLoading && configFields.length === 0" description="该插件没有声明可编辑配置" />
      <template #footer>
        <el-button @click="configDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="saveConfig" :loading="configSaving" :disabled="configFields.length === 0">
          保存配置
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import type { UploadFile } from 'element-plus'
import { Connection, Document, Download, Refresh, Setting, Upload } from '@element-plus/icons-vue'
import { pluginApi } from '@/api'

const plugins = ref<any[]>([])
const loading = ref(false)
const importing = ref(false)
const replaceOnImport = ref(false)
const logDialogVisible = ref(false)
const logsLoading = ref(false)
const selectedPluginName = ref('')
const pluginLogs = ref<any[]>([])
const configDialogVisible = ref(false)
const configLoading = ref(false)
const configSaving = ref(false)
const configFields = ref<any[]>([])
const configForm = ref<Record<string, any>>({})
const configDescription = ref('')

const responseItems = (payload: any): any[] => {
  const candidates = [
    payload?.data?.items,
    payload?.items,
    payload?.data,
    payload,
  ]
  for (const candidate of candidates) {
    if (Array.isArray(candidate)) return candidate
  }
  return []
}

const load = async () => {
  loading.value = true
  try {
    const r = (await pluginApi.list()) as any
    plugins.value = responseItems(r)
  } catch {
    // 插件接口可能不可用，静默处理
    plugins.value = []
  }
  loading.value = false
}

const togglePlugin = async (row: any, enabled: boolean) => {
  const previousStatus = row.status
  try {
    if (enabled) {
      await pluginApi.enable(row.name)
      ElMessage.success(`插件 ${row.name} 已启用`)
    } else {
      await pluginApi.disable(row.name)
      ElMessage.success(`插件 ${row.name} 已禁用`)
    }
    await load()
  } catch {
    ElMessage.error('操作失败')
    row.status = previousStatus
  }
}

const onTogglePlugin = (row: any, enabled: boolean | string | number) => {
  togglePlugin(row, Boolean(enabled))
}

const doUninstall = async (name: string) => {
  try {
    await pluginApi.uninstall(name)
    ElMessage.success('插件已卸载')
    load()
  } catch {
    ElMessage.error('卸载失败')
  }
}

const doExport = async (row: any) => {
  try {
    const blob = (await pluginApi.exportPackage(row.name)) as any
    const downloadBlob = blob instanceof Blob ? blob : new Blob([blob])
    const url = URL.createObjectURL(downloadBlob)
    const link = document.createElement('a')
    link.href = url
    link.download = `${row.name}-${row.version || '0.0.0'}.campusos-plugin.tar.gz`
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
    URL.revokeObjectURL(url)
    ElMessage.success('插件包已导出')
  } catch {
    ElMessage.error('导出插件包失败')
  }
}

const handleImportChange = async (uploadFile: UploadFile) => {
  if (!uploadFile.raw) return
  importing.value = true
  try {
    const precheckRes = (await pluginApi.precheckPackage(uploadFile.raw)) as any
    const precheck = precheckRes?.data || precheckRes
    if (precheck?.allowed === false) {
      ElMessage.error(`插件包预检失败：${(precheck.errors || []).join('；') || '未知错误'}`)
      return
    }
    if (precheck?.conflict && !replaceOnImport.value) {
      ElMessage.warning('检测到同名插件，请开启“覆盖”后再导入')
      return
    }
    await pluginApi.importPackage(uploadFile.raw, replaceOnImport.value)
    ElMessage.success(`插件包已导入${precheck?.checksum ? `：${precheck.checksum}` : ''}`)
    await load()
  } catch (error: any) {
    ElMessage.error(error?.message || '导入插件包失败')
  } finally {
    importing.value = false
  }
}

const showLogs = async (name: string) => {
  selectedPluginName.value = name
  logDialogVisible.value = true
  await loadLogs()
}

const openConfig = async (row: any) => {
  selectedPluginName.value = row.name
  configDialogVisible.value = true
  configLoading.value = true
  configFields.value = []
  configForm.value = {}
  configDescription.value = ''
  try {
    const r = (await pluginApi.get(row.name)) as any
    const detail = r?.data || r
    const fields = detail?.config_schema?.fields || []
    const config = detail?.config || {}
    configDescription.value = detail?.description || ''
    configFields.value = fields
    const next: Record<string, any> = {}
    for (const field of fields) {
      const value = config[field.key] ?? field.default ?? defaultValueForField(field)
      next[field.key] = normalizeFieldValue(field, value)
    }
    configForm.value = next
  } catch (error: any) {
    ElMessage.error(error?.msg || '加载插件配置失败')
  } finally {
    configLoading.value = false
  }
}

const saveConfig = async () => {
  if (!selectedPluginName.value) return
  configSaving.value = true
  try {
    const payload: Record<string, any> = {}
    for (const field of configFields.value) {
      payload[field.key] = normalizeFieldValue(field, configForm.value[field.key])
    }
    await pluginApi.updateConfig(selectedPluginName.value, payload)
    ElMessage.success('插件配置已保存')
    configDialogVisible.value = false
    await load()
  } catch (error: any) {
    ElMessage.error(error?.msg || '保存插件配置失败')
  } finally {
    configSaving.value = false
  }
}

const defaultValueForField = (field: any) => {
  if (field.type === 'boolean') return false
  if (field.type === 'number') return 0
  return ''
}

const normalizeFieldValue = (field: any, value: any) => {
  if (field.type === 'boolean') {
    if (typeof value === 'string') return ['true', '1', 'yes', 'on'].includes(value.trim().toLowerCase())
    return Boolean(value)
  }
  if (field.type === 'number') {
    const parsed = Number(value)
    return Number.isFinite(parsed) ? parsed : 0
  }
  return value ?? ''
}

const loadLogs = async () => {
  if (!selectedPluginName.value) return
  logsLoading.value = true
  try {
    const r = (await pluginApi.logs(selectedPluginName.value, { limit: 100 })) as any
    pluginLogs.value = responseItems(r)
  } catch {
    pluginLogs.value = []
    ElMessage.error('加载插件日志失败')
  }
  logsLoading.value = false
}

const isPluginEnabled = (status: string) => status === 'enabled' || status === 'running'

const statusLabel = (status: string) => {
  if (status === 'enabled' || status === 'running') return '已启用'
  if (status === 'error') return '异常'
  if (status === 'installed') return '未启用'
  if (status === 'stopped') return '已禁用'
  return status || '未知'
}

const statusTag = (status: string) => {
  if (status === 'enabled' || status === 'running') return 'success'
  if (status === 'error') return 'danger'
  if (status === 'installed') return 'info'
  return 'warning'
}

const logLevelTag = (level: string) => {
  if (level === 'error') return 'danger'
  if (level === 'warn') return 'warning'
  if (level === 'info') return 'success'
  return 'info'
}

const formatTime = (value: string) => {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

const formatMetadata = (metadata: any) => {
  if (!metadata) return '无'
  if (typeof metadata === 'string') {
    try {
      return JSON.stringify(JSON.parse(metadata), null, 2)
    } catch {
      return metadata
    }
  }
  return JSON.stringify(metadata, null, 2)
}

onMounted(load)
</script>

<style scoped>
.admin-plugins {
  max-width: 1400px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
}

.header-title,
.header-actions {
  display: flex;
  align-items: center;
  gap: 10px;
}

.plugin-name {
  display: flex;
  align-items: center;
}

.log-toolbar {
  display: flex;
  justify-content: flex-end;
  margin-bottom: 12px;
}

.config-alert {
  margin-bottom: 14px;
}

.field-description {
  margin: 6px 0 0;
  color: #909399;
  font-size: 12px;
  line-height: 1.5;
}

.metadata-pre {
  max-height: 320px;
  overflow-y: auto;
  margin: 0;
  white-space: pre-wrap;
  word-break: break-word;
}

.empty-text {
  color: #a8abb2;
}
</style>
