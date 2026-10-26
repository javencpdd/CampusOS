// @vitest-environment jsdom
import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, afterEach, expect, it, vi } from 'vitest'
import RAGAnswerPanel from '../src/modules/knowledge/components/RAGAnswerPanel.vue'
import { ragApi } from '../src/modules/knowledge/rag-api'
vi.mock('../src/modules/knowledge/rag-api', () => ({
  ragApi: { status: vi.fn(), prepare: vi.fn(), answer: vi.fn(), revoke: vi.fn(), publicAnswer: vi.fn() },
}))
vi.mock('../src/modules/knowledge/api', () => ({ knowledgeApi: { source: vi.fn() } }))
vi.mock('../src/modules/knowledge/private-api', () => ({ privateKnowledgeApi: { source: vi.fn() } }))
const model = {
  profile_id: 'remote-chat',
  profile_revision: '11',
  binding_revision: '12',
  definition: 'builtin.openai-compatible.text',
  model: 'owned-model',
  endpoint: 'https://owned.example.test/v1/chat/completions',
  system_prompt: 'Fixed host rules',
  max_tokens: 128,
}
const review = () => ({
  status: 'ready',
  approval_id: 'kr_' + 'a'.repeat(32),
  digest: 'b'.repeat(64),
  expires_at_ms: Date.now() + 120000,
  model,
  content: '{"question":"图书馆","evidence":[{"id":"E1","excerpt":"<img src=x onerror=boom()> 私人片段"}]}',
  request: {
    selection_id: 'kp_' + 'c'.repeat(32),
    revision: 'd'.repeat(64),
    question: '图书馆',
    run_id: 'ar_' + 'e'.repeat(32),
    binding_revision: '12',
  },
})
const result = { status: 'answered', answer: '<script>secret()</script>有证据回答', citations: [] }
const button = (w: any, name: string) => w.findAll('button').find((b: any) => b.text() === name)!
beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(ragApi.status).mockResolvedValue({ data: { model } } as any)
  vi.mocked(ragApi.prepare).mockResolvedValue({ data: review() } as any)
  vi.mocked(ragApi.answer).mockResolvedValue({ data: result } as any)
  vi.mocked(ragApi.revoke).mockResolvedValue({ data: { revoked: true } } as any)
  vi.mocked(ragApi.publicAnswer).mockResolvedValue({ data: result } as any)
})
afterEach(() => vi.restoreAllMocks())
async function prepared() {
  const w = mount(RAGAnswerPanel, { props: { selection: { id: 'kp_' + 'c'.repeat(32), revision: 'd'.repeat(64) } } })
  await w.get('input[aria-label="问答问题"]').setValue('图书馆')
  await button(w, '核对本次发送内容').trigger('click')
  await flushPromises()
  return w
}
it('displays exact private contents and destination without sending or prechecking consent', async () => {
  const w = await prepared()
  expect(ragApi.answer).not.toHaveBeenCalled()
  expect(w.text()).toContain(model.endpoint)
  expect(w.text()).toContain('私人片段')
  expect(w.find('img').exists()).toBe(false)
  expect((w.get('input[type=checkbox]').element as HTMLInputElement).checked).toBe(false)
  expect(button(w, '同意并发送本次').attributes('disabled')).toBeDefined()
  w.unmount()
})
it('requires distinct affirmative consent and treats the answer as plain text', async () => {
  const w = await prepared()
  await w.get('input[type=checkbox]').setValue(true)
  await button(w, '同意并发送本次').trigger('click')
  await flushPromises()
  expect(ragApi.answer).toHaveBeenCalledTimes(1)
  expect(w.text()).toContain(result.answer)
  expect(w.find('script').exists()).toBe(false)
  expect(w.find('input[type=checkbox]').exists()).toBe(false)
  w.unmount()
})
it('invalidates approval and plaintext when question or exact selection changes', async () => {
  const w = await prepared()
  await w.get('input[aria-label="问答问题"]').setValue('新问题')
  expect(w.text()).not.toContain('私人片段')
  expect(ragApi.revoke).toHaveBeenCalledWith(review().approval_id)
  expect(ragApi.answer).not.toHaveBeenCalled()
  await w.setProps({ selection: { id: 'kp_' + 'f'.repeat(32), revision: 'd'.repeat(64) } })
  expect(w.find('input[type=checkbox]').exists()).toBe(false)
  w.unmount()
})
it('does not send expired approvals', async () => {
  vi.mocked(ragApi.prepare).mockResolvedValue({ data: { ...review(), expires_at_ms: Date.now() - 1 } } as any)
  const w = await prepared()
  await w.get('input[type=checkbox]').setValue(true)
  await button(w, '同意并发送本次').trigger('click')
  await flushPromises()
  expect(ragApi.answer).not.toHaveBeenCalled()
  expect(w.text()).toContain('请重新核对')
  w.unmount()
})
it('discards late successful private replies after cancellation and revokes the ticket', async () => {
  let resolve!: (v: any) => void
  vi.mocked(ragApi.answer).mockImplementation(
    () =>
      new Promise((r) => {
        resolve = r
      }),
  )
  const w = await prepared()
  await w.get('input[type=checkbox]').setValue(true)
  await button(w, '同意并发送本次').trigger('click')
  await button(w, '取消并清空本次问答').trigger('click')
  resolve({ data: result })
  await flushPromises()
  expect(w.text()).not.toContain(result.answer)
  expect(ragApi.revoke).toHaveBeenCalled()
  w.unmount()
})
it('clears review and private output when the page becomes hidden', async () => {
  const w = await prepared()
  vi.spyOn(document, 'hidden', 'get').mockReturnValue(true)
  document.dispatchEvent(new Event('visibilitychange'))
  await flushPromises()
  expect(w.text()).not.toContain('私人片段')
  expect(w.find('input[type=checkbox]').exists()).toBe(false)
  expect(ragApi.revoke).toHaveBeenCalled()
  w.unmount()
})
it('uses the current binding and fresh nonce for public questions without private prepare', async () => {
  const w = mount(RAGAnswerPanel, { props: { collectionID: 'kc_' + 'a'.repeat(32) } })
  await w.get('input').setValue('校园卡')
  await button(w, '根据公开资料提问').trigger('click')
  await flushPromises()
  expect(ragApi.prepare).not.toHaveBeenCalled()
  expect(ragApi.publicAnswer.mock.calls[0]?.[0]).toMatchObject({
    collection_id: 'kc_' + 'a'.repeat(32),
    question: '校园卡',
    binding_revision: '12',
    step: 'chat',
  })
  expect(ragApi.publicAnswer.mock.calls[0]?.[0].run_id).toMatch(/^ar_[0-9a-f]{32}$/)
  w.unmount()
})
it('clears buffered answers on an execution failure', async () => {
  vi.mocked(ragApi.answer).mockRejectedValue(new Error('当前来源已变化'))
  const w = await prepared()
  await w.get('input[type=checkbox]').setValue(true)
  await button(w, '同意并发送本次').trigger('click')
  await flushPromises()
  expect(w.text()).toContain('当前来源已变化')
  expect(w.text()).not.toContain('私人片段')
  expect(w.text()).not.toContain(result.answer)
  w.unmount()
})
