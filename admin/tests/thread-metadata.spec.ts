// @vitest-environment jsdom
import { flushPromises, shallowMount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { h } from 'vue'
const { list, trash, history, permissionsPreview, error, warning } = vi.hoisted(() => ({ list: vi.fn(), trash: vi.fn(), history: vi.fn(), permissionsPreview: vi.fn(), error: vi.fn(), warning: vi.fn() }))
vi.mock('../src/modules/community/api', () => ({ threadApi: { list, trash, moderationActions: history, permissionsPreview }, categoryApi: { list: vi.fn().mockResolvedValue({ data: [] }) } }))
vi.mock('element-plus', () => ({ ElMessage: { error, warning }, ElMessageBox: {} }))
import ThreadManageView from '../src/modules/community/pages/ThreadManageView.vue'
const row = { id: '2581001', author_id: '2581002', category_id: '2581003', publication_status: 'private', moderation_status: 'pending', deletion_status: 'active' }
const data = (items: unknown[]) => ({ data: { items, pagination: { total: items.length } } })
const mount = () => shallowMount(ThreadManageView, { global: { directives: { loading: {} }, renderStubDefaultSlot: true, stubs: { 'el-table-column': { render: () => h('el-table-column-stub') }, 'el-alert': true, 'el-table': { methods: { clearSelection() {} }, render() { return h('el-table-stub', this.$slots.default?.()) } }, 'el-tag': true, 'el-tooltip': true, 'el-popconfirm': true, 'el-select': true, 'el-option': true, 'el-icon': true, 'el-button': true, 'el-input': true, 'el-dialog': true, 'el-empty': true, 'el-segmented': true, 'el-pagination': true } } })
describe('checked Admin thread metadata', () => {
 beforeEach(() => { vi.clearAllMocks(); permissionsPreview.mockImplementation(async (ids: string[]) => ({ data: { items: ids.map(id => ({ id, permissions: Object.fromEntries(['pin', 'unpin', 'lock', 'unlock', 'approve', 'reject', 'take_down', 'direct_restore', 'trash', 'restore_trash', 'purge'].map(action => [action, false])) })) } })); list.mockResolvedValue(data([row])); trash.mockResolvedValue(data([])); history.mockResolvedValue(data([])) })
 it('renders only management metadata columns and searches by ID', async () => {
  list.mockResolvedValue(data([{ ...row, title: 'PRIVATE_TITLE', content: 'PRIVATE_BODY' }]))
  const w = mount(); await flushPromises()
  const props = w.findAll('el-table-column-stub').map(c => c.attributes('prop'))
  for (const key of ['title', 'content', 'author_name', 'reason', 'before_state', 'after_state', 'actor_id']) expect(props).not.toContain(key)
  expect(props).toContain('author_id'); expect(props).toContain('actor_ref_id')
  expect(w.text()).not.toContain('PRIVATE_')
  expect(w.find('el-input-stub').attributes('placeholder')).toContain('ID')
  w.unmount()
 })
 it('clears stale rows and counts when current read is denied', async () => {
  const w = mount(); await flushPromises(); const view = w.vm as any
  expect(view.threads).toHaveLength(1)
  list.mockRejectedValueOnce(new Error('permission revoked')); await view.load()
  expect(view.threads).toEqual([]); expect(view.total).toBe(0); expect(error).toHaveBeenCalled(); w.unmount()
 })
 it('discards an old list after a newer filter result', async () => {
  let old!: (v: unknown) => void; list.mockReturnValueOnce(new Promise(resolve => { old = resolve }))
  const w = mount(); const view = w.vm as any; view.searchKeyword = '2581999'
  const latest = { ...row, id: '2581999' }; list.mockResolvedValueOnce(data([latest])); await view.load(); old(data([row])); await flushPromises()
  expect(view.threads).toEqual([latest]); expect(view.loading).toBe(false); w.unmount()
 })
 it('keeps the selected history when an earlier thread finishes late', async () => {
  const w = mount(); await flushPromises(); const view = w.vm as any
  let old!: (v: unknown) => void; history.mockReturnValueOnce(new Promise(resolve => { old = resolve }))
  const pending = view.showActions(row); const latest = { id: '2', thread_id: '2581999', actor_kind: 'admin', actor_ref_id: 'opaque.admin', action: 'pin' }
  history.mockResolvedValueOnce(data([latest])); await view.showActions({ ...row, id: '2581999' }); old(data([{ ...latest, id: '1', thread_id: row.id }])); await pending
  expect(view.moderationActions).toEqual([latest]); expect(history).toHaveBeenLastCalledWith('2581999', { page: 1, page_size: 20 }); w.unmount()
 })

 it('uses exact server permissions for selected batch actions', async () => {
  const flags = Object.fromEntries(['pin', 'unpin', 'lock', 'unlock', 'approve', 'reject', 'take_down', 'direct_restore', 'trash', 'restore_trash', 'purge'].map(action => [action, action === 'pin']))
  permissionsPreview.mockResolvedValueOnce({ data: { items: [{ id: row.id, permissions: flags }] } })
  const w = mount(); await flushPromises(); const view = w.vm as any; view.selectedRows = [row]
  expect(permissionsPreview).toHaveBeenCalledWith([row.id]); expect(view.hasPermission(row, 'pin')).toBe(true)
  expect(view.batchActionOptions.map((item: any) => item.value)).toEqual(['pin']); w.unmount()
 })
 it('clears current metadata and operation flags after a denied preview', async () => {
  const w = mount(); await flushPromises(); const view = w.vm as any
  permissionsPreview.mockRejectedValueOnce(new Error('permission revoked')); await view.load()
  expect(view.threads).toEqual([]); expect(view.permissionMap).toEqual({}); expect(view.total).toBe(0); w.unmount()
 })
 it('discards permissions returned for a previous list request', async () => {
  let old!: (v: unknown) => void; permissionsPreview.mockReturnValueOnce(new Promise(resolve => { old = resolve }))
  const w = mount(); await flushPromises(); const view = w.vm as any; const latest = { ...row, id: '2581999' }
  list.mockResolvedValueOnce(data([latest])); await view.load()
  const flags = Object.fromEntries(['pin', 'unpin', 'lock', 'unlock', 'approve', 'reject', 'take_down', 'direct_restore', 'trash', 'restore_trash', 'purge'].map(action => [action, true]))
  old({ data: { items: [{ id: row.id, permissions: flags }] } }); await flushPromises()
  expect(view.threads).toEqual([latest]); expect(Object.keys(view.permissionMap)).toEqual([latest.id]); expect(view.hasPermission(latest, 'pin')).toBe(false); w.unmount()
 })
 it('rejects mismatched previews and prevents unapproved batch execution', async () => {
  permissionsPreview.mockResolvedValueOnce({ data: { items: [{ id: 'other', permissions: {} }] } })
  const w = mount(); await flushPromises(); const view = w.vm as any
  expect(view.threads).toEqual([]); expect(view.permissionMap).toEqual({}); view.selectedRows = [row]
  await view.runBatchAction('pin'); expect(warning).toHaveBeenCalled(); w.unmount()
 })
})
