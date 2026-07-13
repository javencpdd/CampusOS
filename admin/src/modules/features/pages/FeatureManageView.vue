<template>
  <div class="feature-page">
    <header class="page-header">
      <div>
        <p class="eyebrow">Built-in Feature Registry</p>
        <h2>内置功能</h2>
        <p>内置功能随 CampusOS 发布，可配置或停用，但不能作为外部插件安装、升级或卸载。</p>
      </div>
      <el-button :icon="Refresh" circle title="刷新" :loading="loading" @click="load" />
    </header>

    <el-alert
      type="info"
      :closable="false"
      show-icon
      title="Feature Store 是启停和配置的唯一权威状态；restart 功能在 API 重启后生效，停用不会删除数据。"
    />

    <section class="core-band">
      <div>
        <strong>core.moderation</strong>
        <p>板块作用域、权限策略、审计和完整性检查始终启用。</p>
      </div>
      <div class="row-actions">
        <el-tag type="success">always-on</el-tag>
        <el-button type="primary" plain @click="$router.push('/moderators')">配置动作与范围</el-button>
      </div>
    </section>

    <el-table :data="features" v-loading="loading" border stripe>
      <el-table-column prop="label" label="功能" min-width="210">
        <template #default="{ row }">
          <strong>{{ row.label }}</strong>
          <small>{{ row.id }}</small>
        </template>
      </el-table-column>
      <el-table-column prop="description" label="职责" min-width="280" />
      <el-table-column label="生效方式" width="120" align="center">
        <template #default="{ row }"><el-tag effect="plain">{{ row.state?.activation_mode || '-' }}</el-tag></template>
      </el-table-column>
      <el-table-column label="当前状态" width="150" align="center">
        <template #default="{ row }">
          <el-tag :type="row.state?.status === 'running' ? 'success' : 'info'">
            {{ row.state?.status === 'running' ? '已启用' : '已停用' }}
          </el-tag>
          <el-tag v-if="row.state?.pending_restart" type="warning">待重启</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="配置入口" min-width="240">
        <template #default="{ row }">
          <el-button
            v-for="source in row.configSources"
            :key="source.name"
            size="small"
            plain
            @click="openConfig(source.name)"
          >{{ source.label }}</el-button>
        </template>
      </el-table-column>
      <el-table-column label="目标状态" width="150" align="center" fixed="right">
        <template #default="{ row }">
          <el-switch
            :model-value="Boolean(row.state?.desired_enabled)"
            inline-prompt
            active-text="启用"
            inactive-text="停用"
            @change="toggle(row, Boolean($event))"
          />
        </template>
      </el-table-column>
    </el-table>

    <section class="appearance-band">
      <div class="section-heading">
        <div>
          <h3>首页资源包</h3>
          <p>资源包保存在数据目录中，由 Appearance 功能校验、应用和回滚。</p>
        </div>
        <el-button type="warning" plain :loading="rollingBack" @click="rollbackPack">回滚</el-button>
      </div>
      <div class="pack-controls">
        <el-select v-model="selectedPack" filterable :loading="packsLoading" placeholder="选择已校验的首页资源包">
          <el-option
            v-for="pack in packs"
            :key="pack.name"
            :label="`${pack.display_name || pack.name}${pack.version ? ` v${pack.version}` : ''}`"
            :value="pack.name"
            :disabled="!pack.validation?.valid"
          />
        </el-select>
        <el-button :loading="packsLoading" @click="loadPacks">刷新目录</el-button>
        <el-button type="primary" :disabled="!selectedPack" :loading="applyingPack" @click="applySourcePack">应用</el-button>
        <input ref="packInput" class="hidden" type="file" accept=".zip,application/zip" @change="selectPackFile" />
        <el-button @click="packInput?.click()">选择 zip</el-button>
        <el-button :disabled="!packFile" :loading="validatingPack" @click="validatePack">筛查</el-button>
        <el-button type="primary" :disabled="!packFile || validation?.valid !== true" :loading="uploadingPack" @click="applyPack">导入并应用</el-button>
        <el-button :loading="downloadingExample" @click="downloadExample">导出当前示例</el-button>
      </div>
      <p v-if="packFile" class="file-name">{{ packFile.name }}</p>
      <el-alert
        v-if="validation"
        :type="validation.valid ? 'success' : 'error'"
        :closable="false"
        show-icon
        :title="validation.valid ? '资源包筛查通过' : `筛查失败：${(validation.errors || []).join('；')}`"
      />
    </section>

    <el-dialog v-model="configVisible" :title="`${selectedConfigName} 配置`" width="720px">
      <el-form label-position="top" v-loading="configLoading">
        <el-form-item v-for="field in configFields" :key="field.key" :label="field.label || field.key">
          <el-switch v-if="field.type === 'boolean'" v-model="configForm[field.key]" />
          <el-input-number v-else-if="field.type === 'number'" v-model="configForm[field.key]" :min="0" :max="1000000" />
          <el-select v-else-if="field.type === 'select'" v-model="configForm[field.key]" style="width: 100%">
            <el-option v-for="option in field.options || []" :key="String(option.value)" :label="option.label" :value="option.value" />
          </el-select>
          <el-input v-else-if="field.type === 'text' || field.type === 'json'" v-model="configForm[field.key]" type="textarea" :rows="field.type === 'json' ? 7 : 4" />
          <el-input v-else v-model="configForm[field.key]" />
          <p class="field-help">{{ field.description }}</p>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="configVisible = false">取消</el-button>
        <el-button type="primary" :loading="configSaving" @click="saveConfig">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import { featureApi } from '@/modules/features/api'
