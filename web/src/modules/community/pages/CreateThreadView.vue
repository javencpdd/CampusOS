<template>
  <div ref="createThreadRoot" class="create-thread">
    <el-card class="editor-card" shadow="never">
      <template #header>
        <div class="editor-header">
          <h2>{{ editorTitle }}</h2>
          <div v-if="!isEditMode" class="publish-mode-picker">
            <span class="publish-mode-label">发布类型</span>
            <el-select
              v-if="isCompact"
              :model-value="publishMode"
              class="publish-mode-select"
              aria-label="选择帖子发布类型"
              @change="selectPublishMode"
            >
              <el-option
                v-for="option in publishOptions"
                :key="option.value"
                :label="option.label"
                :value="option.value"
                :disabled="option.disabled"
              />
            </el-select>
            <el-segmented
              v-else
              :model-value="publishMode"
              :options="publishOptions"
              aria-label="选择帖子发布类型"
              @change="selectPublishMode"
            />
          </div>
        </div>
      </template>

      <el-form
        v-if="templateMode === 'plain_text'"
        :model="plainForm"
        @submit.prevent="submitPlain"
        label-position="top"
      >
        <el-form-item label="标题" required>
          <el-input v-model="plainForm.title" placeholder="请输入帖子标题" maxlength="255" show-word-limit />
        </el-form-item>
        <el-form-item label="版块" required>
          <el-select
            v-model="plainForm.category_id"
            :loading="categoryLoading"
            filterable
            :disabled="isEditMode"
            placeholder="请选择版块"
            class="field-full"
          >
            <el-option v-for="category in categories" :key="category.id" :label="category.name" :value="category.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="内容" required>
          <el-input v-model="plainForm.content" type="textarea" :rows="10" placeholder="请输入帖子内容" />
        </el-form-item>
        <el-form-item label="标签">
          <el-select
            v-model="plainForm.tags"
            multiple
            filterable
            allow-create
            placeholder="输入标签后回车"
            class="field-full"
          >
            <el-option v-for="tag in currentPlainCategory?.default_tags || []" :key="tag" :label="tag" :value="tag" />
          </el-select>
        </el-form-item>
        <el-form-item label="可见性">
          <el-switch v-model="plainForm.is_private" active-text="私密，仅自己可见" inactive-text="公开发布" />
        </el-form-item>
        <div class="editor-actions">
          <el-button type="primary" @click="submitPlain" :loading="loading">{{
            isEditMode ? '保存修改' : '发布帖子'
          }}</el-button>
          <el-button @click="$router.back()">取消</el-button>
        </div>
      </el-form>

      <el-form v-else :model="articleForm" @submit.prevent="publishArticle" label-position="top">
        <el-form-item label="标题" required>
          <el-input v-model="articleForm.title" placeholder="请输入文章标题" maxlength="255" show-word-limit />
        </el-form-item>
        <el-row :gutter="14">
          <el-col :xs="24" :md="12">
            <el-form-item label="版块" required>
              <el-select
                v-model="articleForm.category_id"
                :loading="categoryLoading"
                filterable
                :disabled="isEditMode"
                placeholder="请选择版块"
                class="field-full"
              >
                <el-option
                  v-for="category in categories"
                  :key="category.id"
                  :label="category.name"
                  :value="category.id"
                />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :xs="24" :md="12">
            <el-form-item label="标签">
              <el-select
                v-model="articleForm.tags"
                multiple
                filterable
                allow-create
                :disabled="isEditMode"
                placeholder="输入标签后回车"
                class="field-full"
              >
                <el-option
                  v-for="tag in currentArticleCategory?.default_tags || []"
                  :key="tag"
                  :label="tag"
                  :value="tag"
                />
              </el-select>
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item label="摘要">
          <el-input
            v-model="articleForm.summary"
            type="textarea"
            :rows="3"
            maxlength="500"
            show-word-limit
            placeholder="用于列表和详情页的文章摘要"
          />
        </el-form-item>
        <el-form-item label="封面图">
          <div class="cover-row">
            <el-input v-model="articleForm.cover_url" placeholder="https://example.com/cover.jpg 或上传站内图片" />
            <input
              ref="coverInput"
              class="hidden-input"
              type="file"
              accept="image/png,image/jpeg,image/gif,image/webp"
              @change="uploadCover"
            />
            <el-button @click="chooseCover" :loading="assetUploading">上传封面</el-button>
          </div>
        </el-form-item>
        <el-form-item label="正文 HTML" required>
          <div class="body-toolbar">
            <el-button size="small" @click="insertSnippet('<h2>小标题</h2>')">H2</el-button>
            <el-button size="small" @click="insertSnippet('<p>段落内容</p>')">段落</el-button>
            <el-button size="small" @click="insertSnippet('<blockquote>引用内容</blockquote>')">引用</el-button>
            <input
              ref="bodyImageInput"
              class="hidden-input"
              type="file"
              accept="image/png,image/jpeg,image/gif,image/webp"
              @change="uploadBodyImage"
            />
            <el-button size="small" @click="chooseBodyImage" :loading="assetUploading">插入图片</el-button>
          </div>
          <el-input
            v-model="articleForm.content_html"
            type="textarea"
            :rows="14"
            class="html-editor"
            spellcheck="false"
            placeholder="<p>从这里开始写文章正文...</p>"
          />
        </el-form-item>
        <el-form-item label="文章附件">
          <div class="attachment-editor">
            <p class="attachment-hint">
              附件不会插入正文排版。支持 PDF、MP3、MP4、DOC/DOCX、XLS/XLSX、ZIP、RAR；单个不超过 20 MiB，每篇最多 10
              个且总计不超过 40 MiB。
            </p>
            <div class="body-toolbar">
              <input
                ref="attachmentInput"
                class="hidden-input"
                type="file"
                accept=".pdf,.mp3,.mp4,.doc,.docx,.xls,.xlsx,.zip,.rar"
                @change="uploadAttachment"
              />
              <el-button size="small" :loading="attachmentUploading" @click="chooseAttachment">上传附件</el-button>
              <el-button size="small" @click="openAssetPicker">从我的附件添加</el-button>
            </div>
            <el-empty v-if="attachments.length === 0" :image-size="42" description="尚未添加附件" />
            <div v-for="(attachment, index) in attachments" :key="attachment.id" class="attachment-editor-row">
              <div class="attachment-editor-name">
                <el-input
                  v-model="attachment.display_name"
                  size="small"
                  maxlength="255"
                  aria-label="附件显示名称"
                  @change="renameAttachment(attachment)"
                />
                <span
                  >{{ attachment.asset?.mime_type }} · {{ formatAttachmentSize(attachment.asset?.size_bytes) }}</span
                >
              </div>
              <div class="attachment-editor-actions">
                <el-button text size="small" :disabled="index === 0" @click="moveAttachment(index, -1)">上移</el-button>
                <el-button
                  text
                  size="small"
                  :disabled="index === attachments.length - 1"
                  @click="moveAttachment(index, 1)"
                  >下移</el-button
                >
                <el-button text type="danger" size="small" @click="removeAttachment(attachment.id)">移除</el-button>
              </div>
            </div>
          </div>
        </el-form-item>
        <div class="editor-actions">
          <el-button @click="saveDraft" :loading="savingDraft">保存草稿</el-button>
          <el-button @click="previewArticle" :loading="previewing">预览</el-button>
          <el-button type="primary" @click="publishArticle" :loading="publishing">发布文章</el-button>
          <el-button @click="$router.back()">取消</el-button>
        </div>
      </el-form>
    </el-card>

    <el-drawer v-model="previewVisible" title="文章预览" size="60%">
      <article class="article-content" v-html="previewHtml"></article>
    </el-drawer>
    <el-dialog v-model="assetPickerVisible" title="我的附件与回收站" width="min(720px, 92vw)">
      <el-radio-group v-model="assetPickerStatus" class="asset-picker-tabs" @change="loadUserAssets()">
        <el-radio-button label="active">可添加附件</el-radio-button>
        <el-radio-button label="trashed">回收站</el-radio-button>
      </el-radio-group>
      <p class="attachment-hint">回收站附件不可添加到文章；恢复后才可再次使用。已经被文章引用的附件不能移入回收站。</p>
      <el-empty
        v-if="userAssets.length === 0"
        :description="assetPickerStatus === 'trashed' ? '回收站为空。' : '暂无可添加的个人附件，请先上传一个附件。'"
      />
      <el-table v-else :data="userAssets" size="small" max-height="360">
        <el-table-column prop="original_name" label="文件名" min-width="220" />
        <el-table-column label="类型/大小" min-width="170"
          ><template #default="scope"
            >{{ scope.row.mime_type }} · {{ formatAttachmentSize(scope.row.size_bytes) }}</template
          ></el-table-column
        >
        <el-table-column label="操作" width="132"
          ><template #default="scope"
            ><el-button v-if="assetPickerStatus === 'active'" text type="primary" @click="bindExistingAsset(scope.row)"
              >添加</el-button
            >
            <el-button v-if="assetPickerStatus === 'active'" text type="danger" @click="trashUserAsset(scope.row)"
              >移入回收站</el-button
            >
            <el-button v-else text type="primary" @click="restoreUserAsset(scope.row)">恢复</el-button></template
          ></el-table-column
        >
      </el-table>
      <el-button v-if="assetNextCursor" :loading="assetsLoading" @click="loadUserAssets(true)">加载更多附件</el-button>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { onBeforeRouteLeave, useRoute, useRouter } from 'vue-router'
