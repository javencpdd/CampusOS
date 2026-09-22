<template>
  <section class="documents-view" v-loading="loading">
    <el-card shadow="never">
      <template #header>
        <div class="header">
          <div>
            <h2>我的文档</h2>
            <span>文档仅自己可见；每次保存都会生成可恢复的新版本。文章附件与图片资源会在各自的只读资源页展示。</span>
          </div>
          <el-button v-if="activePane === 'documents'" type="primary" @click="createDocument">新建文本</el-button>
        </div>
      </template>

      <el-tabs v-model="activePane" class="documents-tabs">
        <el-tab-pane label="我的文档" name="documents">
          <div class="toolbar">
            <el-button @click="chooseUpload">上传文件</el-button>
            <el-button :type="status === 'active' ? 'primary' : 'default'" plain @click="load('active')"
              >我的文档</el-button
            >
            <el-button :type="status === 'trashed' ? 'primary' : 'default'" plain @click="load('trashed')"
              >回收站</el-button
            >
            <input
              ref="uploadInput"
              class="hidden"
              type="file"
              accept=".txt,.md,.markdown,.campusdoc,.json,.pdf,.docx"
              @change="upload"
            />
          </div>
          <el-alert
            type="info"
            :closable="false"
            show-icon
            title="文本、Markdown 和 CampusDoc 支持在线编辑；PDF 可通过第一方 PDF 阅读器在线预览，DOCX 保持私有下载，不在服务器中转换。"
          />
          <el-empty v-if="!items.length" description="暂时没有文档" />
          <el-table v-else :data="items" @row-click="openDocument">
            <el-table-column prop="name" label="名称" min-width="180" />
            <el-table-column prop="format" label="格式" width="100" />
            <el-table-column label="版本" width="90">
              <template #default="{ row }">v{{ row.current_version?.version_number || 0 }}</template>
            </el-table-column>
            <el-table-column prop="updated_at" label="更新时间" min-width="160" />
            <el-table-column label="操作" width="200">
              <template #default="{ row }">
                <el-button v-if="row.format === 'pdf'" link type="primary" @click.stop="previewPDF(row)"
                  >预览</el-button
                >
                <el-button link type="primary" @click.stop="download(row)">下载</el-button>
                <el-button v-if="row.status === 'active'" link type="danger" @click.stop="changeStatus(row, true)"
                  >移入回收站</el-button
                >
                <el-button v-else link type="success" @click.stop="changeStatus(row, false)">恢复</el-button>
              </template>
            </el-table-column>
          </el-table>
        </el-tab-pane>

        <el-tab-pane label="文章附件" name="attachments">
          <div class="media-panel" v-loading="articleAttachmentLoading">
            <el-alert
              type="info"
              :closable="false"
              show-icon
              title="这里汇总你上传或从个人文档添加到图文文章的附件。它们属于个人空间，仅你可在此下载或预览；已被文章引用的文件不能移入回收站。"
            />
            <div class="toolbar">
              <el-button
                :type="articleAttachmentStatus === 'active' ? 'primary' : 'default'"
                plain
                @click="loadArticleAttachments('active')"
                >我的附件</el-button
              >
              <el-button
                :type="articleAttachmentStatus === 'trashed' ? 'primary' : 'default'"
                plain
                @click="loadArticleAttachments('trashed')"
                >回收站</el-button
              >
            </div>
            <el-empty
              v-if="!articleAttachmentLoading && articleAttachments.length === 0"
              :description="articleAttachmentStatus === 'trashed' ? '附件回收站为空。' : '暂时没有文章附件。'"
            />
            <el-table v-else :data="articleAttachments">
              <el-table-column prop="original_name" label="文件名" min-width="220" />
              <el-table-column label="类型 / 大小" min-width="180">
                <template #default="{ row }"
                  >{{ row.mime_type || '未知类型' }} · {{ formatBytes(Number(row.size_bytes || 0)) }}</template
                >
              </el-table-column>
              <el-table-column prop="updated_at" label="更新时间" min-width="170" />
              <el-table-column label="操作" width="220">
                <template #default="{ row }">
                  <el-button v-if="isPDFAsset(row)" link type="primary" @click="previewArticleAttachmentPDF(row)"
                    >预览</el-button
                  >
                  <el-button link type="primary" @click="downloadArticleAttachment(row)">下载</el-button>
                  <el-button
                    v-if="row.status === 'active'"
                    link
                    type="danger"
                    @click="changeArticleAttachmentStatus(row, true)"
                    >移入回收站</el-button
                  >
                  <el-button v-else link type="success" @click="changeArticleAttachmentStatus(row, false)"
                    >恢复</el-button
                  >
                </template>
              </el-table-column>
            </el-table>
            <el-button
              v-if="articleAttachmentNextCursor"
              :loading="articleAttachmentLoading"
              @click="loadArticleAttachments(articleAttachmentStatus, true)"
              >加载更多附件</el-button
            >
          </div>
        </el-tab-pane>

        <el-tab-pane label="已上传资源" name="media">
          <div class="media-panel" v-loading="mediaLoading">
            <el-alert
              type="info"
              :closable="false"
              show-icon
              title="这里汇总头像历史、普通帖子正文图片和图文文章图片。它们仍会计入个人空间，但可能已被帖子引用，因此本页只提供预览，不提供删除或移动。"
            />
            <el-alert
              v-if="mediaLoadNotice"
              class="media-notice"
              type="warning"
              :closable="false"
              show-icon
              :title="mediaLoadNotice"
            />
            <el-empty v-if="!mediaLoading && !mediaItems.length" description="暂时没有已上传的图片资源" />
            <el-table v-else :data="mediaItems" class="media-table">
              <el-table-column label="预览" width="92">
                <template #default="{ row }">
                  <el-image
                    class="media-preview"
                    :src="row.url"
                    :preview-src-list="[row.url]"
                    fit="cover"
                    preview-teleported
                  >
                    <template #error><span class="media-preview-fallback">图片不可预览</span></template>
                  </el-image>
                </template>
              </el-table-column>
              <el-table-column label="来源" width="140">
                <template #default="{ row }">
                  <el-tag :type="row.source === '头像历史' ? 'success' : 'info'" effect="plain">{{
                    row.source
                  }}</el-tag>
                  <span v-if="row.active" class="active-avatar">当前头像</span>
                </template>
              </el-table-column>
              <el-table-column label="文件" min-width="190">
                <template #default="{ row }">
                  <span class="file-name" :title="row.fileName">{{ row.fileName }}</span>
                  <small>{{ row.mimeType || '图片' }}</small>
                </template>
              </el-table-column>
              <el-table-column label="大小 / 尺寸" min-width="150">
                <template #default="{ row }">
                  {{ formatBytes(row.fileSize) }}
                  <small v-if="row.width && row.height">{{ row.width }} × {{ row.height }}</small>
                  <small v-else>尺寸未记录</small>
                </template>
              </el-table-column>
              <el-table-column label="上传时间" min-width="170">
                <template #default="{ row }">{{ formatDate(row.createdAt) }}</template>
              </el-table-column>
              <el-table-column label="操作" width="110">
                <template #default="{ row }">
                  <el-button v-if="row.source === '头像历史'" link type="primary" @click="goSpaceSettings"
                    >管理头像</el-button
                  >
                  <span v-else class="readonly-label">仅预览</span>
                </template>
              </el-table-column>
            </el-table>
            <p v-if="mediaItems.length" class="media-hint">
              为避免一次加载过多历史资源，本页每种图片来源最多展示最近 200 项。
            </p>
          </div>
        </el-tab-pane>
      </el-tabs>
    </el-card>

    <el-dialog v-model="editorVisible" :title="editor.name || '文档'" width="min(760px, 94vw)">
      <template v-if="editable">
        <el-input v-model="editor.name" maxlength="255" aria-label="文档名称" />
        <DocumentContentEditor
          :model-value="editor.content || ''"
          :format="editor.format"
          :allow-format-change="!editor.id"
          @update:model-value="editor.content = $event"
          @update:format="changeEditorFormat"
        />
        <div class="editor-actions">
          <el-button v-if="editor.id" @click="preview">安全预览</el-button>
          <el-button v-if="editor.id" @click="showVersions">版本历史</el-button>
        </div>
      </template>
      <el-alert v-else type="info" :closable="false" title="该格式不可在线编辑，请下载后在本地打开。" />
      <template #footer>
        <el-button @click="editorVisible = false">关闭</el-button>
        <el-button v-if="editable" type="primary" :loading="saving" @click="save">保存为新版本</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="versionsVisible" title="版本历史" width="min(620px, 94vw)">
      <el-table :data="versions">
        <el-table-column prop="version_number" label="版本" />
        <el-table-column prop="created_at" label="创建时间" />
        <el-table-column label="操作">
          <template #default="{ row }"
            ><el-button link type="primary" @click="restoreVersion(row)">恢复为新版本</el-button></template
          >
        </el-table-column>
      </el-table>
    </el-dialog>
    <el-drawer v-model="previewVisible" title="文档安全预览" size="min(720px, 94vw)">
      <!-- previewHTML is generated and sanitized by the API Content Editor Core; never assign editor input directly. -->
      <article class="document-preview" v-html="previewHTML" />
    </el-drawer>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { contentApi } from '@/modules/community/content'
