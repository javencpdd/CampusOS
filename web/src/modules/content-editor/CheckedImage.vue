<template>
  <el-image v-if="preview" :src="url || undefined" :preview-src-list="url ? [url] : []" fit="cover" preview-teleported>
    <template #error><slot name="error">图片暂不可用</slot></template>
  </el-image>
  <img v-else :src="url || undefined" :alt="alt || '图片'" />
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { loadCheckedImage } from './checked-images'

const props = defineProps<{ src: string; alt?: string; threadId?: string; article?: boolean; preview?: boolean }>()
const url = ref('')
let owned = false
let controller: AbortController | undefined
function clear() {
  controller?.abort()
  if (owned && url.value) URL.revokeObjectURL(url.value)
  owned = false
  url.value = ''
}
async function refresh() {
  clear()
  const current = new AbortController()
  controller = current
  try {
    const value = await loadCheckedImage(
      props.src,
      { threadId: props.threadId, article: props.article },
      current.signal,
    )
    if (current.signal.aborted) {
      if (value.owned) URL.revokeObjectURL(value.url)
      return
    }
    owned = value.owned
    url.value = value.url
  } catch {
    /* Keep a failed private image empty. */
  }
}
watch(() => [props.src, props.threadId, props.article], refresh)
onMounted(() => {
  window.addEventListener('campusos:identity-session-changed', refresh)
  void refresh()
})
onBeforeUnmount(() => {
  window.removeEventListener('campusos:identity-session-changed', refresh)
  clear()
})
</script>