import { categoryApi, threadApi } from '@/modules/community/api'
import { richTextApi } from '@/modules/richtext/api'
import { mutualAidApi } from '@/modules/mutual-aid/api'
import { secondhandApi } from '@/modules/secondhand/api'
import { useLayoutCapability } from '@/shared/layout/useLayoutCapability'
import { ElMessage } from 'element-plus'

const route = useRoute()
const router = useRouter()
const createThreadRoot = ref<HTMLElement | null>(null)
const { isCompact } = useLayoutCapability(createThreadRoot)

const loading = ref(false)
const categoryLoading = ref(false)
const savingDraft = ref(false)
const publishing = ref(false)
const previewing = ref(false)
const assetUploading = ref(false)
const richTextEnabled = ref(false)
const templateMode = ref<'richtext' | 'plain_text'>('richtext')
type PublishMode = 'richtext' | 'plain_text' | 'mutual_aid' | 'secondhand'
const publishMode = ref<PublishMode>('richtext')
const mutualAidEnabled = ref(true)
const secondhandEnabled = ref(true)
const draftThreadId = ref('')
const articleContentId = ref('')
const previewVisible = ref(false)
const previewHtml = ref('')
const coverInput = ref<HTMLInputElement | null>(null)
const bodyImageInput = ref<HTMLInputElement | null>(null)
const attachmentInput = ref<HTMLInputElement | null>(null)
const attachmentUploading = ref(false)
const attachments = ref<any[]>([])
const assetPickerVisible = ref(false)
const userAssets = ref<any[]>([])
const assetPickerStatus = ref<'active' | 'trashed'>('active')
const assetNextCursor = ref('')
const assetsLoading = ref(false)
let assetListRevision = 0
const categories = ref<
  Array<{
    id: string
    name: string
    default_tags?: string[]
    node_kind?: string
    lifecycle_status?: string
    is_closed?: boolean
  }>
