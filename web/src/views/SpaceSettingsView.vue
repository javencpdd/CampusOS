<template>
  <div class="space-settings">
    <div class="page-header">
      <div>
        <h2>个人主页</h2>
        <p>配置公开主页、内容同步和风格包。</p>
      </div>
      <div class="header-actions">
        <el-button @click="goPublicSpace" :disabled="!owner?.username">
          <el-icon><View /></el-icon>
          查看主页
        </el-button>
        <el-button @click="exportCurrentStyle" :loading="exporting">
          <el-icon><Download /></el-icon>
          导出风格
        </el-button>
      </div>
    </div>

    <el-row :gutter="20">
      <el-col :xs="24" :lg="10">
        <el-card class="panel" shadow="never" v-loading="loading">
          <template #header>
            <div class="panel-header">
              <span>主页配置</span>
              <el-tag v-if="currentSpace?.style_name" type="success" effect="plain">
                {{ currentSpace.style_name }}@{{ currentSpace.style_version }}
              </el-tag>
            </div>
          </template>

          <el-form :model="form" label-position="top">
            <el-form-item label="头像">
              <div class="avatar-uploader">
                <el-avatar :size="72" :src="form.avatar">
                  {{ avatarInitial }}
                </el-avatar>
                <div class="avatar-controls">
                  <input
                    ref="avatarInput"
                    class="avatar-input"
                    type="file"
                    accept="image/png,image/jpeg,image/gif,image/webp"
                    @change="uploadAvatar"
                  />
                  <el-button @click="chooseAvatar" :loading="uploadingAvatar">
                    上传头像
                  </el-button>
                  <span v-if="storage" class="storage-hint">
                    {{ formatBytes(storage.used_bytes) }} / {{ formatBytes(storage.quota_bytes) }}，保留最近
                    {{ storage.avatar_keep_limit }} 个源文件
                  </span>
                </div>
              </div>
            </el-form-item>
            <el-form-item label="标题">
              <el-input v-model="form.title" maxlength="120" show-word-limit />
            </el-form-item>
            <el-form-item label="简介">
              <el-input v-model="form.bio" type="textarea" :rows="4" maxlength="500" show-word-limit />
            </el-form-item>
            <el-form-item label="封面图">
              <el-input v-model="form.cover_image" placeholder="https://example.com/cover.png" />
            </el-form-item>
            <el-row :gutter="12">
              <el-col :span="12">
                <el-form-item label="可见性">
                  <el-select v-model="form.visibility" class="field-full">
                    <el-option label="公开" value="public" />
                    <el-option label="隐藏链接" value="unlisted" />
                    <el-option label="私有" value="private" />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item label="布局">
                  <el-select v-model="form.layout" class="field-full">
                    <el-option label="Blog" value="blog" />
                    <el-option label="Grid" value="grid" />
                    <el-option label="Timeline" value="timeline" />
                    <el-option label="Magazine" value="magazine" />
                  </el-select>
                </el-form-item>
              </el-col>
            </el-row>
            <el-form-item label="内容同步">
              <el-switch v-model="form.sync_enabled" active-text="启用" inactive-text="关闭" />
            </el-form-item>
            <el-form-item label="同步版块">
              <el-select
                v-model="form.sync_categories"
                class="field-full"
                multiple
                filterable
                allow-create
                default-first-option
              />
            </el-form-item>
            <el-form-item label="同步标签">
              <el-select
                v-model="form.sync_tags"
                class="field-full"
                multiple
                filterable
                allow-create
                default-first-option
              />
            </el-form-item>
            <el-button type="primary" @click="saveSpace" :loading="saving">
              <el-icon><Check /></el-icon>
              保存配置
            </el-button>
          </el-form>
        </el-card>
      </el-col>

      <el-col :xs="24" :lg="14">
        <el-card class="panel" shadow="never">
          <template #header>
            <div class="panel-header">
              <span>风格包</span>
              <el-tag v-if="validation?.valid" type="success" effect="plain">校验通过</el-tag>
              <el-tag v-else-if="validation" type="danger" effect="plain">校验失败</el-tag>
            </div>
          </template>

          <div class="style-grid">
            <button
              v-for="item in styleExamples"
              :key="item.manifest.name"
              class="style-option"
              :class="{ active: selectedStyleName === item.manifest.name }"
              type="button"
              @click="selectExample(item)"
            >
              <span class="style-swatch" :style="{ background: item.manifest.tokens?.['color.primary'] || '#409eff' }" />
              <span class="style-title">{{ item.manifest.name }}</span>
              <span class="style-layout">{{ item.manifest.layout }}</span>
            </button>
          </div>

          <el-input v-model="styleText" type="textarea" :rows="13" class="style-editor" spellcheck="false" />

          <div v-if="validation?.errors?.length" class="error-list">
            <el-alert
              v-for="error in validation.errors"
              :key="error"
              :title="error"
              type="error"
              show-icon
              :closable="false"
            />
          </div>

          <div class="style-actions">
            <el-button @click="validateStyle" :loading="validating">
              <el-icon><CircleCheck /></el-icon>
              校验
            </el-button>
            <el-button @click="previewStyle" :loading="previewing">
              <el-icon><View /></el-icon>
              预览
            </el-button>
            <el-button type="primary" @click="applyStyle" :loading="applying">
              <el-icon><Switch /></el-icon>
              应用
            </el-button>
            <el-button @click="rollbackStyle" :loading="rollbacking">
              <el-icon><RefreshLeft /></el-icon>
              回滚
            </el-button>
            <el-button @click="restoreDefaultStyle" :loading="defaulting">
              <el-icon><Refresh /></el-icon>
              默认
            </el-button>
          </div>

          <div class="preview-panel" :style="previewStyleVars">
            <div class="preview-header">
              <div>
                <strong>{{ activeManifest?.name || currentSpace?.style_name || 'default' }}</strong>
                <span>{{ activeManifest?.layout || currentSpace?.layout || 'blog' }}</span>
              </div>
              <el-tag size="small" effect="plain">{{ activeManifest?.version || currentSpace?.style_version || '0.1.0' }}</el-tag>
            </div>
            <div class="component-list">
              <el-tag v-for="component in activeComponents" :key="`${component.slot}-${component.type}`" effect="plain">
                {{ component.slot }} / {{ component.type }}
              </el-tag>
            </div>
          </div>
        </el-card>

        <el-card class="panel" shadow="never">
          <template #header>
            <div class="panel-header">
              <span>HTML 风格</span>
              <el-tag v-if="htmlValidation?.valid" type="success" effect="plain">检测通过</el-tag>
              <el-tag v-else-if="htmlValidation" type="danger" effect="plain">检测失败</el-tag>
            </div>
          </template>

          <el-input
            v-model="htmlText"
            type="textarea"
            :rows="12"
            class="style-editor"
            spellcheck="false"
            placeholder="<section>...</section>"
          />

          <div v-if="htmlValidation?.errors?.length" class="error-list">
            <el-alert
              v-for="error in htmlValidation.errors"
              :key="error"
              :title="error"
              type="error"
              show-icon
              :closable="false"
            />
          </div>
          <div v-if="htmlValidation?.warnings?.length" class="warning-list">
            <el-alert
              v-for="warning in htmlValidation.warnings"
              :key="warning"
              :title="warning"
              type="warning"
              show-icon
              :closable="false"
            />
          </div>

          <div class="style-actions">
            <el-button @click="generateHTMLExample" :loading="htmlGenerating">
              <el-icon><Download /></el-icon>
              生成示例
            </el-button>
            <el-button @click="validateHTML" :loading="htmlValidating">
              <el-icon><CircleCheck /></el-icon>
              检测 HTML
            </el-button>
            <el-button type="primary" @click="applyHTMLStyle" :loading="htmlApplying">
              <el-icon><Switch /></el-icon>
              应用 HTML
            </el-button>
          </div>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { CircleCheck, Check, Download, Refresh, RefreshLeft, Switch, View } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { spaceApi } from '@/api'
