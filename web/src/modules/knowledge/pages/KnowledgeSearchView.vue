<template>
  <main class="knowledge-search" aria-labelledby="knowledge-title">
    <header>
      <h1 id="knowledge-title">知识检索</h1>
      <p>选择公开集合，按关键词查找资料；查看引用时会重新核验当前来源。</p>
    </header>

    <section v-if="catalogState !== 'ready'" class="knowledge-status" :aria-busy="catalogState === 'loading'">
      <p v-if="catalogState === 'loading'" role="status">正在加载公开集合…</p>
      <template v-else>
        <p :role="catalogState === 'error' ? 'alert' : 'status'">{{ catalogMessage }}</p>
        <button type="button" @click="loadCollections">重新加载集合</button>
      </template>
    </section>

    <template v-else>
      <form class="search-form" aria-label="公开知识检索" @submit.prevent="search(1)">
        <label>
          公开集合
          <select v-model="collectionID" aria-label="公开集合">
            <option value="" disabled>请选择公开集合</option>
            <option v-for="collection in collections" :key="collection.id" :value="collection.id">
              {{ collection.title }}
            </option>
          </select>
        </label>
        <label class="query-field">
          搜索词
          <input v-model="query" type="search" aria-label="搜索词" placeholder="输入关键词" autocomplete="off" />
        </label>
        <button type="submit" :disabled="!collectionID || !query.trim() || searchState === 'loading'">搜索</button>
      </form>

      <section :aria-busy="searchState === 'loading'" aria-label="检索结果" class="result-section">
        <p v-if="searchState === 'loading'" role="status">正在检索…</p>
        <p v-else-if="searchState === 'idle'" role="status">
          {{ collectionID ? '输入搜索词开始检索。' : '请先选择一个公开集合。' }}
        </p>
        <div v-else-if="searchState === 'error'" role="alert" class="knowledge-status">
          <p>{{ searchError }}</p>
          <button v-if="lastSearch && retryableSearch" type="button" @click="search(lastSearch.page)">重试检索</button>
        </div>
        <template v-else-if="results">
          <p role="status">找到 {{ results.total }} 条公开资料</p>
          <p v-if="results.total === 0">没有匹配的公开资料，请调整搜索词或选择其他集合。</p>
          <ol class="result-list">
            <li v-for="hit in results.items" :key="hit.citation.member_id" class="knowledge-hit">
              <h2>{{ hit.title }}</h2>
              <p class="source-kind">{{ sourceKind(hit.citation.source.kind) }}</p>
              <p class="snippet">{{ hit.snippet }}</p>
              <button type="button" @click="openCitation(hit.citation)">查看当前引用</button>
            </li>
          </ol>
          <nav v-if="results.total > results.page_size" class="result-pagination" aria-label="检索分页">
            <button type="button" :disabled="results.page <= 1" @click="search(results.page - 1)">上一页</button>
            <span>第 {{ results.page }} 页</span>
            <button
              type="button"
              :disabled="results.page * results.page_size >= results.total"
              @click="search(results.page + 1)"
            >
              下一页
            </button>
          </nav>
        </template>
      </section>

      <section
        v-if="sourceState !== 'idle'"
        ref="sourcePanel"
        tabindex="-1"
        class="current-source"
        aria-labelledby="current-source-title"
        :aria-busy="sourceState === 'loading'"
      >
        <h2 id="current-source-title">当前引用</h2>
        <p v-if="sourceState === 'loading'" role="status">正在核验来源…</p>
        <div v-else-if="sourceState === 'error'" role="alert">
          <p>{{ sourceError }}</p>
          <button v-if="sourceNeedsSearch" type="button" @click="search(1)">重新搜索</button>
          <button v-else-if="selectedCitation" type="button" @click="openCitation(selectedCitation)">重试引用</button>
        </div>
        <template v-else-if="source">
          <h3>{{ source.source.title }}</h3>
          <p class="source-kind">{{ sourceKind(source.source.reference.kind) }}</p>
          <pre class="source-content">{{ source.source.content }}</pre>
          <router-link v-if="originalThreadPath" :to="originalThreadPath">打开原帖</router-link>
        </template>
      </section>
      <RAGAnswerPanel v-if="collectionID" :collection-i-d="collectionID" />
    </template>
  </main>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import {
  knowledgeApi,
  type KnowledgeCitation,
  type KnowledgeSearchPage,
  type KnowledgeSource,
  type PublicCollection,
} from '../api'