>([])
const plainDefaults = ref<string[]>([])
const articleDefaults = ref<string[]>([])
const articleDirty = ref(false)
const initializingArticle = ref(true)
const plainDirty = ref(false)
const initializingPlain = ref(true)
const allowLeave = ref(false)

const publishOptions = computed(() => [
  { label: '图文文章', value: 'richtext', disabled: !richTextEnabled.value },
  { label: '普通文本', value: 'plain_text' },
  { label: '校园互助', value: 'mutual_aid', disabled: !mutualAidEnabled.value },
  { label: '校园二手', value: 'secondhand', disabled: !secondhandEnabled.value },
])

const isEditMode = computed(() => Boolean(route.params.id))
const editorTitle = computed(() => {
  if (!isEditMode.value) return '发布帖子'
  return templateMode.value === 'plain_text' ? '编辑普通文本帖子' : '编辑图文文章'
})

const plainForm = reactive({
  title: '',
  content: '',
  category_id: '',
  tags: [] as string[],
  is_private: false,
})
const articleForm = reactive({
  title: '',
  summary: '',
  cover_url: '',
  category_id: '',
  tags: [] as string[],
  content_html: '<p></p>',
  content_json: {} as Record<string, any>,
})

const currentPlainCategory = computed(() => categories.value.find((category) => category.id === plainForm.category_id))
const currentArticleCategory = computed(() =>
  categories.value.find((category) => category.id === articleForm.category_id),
)