import { styleExamples, type StylePackage } from '@/data/spaceStyleExamples'
import { useUserStore } from '@/stores/user'

interface ApiResponse<T> {
  code: number
  msg: string
  data: T
}

interface Owner {
  id: string
  username: string
  nickname: string
  avatar?: string
  bio?: string
}

interface Space {
  id?: string
  user_id: string
  title: string
  bio: string
  avatar?: string
  cover_image?: string
  theme: string
  layout: string
  style_name?: string
  style_version?: string
  style_manifest?: StylePackage['manifest']
  visibility: string
  sync_enabled: boolean
  sync_categories?: string[]
  sync_tags?: string[]
  disabled_at?: string
  disabled_reason?: string
  last_sync_at?: string
  last_sync_error?: string
}

interface PublicSpacePayload {
  owner: Owner
  space: Space
}

interface StyleValidationResult {
  valid: boolean
  errors?: string[]
  warnings?: string[]
}

interface StylePreview {
  validation: StyleValidationResult
  manifest?: StylePackage['manifest']
  layout?: string
  components?: StylePackage['manifest']['components']
  tokens?: Record<string, string>
}

interface StyleApplyResult {
  validation: StyleValidationResult
  space: Space
  applied?: StylePackage['manifest']
}

interface StyleExportResult {
  package: StylePackage
  filename: string
  validation: StyleValidationResult
}