import RAGAnswerPanel from '../components/RAGAnswerPanel.vue'

type CatalogState = 'loading' | 'ready' | 'empty' | 'disabled' | 'error'
type ReadState = 'idle' | 'loading' | 'ready' | 'error'
const catalogState = ref<CatalogState>('loading')
const collections = ref<PublicCollection[]>([])
const collectionID = ref('')
const query = ref('')
const searchState = ref<ReadState>('idle')
const results = ref<KnowledgeSearchPage | null>(null)
const searchError = ref('')
const retryableSearch = ref(true)
const lastSearch = ref<{ page: number } | null>(null)
const sourceState = ref<ReadState>('idle')
const sourcePanel = ref<HTMLElement | null>(null)
const source = ref<KnowledgeSource | null>(null)
const sourceError = ref('')
const sourceNeedsSearch = ref(false)
const selectedCitation = ref<KnowledgeCitation | null>(null)
let catalogVersion = 0,
  searchVersion = 0,
  sourceVersion = 0
let catalogController: AbortController | undefined
let searchController: AbortController | undefined
let sourceController: AbortController | undefined

const catalogMessage = computed(
  () =>
    ({
      disabled: '知识检索暂未启用，请联系管理员。',
      empty: '尚未配置公开知识集合，请联系管理员添加资料。',
      error: '公开集合暂不可用，请稍后重试。',
      loading: '',
      ready: '',
    })[catalogState.value],
)
const sourceKinds: Record<string, string> = {
  'community.thread': '社区帖子',
  'richtext.article': '公开文章',
  'knowledge.official': '官方资料',
}
const sourceKind = (kind: string) => sourceKinds[kind] || '公开资料'
const originalThreadPath = computed(() => {
  const origin = source.value?.source.reference
  return origin &&
    ['community.thread', 'richtext.article'].includes(origin.kind) &&
    /^[1-9][0-9]{0,18}$/.test(origin.id)
    ? `/threads/${origin.id}`
    : ''
})
const codeOf = (error: unknown) => (error as { machineCode?: string })?.machineCode

function clearSource() {
  sourceVersion++
  sourceController?.abort()
  source.value = null
  sourceState.value = 'idle'
  sourceError.value = ''
  selectedCitation.value = null
}
function clearSearch() {
  searchVersion++
  searchController?.abort()
  results.value = null
  searchState.value = 'idle'
  lastSearch.value = null
  clearSource()
}
function disable() {
  clearSearch()
  collections.value = []
  collectionID.value = ''
  catalogState.value = 'disabled'
}
watch([collectionID, query], clearSearch, { flush: 'sync' })

