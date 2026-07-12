<template>
  <div class="appearance-settings" v-loading="themeStore.loading">
    <header class="appearance-header">
      <div>
        <h2>界面风格</h2>
        <p>从管理员提供并通过筛查的系统风格包中选择。当前选择只保存在本机的当前用户下。</p>
      </div>
      <el-button :icon="RefreshLeft" :disabled="!themeStore.catalog.enabled" @click="restoreDefault"
        >恢复默认</el-button
      >
    </header>

    <el-alert v-if="themeStore.error" :title="themeStore.error" type="error" show-icon :closable="false" />
    <el-empty v-else-if="!themeStore.catalog.enabled" description="系统风格插件当前未运行" />
    <el-empty v-else-if="themeStore.catalog.items.length === 0" description="管理员尚未提供可用风格包" />

    <section v-else class="theme-list" aria-label="系统风格包">
      <article
        v-for="item in themeStore.catalog.items"
        :key="item.name"
        class="theme-item"
        :class="{ active: item.name === themeStore.activeName }"
      >
        <img v-if="item.preview_url" :src="item.preview_url" :alt="`${item.display_name} 预览`" />
        <div v-else class="theme-preview-placeholder">
          <el-icon><Picture /></el-icon>
        </div>
        <div class="theme-content">
          <div class="theme-title-row">
            <div>
              <h3>{{ item.display_name }}</h3>
              <code>{{ item.name }}@{{ item.version }}</code>
            </div>
            <el-tag v-if="item.name === themeStore.activeName" type="success" effect="plain">正在使用</el-tag>
          </div>
          <p>{{ item.description || '管理员提供的 CampusOS 系统风格包。' }}</p>
          <div class="token-row">
            <span
              v-for="token in colorTokens(item)"
              :key="token.key"
              class="color-token"
              :title="`${token.key}: ${token.value}`"
              :style="{ backgroundColor: token.value }"
            />
          </div>
          <div v-if="item.capabilities?.length" class="capability-list">
            <el-tag
              v-for="capability in item.capabilities"
              :key="capability"
              size="small"
              :type="capability === 'schedule.me.read' ? 'warning' : 'info'"
              effect="plain"
            >
              {{ capabilityLabel(capability) }}
            </el-tag>
          </div>
          <div class="theme-actions">
            <el-button
              type="primary"
              :disabled="item.name === themeStore.activeName || !themeStore.catalog.allow_user_switch"
              @click="selectTheme(item)"
            >
              应用风格
            </el-button>
            <el-button
              v-if="item.name === themeStore.activeName && Object.keys(themeStore.configurationFields).length"
              :icon="Setting"
              @click="openConfiguration"
            >
              个性设置
            </el-button>
          </div>
        </div>
      </article>
    </section>

    <el-dialog v-model="configurationVisible" title="风格个性设置" width="min(560px, calc(100vw - 28px))">
      <el-alert
        v-if="contrastIssues.length"
        :title="contrastIssues.join('；')"
        type="error"
        show-icon
        :closable="false"
      />
      <div class="configuration-preview" :style="configurationPreviewStyle">
        <strong>CampusOS 风格预览</strong>
        <span>文字与背景需要保持清晰可读。</span>
      </div>
      <el-form label-position="top" class="configuration-form">
        <el-form-item v-for="(field, key) in themeStore.configurationFields" :key="key" :label="field.title || key">
          <el-color-picker v-if="field.format === 'color'" v-model="configurationDraft[key]" show-alpha />
          <el-select v-else-if="field.enum?.length" v-model="configurationDraft[key]" class="field-control">
            <el-option
              v-for="(option, index) in field.enum"
              :key="String(option)"
              :label="field.enum_names?.[index] || option || '不使用背景图'"
              :value="option"
            />
          </el-select>
          <el-switch v-else-if="field.type === 'boolean'" v-model="configurationDraft[key]" />
          <el-slider
            v-else-if="field.type === 'number' || field.type === 'integer'"
            v-model="configurationDraft[key]"
            :min="field.minimum ?? 0"
            :max="field.maximum ?? 100"
            show-input
          />
          <el-input v-else v-model="configurationDraft[key]" class="field-control" />
          <p v-if="field.description" class="field-description">{{ field.description }}</p>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="resetConfiguration">恢复风格包默认值</el-button>
        <el-button type="primary" :disabled="contrastIssues.length > 0" @click="saveConfiguration">
          应用设置
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ElMessage, ElMessageBox } from 'element-plus'
import { computed, ref } from 'vue'
import { Picture, RefreshLeft, Setting } from '@element-plus/icons-vue'
import { useUserStore } from '@/stores/user'
import { useWebThemeStore, type WebThemeItem } from '@/stores/webTheme'