import { homeStylePackApi } from '@/modules/appearance/api'

interface FeatureRow {
  id: string
  label: string
  description: string
  representative: string
  configSources: Array<{ name: string; label: string }>
  state?: Record<string, any>
}

const definitions: FeatureRow[] = [
  { id: 'personal-space', label: '个人空间', description: '个人主页、内容同步与个人文件入口。', representative: 'personal-space', configSources: [{ name: 'personal-space', label: '空间配置' }] },
  { id: 'controlled-richtext-article', label: '图文文章', description: '文章草稿、发布、清洗、资产和模板。', representative: 'controlled-richtext-article', configSources: [{ name: 'controlled-richtext-article', label: '文章配置' }] },
  { id: 'personal-schedule', label: '个人课表', description: '学期课表、Excel 导入、周视图和日历。', representative: 'personal-schedule', configSources: [{ name: 'personal-schedule', label: '课表配置' }] },
  { id: 'appearance', label: '界面与风格', description: '系统主题、首页布局和风格资源包。', representative: 'web-theme', configSources: [{ name: 'web-theme', label: '主题配置' }, { name: 'homepage-customizer', label: '首页配置' }] },
]

const loading = ref(false)
const features = ref<FeatureRow[]>(definitions)
const configVisible = ref(false)
const configLoading = ref(false)
const configSaving = ref(false)
const selectedConfigName = ref('')
const configFields = ref<any[]>([])
const configForm = ref<Record<string, any>>({})
const packs = ref<any[]>([])
const packsLoading = ref(false)
const selectedPack = ref('')
const applyingPack = ref(false)
const rollingBack = ref(false)
const packInput = ref<HTMLInputElement | null>(null)
const packFile = ref<File | null>(null)
const validation = ref<any>(null)
const validatingPack = ref(false)
const uploadingPack = ref(false)
const downloadingExample = ref(false)

const unwrap = (payload: any) => payload?.data || payload
const listItems = (payload: any) => unwrap(payload)?.items || []

const load = async () => {
  loading.value = true
  try {
    const items = listItems(await featureApi.list())
    const byName = new Map(items.filter((item: any) => item?.capability_class === 'legacy-builtin').map((item: any) => [item.name, item]))
    features.value = definitions.map((definition) => ({ ...definition, state: byName.get(definition.representative) as Record<string, any> | undefined }))
  } catch (error: any) {
    ElMessage.error(error?.msg || '加载内置功能失败')
  } finally {
    loading.value = false
  }
}

const toggle = async (row: FeatureRow, enabled: boolean) => {
  try {
    const payload = unwrap(enabled
      ? await featureApi.enableCompatibility(row.representative)
      : await featureApi.disableCompatibility(row.representative))
    ElMessage.success(payload?.message || '目标状态已保存')
    await load()
  } catch (error: any) {
    ElMessage.error(error?.msg || '更新内置功能状态失败')
  }
}

const openConfig = async (name: string) => {
  selectedConfigName.value = name
  configVisible.value = true
  configLoading.value = true
  try {
    const detail = unwrap(await featureApi.getCompatibility(name))
    configFields.value = detail?.config_schema?.fields || []
    const config = detail?.config || {}
    configForm.value = Object.fromEntries(configFields.value.map((field) => [field.key, normalize(field, config[field.key] ?? field.default)]))
  } catch (error: any) {
    ElMessage.error(error?.msg || '加载功能配置失败')
  } finally {
    configLoading.value = false
  }
}