const unwrap = (res: any) => res?.data || res

const loadStatus = async () => {
  try {
    const status = unwrap(await richTextApi.status())
    richTextEnabled.value = Boolean(status.enabled)
    templateMode.value = richTextEnabled.value ? 'richtext' : 'plain_text'
    publishMode.value = templateMode.value
  } catch {
    richTextEnabled.value = false
    templateMode.value = 'plain_text'
    publishMode.value = 'plain_text'
  }
}

const loadStructuredStatuses = async () => {
  const [mutualAid, secondhand] = await Promise.allSettled([mutualAidApi.status(), secondhandApi.status()])
  if (mutualAid.status === 'fulfilled') {
    mutualAidEnabled.value = unwrap(mutualAid.value)?.enabled !== false
  }
  if (secondhand.status === 'fulfilled') {
    secondhandEnabled.value = unwrap(secondhand.value)?.enabled !== false
  }
}

const loadCategories = async () => {
  categoryLoading.value = true
  try {
    const res: any = await categoryApi.list()
    if (res.code === 0) {
      categories.value = (res.data || []).filter(
        (category: any) =>
          (category.node_kind || 'board') === 'board' &&
          (category.lifecycle_status || 'active') === 'active' &&
          !category.is_closed,
      )
      if (!plainForm.category_id && categories.value.length > 0) plainForm.category_id = categories.value[0].id
      if (!articleForm.category_id && categories.value.length > 0) articleForm.category_id = categories.value[0].id
      applyPlainDefaultTags()
      applyArticleDefaultTags()
    }
  } catch (error: any) {
    ElMessage.error(error?.msg || '获取版块失败')
  } finally {
    categoryLoading.value = false
  }
}

const loadEditingThread = async () => {
  if (!isEditMode.value) return
  draftThreadId.value = String(route.params.id)
  try {
    const threadRes = await threadApi.getMine(draftThreadId.value)
    const thread = unwrap(threadRes)
    if (thread.content_format !== 'richtext_article') {
      templateMode.value = 'plain_text'
      plainForm.title = thread.title || ''
      plainForm.content = thread.content || ''
      plainForm.category_id = thread.category_id || plainForm.category_id
      plainForm.tags = [...(thread.tags || [])]
      plainForm.is_private = thread.status === 'private'
      return
    }

    templateMode.value = 'richtext'
    const articleRes = await richTextApi.getMine(draftThreadId.value)
    const article = unwrap(articleRes)
    articleForm.title = article.title || thread.title || ''
    articleForm.summary = article.summary || ''
    articleForm.cover_url = article.cover_url || ''
    articleForm.category_id = thread.category_id || articleForm.category_id
    articleForm.tags = [...(thread.tags || [])]
    articleForm.content_html = article.content_html || article.sanitized_html || '<p></p>'
    articleForm.content_json = article.content_json || {}
    articleContentId.value = article.id || ''
    await loadAttachments()
  } catch (error: any) {
    ElMessage.error(error?.msg || '加载帖子失败')
  }
}