interface StyleHTMLExampleResult {
  html: string
  package: StylePackage
  validation: StyleValidationResult
}

interface SpaceStorageStatus {
  user_id: string
  quota_bytes: number
  used_bytes: number
  available_bytes: number
  avatar_keep_limit: number
}

interface AvatarUploadResult {
  file_name: string
  url: string
  size: number
  storage: SpaceStorageStatus
  owner: Owner
  space: Space
}

interface SpaceForm {
  title: string
  bio: string
  avatar: string
  cover_image: string
  layout: string
  visibility: string
  sync_enabled: boolean
  sync_categories: string[]
  sync_tags: string[]
}

const router = useRouter()
const userStore = useUserStore()

const owner = ref<Owner | null>(null)
const currentSpace = ref<Space | null>(null)
const storage = ref<SpaceStorageStatus | null>(null)
const loading = ref(false)
const saving = ref(false)
const uploadingAvatar = ref(false)
const validating = ref(false)
const previewing = ref(false)
const applying = ref(false)
const exporting = ref(false)
const rollbacking = ref(false)
const defaulting = ref(false)
const htmlGenerating = ref(false)
const htmlValidating = ref(false)
const htmlApplying = ref(false)
const validation = ref<StyleValidationResult | null>(null)
const htmlValidation = ref<StyleValidationResult | null>(null)
const preview = ref<StylePreview | null>(null)
const avatarInput = ref<HTMLInputElement | null>(null)
const selectedStyleName = ref(styleExamples[0].manifest.name)
const styleText = ref(JSON.stringify(styleExamples[0], null, 2))
const htmlText = ref('')

const form = reactive<SpaceForm>({
  title: '',
  bio: '',
  avatar: '',
  cover_image: '',
  layout: 'blog',
  visibility: 'public',
  sync_enabled: true,
  sync_categories: [],
  sync_tags: [],
})

