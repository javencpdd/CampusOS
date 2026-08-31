<template>
  <div class="document-content-editor">
    <el-segmented
      v-if="allowFormatChange"
      :model-value="format"
      :options="editableDocumentFormats"
      aria-label="文档格式"
      @update:model-value="changeFormat"
    />
    <el-input
      :model-value="modelValue"
      type="textarea"
      :rows="18"
      class="document-content-editor__input"
      :placeholder="placeholder"
      @update:model-value="$emit('update:modelValue', $event)"
    />
    <p v-if="format === 'campusdoc'" class="document-content-editor__hint">
      CampusDoc v1 使用 JSON：<code>version: 1</code> 与非空 <code>blocks</code>；图片块只能填写私有 Object
      ID，不能填写外部 URL。
    </p>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { defaultDocumentContent, editableDocumentFormats, type DocumentFormat } from '../document'

const props = withDefaults(
  defineProps<{
    modelValue: string
    format: DocumentFormat
    allowFormatChange?: boolean
  }>(),
  { allowFormatChange: false },
)

const emit = defineEmits<{
  'update:modelValue': [value: string]
  'update:format': [value: DocumentFormat]
}>()

const placeholder = computed(() =>
  props.format === 'campusdoc'
    ? '请输入 CampusDoc v1 JSON'
    : props.format === 'markdown'
      ? '请输入 Markdown 文本'
      : '请输入文本内容',
)

const changeFormat = (format: DocumentFormat) => {
  if (format === props.format) return
  emit('update:format', format)
  if (!props.modelValue.trim()) emit('update:modelValue', defaultDocumentContent(format))
}
</script>

<style scoped>
.document-content-editor {
  display: grid;
  gap: 12px;
  margin-top: 12px;
}
.document-content-editor__input :deep(textarea) {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  line-height: 1.65;
}
.document-content-editor__hint {
  margin: 0;
  color: var(--el-text-color-secondary);
  font-size: 12px;
  line-height: 1.6;
}
</style>