const normalize = (field: any, value: any) => {
  if (field.type === 'boolean') return typeof value === 'string' ? ['true', '1', 'yes', 'on'].includes(value.toLowerCase()) : Boolean(value)
  if (field.type === 'number') return Number.isFinite(Number(value)) ? Number(value) : 0
  return value ?? ''
}

const saveConfig = async () => {
  configSaving.value = true
  try {
    const payload = Object.fromEntries(configFields.value.map((field) => [field.key, normalize(field, configForm.value[field.key])]))
    await featureApi.updateCompatibilityConfig(selectedConfigName.value, payload)
    ElMessage.success('内置功能配置已保存')
    configVisible.value = false
    await load()
  } catch (error: any) {
    ElMessage.error(error?.msg || '保存功能配置失败')
  } finally {
    configSaving.value = false
  }
}

const loadPacks = async () => {
  packsLoading.value = true
  try {
    packs.value = unwrap(await homeStylePackApi.sources())?.items || []
    if (!packs.value.some((pack) => pack.name === selectedPack.value && pack.validation?.valid)) {
      selectedPack.value = packs.value.find((pack) => pack.validation?.valid)?.name || ''
    }
  } catch (error: any) {
    ElMessage.error(error?.msg || '加载首页资源包失败')
  } finally {
    packsLoading.value = false
  }
}

const applySourcePack = async () => {
  applyingPack.value = true
  try {
    await homeStylePackApi.applySource(selectedPack.value)
    ElMessage.success('首页资源包已应用')
  } catch (error: any) {
    ElMessage.error(error?.msg || '应用资源包失败')
  } finally {
    applyingPack.value = false
  }
}

const selectPackFile = (event: Event) => {
  packFile.value = (event.target as HTMLInputElement).files?.[0] || null
  validation.value = null
}

const validatePack = async () => {
  if (!packFile.value) return
  validatingPack.value = true
  try {
    validation.value = unwrap(await homeStylePackApi.validate(packFile.value))?.validation
  } catch (error: any) {
    ElMessage.error(error?.msg || '资源包筛查失败')
  } finally {
    validatingPack.value = false
  }
}

const applyPack = async () => {
  if (!packFile.value || !validation.value?.valid) return
  uploadingPack.value = true
  try {
    await homeStylePackApi.apply(packFile.value)
    ElMessage.success('首页资源包已导入并应用')
    await loadPacks()
  } catch (error: any) {
    ElMessage.error(error?.msg || '导入资源包失败')
  } finally {
    uploadingPack.value = false
  }
}

const rollbackPack = async () => {
  rollingBack.value = true
  try {
    await homeStylePackApi.rollback()
    ElMessage.success('首页风格已回滚')
  } catch (error: any) {
    ElMessage.error(error?.msg || '回滚失败')
  } finally {
    rollingBack.value = false
  }
}

const downloadExample = async () => {
  downloadingExample.value = true
  try {
    const payload: any = await homeStylePackApi.exampleZip()
    const blob = payload instanceof Blob ? payload : new Blob([payload], { type: 'application/zip' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = 'homepage-style-pack.zip'
    link.click()
    URL.revokeObjectURL(url)
  } catch (error: any) {
    ElMessage.error(error?.msg || '导出示例失败')
  } finally {
    downloadingExample.value = false
  }
}

onMounted(async () => {
  await Promise.all([load(), loadPacks()])
})
</script>

<style scoped>
.feature-page { display: grid; gap: 16px; max-width: 1440px; }
.page-header, .section-heading, .core-band, .row-actions, .pack-controls { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.page-header { align-items: flex-start; }
.page-header h2, .section-heading h3 { margin: 0; }
.page-header p, .section-heading p, .core-band p { margin: 6px 0 0; color: #606266; }
.eyebrow { color: #909399 !important; font-size: 12px; }
.core-band, .appearance-band { padding: 18px; background: #fff; border: 1px solid #e4e7ed; }
.row-actions, .pack-controls { justify-content: flex-start; flex-wrap: wrap; }
.pack-controls .el-select { width: min(360px, 100%); }
strong + small { display: block; margin-top: 4px; color: #909399; }
.field-help, .file-name { margin: 6px 0 0; color: #909399; font-size: 12px; }
.hidden { display: none; }
@media (max-width: 760px) {
  .page-header, .section-heading, .core-band { align-items: stretch; flex-direction: column; }
  .pack-controls > * { width: 100%; }
}
</style>
