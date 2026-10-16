<template><article ref="root" v-html="prepared"></article></template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { checkedImageHTML, loadCheckedImage } from './checked-images'

const props = defineProps<{ html: string; threadId?: string; article?: boolean }>()
const root = ref<HTMLElement>()
const prepared = computed(() => checkedImageHTML(props.html || ''))
let controller: AbortController | undefined
const urls = new Set<string>()
function clear() {
  controller?.abort()
  root.value?.querySelectorAll('img[data-checked-image-source]').forEach((node) => node.removeAttribute('src'))
  urls.forEach((url) => URL.revokeObjectURL(url))
  urls.clear()
}
async function refresh() {
  clear()
  const current = new AbortController()
  controller = current
  await nextTick()
  const images = root.value?.querySelectorAll<HTMLImageElement>('img[data-checked-image-source]') || []
  await Promise.all(
    Array.from(images, async (image) => {
      try {
        const value = await loadCheckedImage(
          image.dataset.checkedImageSource || '',
          { threadId: props.threadId, article: props.article },
          current.signal,
        )
        if (current.signal.aborted) {
          if (value.owned) URL.revokeObjectURL(value.url)
          return
        }
        if (value.owned) urls.add(value.url)
        image.src = value.url
      } catch {
        image.removeAttribute('src')
        image.alt ||= '图片暂不可用'
      }
    }),
  )
}
watch(() => [props.html, props.threadId, props.article], refresh, { flush: 'post' })
onMounted(() => {
  window.addEventListener('campusos:identity-session-changed', refresh)
  void refresh()
})
onBeforeUnmount(() => {
  window.removeEventListener('campusos:identity-session-changed', refresh)
  clear()
})
</script>