const activeManifest = computed(() => preview.value?.manifest || currentSpace.value?.style_manifest || null)
const activeComponents = computed(() => activeManifest.value?.components || [])
const avatarInitial = computed(() => {
  const name = owner.value?.nickname || owner.value?.username || 'U'
  return name.slice(0, 1).toUpperCase()
})
const previewStyleVars = computed<Record<string, string>>(() => {
  const tokens = activeManifest.value?.tokens || {}
  return {
    '--space-primary': tokens['color.primary'] || '#2563eb',
    '--space-bg': tokens['color.background'] || '#ffffff',
    '--space-surface': tokens['color.surface'] || '#f8fafc',
    '--space-radius': tokens['radius.card'] || '8px',
  }
})

const unwrap = <T,>(res: unknown): T => (res as ApiResponse<T>).data

const fillForm = (space: Space) => {
  form.title = space.title || ''
  form.bio = space.bio || ''
  form.avatar = space.avatar || owner.value?.avatar || ''
  form.cover_image = space.cover_image || ''
  form.layout = space.layout || 'blog'
  form.visibility = space.visibility || 'public'
  form.sync_enabled = space.sync_enabled
  form.sync_categories = [...(space.sync_categories || [])]
  form.sync_tags = [...(space.sync_tags || [])]
}

const syncHTMLEditor = (space: Space) => {
  htmlText.value = space.style_manifest?.custom_html || ''
  htmlValidation.value = null
}

const loadSpace = async () => {
  loading.value = true
  try {
    const [spaceRes, storageRes] = await Promise.all([spaceApi.me(), spaceApi.storage()])
    const payload = unwrap<PublicSpacePayload>(spaceRes)
    owner.value = payload.owner
    currentSpace.value = payload.space
    fillForm(payload.space)
    syncHTMLEditor(payload.space)
    storage.value = unwrap<SpaceStorageStatus>(storageRes)
  } catch (error: any) {
    ElMessage.error(error?.msg || '加载个人主页失败')
  } finally {
    loading.value = false
  }
}

const saveSpace = async () => {
  saving.value = true
  try {
    const payload = unwrap<PublicSpacePayload>(await spaceApi.updateMe({ ...form }))
    owner.value = payload.owner
    currentSpace.value = payload.space
    fillForm(payload.space)
    syncHTMLEditor(payload.space)
    if (payload.owner.avatar) {
      userStore.setAvatar(payload.owner.avatar)
    }
    ElMessage.success('已保存')
  } catch (error: any) {
    ElMessage.error(error?.msg || '保存失败')
  } finally {
    saving.value = false
  }
}

const chooseAvatar = () => {
  avatarInput.value?.click()
}

const uploadAvatar = async (event: Event) => {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  uploadingAvatar.value = true
  try {
    const payload = unwrap<AvatarUploadResult>(await spaceApi.uploadAvatar(file))
    owner.value = payload.owner
    currentSpace.value = payload.space
    storage.value = payload.storage
    fillForm(payload.space)
    syncHTMLEditor(payload.space)
    userStore.setAvatar(payload.url)
    ElMessage.success('头像已更新')
  } catch (error: any) {
    ElMessage.error(error?.msg || '头像上传失败')
  } finally {
    uploadingAvatar.value = false
    input.value = ''
  }
}