import { richTextApi } from '@/modules/richtext/api'
import { spaceApi } from '@/modules/space/api'
import { personalDocumentsApi } from '../api'
import DocumentContentEditor from '@/modules/content-editor/components/DocumentContentEditor.vue'
import { openPluginSurface, type SurfacePresentation } from '@/campus-ui/surfaceHost'
import { ensurePluginCapabilityConsent } from '@/campus-ui/pluginConsent'
import {
  defaultDocumentContent,
  documentNameForFormat,
  isEditableDocumentFormat,
  type DocumentFormat,
} from '@/modules/content-editor/document'

type MediaItem = {
  source: '头像历史' | '帖子正文图片' | '图文文章图片'
  url: string
  fileName: string
  fileSize: number
  mimeType?: string
  width?: number
  height?: number
  createdAt?: string
  active?: boolean
}

const router = useRouter()
const loading = ref(false)
const saving = ref(false)
const items = ref<any[]>([])
const status = ref('active')
const uploadInput = ref<HTMLInputElement>()
const editorVisible = ref(false)
const versionsVisible = ref(false)
const versions = ref<any[]>([])
const previewVisible = ref(false)
const previewHTML = ref('')
const editor = ref<any>({})
const activePane = ref<'documents' | 'attachments' | 'media'>('documents')
const mediaLoading = ref(false)
const mediaLoaded = ref(false)
const mediaLoadNotice = ref('')
const avatars = ref<any[]>([])
const contentImages = ref<any[]>([])
const richTextImages = ref<any[]>([])
const articleAttachmentLoading = ref(false)
const articleAttachmentStatus = ref<'active' | 'trashed'>('active')
const articleAttachments = ref<any[]>([])
const articleAttachmentNextCursor = ref('')
const editable = computed(() => isEditableDocumentFormat(editor.value.format))
const data = (response: any) => response?.data?.data ?? response?.data ?? response

