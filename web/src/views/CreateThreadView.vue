<template>
  <div class="create-thread">
    <el-card class="editor-card" shadow="never">
      <template #header>
        <div class="editor-header">
          <h2>{{ editorTitle }}</h2>
          <el-segmented
            v-if="richTextEnabled && !isEditMode"
            v-model="templateMode"
            :options="templateOptions"
          />
        </div>
      </template>

      <el-form v-if="templateMode === 'plain_text'" :model="plainForm" @submit.prevent="submitPlain" label-position="top">
        <el-form-item label="标题" required>
          <el-input v-model="plainForm.title" placeholder="请输入帖子标题" maxlength="255" show-word-limit />
        </el-form-item>
        <el-form-item label="版块" required>
          <el-select v-model="plainForm.category_id" :loading="categoryLoading" filterable :disabled="isEditMode" placeholder="请选择版块" class="field-full">
            <el-option v-for="category in categories" :key="category.id" :label="category.name" :value="category.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="内容" required>
          <el-input v-model="plainForm.content" type="textarea" :rows="10" placeholder="请输入帖子内容" />
        </el-form-item>
        <el-form-item label="标签">
          <el-select v-model="plainForm.tags" multiple filterable allow-create placeholder="输入标签后回车" class="field-full">
            <el-option v-for="tag in currentPlainCategory?.default_tags || []" :key="tag" :label="tag" :value="tag" />
          </el-select>
        </el-form-item>
        <el-form-item label="可见性">
          <el-switch
            v-model="plainForm.is_private"
            active-text="私密，仅自己可见"
            inactive-text="公开发布"
          />
        </el-form-item>
        <div class="editor-actions">
          <el-button type="primary" @click="submitPlain" :loading="loading">{{ isEditMode ? '保存修改' : '发布帖子' }}</el-button>
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
              <el-select v-model="articleForm.category_id" :loading="categoryLoading" filterable :disabled="isEditMode" placeholder="请选择版块" class="field-full">
                <el-option v-for="category in categories" :key="category.id" :label="category.name" :value="category.id" />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :xs="24" :md="12">
            <el-form-item label="标签">
              <el-select v-model="articleForm.tags" multiple filterable allow-create :disabled="isEditMode" placeholder="输入标签后回车" class="field-full">
                <el-option v-for="tag in currentArticleCategory?.default_tags || []" :key="tag" :label="tag" :value="tag" />
              </el-select>
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item label="摘要">
          <el-input v-model="articleForm.summary" type="textarea" :rows="3" maxlength="500" show-word-limit placeholder="用于列表和详情页的文章摘要" />
        </el-form-item>
        <el-form-item label="封面图">
          <div class="cover-row">
            <el-input v-model="articleForm.cover_url" placeholder="https://example.com/cover.jpg 或上传站内图片" />
            <input ref="coverInput" class="hidden-input" type="file" accept="image/png,image/jpeg,image/gif,image/webp" @change="uploadCover" />
            <el-button @click="chooseCover" :loading="assetUploading">上传封面</el-button>
          </div>
        </el-form-item>
        <el-form-item label="正文 HTML" required>
          <div class="body-toolbar">
            <el-button size="small" @click="insertSnippet('<h2>小标题</h2>')">H2</el-button>
            <el-button size="small" @click="insertSnippet('<p>段落内容</p>')">段落</el-button>
            <el-button size="small" @click="insertSnippet('<blockquote>引用内容</blockquote>')">引用</el-button>
            <input ref="bodyImageInput" class="hidden-input" type="file" accept="image/png,image/jpeg,image/gif,image/webp" @change="uploadBodyImage" />
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
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { onBeforeRouteLeave, useRoute, useRouter } from 'vue-router'
import { categoryApi, richTextApi, threadApi } from '@/api'
import { ElMessage } from 'element-plus'

const route = useRoute()
const router = useRouter()

const loading = ref(false)
const categoryLoading = ref(false)
const savingDraft = ref(false)
const publishing = ref(false)
const previewing = ref(false)
const assetUploading = ref(false)
const richTextEnabled = ref(false)
const templateMode = ref<'richtext' | 'plain_text'>('richtext')
const draftThreadId = ref('')
const articleContentId = ref('')
const previewVisible = ref(false)
const previewHtml = ref('')
const coverInput = ref<HTMLInputElement | null>(null)
const bodyImageInput = ref<HTMLInputElement | null>(null)
const categories = ref<Array<{ id: string; name: string; default_tags?: string[] }>>([])
const plainDefaults = ref<string[]>([])
const articleDefaults = ref<string[]>([])
const articleDirty = ref(false)
const initializingArticle = ref(true)
const plainDirty = ref(false)
const initializingPlain = ref(true)
const allowLeave = ref(false)

const templateOptions = [
  { label: '图文文章', value: 'richtext' },
  { label: '普通文本', value: 'plain_text' },
]

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
const currentArticleCategory = computed(() => categories.value.find((category) => category.id === articleForm.category_id))

const unwrap = (res: any) => res?.data || res

const loadStatus = async () => {
  try {
    const status = unwrap(await richTextApi.status())
    richTextEnabled.value = Boolean(status.enabled)
    templateMode.value = richTextEnabled.value ? 'richtext' : 'plain_text'
  } catch {
    richTextEnabled.value = false
    templateMode.value = 'plain_text'
  }
}

const loadCategories = async () => {
  categoryLoading.value = true
  try {
    const res: any = await categoryApi.list()
    if (res.code === 0) {
      categories.value = res.data || []
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
  const threadId = draftThreadId.value || await saveDraft()
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

const chooseCover = () => coverInput.value?.click()
const chooseBodyImage = () => bodyImageInput.value?.click()

const uploadCover = async (event: Event) => {
  const input = event.target as HTMLInputElement
  const asset = await uploadSelectedAsset(input)
  if (asset?.file_url) articleForm.cover_url = asset.file_url
}

const uploadBodyImage = async (event: Event) => {
  const input = event.target as HTMLInputElement
  const asset = await uploadSelectedAsset(input)
  if (asset?.file_url) insertSnippet(`<figure><img src="${asset.file_url}" alt="${asset.file_name || 'image'}" loading="lazy"><figcaption>图片说明</figcaption></figure>`)
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
  const custom = plainForm.tags.filter((tag) => !plainDefaults.value.map((item) => item.toLowerCase()).includes(tag.toLowerCase()))
  const defaults = currentPlainCategory.value?.default_tags || []
  plainForm.tags = mergeTags(defaults, custom)
  plainDefaults.value = [...defaults]
}

const applyArticleDefaultTags = () => {
  if (isEditMode.value) return
  const custom = articleForm.tags.filter((tag) => !articleDefaults.value.map((item) => item.toLowerCase()).includes(tag.toLowerCase()))
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
watch(plainForm, () => {
  if (!initializingPlain.value && templateMode.value === 'plain_text') {
    plainDirty.value = true
  }
}, { deep: true })
watch(articleForm, () => {
  if (!initializingArticle.value && templateMode.value === 'richtext') {
    articleDirty.value = true
  }
}, { deep: true })

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
  await Promise.all([loadStatus(), loadCategories()])
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
}
</style>