const formatBytes = (value: number) => {
  if (!Number.isFinite(value) || value <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  let size = value
  let unit = 0
  while (size >= 1024 && unit < units.length - 1) {
    size /= 1024
    unit++
  }
  return `${size.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`
}

const parseStylePackage = (): StylePackage | null => {
  try {
    const parsed = JSON.parse(styleText.value) as StylePackage
    if (!parsed?.manifest) {
      throw new Error('missing manifest')
    }
    return parsed
  } catch {
    ElMessage.error('风格包 JSON 无效')
    return null
  }
}

const selectExample = (item: StylePackage) => {
  selectedStyleName.value = item.manifest.name
  styleText.value = JSON.stringify(item, null, 2)
  validation.value = null
  preview.value = null
}

const validateStyle = async () => {
  const pkg = parseStylePackage()
  if (!pkg) return
  validating.value = true
  try {
    validation.value = unwrap<StyleValidationResult>(await spaceApi.validateStyle(pkg))
    if (validation.value.valid) {
      ElMessage.success('校验通过')
    } else {
      ElMessage.warning('校验失败')
    }
  } catch (error: any) {
    ElMessage.error(error?.msg || '校验失败')
  } finally {
    validating.value = false
  }
}

const previewStyle = async () => {
  const pkg = parseStylePackage()
  if (!pkg) return
  previewing.value = true
  try {
    const payload = unwrap<StylePreview>(await spaceApi.previewStyle(pkg))
    validation.value = payload.validation
    preview.value = payload.validation.valid ? payload : null
    if (payload.validation.valid) {
      ElMessage.success('预览已生成')
    } else {
      ElMessage.warning('预览校验失败')
    }
  } catch (error: any) {
    ElMessage.error(error?.msg || '预览失败')
  } finally {
    previewing.value = false
  }
}

const applyStyle = async () => {
  const pkg = parseStylePackage()
  if (!pkg) return
  applying.value = true
  try {
    const payload = unwrap<StyleApplyResult>(await spaceApi.applyStyle(pkg))
    validation.value = payload.validation
    currentSpace.value = payload.space
    preview.value = payload.applied
      ? {
          validation: payload.validation,
          manifest: payload.applied,
          layout: payload.applied.layout,
          components: payload.applied.components,
          tokens: payload.applied.tokens,
        }
      : null
    fillForm(payload.space)
    syncHTMLEditor(payload.space)
    ElMessage.success('风格已应用')
  } catch (error: any) {
    ElMessage.error(error?.msg || '应用失败')
  } finally {
    applying.value = false
  }
}

const generateHTMLExample = async () => {
  htmlGenerating.value = true
  try {
    const payload = unwrap<StyleHTMLExampleResult>(await spaceApi.customHtmlExample())
    htmlText.value = payload.html
    htmlValidation.value = payload.validation
    validation.value = payload.validation
    styleText.value = JSON.stringify(payload.package, null, 2)
    selectedStyleName.value = payload.package.manifest.name
    if (payload.validation.valid) {
      ElMessage.success('示例已生成')
    } else {
      ElMessage.warning('示例需要调整')
    }
  } catch (error: any) {
    ElMessage.error(error?.msg || '生成示例失败')
  } finally {
    htmlGenerating.value = false
  }
}

const validateHTML = async () => {
  htmlValidating.value = true
  try {
    htmlValidation.value = unwrap<StyleValidationResult>(await spaceApi.validateCustomHtml(htmlText.value))
    if (htmlValidation.value.valid) {
      ElMessage.success('HTML 检测通过')
    } else {
      ElMessage.warning('HTML 检测失败')
    }
  } catch (error: any) {
    ElMessage.error(error?.msg || 'HTML 检测失败')
  } finally {
    htmlValidating.value = false
  }
}

const applyHTMLStyle = async () => {
  htmlApplying.value = true
  try {
    const payload = unwrap<StyleApplyResult>(await spaceApi.applyCustomHtml(htmlText.value))
    validation.value = payload.validation
    currentSpace.value = payload.space
    preview.value = payload.applied
      ? {
          validation: payload.validation,
          manifest: payload.applied,
          layout: payload.applied.layout,
          components: payload.applied.components,
          tokens: payload.applied.tokens,
        }
      : null
    if (payload.applied) {
      styleText.value = JSON.stringify({ manifest: payload.applied }, null, 2)
      selectedStyleName.value = payload.applied.name
    }
    fillForm(payload.space)
    syncHTMLEditor(payload.space)
    htmlValidation.value = payload.validation
    ElMessage.success('HTML 风格已应用')
  } catch (error: any) {
    ElMessage.error(error?.msg || '应用 HTML 失败')
  } finally {
    htmlApplying.value = false
  }
}

const rollbackStyle = async () => {
  rollbacking.value = true
  try {
    const payload = unwrap<PublicSpacePayload>(await spaceApi.rollbackStyle())
    owner.value = payload.owner
    currentSpace.value = payload.space
    fillForm(payload.space)
    syncHTMLEditor(payload.space)
    preview.value = null
    ElMessage.success('已回滚到上一个风格快照')
  } catch (error: any) {
    ElMessage.error(error?.msg || '回滚失败，可能还没有可用快照')
  } finally {
    rollbacking.value = false
  }
}

const restoreDefaultStyle = async () => {
  defaulting.value = true
  try {
    const payload = unwrap<PublicSpacePayload>(await spaceApi.restoreDefaultStyle())
    owner.value = payload.owner
    currentSpace.value = payload.space
    fillForm(payload.space)
    syncHTMLEditor(payload.space)
    preview.value = null
    ElMessage.success('已恢复默认风格')
  } catch (error: any) {
    ElMessage.error(error?.msg || '恢复默认失败')
  } finally {
    defaulting.value = false
  }
}

const exportCurrentStyle = async () => {
  exporting.value = true
  try {
    const payload = unwrap<StyleExportResult>(await spaceApi.exportStyle())
    const blob = new Blob([JSON.stringify(payload.package, null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = payload.filename
    link.click()
    URL.revokeObjectURL(url)
  } catch (error: any) {
    ElMessage.error(error?.msg || '导出失败')
  } finally {
    exporting.value = false
  }
}

const goPublicSpace = () => {
  if (owner.value?.username) {
    router.push(`/u/${owner.value.username}`)
  }
}

onMounted(loadSpace)
</script>

<style scoped>
.space-settings {
  padding: 24px 0 32px;
}
.page-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 20px;
}
.page-header h2 {
  margin: 0 0 6px;
}
.page-header p {
  margin: 0;
  color: #606266;
}
.header-actions,
.style-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}
.panel {
  margin-bottom: 20px;
}
.panel-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.avatar-uploader {
  display: flex;
  align-items: center;
  gap: 14px;
  min-width: 0;
}
.avatar-controls {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
  min-width: 0;
}
.avatar-input {
  display: none;
}
.storage-hint {
  color: #606266;
  font-size: 13px;
  line-height: 1.5;
}
.field-full {
  width: 100%;
}
.style-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: 10px;
  margin-bottom: 14px;
}
.style-option {
  display: grid;
  grid-template-columns: 18px 1fr auto;
  align-items: center;
  gap: 8px;
  min-height: 42px;
  padding: 8px 10px;
  border: 1px solid #dcdfe6;
  border-radius: 8px;
  background: #fff;
  color: #303133;
  cursor: pointer;
  text-align: left;
}
.style-option.active {
  border-color: #409eff;
  background: #ecf5ff;
}
.style-swatch {
  width: 18px;
  height: 18px;
  border-radius: 50%;
}
.style-title {
  font-weight: 600;
  overflow-wrap: anywhere;
}
.style-layout {
  color: #909399;
  font-size: 12px;
}
.style-editor {
  margin-bottom: 12px;
}
.error-list {
  display: grid;
  gap: 8px;
  margin-bottom: 12px;
}
.warning-list {
  display: grid;
  gap: 8px;
  margin-bottom: 12px;
}
.preview-panel {
  margin-top: 16px;
  padding: 18px;
  border: 1px solid #dcdfe6;
  border-radius: var(--space-radius);
  background: var(--space-bg);
  color: #303133;
}
.preview-header {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 14px;
  padding-bottom: 12px;
  border-bottom: 2px solid var(--space-primary);
}
.preview-header strong {
  display: block;
  margin-bottom: 4px;
}
.preview-header span {
  color: #606266;
}
.component-list {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
@media (max-width: 720px) {
  .page-header {
    align-items: flex-start;
    flex-direction: column;
  }
}
</style>