const mediaItems = computed<MediaItem[]>(() => {
  const result: MediaItem[] = [
    ...avatars.value.map((item) => ({
      source: '头像历史' as const,
      url: item.url,
      fileName: item.file_name || '头像图片',
      fileSize: Number(item.size || 0),
      mimeType: '头像图片',
      createdAt: item.uploaded_at,
      active: Boolean(item.active),
    })),
    ...contentImages.value.map((item) => ({
      source: '帖子正文图片' as const,
      url: item.file_url,
      fileName: item.file_name || '正文图片',
      fileSize: Number(item.file_size || 0),
      mimeType: item.mime_type,
      width: item.width,
      height: item.height,
      createdAt: item.created_at,
    })),
    ...richTextImages.value.map((item) => ({
      source: '图文文章图片' as const,
      url: item.file_url,
      fileName: item.file_name || '文章图片',
      fileSize: Number(item.file_size || 0),
      mimeType: item.mime_type,
      width: item.width,
      height: item.height,
      createdAt: item.created_at,
    })),
  ].filter((item) => Boolean(item.url))
  return result.sort((left, right) => Date.parse(right.createdAt || '') - Date.parse(left.createdAt || ''))
})

async function load(next = status.value) {
  status.value = next
  loading.value = true
  try {
    items.value = data(await personalDocumentsApi.list(next)).items || []
  } catch (error: any) {
    ElMessage.error(error?.msg || '加载文档失败')
  } finally {
    loading.value = false
  }
}

