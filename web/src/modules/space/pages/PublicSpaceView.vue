<template>
  <div
    class="public-space"
    v-loading="loading"
    data-campusos-space
    :data-space-owner="payload?.owner.username || username"
    :data-style-pack="payload?.space.style_name || 'default'"
    :style="spaceStyleVars"
  >
    <StyleEffectLayer :script="customEffectJS" :capabilities="styleCapabilities" :resolve-query="resolveStyleQuery" />
    <el-alert v-if="loadError" :title="loadError" type="error" show-icon :closable="false" />

    <template v-else-if="payload">
      <section class="space-hero">
        <div>
          <h1>{{ payload.space.title }}</h1>
          <p>{{ payload.space.bio || payload.owner.bio || payload.owner.nickname || payload.owner.username }}</p>
        </div>
        <el-avatar :size="72" :src="payload.space.avatar || payload.owner.avatar">
          {{ avatarText }}
        </el-avatar>
      </section>

      <section class="space-meta">
        <el-tag effect="plain">{{ payload.space.layout }}</el-tag>
        <el-tag v-if="payload.space.style_name" type="success" effect="plain">
          {{ payload.space.style_name }}@{{ payload.space.style_version }}
        </el-tag>
        <el-tag v-if="payload.space.sync_enabled" type="info" effect="plain">展示我的帖子</el-tag>
      </section>

      <el-alert
        v-if="isOwner"
        class="owner-content-notice"
        type="info"
        :closable="false"
        show-icon
        title="这是你的主页预览"
        description="访客只能看到公开且审核通过的帖子；你还可以看到草稿、私密、待审核、已拒绝和已下架内容。"
      />

      <section v-if="customHTML" class="custom-space-html" v-html="customHTML" />

      <section class="content-section" :class="layoutClass">
        <article v-for="item in contents" :key="item.id" class="content-item">
          <router-link :to="`/threads/${item.thread_id}`" class="content-title">
            {{ item.title }}
          </router-link>
          <p>{{ item.excerpt }}</p>
          <div class="content-meta">
            <span>{{ formatDate(item.thread_created_at) }}</span>
            <el-tag v-if="isOwner && item.publication_status === 'draft'" size="small" type="info">草稿</el-tag>
            <el-tag v-if="isOwner && item.publication_status === 'private'" size="small" type="warning">私密</el-tag>
            <el-tag v-if="isOwner && item.moderation_status === 'pending'" size="small" type="warning">待审核</el-tag>
            <el-tag v-if="isOwner && item.moderation_status === 'rejected'" size="small" type="danger">已拒绝</el-tag>
            <el-tag v-if="isOwner && item.moderation_status === 'taken_down'" size="small" type="danger">已下架</el-tag>
            <el-tag v-for="tag in item.tags || []" :key="tag" size="small" effect="plain">{{ tag }}</el-tag>
          </div>
          <p v-if="isOwner && item.moderation_reason" class="content-governance-reason">
            治理说明：{{ item.moderation_reason }}
          </p>
        </article>
      </section>

      <el-empty v-if="contents.length === 0 && !loading" description="暂无符合展示条件的帖子" />
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { scheduleApi } from '@/modules/schedule/api'
import { spaceApi } from '@/modules/space/api'
import StyleEffectLayer from '@/components/StyleEffectLayer.vue'
import { useUserStore } from '@/modules/identity/store'

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

interface StyleManifest {
  name: string
  version: string
  layout: string
  tokens?: Record<string, string>
  custom_html_enabled?: boolean
  custom_html?: string
  custom_css?: string
  custom_effect_js?: string
  capabilities?: string[]
}

interface Space {
  title: string
  bio: string
  avatar?: string
  layout: string
  style_name?: string
  style_version?: string
  style_manifest?: StyleManifest
  sync_enabled: boolean
}

interface PublicSpacePayload {
  owner: Owner
  space: Space
}

interface SpaceContent {
  id: string
  thread_id: string
  title: string
  excerpt: string
  tags?: string[]
  publication_status?: string
  moderation_status?: string
  deletion_status?: string
  moderation_reason?: string
  thread_created_at: string
}

interface ListPayload<T> {
  items: T[]
}

const route = useRoute()
const userStore = useUserStore()
const payload = ref<PublicSpacePayload | null>(null)
const contents = ref<SpaceContent[]>([])
const loading = ref(false)
const loadError = ref('')
const username = computed(() => String(route.params.username || ''))
const isOwner = computed(() => Boolean(userStore.isLoggedIn && payload.value?.owner.id === userStore.user?.id))

const spaceStyleVars = computed<Record<string, string>>(() => {
  const tokens = payload.value?.space.style_manifest?.tokens || {}
  return {
    '--space-primary': tokens['color.primary'] || '#2563eb',
    '--space-text': tokens['color.text'] || '#1f2937',
    '--space-muted': tokens['color.muted'] || '#606266',
    '--space-bg': tokens['color.background'] || '#ffffff',
    '--space-surface': tokens['color.surface'] || '#f8fafc',
    '--space-radius': tokens['radius.card'] || '8px',
    '--space-font': tokens['font.body'] || 'Inter, Noto Sans SC, system-ui, sans-serif',
  }
})