const submitPlain = async () => {
  if (!plainForm.title || !plainForm.content || !plainForm.category_id) {
    ElMessage.warning('请填写标题、内容和版块')
    return
  }
  loading.value = true
  try {
    const payload = {
      title: plainForm.title,
      content: plainForm.content,
      category_id: plainForm.category_id,
      tags: plainForm.tags,
      is_private: plainForm.is_private,
    }
    const res: any = isEditMode.value
      ? await threadApi.update(String(route.params.id), {
          title: payload.title,
          content: payload.content,
          tags: payload.tags,
          status: plainForm.is_private ? 'private' : 'published',
        })
      : await threadApi.create(payload)
    if (res.code === 0) {
      ElMessage.success(isEditMode.value ? '修改已保存' : '发布成功')
      plainDirty.value = false
      allowLeave.value = true
      router.push(`/threads/${res.data.id}`)
    }
  } catch (error: any) {
    ElMessage.error(error?.msg || (isEditMode.value ? '保存失败' : '发布失败'))
  } finally {
    loading.value = false
  }
}

const articlePayload = () => ({
  title: articleForm.title,
  summary: articleForm.summary,
  cover_url: articleForm.cover_url,
  category_id: articleForm.category_id,
  tags: articleForm.tags,
  content_html: articleForm.content_html,
  content_json: articleForm.content_json,
})

const saveDraft = async () => {
  if (!articleForm.title || !articleForm.content_html || (!draftThreadId.value && !articleForm.category_id)) {
    ElMessage.warning('请填写标题、正文和版块')
    return ''
  }
  savingDraft.value = true
  try {
    const res: any = draftThreadId.value
      ? await richTextApi.updateDraft(draftThreadId.value, articlePayload())
      : await richTextApi.createDraft(articlePayload())
    const data = unwrap(res)
    draftThreadId.value = data.thread_id
    articleContentId.value = data.article_content_id
    articleDirty.value = false
    ElMessage.success('草稿已保存')
    return draftThreadId.value
  } catch (error: any) {
    ElMessage.error(error?.msg || '保存草稿失败')
    return ''
  } finally {
    savingDraft.value = false
  }
}

const publishArticle = async () => {
  const threadId = draftThreadId.value || (await saveDraft())
  if (!threadId) return
  publishing.value = true
  try {
    const res: any = await richTextApi.publish(threadId)
    const data = unwrap(res)
    ElMessage.success('文章已发布')
    allowLeave.value = true
    router.push(`/threads/${data.thread_id}`)
  } catch (error: any) {
    ElMessage.error(error?.msg || '发布文章失败')
  } finally {
    publishing.value = false
  }
}

const previewArticle = async () => {
  previewing.value = true
  try {
    const res: any = await richTextApi.preview(articleForm.content_html)
    const data = unwrap(res)
    previewHtml.value = data.sanitized_html
    previewVisible.value = true
  } catch (error: any) {
    ElMessage.error(error?.msg || '预览失败')
  } finally {
    previewing.value = false
  }
}

const selectPublishMode = (value: PublishMode) => {
  if (value === 'mutual_aid' || value === 'secondhand') {
    if (hasUnsavedChanges() && !window.confirm('当前内容尚未保存，确定切换到其他发布类型吗？')) {
      return
    }
    allowLeave.value = true
    void router.push(value === 'mutual_aid' ? '/mutual-aid/create' : '/secondhand/create')
    return
  }
  publishMode.value = value
  templateMode.value = value
}

const chooseCover = () => coverInput.value?.click()
const chooseBodyImage = () => bodyImageInput.value?.click()
const chooseAttachment = () => attachmentInput.value?.click()