async function loadMedia(force = false) {
  if (mediaLoading.value || (mediaLoaded.value && !force)) return
  mediaLoading.value = true
  mediaLoadNotice.value = ''
  const results = await Promise.allSettled([spaceApi.avatars(), contentApi.listMyImages(), richTextApi.listMyAssets()])
  const [avatarResult, contentResult, richTextResult] = results
  let unavailableSources = 0
  if (avatarResult.status === 'fulfilled') avatars.value = data(avatarResult.value).items || []
  else unavailableSources++
  if (contentResult.status === 'fulfilled') contentImages.value = data(contentResult.value).items || []
  else unavailableSources++
  if (richTextResult.status === 'fulfilled') richTextImages.value = data(richTextResult.value).items || []
  else unavailableSources++
  // RichText may be disabled by the administrator. Its absence must not hide
  // avatar or normal-post images, so show one concise partial-result notice.
  if (unavailableSources > 0)
    mediaLoadNotice.value = '部分图片资源暂时无法加载，可能是对应功能未启用或服务正在恢复；请稍后刷新重试。'
  mediaLoaded.value = true
  mediaLoading.value = false
}

async function loadArticleAttachments(next = articleAttachmentStatus.value, append = false) {
  if (append && (!articleAttachmentNextCursor.value || articleAttachmentLoading.value)) return
  articleAttachmentStatus.value = next
  articleAttachmentLoading.value = true
  if (!append) {
    articleAttachments.value = []
    articleAttachmentNextCursor.value = ''
  }
  try {
    const result = data(await richTextApi.listUserAssets(next, append ? articleAttachmentNextCursor.value : ''))
    const items = (result?.items || []).filter((asset: any) => asset.kind === 'article_attachment')
    articleAttachments.value = append ? [...articleAttachments.value, ...items] : items
    articleAttachmentNextCursor.value = result?.next_cursor || ''
  } catch (error: any) {
    ElMessage.error(error?.msg || '加载文章附件失败')
  } finally {
    articleAttachmentLoading.value = false
  }
}

function createDocument() {
  editor.value = { name: '未命名文档.txt', format: 'text', content: defaultDocumentContent('text'), version: 0, id: '' }
  editorVisible.value = true
}

async function openDocument(row: any) {
  if (row.format === 'pdf') {
    await previewPDF(row)
    return
  }
  editor.value = { ...row, content: '' }
  editorVisible.value = true
  if (['text', 'markdown', 'campusdoc'].includes(row.format)) {
    try {
      const result = data(await personalDocumentsApi.content(row.id))
      editor.value = { ...result.document, content: result.content }
    } catch (error: any) {
      ElMessage.error(error?.response?.data?.msg || error?.msg || '读取文档失败')
    }
  }
}

function changeEditorFormat(format: DocumentFormat) {
  editor.value.format = format
  editor.value.name = documentNameForFormat(editor.value.name || '未命名文档', format)
  if (!String(editor.value.content || '').trim()) editor.value.content = defaultDocumentContent(format)
}

async function save() {
  saving.value = true
  try {
    if (!editor.value.id) {
      await personalDocumentsApi.create({
        name: editor.value.name,
        format: editor.value.format,
        content: editor.value.content,
      })
      ElMessage.success('文档已创建')
    } else {
      const result = data(
        await personalDocumentsApi.save(editor.value.id, {
          expected_version: editor.value.version,
          name: editor.value.name,
          content: editor.value.content,
        }),
      )
      editor.value = result
      ElMessage.success('已保存为新版本')
    }
    editorVisible.value = false
    await load()
  } catch (error: any) {
    ElMessage.error(error?.response?.data?.msg || error?.msg || '保存失败')
  } finally {
    saving.value = false
  }
}

function chooseUpload() {
  uploadInput.value?.click()
}

async function upload(event: Event) {
  const file = (event.target as HTMLInputElement).files?.[0]
  if (!file) return
  try {
    await personalDocumentsApi.upload(file)
    ElMessage.success('文件已上传')
    await load()
  } catch (error: any) {
    ElMessage.error(error?.response?.data?.msg || error?.msg || '上传失败')
  } finally {
    if (uploadInput.value) uploadInput.value.value = ''
  }
}

