<template>
  <main class="private-knowledge">
    <header>
      <RouterLink to="/knowledge">公开知识检索</RouterLink>
      <h1>我的私人知识</h1>
      <p>只有逐项选入的本人内容才会加入私人索引。仅供本人检索，不授权向 AI 或其他提供方发送内容。</p>
    </header>
    <p v-if="error" role="alert">{{ error }} 请刷新当前状态后重试。</p>
    <p v-if="notice" role="status">{{ notice }}</p>
    <button :disabled="busy" @click="refresh">刷新状态与列表</button>
    <p v-if="status && !status.can_index" role="status">
      当前部署未开启受保护的私人索引，请联系管理员。原文档仍可在“我的文档”管理。
    </p>
    <section v-if="status?.can_index">
      <h2>逐项选择来源</h2>
      <label
        >来源类型<select v-model="kind" :disabled="busy">
          <option value="personal_document">我的文档</option>
          <option value="community_thread">我的普通帖子</option>
        </select></label
      >
      <button :disabled="busy" @click="loadChoices">加载本人来源</button>
      <label
        >来源<select v-model="sourceID" :disabled="busy">
          <option value="">请选择一项</option>
          <option v-for="item in choices" :key="item.id" :value="item.id">
            {{ item.name || item.title || item.id }}
          </option>
        </select></label
      >
      <p>支持 TXT、Markdown、CampusDoc 及普通文本帖子，每项最多 64 KiB、最多 100 项；PDF、DOCX 和图文文章暂不支持。</p>
      <button :disabled="busy || !sourceID" @click="preview">核对当前来源</button>
      <div v-if="candidate">
        <p>{{ candidate.title }} · {{ candidate.format }}</p>
        <label
          ><input
            v-model="consent"
            type="checkbox"
            :disabled="busy || !canSelect"
          />我同意将这一项的当前版本加入仅本人可检索的私人索引</label
        ><button :disabled="busy || !canSelect || !consent" @click="select">确认选入这一项</button>
      </div>
      <h2>已选来源</h2>
      <p v-if="rows.length === 0">尚未选择来源，默认不入库。</p>
      <ul>
        <li v-for="row in rows" :key="row.id">
          <span
            >{{ row.title || (row.kind === 'personal_document' ? '文档 ' : '帖子 ') + row.source_id }} ·
            {{ stateLabel(row) }}</span
          ><button :disabled="busy" @click="withdraw(row)">撤回并清理</button
          ><button v-if="['retry', 'dead'].includes(row.task_state)" :disabled="busy" @click="retry(row)">
            重试任务
          </button>
        </li>
      </ul>
      <p>撤回会立即禁止后续读取并清理任务和索引。来源改版、删除或权限失效后，旧引用不可读，需重新逐项选择。</p>
      <h2>私人检索</h2>
      <form @submit.prevent="search">
        <label>关键词<input v-model="query" :disabled="writing" autocomplete="off" maxlength="1024" /></label
        ><button :disabled="busy || !query.trim()">搜索本人已选内容</button>
      </form>
      <p v-if="searched && hits.length === 0">没有当前可读的匹配结果；索引处理中可刷新状态后重新搜索。</p>
      <ul>
        <li v-for="hit in hits" :key="hit.id">
          <h3>{{ hit.title }}</h3>
          <p>{{ hit.snippet }}</p>
          <button :disabled="busy" @click="openSource(hit)">读取当前引用</button>
        </li>
      </ul>
      <section v-if="source" ref="sourcePanel" tabindex="-1" aria-label="私人引用">
        <h2>{{ source.title }}</h2>
        <pre>{{ source.text }}</pre>
      </section>
    </section>
  </main>
</template>
<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import {
  privateKnowledgeApi as api,
  type PrivateStatus,
  type PrivateSource,
  type PrivateSelection,
  type PrivateHit,
} from '../private-api'
const route = useRoute()
const status = ref<PrivateStatus | null>(null),
  rows = ref<PrivateSelection[]>([]),
  choices = ref<{ id: string; name?: string; title?: string }[]>([])
const kind = ref('personal_document'),
  sourceID = ref(''),
  candidate = ref<PrivateSource | null>(null),
  canSelect = ref(false),
  consent = ref(false),
  query = ref(''),
  hits = ref<PrivateHit[]>([]),
  source = ref<PrivateSource | null>(null),
  searched = ref(false),
  error = ref(''),
  notice = ref(''),
  busy = ref(false),
  writing = ref(false),
  sourcePanel = ref<HTMLElement | null>(null)
let generation = 0,
  controller: AbortController | null = null,
  disposed = false