const loadAttachments = async () => {
  if (!draftThreadId.value) {
    attachments.value = []
    return
  }
  try {
    const result: any = await richTextApi.listAttachments(draftThreadId.value)
    attachments.value = unwrap(result)?.items || []
  } catch (error: any) {
    ElMessage.error(error?.msg || '加载文章附件失败')
  }
}

const ensureDraftForAttachments = async () => {
  if (draftThreadId.value) return draftThreadId.value
  return saveDraft()
}

const uploadAttachment = async (event: Event) => {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  attachmentUploading.value = true
  try {
    const threadId = await ensureDraftForAttachments()
    if (!threadId) return
    await richTextApi.uploadAttachment(threadId, file)
    await loadAttachments()
    ElMessage.success('附件已添加到文章草稿')
  } catch (error: any) {
    ElMessage.error(error?.msg || '附件上传失败')
  } finally {
    input.value = ''
    attachmentUploading.value = false
  }
}

const openAssetPicker = async () => {
  const threadId = await ensureDraftForAttachments()
  if (!threadId) return
  assetPickerStatus.value = 'active'
  assetPickerVisible.value = true
  await loadUserAssets()
}

const loadUserAssets = async (append = false) => {
  if (append && (assetsLoading.value || !assetNextCursor.value)) return
  const revision = ++assetListRevision
  assetsLoading.value = true
  if (!append) {
    assetNextCursor.value = ''
    userAssets.value = []
  }
  try {
    const result: any = await richTextApi.listUserAssets(assetPickerStatus.value, append ? assetNextCursor.value : '')
    if (revision !== assetListRevision) return
    const payload = unwrap(result)
    const items = (payload?.items || []).filter((asset: any) => asset.kind === 'article_attachment')
    userAssets.value = append ? [...userAssets.value, ...items] : items
    assetNextCursor.value = payload?.next_cursor || ''
  } catch (error: any) {
    if (revision !== assetListRevision) return
    ElMessage.error(error?.msg || '无法读取个人附件')
  } finally {
    if (revision === assetListRevision) assetsLoading.value = false
  }
}

const bindExistingAsset = async (asset: any) => {
  if (!draftThreadId.value) return
  try {
    await richTextApi.bindAttachment(draftThreadId.value, { asset_id: asset.id })
    assetPickerVisible.value = false
    await loadAttachments()
    ElMessage.success('已添加已有附件')
  } catch (error: any) {
    ElMessage.error(error?.msg || '添加附件失败')
  }
}

const trashUserAsset = async (asset: any) => {
  try {
    await richTextApi.trashUserAsset(asset.id)
    ElMessage.success('附件已移入回收站。')
    await loadUserAssets()
  } catch (error: any) {
    ElMessage.error(error?.msg || '附件无法移入回收站。')
  }
}

const restoreUserAsset = async (asset: any) => {
  try {
    await richTextApi.restoreUserAsset(asset.id)
    ElMessage.success('附件已恢复，可重新添加到文章。')
    await loadUserAssets()
  } catch (error: any) {
    ElMessage.error(error?.msg || '附件恢复失败。')
  }
}

const moveAttachment = async (index: number, offset: number) => {
  if (!draftThreadId.value) return
  const next = [...attachments.value]
  const target = index + offset
  if (target < 0 || target >= next.length) return
  ;[next[index], next[target]] = [next[target], next[index]]
  try {
    await richTextApi.reorderAttachments(
      draftThreadId.value,
      next.map((item) => item.id),
    )
    attachments.value = next
  } catch (error: any) {
    ElMessage.error(error?.msg || '附件排序失败')
  }
}

const removeAttachment = async (attachmentID: string) => {
  if (!draftThreadId.value) return
  try {
    await richTextApi.removeAttachment(draftThreadId.value, attachmentID)
    await loadAttachments()
    ElMessage.success('附件已从文章移除，仍保留在你的个人附件中')
  } catch (error: any) {
    ElMessage.error(error?.msg || '移除附件失败')
  }
}

