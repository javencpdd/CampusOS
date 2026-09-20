<template>
  <section class="pdf-viewer" v-loading="loading">
    <header class="pdf-toolbar">
      <div>
        <strong>{{ attachment?.display_name || 'PDF 附件' }}</strong>
        <p>受当前文章、个人附件或个人文档的访问权限保护；预览不会生成公开链接。</p>
      </div>
      <div class="pdf-actions">
        <el-button size="small" :disabled="pageNumber <= 1" @click="goToPage(pageNumber - 1)">上一页</el-button>
        <el-input-number
          v-model="pageNumber"
          :min="1"
          :max="pageCount || 1"
          controls-position="right"
          size="small"
          @change="goToPage(pageNumber)"
        />
        <span>/ {{ pageCount || '-' }}</span>
        <el-button size="small" :disabled="pageCount === 0 || pageNumber >= pageCount" @click="goToPage(pageNumber + 1)"
          >下一页</el-button
        >
        <el-button size="small" @click="zoomOut">缩小</el-button>
        <span>{{ Math.round(scale * 100) }}%</span>
        <el-button size="small" @click="zoomIn">放大</el-button>
        <el-dropdown v-if="outlineItems.length" trigger="click" @command="goToPage">
          <el-button size="small">目录</el-button>
          <template #dropdown>
            <el-dropdown-menu class="pdf-outline-menu">
              <el-dropdown-item v-for="item in outlineItems" :key="`${item.page}:${item.title}`" :command="item.page">
                {{ item.title }} · 第 {{ item.page }} 页
              </el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
        <el-button size="small" @click="download">下载</el-button>
      </div>
    </header>
    <div class="pdf-search">
      <el-input v-model="searchQuery" clearable placeholder="搜索 PDF 文本" @keyup.enter="search" />
      <el-button :loading="searching" @click="search">搜索</el-button>
    </div>
    <el-alert v-if="error" type="warning" :closable="false" show-icon :title="error" />
    <div
      v-else-if="document"
      class="pdf-canvas-wrap"
      tabindex="0"
      @keydown.left.prevent="goToPage(pageNumber - 1)"
      @keydown.right.prevent="goToPage(pageNumber + 1)"
    >
      <canvas ref="canvas" class="pdf-canvas" />
    </div>
    <el-empty v-else-if="!loading" description="PDF 内容不可用，请下载后使用本地阅读器打开。" />
  </section>
</template>

<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { getDocument, GlobalWorkerOptions, type PDFDocumentProxy, type PDFPageProxy } from 'pdfjs-dist'
import pdfWorkerURL from 'pdfjs-dist/build/pdf.worker.min.mjs?url'
import { richTextApi } from '@/modules/richtext/api'
import { getAccessToken } from '@/modules/identity/session'
import { pluginCenterApi } from '@/modules/plugin-center/api'
import { ensurePDFViewerConsent } from '@/modules/pdf-viewer/authorization'

const props = defineProps<{ invocationId?: string }>()
const attachment = ref<any>(null)
const invocation = ref<any>(null)
const canvas = ref<HTMLCanvasElement | null>(null)
// PDF.js instances contain private fields and must not be wrapped in Vue proxies.
const document = shallowRef<PDFDocumentProxy | null>(null)
const loading = ref(false)
const error = ref('')
const pageNumber = ref(1)
const pageCount = ref(0)
const scale = ref(1.2)
const searchQuery = ref('')
const searching = ref(false)
const outlineItems = ref<Array<{ title: string; page: number }>>([])
let renderingTask: { cancel: () => void; promise: Promise<void> } | null = null
let loadingTask: ReturnType<typeof getDocument> | null = null
let loadRevision = 0
let renderRevision = 0
let readingPositionTimer: number | undefined
const readingPosition = ref<{ key: string; version: number } | null>(null)

GlobalWorkerOptions.workerSrc = pdfWorkerURL

const invocationID = () =>
  String(props.invocationId || new URLSearchParams(window.location.search).get('invocation') || '').trim()

const readingPositionKey = () => {
  const sourceID = String(attachment.value?.asset?.storage_object_id || '').trim()
  if (!sourceID) return ''
  const contextKind = String(invocation.value?.context_kind || '').trim()
  const kind =
    contextKind === 'article_attachment'
      ? 'a'
      : contextKind === 'personal_asset'
        ? 's'
        : contextKind === 'personal_document'
          ? 'd'
          : ''
  // `plugin_records` validates record keys. Use a stable object-version key,
  // not the short-lived invocation ID or a browser-local storage key.
  return kind ? `pdf-${kind}-${sourceID}` : ''
}

const unwrap = (payload: any) => payload?.data ?? payload
const isNotFound = (cause: any) => Number(cause?.response?.status || cause?.status || 0) === 404

