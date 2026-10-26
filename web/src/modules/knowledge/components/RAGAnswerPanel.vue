<template>
  <section class="rag-panel" aria-label="有据问答">
    <h2>有据问答</h2>
    <p>答案仅依据当前资料，可能拒答。模型输出不代表官方结论。</p>
    <p v-if="selection">私人索引同意不包含模型发送同意；请单独核对本次完整内容和目标。</p>
    <label>问题<input v-model="question" aria-label="问答问题" autocomplete="off" maxlength="1024" /></label>
    <button :disabled="busy || !question.trim() || (!collectionID && !selection)" @click="prepareOrAsk">
      {{ selection ? '核对本次发送内容' : '根据公开资料提问' }}
    </button>
    <button v-if="busy || review || output" @click="cancel">取消并清空本次问答</button>
    <p v-if="busy" role="status">正在处理本次问答…</p>
    <p v-if="error" role="alert">{{ error }}</p>
    <p v-if="model">本次模型：{{ model.model }} · {{ model.endpoint }} · 仅 Chat 步骤</p>
    <div v-if="review" class="rag-review">
      <h3>本次独立发送核对</h3>
      <p>目标：{{ review.model.model }} · {{ review.model.endpoint }}</p>
      <p>有效至 {{ new Date(review.expires_at_ms).toLocaleTimeString() }}；同意仅限本会话、本问题和本次片段。</p>
      <pre class="rag-text">{{ review.content }}</pre>
      <details>
        <summary>查看固定系统提示与输出上限</summary>
        <pre class="rag-text">{{ review.model.system_prompt }}</pre>
        <p>输出上限 {{ review.model.max_tokens }} token</p>
      </details>
      <label
        ><input
          v-model="consented"
          type="checkbox"
          :disabled="busy"
        />我已核对上述完整内容和目标，同意本次向该模型发送</label
      >
      <button :disabled="busy || !consented" @click="sendApproved">同意并发送本次</button>
    </div>
    <div v-if="output" aria-label="问答结果">
      <p v-if="output.status === 'refused'" role="status">当前资料不足，模型未提供答案。</p>
      <pre v-else class="rag-text">{{ output.answer }}</pre>
      <ol>
        <li v-for="citation in output.citations" :key="citation.id">
          <p>{{ citation.title }}</p>
          <p>{{ citation.excerpt }}</p>
          <button @click="openCitation(citation)">复核当前问答引用</button>
        </li>
      </ol>
    </div>
    <pre v-if="sourceText" class="rag-text" aria-label="问答当前引用">{{ sourceText }}</pre>
  </section>
</template>
<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { ragApi, type RAGModel, type RAGReview, type RAGAnswer, type RAGRequest } from '../rag-api'
import { knowledgeApi } from '../api'
import { privateKnowledgeApi } from '../private-api'

const props = defineProps<{ collectionID?: string; selection?: { id: string; revision: string } | null }>()
const question = ref(''),
  consented = ref(false),
  busy = ref(false),
  error = ref(''),
  sourceText = ref('')
const review = ref<RAGReview | null>(null),
  output = ref<RAGAnswer | null>(null),
  model = ref<RAGModel | null>(null)
let generation = 0,
  disposed = false,
  controller: AbortController | undefined
function clear(revoke = true) {
  generation++
  controller?.abort()
  const id = review.value?.approval_id
  review.value = null
  output.value = null
  consented.value = false
  sourceText.value = ''
  busy.value = false
  if (revoke && id) void ragApi.revoke(id).catch(() => {})
}
function cancel() {
  clear()
  error.value = ''
}
watch([question, () => props.collectionID, () => props.selection?.id, () => props.selection?.revision], cancel, {
  flush: 'sync',
})
async function run(action: (signal: AbortSignal, current: () => boolean) => Promise<void>) {
  controller?.abort()
  const mine = ++generation
  controller = new AbortController()
  busy.value = true
  error.value = ''
  const current = () => !disposed && mine === generation
  try {
    await action(controller.signal, current)
  } catch (e) {
    if (current()) {
      clear()
      error.value = e instanceof Error ? e.message : '问答未完成，请重新核对当前资料和模型。'
    }
  } finally {
    if (current()) busy.value = false
  }
}
async function prepareOrAsk() {
  clear()
  await run(async (signal, current) => {
    const status: any = await ragApi.status(signal)
    if (!current()) return
    model.value = status.data.model
    const body: RAGRequest = {
      question: question.value,
      run_id:
        'ar_' +
        Array.from(crypto.getRandomValues(new Uint8Array(16)), (byte) => byte.toString(16).padStart(2, '0')).join(''),
      binding_revision: status.data.model.binding_revision,
      step: 'chat',
    }
    if (props.selection) {
      body.selection_id = props.selection.id
      body.revision = props.selection.revision
      const result: any = await ragApi.prepare(body, signal)
      if (!current()) return
      if (result.data.status === 'refused') output.value = { status: 'refused', answer: '', citations: [] }
      else {
        review.value = result.data
        consented.value = false
      }
    } else {
      body.collection_id = props.collectionID
      const result: any = await ragApi.publicAnswer(body, signal)
      if (current()) output.value = result.data
    }
  })
}
async function sendApproved() {
  const prepared = review.value
  if (!prepared || !consented.value || prepared.expires_at_ms <= Date.now()) {
    clear()
    error.value = '请重新核对本次发送内容。'
    return
  }
  await run(async (signal, current) => {
    const result: any = await ragApi.answer(prepared, signal)
    if (current()) {
      review.value = null
      consented.value = false
      output.value = result.data
    }
  })
}
async function openCitation(citation: RAGAnswer['citations'][number]) {
  sourceText.value = ''
  await run(async (signal, current) => {
    if (citation.citation) {
      const result = await knowledgeApi.source(citation.citation, signal)
      if (current()) sourceText.value = result.source.content
    } else if (citation.selection_id && citation.revision) {
      const result: any = await privateKnowledgeApi.source(
        { id: citation.selection_id, revision: citation.revision } as any,
        signal,
      )
      if (current()) sourceText.value = result.data.text
    }
  })
}
function hide() {
  if (document.hidden) clear()
}
onMounted(() => document.addEventListener('visibilitychange', hide))
onBeforeUnmount(() => {
  disposed = true
  clear()
  document.removeEventListener('visibilitychange', hide)
})
</script>
<style scoped>
.rag-panel {
  border: 1px solid #cbd5e1;
  border-radius: 8px;
  padding: 1rem;
  margin-top: 1rem;
}
.rag-panel button {
  margin: 0.5rem;
}
.rag-panel input:not([type='checkbox']) {
  display: block;
  width: min(100%, 36rem);
}
.rag-text {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  max-width: 100%;
}
.rag-review {
  border: 1px solid #94a3b8;
  padding: 0.75rem;
}
</style>
