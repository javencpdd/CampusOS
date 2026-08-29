<template>
  <div class="safe-content-editor">
    <div class="editor-mode-row">
      <el-segmented v-model="mode" :options="modeOptions" aria-label="正文格式" />
      <div class="editor-commands">
        <template v-if="mode === 'safe_html'">
          <input
            ref="imageInput"
            class="hidden-input"
            type="file"
            accept="image/png,image/jpeg,image/gif,image/webp"
            @change="uploadImage"
          />
          <el-tooltip content="上传并插入图片">
            <el-button :icon="Picture" :loading="uploading" @click="imageInput?.click()">插入图片</el-button>
          </el-tooltip>
        </template>
        <el-tooltip content="预览发布效果">
          <el-button :icon="View" :loading="previewing" @click="preview">预览</el-button>
        </el-tooltip>
      </div>
    </div>

    <el-input
      v-if="mode === 'markdown'"
      :model-value="modelValue"
      type="textarea"
      :rows="10"
      maxlength="100000"
      show-word-limit
      :placeholder="plainPlaceholder"
      @update:model-value="$emit('update:modelValue', $event)"
    />

    <template v-else>
      <div class="format-toolbar" role="toolbar" aria-label="图文正文排版">
        <el-tooltip content="插入二级标题"
          ><el-button size="small" @click="insert('<h2>小标题</h2>')">H2</el-button></el-tooltip
        >
        <el-tooltip content="插入段落"
          ><el-button size="small" @click="insert('<p>段落内容</p>')">段落</el-button></el-tooltip
        >
        <el-tooltip content="插入加粗文字"
          ><el-button size="small" @click="insert('<p><strong>重点内容</strong></p>')">B</el-button></el-tooltip
        >
        <el-tooltip content="插入引用"
          ><el-button size="small" @click="insert('<blockquote>引用内容</blockquote>')">引用</el-button></el-tooltip
        >
        <el-tooltip content="插入列表"
          ><el-button size="small" @click="insert('<ul><li>列表项</li></ul>')">列表</el-button></el-tooltip
        >
      </div>
      <el-input
        :model-value="modelValue"
        type="textarea"
        :rows="13"
        class="html-editor"
        maxlength="100000"
        spellcheck="false"
        :placeholder="htmlPlaceholder"
        @update:model-value="$emit('update:modelValue', $event)"
      />
      <div v-if="uploadedImages.length" class="image-preview-strip">
        <el-image
          v-for="image in uploadedImages"
          :key="image.file_url"
          :src="image.file_url"
          :preview-src-list="uploadedImages.map((item) => item.file_url)"
          fit="cover"
          preview-teleported
        />
      </div>
    </template>

    <el-drawer v-model="previewVisible" title="正文预览" size="min(720px, 94vw)">
      <article class="content-preview" v-html="previewHTML"></article>
    </el-drawer>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Picture, View } from '@element-plus/icons-vue'
import {
  contentApi,
  type ContentImage,
} from '../content'
import { htmlToPlainText, plainTextToHTML, type StructuredContentFormat } from '@/modules/content-editor/safe-html'

const props = withDefaults(
  defineProps<{
    modelValue: string
    contentFormat: StructuredContentFormat
    plainPlaceholder?: string
    htmlPlaceholder?: string
  }>(),
  {
    plainPlaceholder: '请输入正文内容',
    htmlPlaceholder: '<p>请输入图文正文...</p>',
  },
)

const emit = defineEmits<{
  'update:modelValue': [value: string]
  'update:contentFormat': [value: StructuredContentFormat]
}>()

const imageInput = ref<HTMLInputElement | null>(null)
const uploading = ref(false)
const previewing = ref(false)
const previewVisible = ref(false)
const previewHTML = ref('')
const uploadedImages = ref<ContentImage[]>([])
const modeOptions = [
  { label: '图文', value: 'safe_html' },
  { label: '纯文本', value: 'markdown' },
]

const mode = computed<StructuredContentFormat>({
  get: () => props.contentFormat,
  set: (next) => {
    if (next === props.contentFormat) return
    const converted = next === 'safe_html' ? plainTextToHTML(props.modelValue) : htmlToPlainText(props.modelValue)
    emit('update:modelValue', converted)
    emit('update:contentFormat', next)
  },
})

const insert = (snippet: string) => {
  const content = props.modelValue.trim()
  emit('update:modelValue', `${content}${content ? '\n' : ''}${snippet}`)
}

const uploadImage = async (event: Event) => {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  uploading.value = true
  try {
    const response: any = await contentApi.uploadImage(file)
    const asset = (response?.data || response) as ContentImage
    if (!asset?.file_url) throw new Error('missing uploaded image URL')
    uploadedImages.value.push(asset)
    insert(
      `<figure><img src="${asset.file_url}" alt="内容图片" loading="lazy"><figcaption>图片说明</figcaption></figure>`,
    )
    ElMessage.success('图片已插入正文')
  } catch (error: any) {
    ElMessage.error(error?.msg || '图片上传失败')
  } finally {
    input.value = ''
    uploading.value = false
  }
}

const preview = async () => {
  previewing.value = true
  try {
    if (mode.value === 'safe_html') {
      const response: any = await contentApi.preview(props.modelValue)
      previewHTML.value = response?.data?.sanitized_html || response?.sanitized_html || ''
    } else {
      previewHTML.value = plainTextToHTML(props.modelValue)
    }
    previewVisible.value = true
  } catch (error: any) {
    ElMessage.error(error?.msg || '正文未通过安全检查，暂时无法预览')
  } finally {
    previewing.value = false
  }
}
</script>

<style scoped>
.safe-content-editor {
  width: 100%;
  display: grid;
  gap: 10px;
}
.editor-mode-row,
.editor-commands,
.format-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.editor-mode-row {
  justify-content: space-between;
}
.hidden-input {
  display: none;
}
.html-editor :deep(textarea) {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  line-height: 1.65;
}
.image-preview-strip {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(88px, 112px));
  gap: 10px;
}
.image-preview-strip .el-image {
  width: 100%;
  aspect-ratio: 4 / 3;
  border: 1px solid var(--el-border-color);
  border-radius: 6px;
}
.content-preview {
  font-size: 16px;
  line-height: 1.8;
  color: var(--campus-text-color, #303133);
  overflow-wrap: anywhere;
}
.content-preview :deep(img) {
  display: block;
  max-width: 100%;
  height: auto;
  margin: 16px auto;
  border-radius: 6px;
}
.content-preview :deep(blockquote) {
  margin: 16px 0;
  padding: 12px 16px;
  border-left: 4px solid var(--el-border-color);
  background: var(--el-fill-color-light);
}
@media (max-width: 640px) {
  .editor-mode-row {
    align-items: flex-start;
    flex-direction: column;
  }
  .editor-commands {
    width: 100%;
  }
}
</style>