async function download(row: any) {
  try {
    const result: any = await personalDocumentsApi.download(row.id)
    const blob = result?.data || result
    if (!(blob instanceof Blob)) throw new Error('下载内容不可用')
    const url = URL.createObjectURL(blob)
    const link = window.document.createElement('a')
    link.href = url
    link.download = row.name || 'document'
    link.click()
    window.setTimeout(() => URL.revokeObjectURL(url), 1000)
    ElMessage.success('已开始下载个人文档')
  } catch (error: any) {
    ElMessage.error(error?.msg || error?.message || '个人文档下载失败，请刷新后重试')
  }
}

async function downloadArticleAttachment(row: any) {
  try {
    const result: any = await richTextApi.downloadUserAsset(row.id)
    const blob = result?.data || result
    if (!(blob instanceof Blob)) throw new Error('下载内容不可用')
    const url = URL.createObjectURL(blob)
    const link = window.document.createElement('a')
    link.href = url
    link.download = row.original_name || 'attachment'
    link.click()
    window.setTimeout(() => URL.revokeObjectURL(url), 1000)
    ElMessage.success('已开始下载个人文章附件')
  } catch (error: any) {
    ElMessage.error(error?.msg || error?.message || '文章附件下载失败，请刷新后重试')
  }
}

function isPDFAsset(row: any) {
  return (
    row?.mime_type === 'application/pdf' ||
    String(row?.original_name || '')
      .toLowerCase()
      .endsWith('.pdf')
  )
}

async function previewArticleAttachmentPDF(row: any) {
  const presentation: SurfacePresentation = 'modal'
  try {
    await ensurePluginCapabilityConsent('campusos.pdf-viewer', 'personal_space_file.self.read')
    await ensurePluginCapabilityConsent('campusos.pdf-viewer', 'plugin_ui.surface.open')
    const result: any = await richTextApi.createPersonalAssetPDFInvocation(row.id, presentation)
    const invocation = data(result)
    if (!invocation?.id) throw new Error('服务器未返回有效的 PDF 预览上下文')
    await openPluginSurface({
      surfaceID: 'campusos.pdf-viewer.preview',
      invocationID: invocation.id,
      presentation,
    })
  } catch (error: any) {
    if (error === 'cancel' || error === 'close') return
    ElMessage.warning(error?.msg || error?.message || 'PDF 预览暂不可用，请下载后使用本地阅读器打开。')
  }
}

async function changeArticleAttachmentStatus(row: any, trash: boolean) {
  try {
    await ElMessageBox.confirm(
      trash ? '未被文章引用的附件会移入回收站，可稍后恢复。' : '恢复该文章附件？',
      trash ? '移入回收站' : '恢复文章附件',
      { type: 'warning' },
    )
    await (trash ? richTextApi.trashUserAsset(row.id) : richTextApi.restoreUserAsset(row.id))
    await loadArticleAttachments()
    ElMessage.success('操作成功')
  } catch (error: any) {
    if (error !== 'cancel' && error !== 'close') ElMessage.error(error?.msg || '操作失败')
  }
}

async function previewPDF(row: any) {
  const presentation: SurfacePresentation = 'modal'
  try {
    await ensurePluginCapabilityConsent('campusos.pdf-viewer', 'personal_space_file.self.read')
    await ensurePluginCapabilityConsent('campusos.pdf-viewer', 'plugin_ui.surface.open')
    const result: any = await personalDocumentsApi.createPDFInvocation(row.id, presentation)
    const invocation = data(result)
    if (!invocation?.id) throw new Error('服务器未返回有效的 PDF 预览上下文')
    await openPluginSurface({
      surfaceID: 'campusos.pdf-viewer.preview',
      invocationID: invocation.id,
      presentation,
    })
  } catch (error: any) {
    if (error === 'cancel' || error === 'close') return
    ElMessage.warning(error?.msg || error?.message || 'PDF 预览暂不可用，请下载后使用本地阅读器打开。')
  }
}