const restoreReadingPosition = async () => {
  const key = readingPositionKey()
  if (!key) return
  try {
    // Reading position is optional personal plugin data. A declined/revoked
    // consent must not block authenticated PDF reading or fall back to browser
    // localStorage/IndexedDB.
    await ensurePDFViewerConsent('plugin_record.self.read')
    await ensurePDFViewerConsent('plugin_record.self.write')
    let record: any
    try {
      record = unwrap(await pluginCenterApi.getRecord('builtin.pdf-viewer', 'reading_positions', key))
    } catch (cause: any) {
      if (!isNotFound(cause)) throw cause
      // The first visit has no record yet. Keep the server-approved namespace
      // and create the record only after the reader actually changes page.
      readingPosition.value = { key, version: 0 }
      return
    }
    const saved = Number(record?.data?.page)
    if (Number.isInteger(saved) && saved >= 1 && saved <= pageCount.value) pageNumber.value = saved
    if (Number(record?.version) > 0) readingPosition.value = { key, version: Number(record.version) }
  } catch {
    readingPosition.value = null
  }
}

const persistReadingPosition = async () => {
  const state = readingPosition.value
  if (!state || !Number.isInteger(pageNumber.value) || pageNumber.value < 1) return
  try {
    const record =
      state.version > 0
        ? unwrap(
            await pluginCenterApi.updateRecord('builtin.pdf-viewer', 'reading_positions', state.key, {
              version: state.version,
              data: { page: pageNumber.value },
            }),
          )
        : unwrap(
            await pluginCenterApi.createRecord('builtin.pdf-viewer', 'reading_positions', {
              record_key: state.key,
              data: { page: pageNumber.value },
            }),
          )
    if (Number(record?.version) > 0) readingPosition.value = { ...state, version: Number(record.version) }
  } catch {
    // A revoked grant, stale optimistic version or a transient API error only
    // disables page memory; it must not affect the open PDF stream.
    readingPosition.value = null
  }
}

const loadOutline = async (pdf: any) => {
  outlineItems.value = []
  try {
    const roots = (await pdf.getOutline()) || []
    const flattened: Array<{ title: string; page: number }> = []
    const visit = async (items: any[], depth = 0): Promise<void> => {
      for (const item of items) {
        const destination = typeof item.dest === 'string' ? await pdf.getDestination(item.dest) : item.dest
        if (Array.isArray(destination) && destination[0]) {
          const pageIndex = await pdf.getPageIndex(destination[0])
          const title = String(item.title || '未命名目录').trim()
          flattened.push({ title: `${'　'.repeat(Math.min(depth, 3))}${title}`, page: pageIndex + 1 })
        }
        if (Array.isArray(item.items) && item.items.length && flattened.length < 100) await visit(item.items, depth + 1)
        if (flattened.length >= 100) return
      }
    }
    await visit(roots)
    if (pdf === document.value) outlineItems.value = flattened
  } catch {
    // A malformed or destination-less outline must not prevent safe reading.
    if (pdf === document.value) outlineItems.value = []
  }
}

const clearDocument = async () => {
  renderRevision += 1
  renderingTask?.cancel()
  renderingTask = null
  const pending = loadingTask
  loadingTask = null
  const current = document.value
  document.value = null
  pageCount.value = 0
  outlineItems.value = []
  if (pending) await pending.destroy()
  else if (current) await current.destroy()
}

const load = async () => {
  const revision = ++loadRevision
  await clearDocument()
  if (revision !== loadRevision) return
  error.value = ''
  attachment.value = null
  invocation.value = null
  readingPosition.value = null
  pageNumber.value = 1
  const id = invocationID()
  if (!id) {
    loading.value = false
    error.value = '缺少安全预览上下文。请返回文章详情或个人空间后重新点击“预览”。'
    return
  }
  loading.value = true
  try {
    const summary: any = await richTextApi.getPDFInvocation(id)
    if (revision !== loadRevision) return
    attachment.value = summary?.data?.attachment || summary?.attachment
    invocation.value = summary?.data?.invocation || summary?.invocation || null
    // PDF.js carries normal authenticated headers and therefore consumes the
    // API's Range/ETag path instead of receiving a public file URL.
    const token = getAccessToken()
    const task = getDocument({
      url: `/api/v1/plugin-ui/invocations/${encodeURIComponent(id)}/content`,
      httpHeaders: token ? { Authorization: `Bearer ${token}` } : undefined,
      withCredentials: true,
      rangeChunkSize: 64 * 1024,
      disableRange: false,
      disableStream: false,
      disableAutoFetch: false,
      isEvalSupported: false,
    })
    loadingTask = task
    const pdf = await task.promise
    if (revision !== loadRevision) return
    document.value = pdf
    pageCount.value = document.value.numPages
    await restoreReadingPosition()
    await loadOutline(document.value)
    if (revision !== loadRevision) return
    // The canvas is behind v-else-if and is absent until Vue flushes the DOM.
    await nextTick()
    await renderPage()
  } catch (cause: any) {
    if (revision !== loadRevision) return
    error.value = cause?.msg || 'PDF 预览暂不可用。你可以下载附件后使用本地阅读器打开。'
  } finally {
    if (revision === loadRevision) loading.value = false
  }
}

