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

    <el-alert
      type="info"
      :closable="false"
      title="校园互助和校园二手归入图文内容能力组；分组只表达界面与能力复用关系，各功能仍有独立开关和数据生命周期。"
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
          <div :class="['feature-name', { 'is-child': row.parentId }]">
            <span v-if="row.parentId" class="child-line" aria-hidden="true"></span>
            <div>
              <strong>{{ row.label }}</strong>
              <small>{{ row.id }}</small>
            </div>
            <el-tag v-if="row.parentId" size="small" type="info" effect="plain">图文子功能</el-tag>
          </div>
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
          <h3>外观资源由独立页面管理</h3>
          <p>这里控制 Appearance Built-in Feature 的启停与配置；首页包切换、系统主题目录和个人主页风格边界在外观页面查看。</p>
        </div>
        <el-button type="primary" plain @click="$router.push('/appearance')">打开外观与风格包</el-button>
      </div>
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
import { builtinFeatureDefinitions, mapBuiltinFeatures, type FeatureRow } from '@/modules/features/catalog'

const loading = ref(false)
const features = ref<FeatureRow[]>(builtinFeatureDefinitions)
const configVisible = ref(false)
const configLoading = ref(false)
const configSaving = ref(false)
const selectedConfigName = ref('')
const configFields = ref<any[]>([])
const configForm = ref<Record<string, any>>({})

const unwrap = (payload: any) => payload?.data || payload
const listItems = (payload: any) => unwrap(payload)?.items || []

const load = async () => {
  loading.value = true
  try {
    const items = listItems(await featureApi.list())
    features.value = mapBuiltinFeatures(items)
  } catch (error: any) {
    ElMessage.error(error?.msg || '加载内置功能失败')
  } finally {
    loading.value = false
  }
}

const toggle = async (row: FeatureRow, enabled: boolean) => {
  try {
    const payload = unwrap(enabled
      ? await featureApi.enable(row.id)
      : await featureApi.disable(row.id))
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
    const detail = unwrap(await featureApi.get(name))
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
    await featureApi.updateConfig(selectedConfigName.value, payload)
    ElMessage.success('内置功能配置已保存')
    configVisible.value = false
    await load()
  } catch (error: any) {
    ElMessage.error(error?.msg || '保存功能配置失败')
  } finally {
    configSaving.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.feature-page { display: grid; gap: 16px; max-width: 1440px; }
.page-header, .section-heading, .core-band, .row-actions { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.page-header { align-items: flex-start; }
.page-header h2, .section-heading h3 { margin: 0; }
.page-header p, .section-heading p, .core-band p { margin: 6px 0 0; color: #606266; }
.eyebrow { color: #909399 !important; font-size: 12px; }
.core-band, .appearance-band { padding: 18px; background: #fff; border: 1px solid #e4e7ed; }
.row-actions { justify-content: flex-start; flex-wrap: wrap; }
strong + small { display: block; margin-top: 4px; color: #909399; }
.feature-name { display: flex; align-items: center; gap: 8px; }
.feature-name.is-child { padding-left: 20px; }
.child-line { width: 12px; height: 16px; border-left: 1px solid #c0c4cc; border-bottom: 1px solid #c0c4cc; }
.field-help { margin: 6px 0 0; color: #909399; font-size: 12px; }
@media (max-width: 760px) {
  .page-header, .section-heading, .core-band { align-items: stretch; flex-direction: column; }
}
</style>
