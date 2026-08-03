<template>
  <section class="branding-manager" aria-labelledby="branding-heading" v-loading="loading">
    <div class="section-heading">
      <div>
        <p class="eyebrow">System Branding</p>
        <h3 id="branding-heading">系统 Logo</h3>
        <p>该 Logo 显示在用户前台顶部。上传后立即生效，无需重启 Docker；服务端会压缩图片并限制最长边。</p>
      </div>
      <el-tag :type="logo.custom ? 'success' : 'info'">{{ logo.custom ? '自定义 Logo' : '系统默认 Logo' }}</el-tag>
    </div>

    <div class="branding-workbench">
      <div class="logo-preview" aria-label="当前或待上传 Logo 预览">
        <img v-if="previewURL || logo.url" :src="previewURL || logo.url" alt="CampusOS 系统 Logo 预览" />
        <span v-else>Logo 暂不可用</span>
      </div>
      <div class="branding-controls">
        <p>
          支持 PNG、JPEG；单文件最大 {{ formatBytes(logo.max_bytes || defaultMaxBytes) }}，上传后最长边压缩到
          1024 px。推荐使用透明背景横版图片。
        </p>
        <p v-if="logo.size_bytes" class="current-meta">
          当前文件：{{ formatBytes(logo.size_bytes) }}<template v-if="logo.width && logo.height">
            · {{ logo.width }} × {{ logo.height }} px</template
          >
        </p>
        <input
          ref="logoInput"
          class="hidden-input"
          type="file"
          accept="image/png,image/jpeg"
          @change="selectLogo"
        />
        <div class="control-row">
          <el-button @click="logoInput?.click()">选择图片</el-button>
          <el-button type="primary" :disabled="!pendingFile" :loading="uploading" @click="uploadLogo">保存并替换</el-button>
          <el-button v-if="logo.custom" type="warning" plain :loading="resetting" @click="resetLogo">恢复默认</el-button>
        </div>
        <span v-if="pendingFile" class="pending-file">待上传：{{ pendingFile.name }} · {{ formatBytes(pendingFile.size) }}</span>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { homeBrandingApi } from '@/modules/appearance/api'

type LogoInfo = {
  url: string
  custom: boolean
  mime_type: string
  size_bytes: number
  width: number
  height: number
  max_bytes: number
  updated_at?: string
}

const defaultMaxBytes = 2 * 1024 * 1024
const loading = ref(false)
const uploading = ref(false)
const resetting = ref(false)
const logoInput = ref<HTMLInputElement | null>(null)
const pendingFile = ref<File | null>(null)
const previewURL = ref('')
const logo = ref<LogoInfo>({
  url: '/api/v1/home/logo?v=default',
  custom: false,
  mime_type: 'image/png',
  size_bytes: 0,
  width: 0,
  height: 0,
  max_bytes: defaultMaxBytes,
})

const unwrap = (payload: any) => payload?.data || payload

const load = async () => {
  loading.value = true
  try {
    const config = unwrap(await homeBrandingApi.config()) || {}
    logo.value = { ...logo.value, ...(config.logo || {}) }
  } catch (error: any) {
    ElMessage.error(error?.msg || '加载系统 Logo 配置失败')
  } finally {
    loading.value = false
  }
}

const clearPending = () => {
  if (previewURL.value) URL.revokeObjectURL(previewURL.value)
  previewURL.value = ''
  pendingFile.value = null
  if (logoInput.value) logoInput.value.value = ''
}

const selectLogo = (event: Event) => {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  const maxBytes = logo.value.max_bytes || defaultMaxBytes
  if (!['image/png', 'image/jpeg'].includes(file.type)) {
    ElMessage.error('Logo 格式不受支持：请选择 PNG 或 JPEG 图片。')
    input.value = ''
    return
  }
  if (file.size > maxBytes) {
    ElMessage.error(`Logo 文件过大：单个文件最大 ${formatBytes(maxBytes)}，请压缩或缩小图片后重试。`)
    input.value = ''
    return
  }
  clearPending()
  pendingFile.value = file
  previewURL.value = URL.createObjectURL(file)
}

const uploadLogo = async () => {
  if (!pendingFile.value) return
  uploading.value = true
  try {
    logo.value = { ...logo.value, ...unwrap(await homeBrandingApi.uploadLogo(pendingFile.value)) }
    clearPending()
    ElMessage.success('系统 Logo 已替换，用户前台刷新后即可看到。')
  } catch (error: any) {
    ElMessage.error(error?.msg || 'Logo 上传失败，请检查格式和文件大小后重试。')
  } finally {
    uploading.value = false
  }
}

const resetLogo = async () => {
  try {
    await ElMessageBox.confirm('确定恢复 CampusOS 默认 Logo 吗？当前自定义 Logo 将被删除。', '恢复默认 Logo', {
      type: 'warning',
      confirmButtonText: '恢复默认',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  resetting.value = true
  try {
    logo.value = { ...logo.value, ...unwrap(await homeBrandingApi.resetLogo()) }
    clearPending()
    ElMessage.success('已恢复系统默认 Logo。')
  } catch (error: any) {
    ElMessage.error(error?.msg || '恢复默认 Logo 失败')
  } finally {
    resetting.value = false
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

onMounted(load)
onBeforeUnmount(() => {
  if (previewURL.value) URL.revokeObjectURL(previewURL.value)
})
</script>

<style scoped>
.branding-manager {
  display: grid;
  gap: 16px;
  padding: 18px;
  border: 1px solid #e4e7ed;
  background: #fff;
}
.section-heading,
.branding-workbench,
.control-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.section-heading {
  align-items: flex-start;
}
.section-heading h3,
.section-heading p,
.branding-controls p {
  margin: 0;
}
.section-heading > div > p:last-child,
.branding-controls p {
  margin-top: 7px;
  color: #606266;
  line-height: 1.6;
}
.eyebrow {
  margin-bottom: 5px !important;
  color: #0f766e;
  font-size: 12px;
  font-weight: 700;
  text-transform: uppercase;
}
.branding-workbench {
  align-items: stretch;
  justify-content: flex-start;
}
.logo-preview {
  width: min(420px, 42%);
  min-height: 138px;
  padding: 18px;
  display: grid;
  place-items: center;
  box-sizing: border-box;
  border: 1px solid #dcdfe6;
  border-radius: 6px;
  background-color: #f8fafc;
  background-image: linear-gradient(45deg, #eef1f5 25%, transparent 25%),
    linear-gradient(-45deg, #eef1f5 25%, transparent 25%), linear-gradient(45deg, transparent 75%, #eef1f5 75%),
    linear-gradient(-45deg, transparent 75%, #eef1f5 75%);
  background-position: 0 0, 0 8px, 8px -8px, -8px 0;
  background-size: 16px 16px;
}
.logo-preview img {
  width: 100%;
  max-height: 122px;
  object-fit: contain;
}
.logo-preview span,
.current-meta,
.pending-file {
  color: #909399;
  font-size: 13px;
}
.branding-controls {
  min-width: 0;
  flex: 1;
}
.control-row {
  justify-content: flex-start;
  flex-wrap: wrap;
  margin-top: 16px;
}
.pending-file {
  display: block;
  margin-top: 10px;
  overflow-wrap: anywhere;
}
.hidden-input {
  display: none;
}
@media (max-width: 760px) {
  .section-heading,
  .branding-workbench {
    align-items: stretch;
    flex-direction: column;
  }
  .logo-preview {
    width: 100%;
  }
  .control-row > * {
    flex: 1 1 auto;
  }
}
</style>
