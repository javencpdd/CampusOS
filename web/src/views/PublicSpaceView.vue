<template>
  <div class="public-space" v-loading="loading" :style="spaceStyleVars">
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
        <el-tag v-if="payload.space.sync_enabled" type="info" effect="plain">内容同步</el-tag>
      </section>

      <section class="content-section" :class="layoutClass">
        <article v-for="item in contents" :key="item.id" class="content-item">
          <router-link :to="`/threads/${item.thread_id}`" class="content-title">
            {{ item.title }}
          </router-link>
          <p>{{ item.excerpt }}</p>
          <div class="content-meta">
            <span>{{ formatDate(item.thread_created_at) }}</span>
            <el-tag v-for="tag in item.tags || []" :key="tag" size="small" effect="plain">{{ tag }}</el-tag>
          </div>
        </article>
      </section>

      <el-empty v-if="contents.length === 0 && !loading" description="暂无同步内容" />
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { spaceApi } from '@/api'

interface ApiResponse<T> {
  code: number
  msg: string
  data: T
}

interface Owner {
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
  thread_created_at: string
}

interface ListPayload<T> {
  items: T[]
}

const route = useRoute()
const payload = ref<PublicSpacePayload | null>(null)
const contents = ref<SpaceContent[]>([])
const loading = ref(false)
const loadError = ref('')
const username = computed(() => String(route.params.username || ''))

const spaceStyleVars = computed<Record<string, string>>(() => {
  const tokens = payload.value?.space.style_manifest?.tokens || {}
  return {
    '--space-primary': tokens['color.primary'] || '#2563eb',
    '--space-bg': tokens['color.background'] || '#ffffff',
    '--space-surface': tokens['color.surface'] || '#f8fafc',
    '--space-radius': tokens['radius.card'] || '8px',
  }
})

const layoutClass = computed(() => `layout-${payload.value?.space.layout || 'blog'}`)
const avatarText = computed(() => {
  const name = payload.value?.owner.nickname || payload.value?.owner.username || 'U'
  return name.slice(0, 1).toUpperCase()
})

const unwrap = <T,>(res: unknown): T => (res as ApiResponse<T>).data

const loadSpace = async () => {
  if (!username.value) return
  loading.value = true
  loadError.value = ''
  try {
    const [spaceRes, contentRes] = await Promise.all([
      spaceApi.publicByUsername(username.value),
      spaceApi.publicContentsByUsername(username.value, { page: 1, page_size: 20 }),
    ])
    payload.value = unwrap<PublicSpacePayload>(spaceRes)
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

watch(username, loadSpace, { immediate: true })
</script>

<style scoped>
.public-space {
  min-height: 70vh;
  padding: 28px 0 40px;
  background: var(--space-bg);
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
  color: #1f2937;
}
.space-hero p {
  max-width: 720px;
  margin: 0;
  color: #606266;
  line-height: 1.7;
}
.space-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 18px;
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
  color: #1f2937;
  font-weight: 700;
  text-decoration: none;
}
.content-title:hover {
  color: var(--space-primary);
}
.content-item p {
  margin: 0 0 12px;
  color: #606266;
  line-height: 1.7;
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