function clearOutput() {
  hits.value = []
  source.value = null
  searched.value = false
  candidate.value = null
  canSelect.value = false
  consent.value = false
}
function invalidate() {
  generation++
  controller?.abort()
  clearOutput()
  if (!writing.value) busy.value = false
}
async function run(action: (signal: AbortSignal, current: () => boolean) => Promise<void>, write = false) {
  if (disposed || writing.value) return
  controller?.abort()
  const mine = ++generation
  controller = new AbortController()
  busy.value = true
  writing.value = write
  error.value = ''
  notice.value = ''
  const current = () => !disposed && mine === generation
  try {
    await action(controller.signal, current)
  } catch (e) {
    if (current()) {
      clearOutput()
      choices.value = []
      rows.value = []
      status.value = null
      error.value = e instanceof Error ? e.message : '操作未完成'
    }
  } finally {
    if (current()) {
      busy.value = false
      writing.value = false
    }
  }
}
async function refresh() {
  invalidate()
  await run(async (signal, current) => {
    const s: any = await api.status(signal)
    if (!current()) return
    status.value = s.data
    if (!s.data.can_index) {
      rows.value = []
      return
    }
    const r: any = await api.list(signal)
    if (current()) rows.value = r.data.items
  })
}
async function loadChoices() {
  invalidate()
  await run(async (signal, current) => {
    const r: any = await api.choices(kind.value, signal)
    if (current())
      choices.value = (r.data.items || [])
        .filter((x: any) => kind.value === 'personal_document' || x.thread_type === 'discussion')
        .slice(0, 100)
  })
}
async function preview() {
  await run(async (signal, current) => {
    const r: any = await api.preview(kind.value, sourceID.value, signal)
    if (current()) {
      candidate.value = r.data.source
      canSelect.value = r.data.can_index
      consent.value = false
      if (!r.data.can_index) notice.value = '此来源格式暂不支持私人索引'
    }
  })
}
async function select() {
  if (!candidate.value || !consent.value || !canSelect.value) return
  const chosen = { ...candidate.value }
  await run(async (signal, current) => {
    await api.select(chosen, signal)
    const r: any = await api.list(signal)
    if (current()) {
      rows.value = r.data.items
      clearOutput()
      notice.value = '已记录本项同意，索引任务已提交；请刷新状态后搜索'
    }
  }, true)
}
async function withdraw(row: PrivateSelection) {
  clearOutput()
  await run(async (signal, current) => {
    await api.withdraw(row.id, signal)
    const r: any = await api.list(signal)
    if (current()) {
      rows.value = r.data.items
      notice.value = '已撤回，后续请求无法再读取这项索引'
    }
  }, true)
}
async function retry(row: PrivateSelection) {
  clearOutput()
  await run(async (signal, current) => {
    await api.retry(row.id, signal)
    const r: any = await api.list(signal)
    if (current()) {
      rows.value = r.data.items
      notice.value = '已提交一次重试，请刷新任务状态'
    }
  }, true)
}
async function search() {
  clearOutput()
  if (new TextEncoder().encode(query.value).length > 1024) {
    error.value = '关键词不能超过1024字节'
    return
  }
  await run(async (signal, current) => {
    const r: any = await api.search(query.value, signal)
    if (current()) {
      hits.value = r.data.items
      searched.value = true
    }
  })
}
async function openSource(hit: PrivateHit) {
  source.value = null
  await run(async (signal, current) => {
    const r: any = await api.source(hit, signal)
    if (current()) {
      source.value = r.data
      await nextTick()
      if (current()) {
        sourcePanel.value?.focus()
        sourcePanel.value?.scrollIntoView?.({ block: 'nearest' })
      }
    }
  })
}
function stateLabel(row: PrivateSelection) {
  if (row.state === 'unavailable') return '保护不可用，请撤回后重新选择'
  return (
    (
      { pending: '处理中', processing: '处理中', idle: '可检索', retry: '等待重试', dead: '任务失败' } as Record<
        string,
        string
      >
    )[row.task_state] || '待处理'
  )
}
watch(
  [query, kind, sourceID],
  () => {
    invalidate()
    error.value = ''
  },
  { flush: 'sync' },
)
watch(
  kind,
  () => {
    choices.value = []
    sourceID.value = ''
  },
  { flush: 'sync' },
)
function visibility() {
  if (document.hidden) {
    invalidate()
    choices.value = []
    rows.value = []
    status.value = null
    query.value = ''
    writing.value = false
    busy.value = false
  }
}
onMounted(() => {
  const seedKind = route?.query?.kind,
    seedID = route?.query?.source_id
  if (
    (seedKind === 'personal_document' || seedKind === 'community_thread') &&
    typeof seedID === 'string' &&
    /^[1-9][0-9]{0,18}$/.test(seedID)
  ) {
    kind.value = seedKind
    sourceID.value = seedID
    choices.value = [{ id: seedID, name: '所选来源（请核对当前版本）' }]
  }
  document.addEventListener('visibilitychange', visibility)
  void refresh()
})
onBeforeUnmount(() => {
  disposed = true
  invalidate()
  choices.value = []
  rows.value = []
  status.value = null
  query.value = ''
  document.removeEventListener('visibilitychange', visibility)
})
</script>
<style scoped>
.private-knowledge {
  max-width: 60rem;
  margin: 0 auto;
  padding: 1rem;
  overflow-wrap: anywhere;
}
.private-knowledge section {
  margin-top: 1.5rem;
}
.private-knowledge label {
  display: block;
  min-height: 44px;
  margin: 0.75rem 0;
}
.private-knowledge input:not([type='checkbox']),
.private-knowledge select {
  min-height: 44px;
  max-width: 100%;
  padding: 0.5rem;
  border: 1px solid #888;
  border-radius: 0.3rem;
}
.private-knowledge button {
  min-height: 44px;
  padding: 0.5rem 0.75rem;
  margin: 0.25rem;
  border: 1px solid #888;
  border-radius: 0.3rem;
}
.private-knowledge input[type='checkbox'] {
  width: 24px;
  height: 24px;
  vertical-align: middle;
  margin-right: 0.5rem;
}
.private-knowledge li {
  padding: 0.75rem 0;
  border-bottom: 1px solid #888;
}
.private-knowledge pre {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  font-family: inherit;
}
.private-knowledge [role='alert'] {
  color: #b42318;
}
</style>