async function loadCollections() {
  const version = ++catalogVersion
  catalogController?.abort()
  const controller = new AbortController()
  catalogController = controller
  clearSearch()
  collections.value = []
  collectionID.value = ''
  catalogState.value = 'loading'
  try {
    const rows = await knowledgeApi.collections(controller.signal)
    if (version !== catalogVersion || controller.signal.aborted) return
    collections.value = rows
    catalogState.value = rows.length ? 'ready' : 'empty'
  } catch (error) {
    if (version !== catalogVersion || controller.signal.aborted) return
    catalogState.value = codeOf(error) === 'knowledge.disabled' ? 'disabled' : 'error'
  }
}
async function search(page: number) {
  if (catalogState.value !== 'ready' || !collectionID.value || !query.value.trim()) return
  clearSearch()
  if (new TextEncoder().encode(query.value.trim()).length > 1024) {
    searchState.value = 'error'
    searchError.value = '搜索词过长，请缩短后重试。'
    retryableSearch.value = false
    return
  }
  const version = searchVersion,
    id = collectionID.value,
    text = query.value.trim()
  const controller = new AbortController()
  searchController = controller
  lastSearch.value = { page }
  searchState.value = 'loading'
  try {
    const data = await knowledgeApi.search(id, text, page, controller.signal)
    if (version !== searchVersion || controller.signal.aborted) return
    results.value = data
    searchState.value = 'ready'
  } catch (error) {
    if (version !== searchVersion || controller.signal.aborted) return
    if (codeOf(error) === 'knowledge.disabled') {
      disable()
      return
    }
    searchState.value = 'error'
    retryableSearch.value = (error as { status?: number })?.status !== 400
    searchError.value = retryableSearch.value ? '检索暂不可用，请稍后重试。' : '请调整搜索词后重试。'
  }
}
async function revealSource(version: number) {
  await nextTick()
  if (version !== sourceVersion) return
  sourcePanel.value?.focus({ preventScroll: true })
  sourcePanel.value?.scrollIntoView?.({ block: 'start' })
}
async function openCitation(citation: KnowledgeCitation) {
  clearSource()
  if (catalogState.value !== 'ready' || citation.collection_id !== collectionID.value) return
  const version = sourceVersion
  const controller = new AbortController()
  sourceController = controller
  selectedCitation.value = { ...citation }
  sourceNeedsSearch.value = false
  sourceState.value = 'loading'
  void revealSource(version)
  try {
    const data = await knowledgeApi.source(citation, controller.signal)
    if (version !== sourceVersion || controller.signal.aborted) return
    if (
      data.collection_id !== citation.collection_id ||
      data.member_id !== citation.member_id ||
      data.revision !== citation.visibility_revision
    ) {
      throw new Error('Current citation response mismatch')
    }
    source.value = data
    sourceState.value = 'ready'
    void revealSource(version)
  } catch (error) {
    if (version !== sourceVersion || controller.signal.aborted) return
    if (codeOf(error) === 'knowledge.disabled') {
      disable()
      return
    }
    const status = (error as { status?: number })?.status
    sourceState.value = 'error'
    sourceNeedsSearch.value = status === 404 || status === 409
    sourceError.value =
      status === 409
        ? '来源已更新，请重新搜索获取当前引用。'
        : status === 404
          ? '引用已不可读或已移除，请重新搜索。'
          : '引用暂不可用，请稍后重试。'
    if (sourceNeedsSearch.value) {
      results.value = null
      searchState.value = 'idle'
    }
    void revealSource(version)
  }
}
function visibilityChanged() {
  if (document.hidden) clearSearch()
}
onMounted(() => {
  void loadCollections()
  document.addEventListener('visibilitychange', visibilityChanged)
})
onBeforeUnmount(() => {
  catalogVersion++
  catalogController?.abort()
  clearSearch()
  document.removeEventListener('visibilitychange', visibilityChanged)
})
</script>

<style scoped>
.knowledge-search {
  max-width: 1000px;
  margin: 0 auto;
  padding: 24px;
  overflow-wrap: anywhere;
}
.knowledge-search header p,
.source-kind {
  color: var(--el-text-color-secondary, #606266);
}
.knowledge-search h1 {
  margin: 0;
  font-size: 26px;
}
.search-form {
  display: flex;
  gap: 12px;
  align-items: end;
  flex-wrap: wrap;
  margin: 24px 0;
}
.search-form label {
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-width: 0;
}
.query-field {
  flex: 1;
}
.search-form select,
.search-form input {
  width: 100%;
  max-width: 100%;
  min-height: 44px;
  padding: 10px;
  border: 1px solid var(--el-border-color, #dcdfe6);
  border-radius: 6px;
  font: inherit;
}
.knowledge-search button {
  min-height: 44px;
  padding: 10px 18px;
  border: 1px solid var(--el-color-primary, #409eff);
  border-radius: 6px;
  background: var(--el-color-primary, #409eff);
  color: white;
  font: inherit;
  cursor: pointer;
}
.knowledge-search button:disabled {
  opacity: 0.5;
  cursor: default;
}
.knowledge-status,
.current-source,
.knowledge-hit {
  border: 1px solid var(--el-border-color, #dcdfe6);
  border-radius: 8px;
  padding: 18px;
  margin: 16px 0;
  background: var(--el-bg-color, white);
}
.result-list {
  list-style: none;
  padding: 0;
}
.knowledge-hit h2 {
  font-size: 18px;
  margin-top: 0;
}
.snippet,
.source-content {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  word-break: break-word;
  font: inherit;
}
.source-content {
  max-height: 480px;
  overflow-y: auto;
}
.result-pagination {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 16px;
}
@media (max-width: 640px) {
  .knowledge-search {
    padding: 16px 12px;
  }
  .search-form {
    align-items: stretch;
  }
  .search-form label,
  .query-field {
    flex-basis: 100%;
  }
  .search-form button {
    width: 100%;
  }
}
</style>
