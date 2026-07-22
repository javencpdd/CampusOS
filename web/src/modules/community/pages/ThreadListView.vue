<template>
  <div ref="threadListRoot" class="thread-list" :data-layout-mode="layoutMode">
    <div class="list-header">
      <h2>帖子列表</h2>
      <div class="search-bar">
        <el-input v-model="keyword" class="thread-search-input" placeholder="搜索帖子..." @keyup.enter="loadThreads">
          <template #append
            ><el-button @click="loadThreads"
              ><el-icon><Search /></el-icon></el-button
          ></template>
        </el-input>
      </div>
    </div>
    <el-tabs v-model="contentTab" class="content-tabs" @tab-change="onTabChange">
      <el-tab-pane label="全部帖子" name="all" />
      <el-tab-pane label="图文文章" name="richtext" />
    </el-tabs>
    <div v-if="isCompact" class="compact-thread-list" v-loading="loading" aria-label="移动端帖子列表">
      <article v-for="thread in threads" :key="thread.id" class="compact-thread-item">
        <router-link :to="`/threads/${thread.id}`" class="compact-thread-title">
          <span class="compact-thread-tags">
            <el-tag v-if="thread.is_pinned" type="danger" size="small">置顶</el-tag>
            <el-tag v-if="thread.content_format === 'richtext_article'" type="success" size="small">图文</el-tag>
          </span>
          <strong>{{ thread.title }}</strong>
        </router-link>
        <div class="compact-thread-meta">
          <span>{{ thread.author_name || '未知用户' }}</span>
          <span>{{ thread.view_count || 0 }} 浏览</span>
          <span>{{ thread.reply_count || 0 }} 回复</span>
          <time :datetime="thread.created_at">{{ formatThreadDate(thread.created_at) }}</time>
        </div>
      </article>
    </div>
    <el-table v-else :data="threads" style="width: 100%" v-loading="loading">
      <el-table-column prop="title" label="标题" min-width="300">
        <template #default="{ row }">
          <router-link :to="`/threads/${row.id}`" class="thread-link">
            <el-tag v-if="row.is_pinned" type="danger" size="small" style="margin-right: 8px">置顶</el-tag>
            <el-tag
              v-if="row.content_format === 'richtext_article'"
              type="success"
              size="small"
              style="margin-right: 8px"
              >图文</el-tag
            >
            {{ row.title }}
          </router-link>
        </template>
      </el-table-column>
      <el-table-column prop="author_name" label="作者" width="120" />
      <el-table-column prop="view_count" label="浏览" width="80" />
      <el-table-column prop="reply_count" label="回复" width="80" />
      <el-table-column prop="created_at" label="发布时间" width="180">
        <template #default="{ row }">{{ new Date(row.created_at).toLocaleString() }}</template>
      </el-table-column>
    </el-table>
    <div class="pagination" v-if="total > 0">
      <el-pagination
        v-model:current-page="page"
        :page-size="20"
        :total="total"
        @current-change="loadThreads"
        layout="prev, pager, next"
      />
    </div>
    <el-empty v-if="!loading && threads.length === 0" description="暂无帖子" />
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Search } from '@element-plus/icons-vue'
import { threadApi } from '@/modules/community/api'
import { useLayoutCapability } from '@/shared/layout/useLayoutCapability'

const route = useRoute()
const router = useRouter()
const threadListRoot = ref<HTMLElement | null>(null)
const { mode: layoutMode, isCompact } = useLayoutCapability(threadListRoot)
const threads = ref<any[]>([])
const loading = ref(false)
const page = ref(1)
const total = ref(0)
const keyword = ref('')
const routeContentTab = () => (route.query.content_format === 'richtext_article' ? 'richtext' : 'all')
const contentTab = ref<'all' | 'richtext'>(routeContentTab())
const formatThreadDate = (value: string) => {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '时间未知'
  return new Intl.DateTimeFormat('zh-CN', {
    month: 'numeric',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  }).format(date)
}

const loadThreads = async () => {
  loading.value = true
  try {
    const categoryID = String(route.query.category_id || '')
    const queryContentFormat = String(route.query.content_format || '')
    if (queryContentFormat === 'richtext_article' && contentTab.value !== 'richtext') {
      contentTab.value = 'richtext'
    }
    const res: any = await threadApi.list({
      page: page.value,
      page_size: 20,
      keyword: keyword.value,
      category_id: categoryID || undefined,
      content_format:
        contentTab.value === 'richtext' || queryContentFormat === 'richtext_article' ? 'richtext_article' : undefined,
    })
    if (res.code === 0) {
      threads.value = res.data?.items || []
      total.value = res.data?.pagination?.total || 0
    }
  } catch (e) {
    console.error(e)
  } finally {
    loading.value = false
  }
}

const onTabChange = () => {
  const query = { ...route.query }
  if (contentTab.value === 'richtext') {
    query.content_format = 'richtext_article'
  } else {
    delete query.content_format
  }
  router.replace({ path: route.path, query })
  page.value = 1
  loadThreads()
}

onMounted(loadThreads)
watch(
  () => route.query.category_id,
  () => {
    page.value = 1
    loadThreads()
  },
)
watch(
  () => route.query.content_format,
  () => {
    contentTab.value = routeContentTab()
    page.value = 1
    loadThreads()
  },
)
</script>

<style scoped>
.list-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}
.content-tabs {
  margin-bottom: 12px;
}
.thread-link {
  color: #303133;
  text-decoration: none;
}
.thread-link:hover {
  color: #409eff;
}
.pagination {
  margin-top: 20px;
  display: flex;
  justify-content: center;
}
.thread-search-input {
  width: min(300px, 100%);
}
.compact-thread-list {
  display: grid;
  gap: 1px;
  overflow: hidden;
  border: 1px solid var(--campus-border-color, #e4e7ed);
  border-radius: 6px;
  background: var(--campus-border-color, #e4e7ed);
}
.compact-thread-item {
  min-width: 0;
  padding: 13px 14px;
  display: grid;
  gap: 9px;
  background: var(--campus-surface-background, #fff);
}
.compact-thread-title {
  min-width: 0;
  display: flex;
  align-items: flex-start;
  gap: 8px;
  color: var(--campus-text-color, #303133);
  line-height: 1.5;
  text-decoration: none;
}
.compact-thread-title strong {
  min-width: 0;
  overflow-wrap: anywhere;
  font-size: 15px;
}
.compact-thread-tags {
  flex: 0 0 auto;
  display: inline-flex;
  gap: 4px;
}
.compact-thread-meta {
  display: flex;
  gap: 5px 12px;
  flex-wrap: wrap;
  color: var(--campus-muted-color, #606266);
  font-size: 12px;
  line-height: 1.4;
}
@media (max-width: 760px), (max-height: 540px) and (max-width: 1000px) {
  .list-header {
    align-items: stretch;
    flex-direction: column;
    gap: 12px;
    margin-bottom: 12px;
  }
  .search-bar,
  .thread-search-input {
    width: 100%;
  }
}
</style>