const renameAttachment = async (attachment: any) => {
  if (!draftThreadId.value) return
  const name = String(attachment?.display_name || '').trim()
  if (!name) {
    ElMessage.warning('附件显示名称不能为空')
    await loadAttachments()
    return
  }
  try {
    await richTextApi.renameAttachment(draftThreadId.value, attachment.id, name)
    attachment.display_name = name
    ElMessage.success('附件显示名称已更新')
  } catch (error: any) {
    ElMessage.error(error?.msg || '更新附件名称失败')
    await loadAttachments()
  }
}

const formatAttachmentSize = (value: unknown) => {
  const bytes = Number(value || 0)
  if (!Number.isFinite(bytes) || bytes < 1024) return `${Math.max(0, bytes)} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KiB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MiB`
}

const uploadCover = async (event: Event) => {
  const input = event.target as HTMLInputElement
  const asset = await uploadSelectedAsset(input)
  if (asset?.file_url) articleForm.cover_url = asset.file_url
}

const uploadBodyImage = async (event: Event) => {
  const input = event.target as HTMLInputElement
  const asset = await uploadSelectedAsset(input)
  if (asset?.file_url)
    insertSnippet(
      `<figure><img src="${asset.file_url}" alt="${asset.file_name || 'image'}" loading="lazy"><figcaption>图片说明</figcaption></figure>`,
    )
}

const uploadSelectedAsset = async (input: HTMLInputElement) => {
  const file = input.files?.[0]
  if (!file) return null
  assetUploading.value = true
  try {
    const res: any = await richTextApi.uploadAsset(file, {
      thread_id: draftThreadId.value,
      article_content_id: articleContentId.value,
    })
    return unwrap(res)
  } catch (error: any) {
    ElMessage.error(error?.msg || '图片上传失败')
    return null
  } finally {
    input.value = ''
    assetUploading.value = false
  }
}

const insertSnippet = (snippet: string) => {
  const prefix = articleForm.content_html.trim()
  articleForm.content_html = `${prefix}${prefix ? '\n' : ''}${snippet}`
}

const applyPlainDefaultTags = () => {
  if (isEditMode.value) return
  const custom = plainForm.tags.filter(
    (tag) => !plainDefaults.value.map((item) => item.toLowerCase()).includes(tag.toLowerCase()),
  )
  const defaults = currentPlainCategory.value?.default_tags || []
  plainForm.tags = mergeTags(defaults, custom)
  plainDefaults.value = [...defaults]
}

const applyArticleDefaultTags = () => {
  if (isEditMode.value) return
  const custom = articleForm.tags.filter(
    (tag) => !articleDefaults.value.map((item) => item.toLowerCase()).includes(tag.toLowerCase()),
  )
  const defaults = currentArticleCategory.value?.default_tags || []
  articleForm.tags = mergeTags(defaults, custom)
  articleDefaults.value = [...defaults]
}

const mergeTags = (...groups: string[][]) => {
  const seen = new Set<string>()
  const result: string[] = []
  for (const tags of groups) {
    for (const tag of tags || []) {
      const value = String(tag || '').trim()
      if (!value) continue
      const key = value.toLowerCase()
      if (seen.has(key)) continue
      seen.add(key)
      result.push(value)
    }
  }
  return result.slice(0, 20)
}

watch(() => plainForm.category_id, applyPlainDefaultTags)
watch(() => articleForm.category_id, applyArticleDefaultTags)
watch(
  plainForm,
  () => {
    if (!initializingPlain.value && templateMode.value === 'plain_text') {
      plainDirty.value = true
    }
  },
  { deep: true },
)
watch(
  articleForm,
  () => {
    if (!initializingArticle.value && templateMode.value === 'richtext') {
      articleDirty.value = true
    }
  },
  { deep: true },
)