const renderPage = async () => {
  const revision = ++renderRevision
  const current = document.value
  if (!current || !canvas.value) return
  const safePage = Math.max(1, Math.min(pageNumber.value, current.numPages))
  pageNumber.value = safePage
  const previous = renderingTask
  previous?.cancel()
  if (previous) await previous.promise.catch(() => undefined)
  const page: PDFPageProxy = await current.getPage(safePage)
  if (revision !== renderRevision || current !== document.value || !canvas.value) return
  const viewport = page.getViewport({ scale: scale.value })
  const target = canvas.value
  const context = target.getContext('2d')
  if (!context) return
  const pixelRatio = Math.min(window.devicePixelRatio || 1, 2)
  target.width = Math.floor(viewport.width * pixelRatio)
  target.height = Math.floor(viewport.height * pixelRatio)
  target.style.width = `${Math.floor(viewport.width)}px`
  target.style.height = `${Math.floor(viewport.height)}px`
  renderingTask = page.render({
    canvasContext: context,
    viewport,
    transform: pixelRatio === 1 ? undefined : [pixelRatio, 0, 0, pixelRatio, 0, 0],
  })
  try {
    await renderingTask.promise
  } catch (cause: any) {
    if (cause?.name !== 'RenderingCancelledException') throw cause
  }
}

const goToPage = async (requested: number | null | undefined) => {
  if (!document.value || !requested) return
  pageNumber.value = Math.max(1, Math.min(Number(requested), pageCount.value))
  await nextTick()
  await renderPage()
}

const zoomIn = async () => {
  scale.value = Math.min(3, Number((scale.value + 0.2).toFixed(2)))
  await renderPage()
}
const zoomOut = async () => {
  scale.value = Math.max(0.6, Number((scale.value - 0.2).toFixed(2)))
  await renderPage()
}

const search = async () => {
  const query = searchQuery.value.trim().toLocaleLowerCase()
  if (!query || !document.value) return
  searching.value = true
  try {
    for (let index = 1; index <= document.value.numPages; index += 1) {
      const page = await document.value.getPage(index)
      const text = await page.getTextContent()
      const value = text.items
        .map((item: any) => item.str || '')
        .join(' ')
        .toLocaleLowerCase()
      if (value.includes(query)) {
        await goToPage(index)
        ElMessage.success(`已定位到第 ${index} 页`)
        return
      }
    }
    ElMessage.info('未找到匹配文本')
  } catch {
    ElMessage.warning('PDF 文本搜索失败，请尝试更短的关键词。')
  } finally {
    searching.value = false
  }
}

const download = async () => {
  const id = invocationID()
  if (!id) return
  try {
    const result: any = await richTextApi.downloadPDFInvocation(id)
    const blob = result?.data || result
    if (!(blob instanceof Blob)) throw new Error('下载内容不可用')
    const url = URL.createObjectURL(blob)
    const link = window.document.createElement('a')
    link.href = url
    link.download = attachment.value?.display_name || 'attachment.pdf'
    link.click()
    window.setTimeout(() => URL.revokeObjectURL(url), 1000)
    ElMessage.success('已开始下载 PDF')
  } catch (cause: any) {
    ElMessage.warning(cause?.msg || 'PDF 下载失败，请返回文章详情或个人空间后重试。')
  }
}

onMounted(load)
watch(() => props.invocationId, load)
watch(pageNumber, (value) => {
  if (!readingPosition.value || !Number.isInteger(value) || value < 1) return
  if (readingPositionTimer) window.clearTimeout(readingPositionTimer)
  readingPositionTimer = window.setTimeout(() => void persistReadingPosition(), 250)
})
onBeforeUnmount(() => {
  loadRevision += 1
  if (readingPositionTimer) window.clearTimeout(readingPositionTimer)
  void clearDocument()
})
</script>

<style scoped>
.pdf-viewer {
  min-height: 58vh;
  display: grid;
  gap: 12px;
}
.pdf-toolbar,
.pdf-actions,
.pdf-search {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.pdf-toolbar {
  justify-content: space-between;
  gap: 16px;
}
.pdf-toolbar p {
  margin: 4px 0 0;
  color: var(--campus-muted-color, #606266);
  font-size: 12px;
}
.pdf-actions .el-input-number {
  width: 106px;
}
.pdf-search .el-input {
  width: min(360px, 100%);
}
.pdf-canvas-wrap {
  min-height: 68vh;
  overflow: auto;
  display: grid;
  place-items: start center;
  padding: 20px;
  border: 1px solid var(--el-border-color);
  border-radius: 6px;
  outline: none;
  background: #525659;
}
.pdf-canvas {
  background: #fff;
  box-shadow: 0 2px 12px rgb(0 0 0 / 35%);
}
.pdf-outline-menu {
  max-width: min(84vw, 420px);
}
</style>