async function changeStatus(row: any, trash: boolean) {
  try {
    await ElMessageBox.confirm(
      trash ? '文档和全部历史版本都会保留，可在回收站恢复。' : '恢复该文档？',
      trash ? '移入回收站' : '恢复文档',
      { type: 'warning' },
    )
    await (trash ? personalDocumentsApi.trash(row.id, row.version) : personalDocumentsApi.restore(row.id, row.version))
    await load()
    ElMessage.success('操作成功')
  } catch (error: any) {
    if (error !== 'cancel' && error !== 'close') ElMessage.error(error?.response?.data?.msg || error?.msg || '操作失败')
  }
}

async function showVersions() {
  try {
    versions.value = data(await personalDocumentsApi.versions(editor.value.id)).items || []
    versionsVisible.value = true
  } catch (error: any) {
    ElMessage.error(error?.response?.data?.msg || error?.msg || '读取版本失败')
  }
}

async function restoreVersion(row: any) {
  try {
    const result = data(await personalDocumentsApi.restoreVersion(editor.value.id, row.id, editor.value.version))
    editor.value = result
    versionsVisible.value = false
    ElMessage.success('已恢复为新版本')
    await load()
  } catch (error: any) {
    ElMessage.error(error?.response?.data?.msg || error?.msg || '恢复失败')
  }
}

async function preview() {
  try {
    const result = data(await personalDocumentsApi.preview(editor.value.id))
    if (result?.status !== 'native' || !result?.rendered_html) {
      ElMessage.info(result?.message || '该文档当前不能在线预览，请下载后查看')
      return
    }
    previewHTML.value = result.rendered_html
    previewVisible.value = true
  } catch (error: any) {
    ElMessage.error(error?.response?.data?.msg || error?.msg || '预览失败')
  }
}

function formatBytes(value: number) {
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

function formatDate(value?: string) {
  if (!value) return '未记录'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '未记录' : date.toLocaleString('zh-CN', { hour12: false })
}

function goSpaceSettings() {
  void router.push('/space/settings')
}

watch(activePane, (next) => {
  if (next === 'media') void loadMedia()
  if (next === 'attachments') void loadArticleAttachments()
})
onMounted(() => load())
</script>

<style scoped>
.documents-view {
  max-width: 1200px;
  margin: 0 auto;
  padding: 20px;
}
.header {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  align-items: center;
}
.header h2 {
  margin: 0 0 6px;
}
.header span {
  color: var(--el-text-color-secondary);
  font-size: 13px;
}
.toolbar {
  display: flex;
  gap: 8px;
  margin-bottom: 14px;
  flex-wrap: wrap;
}
.hidden {
  display: none;
}
.editor-actions {
  display: flex;
  gap: 8px;
}
.document-preview {
  font-size: 16px;
  line-height: 1.8;
  overflow-wrap: anywhere;
}
.document-preview :deep(pre) {
  overflow: auto;
  padding: 12px;
  background: var(--el-fill-color-light);
}
.document-preview :deep(table) {
  max-width: 100%;
  border-collapse: collapse;
}
.document-preview :deep(td) {
  padding: 6px;
  border: 1px solid var(--el-border-color);
}
.media-panel {
  min-height: 180px;
}
.media-notice {
  margin-top: 12px;
}
.media-table {
  margin-top: 14px;
}
.media-preview {
  width: 58px;
  height: 46px;
  border: 1px solid var(--el-border-color);
  border-radius: 5px;
}
.media-preview-fallback {
  display: grid;
  width: 100%;
  height: 100%;
  place-items: center;
  color: var(--el-text-color-secondary);
  font-size: 11px;
  text-align: center;
}
.file-name {
  display: block;
  max-width: 230px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.media-table small {
  display: block;
  color: var(--el-text-color-secondary);
  margin-top: 3px;
}
.active-avatar {
  display: block;
  color: var(--el-color-success);
  font-size: 12px;
  margin-top: 4px;
}
.readonly-label {
  color: var(--el-text-color-secondary);
  font-size: 13px;
}
.media-hint {
  margin: 12px 0 0;
  color: var(--el-text-color-secondary);
  font-size: 13px;
}
@media (max-width: 600px) {
  .documents-view {
    padding: 10px;
  }
  .header {
    align-items: flex-start;
  }
  .header span {
    display: none;
  }
}
</style>