const hasUnsavedChanges = () => {
  if (allowLeave.value) return false
  if (templateMode.value === 'richtext') return articleDirty.value
  return templateMode.value === 'plain_text' && plainDirty.value
}

const handleBeforeUnload = (event: BeforeUnloadEvent) => {
  if (!hasUnsavedChanges()) return
  event.preventDefault()
  event.returnValue = ''
}

onBeforeRouteLeave(() => {
  if (!hasUnsavedChanges()) {
    return true
  }
  if (templateMode.value === 'richtext' && articleDirty.value) {
    return window.confirm('图文文章还有未保存的修改，离开前请先保存草稿或发布。确定要离开吗？')
  }
  if (templateMode.value === 'plain_text' && plainDirty.value) {
    return window.confirm('普通文本帖子还有未保存的修改，离开前请先保存修改或发布。确定要离开吗？')
  }
  return true
})

onMounted(async () => {
  await Promise.all([loadStatus(), loadStructuredStatuses(), loadCategories()])
  await loadEditingThread()
  initializingArticle.value = false
  initializingPlain.value = false
  articleDirty.value = false
  plainDirty.value = false
  window.addEventListener('beforeunload', handleBeforeUnload)
})

onBeforeUnmount(() => {
  window.removeEventListener('beforeunload', handleBeforeUnload)
})
</script>

<style scoped>
.create-thread {
  max-width: 920px;
  margin: 0 auto;
}
.editor-card {
  border-radius: 8px;
}
.editor-header,
.editor-actions,
.cover-row,
.body-toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.editor-header {
  justify-content: space-between;
}
.editor-header h2 {
  margin: 0;
}
.publish-mode-picker {
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 10px;
}
.publish-mode-label {
  color: var(--campus-muted-color, #606266);
  font-size: 13px;
  white-space: nowrap;
}
.publish-mode-select {
  width: 100%;
}
.field-full {
  width: 100%;
}
.cover-row {
  width: 100%;
}
.cover-row .el-input {
  flex: 1;
  min-width: 240px;
}
.body-toolbar {
  margin-bottom: 8px;
}
.attachment-editor {
  width: 100%;
}
.attachment-hint {
  margin: 0 0 10px;
  color: var(--campus-muted-color, #606266);
  font-size: 13px;
  line-height: 1.6;
}
.asset-picker-tabs {
  margin-bottom: 10px;
}
.attachment-editor-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 10px 0;
  border-bottom: 1px solid var(--el-border-color-lighter);
}
.attachment-editor-name {
  display: grid;
  min-width: 0;
  gap: 4px;
}
.attachment-editor-name strong,
.attachment-editor-name span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.attachment-editor-name span {
  color: var(--campus-muted-color, #606266);
  font-size: 12px;
}
.attachment-editor-actions {
  display: flex;
  flex: 0 0 auto;
}
.html-editor {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
}
.hidden-input {
  display: none;
}
.article-content {
  max-width: 760px;
  margin: 0 auto;
  padding: 8px 0 24px;
  font-size: 16px;
  line-height: 1.8;
  color: #222;
}
.article-content :deep(img) {
  max-width: 100%;
  height: auto;
  display: block;
  margin: 16px auto;
  border-radius: 8px;
}
.article-content :deep(blockquote) {
  margin: 16px 0;
  padding: 12px 16px;
  background: #f6f8fa;
  border-left: 4px solid #dcdfe6;
}
@media (max-width: 720px) {
  .editor-header {
    align-items: flex-start;
    flex-direction: column;
  }
  .publish-mode-picker {
    width: 100%;
    align-items: stretch;
    flex-direction: column;
    gap: 6px;
  }
  .editor-actions {
    align-items: stretch;
    flex-direction: column;
  }
  .editor-actions .el-button {
    width: 100%;
    min-height: 44px;
    margin-left: 0;
  }
  .cover-row .el-input {
    min-width: 0;
    flex-basis: 100%;
  }
}
</style>