const layoutClass = computed(() => `layout-${payload.value?.space.layout || 'blog'}`)
const customHTML = computed(() => {
  const manifest = payload.value?.space.style_manifest
  if (!manifest?.custom_html_enabled || !manifest.custom_html) return ''
  return manifest.custom_html
})
const customCSS = computed(() => {
  const manifest = payload.value?.space.style_manifest
  if (!manifest?.custom_html_enabled || !manifest.custom_css) return ''
  return manifest.custom_css
})
const customEffectJS = computed(() => payload.value?.space.style_manifest?.custom_effect_js || '')
const styleCapabilities = computed(() => payload.value?.space.style_manifest?.capabilities || [])
const avatarText = computed(() => {
  const name = payload.value?.owner.nickname || payload.value?.owner.username || 'U'
  return name.slice(0, 1).toUpperCase()
})

const unwrap = <T,>(res: unknown): T => (res as ApiResponse<T>).data

const resolveStyleQuery = async (method: string, params: Record<string, unknown>) => {
  if (method === 'space.profile.read') {
    return payload.value
  }
  if (method === 'space.posts.read') {
    const requested = Number(params.limit || 10)
    const limit = Math.max(1, Math.min(20, Number.isFinite(requested) ? requested : 10))
    return { owner: payload.value?.owner, items: contents.value.slice(0, limit) }
  }
  if (method === 'schedule.me.read') {
    if (!userStore.isLoggedIn || payload.value?.owner.id !== userStore.user?.id) {
      throw new Error('课表只允许主页所有者本人在自己的主页风格中读取')
    }
    return unwrap(await scheduleApi.me())
  }
  throw new Error('不支持的风格包数据能力')
}

const loadSpace = async () => {
  if (!username.value) return
  loading.value = true
  loadError.value = ''
  try {
    const spaceRes = await spaceApi.publicByUsername(username.value)
    payload.value = unwrap<PublicSpacePayload>(spaceRes)
    const contentRes = isOwner.value
      ? await spaceApi.myContents({ page: 1, page_size: 20 })
      : await spaceApi.publicContentsByUsername(username.value, { page: 1, page_size: 20 })
    contents.value = unwrap<ListPayload<SpaceContent>>(contentRes).items || []
  } catch (error: any) {
    payload.value = null
    contents.value = []
    loadError.value = error?.msg || '主页不可访问'
  } finally {
    loading.value = false
  }
}

const formatDate = (value: string) => {
  if (!value) return ''
  return new Date(value).toLocaleDateString()
}

watch([username, () => userStore.user?.id], loadSpace, { immediate: true })

let injectedStyle: HTMLStyleElement | null = null

watch(
  customCSS,
  (css) => {
    if (injectedStyle) {
      injectedStyle.remove()
      injectedStyle = null
    }
    if (!css) return
    injectedStyle = document.createElement('style')
    injectedStyle.setAttribute('data-campusos-style-pack', 'personal-space')
    injectedStyle.textContent = css
    document.head.appendChild(injectedStyle)
  },
  { immediate: true },
)

onUnmounted(() => {
  injectedStyle?.remove()
  injectedStyle = null
})
</script>

<style scoped>
.public-space {
  position: relative;
  isolation: isolate;
  min-height: 70vh;
  padding: 28px 0 40px;
  background: var(--space-bg);
  color: var(--space-text);
  font-family: var(--space-font);
}
.public-space > :not(.style-effect-layer) {
  position: relative;
  z-index: 1;
}
.space-hero {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  margin-bottom: 18px;
  padding: 28px;
  border: 1px solid #e4e7ed;
  border-radius: var(--space-radius);
  background: var(--space-surface);
}
.space-hero h1 {
  margin: 0 0 10px;
  color: var(--space-text);
}
.space-hero p {
  max-width: 720px;
  margin: 0;
  color: var(--space-muted);
  line-height: 1.7;
}
.space-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 18px;
}
.owner-content-notice {
  margin-bottom: 18px;
}
.custom-space-html {
  margin-bottom: 18px;
  overflow-wrap: anywhere;
}
.custom-space-html :deep(*) {
  max-width: 100%;
}
.custom-space-html :deep(img) {
  height: auto;
}
.content-section {
  display: grid;
  gap: 14px;
}
.content-section.layout-grid {
  grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
}
.content-item {
  padding: 18px;
  border: 1px solid #e4e7ed;
  border-top: 3px solid var(--space-primary);
  border-radius: var(--space-radius);
  background: #fff;
}
.content-title {
  display: inline-block;
  margin-bottom: 8px;
  color: var(--space-text);
  font-weight: 700;
  text-decoration: none;
}
.content-title:hover {
  color: var(--space-primary);
}
.content-item p {
  margin: 0 0 12px;
  color: var(--space-muted);
  line-height: 1.7;
}
.content-governance-reason {
  margin-top: -4px !important;
  color: #b42318 !important;
  font-size: 13px;
}
.content-meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  color: #909399;
  font-size: 13px;
}
@media (max-width: 720px) {
  .space-hero {
    align-items: flex-start;
    flex-direction: column;
  }
}
</style>