const userStore = useUserStore()
const themeStore = useWebThemeStore()
const configurationVisible = ref(false)
const configurationDraft = ref<Record<string, string | number | boolean>>({})

const userID = () => userStore.user?.id
const capabilityLabel = (capability: string) =>
  ({
    'community.threads.read': '读取公开帖子摘要',
    'categories.read': '读取公开版块',
    'schedule.me.read': '读取我的课表（需授权）',
  })[capability] || capability

const colorTokens = (item: WebThemeItem) =>
  Object.entries(item.tokens || {})
    .filter(([key, value]) => key.startsWith('color.') && /^#[0-9a-f]{6}$/i.test(value))
    .slice(0, 5)
    .map(([key, value]) => ({ key, value }))

const selectTheme = async (item: WebThemeItem) => {
  const grants: string[] = []
  if (item.capabilities?.includes('schedule.me.read')) {
    await ElMessageBox.confirm(
      '该风格包申请读取当前登录用户自己的课表。主应用会代为调用接口并裁剪数据，风格包不会获得登录令牌。是否授权？',
      '私有数据能力授权',
      { confirmButtonText: '授权并应用', cancelButtonText: '取消', type: 'warning' },
    )
    grants.push('schedule.me.read')
  }
  await themeStore.select(item.name, userID(), grants)
  ElMessage.success(`已应用 ${item.display_name}`)
}

const restoreDefault = async () => {
  await themeStore.restoreDefault(userID())
  ElMessage.success('已恢复管理员设置的默认风格')
}

const openConfiguration = () => {
  configurationDraft.value = { ...themeStore.configValues }
  configurationVisible.value = true
}

const boundValue = (binding: string, fallback = '') => {
  for (const [key, field] of Object.entries(themeStore.configurationFields)) {
    if (field['x-campusos-binding'] === binding && configurationDraft.value[key] !== undefined) {
      return String(configurationDraft.value[key])
    }
  }
  if (binding.startsWith('token.')) return themeStore.activePackage?.manifest.tokens?.[binding.slice(6)] || fallback
  return fallback
}

const parseHex = (value: string) => {
  const match = /^#([0-9a-f]{6})$/i.exec(value.trim())
  if (!match) return null
  return [0, 2, 4].map((offset) => Number.parseInt(match[1].slice(offset, offset + 2), 16) / 255)
}

const contrastRatio = (foreground: string, background: string) => {
  const fg = parseHex(foreground)
  const bg = parseHex(background)
  if (!fg || !bg) return null
  const luminance = (rgb: number[]) => 0.2126 * channel(rgb[0]) + 0.7152 * channel(rgb[1]) + 0.0722 * channel(rgb[2])
  const channel = (value: number) => (value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4)
  const high = Math.max(luminance(fg), luminance(bg))
  const low = Math.min(luminance(fg), luminance(bg))
  return (high + 0.05) / (low + 0.05)
}

const contrastIssues = computed(() => {
  const text = boundValue('token.text_color', boundValue('token.color.text', '#1f2937'))
  const backgrounds = [
    boundValue('token.page_background', boundValue('token.color.background')),
    boundValue('token.surface_background', boundValue('token.color.surface')),
  ].filter(Boolean)
  return backgrounds.flatMap((background) => {
    const ratio = contrastRatio(text, background)
    return ratio !== null && ratio < 4.5 ? [`文字与 ${background} 的对比度仅为 ${ratio.toFixed(2)}:1`] : []
  })
})

const configurationPreviewStyle = computed(() => ({
  color: boundValue('token.text_color', '#1f2937'),
  backgroundColor: boundValue('token.surface_background', '#ffffff'),
}))

const saveConfiguration = () => {
  if (contrastIssues.value.length) return
  themeStore.saveConfiguration(userID(), configurationDraft.value)
  configurationVisible.value = false
  ElMessage.success('风格个性设置已应用')
}

const resetConfiguration = () => {
  themeStore.resetConfiguration(userID())
  configurationDraft.value = { ...themeStore.configValues }
}
</script>

<style scoped>
.appearance-settings {
  display: grid;
  gap: 16px;
  max-width: 1040px;
  margin: 0 auto;
  color: var(--campus-text-color, var(--campus-color-text, #1f2937));
}

.appearance-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 20px;
  padding: 18px;
  border-bottom: 1px solid #dcdfe6;
  background: color-mix(
    in srgb,
    var(--campus-surface-background, var(--campus-color-surface, #ffffff)) 96%,
    transparent
  );
}

.appearance-header h2,
.appearance-header p {
  margin: 0;
}

.appearance-header p {
  margin-top: 8px;
  color: var(--campus-muted-color, var(--campus-color-muted, #606266));
  line-height: 1.7;
}

.theme-list {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
}

.theme-item {
  display: grid;
  grid-template-rows: 190px minmax(0, 1fr);
  overflow: hidden;
  border: 1px solid #dcdfe6;
  border-radius: 6px;
  color: var(--campus-text-color, var(--campus-color-text, #1f2937));
  background: var(--campus-surface-background, var(--campus-color-surface, #ffffff));
}

.theme-item.active {
  border-color: #16a36a;
  box-shadow: 0 0 0 1px #16a36a;
}

.theme-item img,
.theme-preview-placeholder {
  width: 100%;
  height: 190px;
  object-fit: cover;
  background: #eef2f0;
}

.theme-preview-placeholder {
  display: grid;
  place-items: center;
  color: #87958e;
  font-size: 30px;
}

.theme-content {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 12px;
  padding: 18px;
}

.theme-title-row {
  display: flex;
  width: 100%;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}

.theme-title-row h3 {
  margin: 0 0 5px;
  font-size: 18px;
}

.theme-title-row code {
  color: #909399;
  font-size: 12px;
}

.theme-content p {
  margin: 0;
  color: var(--campus-muted-color, var(--campus-color-muted, #606266));
  line-height: 1.65;
}

.token-row,
.capability-list {
  display: flex;
  flex-wrap: wrap;
  gap: 7px;
}

.color-token {
  width: 24px;
  height: 24px;
  border: 1px solid rgba(0, 0, 0, 0.12);
  border-radius: 4px;
}

.theme-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: auto;
}

.configuration-preview {
  display: grid;
  gap: 6px;
  margin: 14px 0 18px;
  padding: 18px;
  border: 1px solid #dcdfe6;
  border-radius: 6px;
}

.configuration-form {
  max-height: min(56vh, 560px);
  overflow: auto;
  padding-right: 6px;
}

.field-control {
  width: 100%;
}

.field-description {
  width: 100%;
  margin: 6px 0 0;
  color: var(--campus-muted-color, var(--campus-color-muted, #606266));
  line-height: 1.55;
}

@media (max-width: 760px) {
  .appearance-header {
    flex-direction: column;
  }

  .theme-list {
    grid-template-columns: 1fr;
  }
}
</style>
