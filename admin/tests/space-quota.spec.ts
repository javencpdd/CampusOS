// @vitest-environment jsdom
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import UserManageView from '../src/modules/identity/pages/UserManageView.vue'

const { api, messages } = vi.hoisted(() => ({
  api: { storage: vi.fn(), setStorageQuota: vi.fn(), users: vi.fn(), roles: vi.fn(), userRoles: vi.fn(), categories: vi.fn() },
  messages: { success: vi.fn(), error: vi.fn(), warning: vi.fn() },
}))
vi.mock('../src/modules/integrations/api', () => ({ spaceAdminApi: api }))
vi.mock('../src/modules/identity/api', () => ({ userApi: { list: api.users }, roleApi: { list: api.roles, getUserRoles: api.userRoles }, moderationApi: {} }))
vi.mock('../src/modules/community/api', () => ({ categoryApi: { list: api.categories } }))
vi.mock('../src/modules/identity/store', () => ({ useAdminStore: () => ({ isAdmin: true }) }))
vi.mock('element-plus', () => ({ ElMessage: messages }))

const user = (id = '1001') => ({ id, username: `owner-${id}`, nickname: `用户${id}` })
const status = (id = '1001', quota = 50 * 1048576) => ({ code: 0, data: { user_id: id, quota_bytes: quota, used_bytes: 1024, available_bytes: quota - 1024, custom_quota: quota !== 50 * 1048576 } })
const box = { template: '<div><slot name="header"/><slot/><slot name="footer"/></div>' }
const stubs = Object.fromEntries(['el-card', 'el-table', 'el-dialog', 'el-descriptions', 'el-descriptions-item', 'el-form', 'el-form-item', 'el-alert', 'el-tag', 'el-popconfirm', 'el-icon'].map((name) => [name, box]))
Object.assign(stubs, { 'el-table-column': true, 'el-input': true, 'el-input-number': true, 'el-pagination': true, 'el-select': true, 'el-option': true, 'el-button': { template: '<button @click="$emit(\'click\')"><slot/></button>' } })
const deferred = () => { let resolve!: (value: any) => void; const promise = new Promise((r) => { resolve = r }); return { promise, resolve } }
const open = async () => {
  const wrapper = mount(UserManageView, { global: { stubs, directives: { loading: {} } } })
  await flushPromises()
  const view = wrapper.vm as any
  await view.openStorageDialog(user())
  return { wrapper, view }
}

describe('Admin current storage quota', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.storage.mockReset().mockResolvedValue(status())
    api.setStorageQuota.mockReset().mockResolvedValue({ code: 0, data: { owner_id: '1001', quota_bytes: 2 * 1048576 } })
    api.users.mockResolvedValue({ data: { items: [], pagination: { total: 0 } } })
    api.roles.mockResolvedValue({ data: [] })
    api.userRoles.mockResolvedValue({ data: [] })
    api.categories.mockResolvedValue({ data: [] })
  })

  it('reads current storage after the safe quota ACK', async () => {
    const { view } = await open()
    api.storage.mockResolvedValueOnce(status('1001', 2 * 1048576))
    view.storageQuotaMB = 2
    await view.saveStorageQuota()
    expect(api.setStorageQuota).toHaveBeenCalledWith('1001', 2 * 1048576)
    expect(api.storage).toHaveBeenCalledTimes(2)
    expect(view.storageStatus.user_id).toBe('1001')
    expect(view.storageStatus.used_bytes).toBe(1024)
    expect(view.storageQuotaMB).toBe(2)
    expect(messages.success).toHaveBeenCalledOnce()
  })

  it('clears old metadata and retains the draft when the current read is denied', async () => {
    const { view } = await open()
    view.storageQuotaMB = 2
    api.storage.mockRejectedValueOnce({ msg: '管理权限已撤销' })
    await view.saveStorageQuota()
    expect(view.storageStatus).toBeNull()
    expect(view.storageQuotaMB).toBe(2)
    expect(messages.error).toHaveBeenCalledWith('管理权限已撤销')
    expect(messages.success).not.toHaveBeenCalled()
  })

  it('rejects an ACK for another owner before reading storage', async () => {
    const { view } = await open()
    view.storageQuotaMB = 2
    api.setStorageQuota.mockResolvedValueOnce({ code: 0, data: { owner_id: '1002', quota_bytes: 2 * 1048576 } })
    await view.saveStorageQuota()
    expect(api.storage).toHaveBeenCalledTimes(1)
    expect(view.storageStatus).toBeNull()
    expect(messages.success).not.toHaveBeenCalled()
  })

  it('clears checked metadata after a rejected write', async () => {
    const { view } = await open()
    view.storageQuotaMB = 2
    api.setStorageQuota.mockRejectedValueOnce({ msg: '请重新验证 MFA' })
    await view.saveStorageQuota()
    expect(view.storageStatus).toBeNull()
    expect(view.storageQuotaMB).toBe(2)
    expect(messages.error).toHaveBeenCalledWith('请重新验证 MFA')
  })

  it('keeps the selected owner when an earlier dialog read finishes later', async () => {
    const { view } = await open()
    const first = deferred()
    api.storage.mockImplementationOnce(() => first.promise).mockResolvedValueOnce(status('1002', 4 * 1048576))
    const pending = view.openStorageDialog(user('1001'))
    await view.openStorageDialog(user('1002'))
    first.resolve(status('1001'))
    await pending
    expect(view.storageSelectedUser.id).toBe('1002')
    expect(view.storageStatus.user_id).toBe('1002')
  })

  it('keeps another owner selected while an earlier quota command completes', async () => {
    const { view } = await open()
    const ack = deferred()
    api.setStorageQuota.mockImplementationOnce(() => ack.promise)
    view.storageQuotaMB = 2
    const pending = view.saveStorageQuota()
    api.storage.mockResolvedValueOnce(status('1002', 4 * 1048576))
    await view.openStorageDialog(user('1002'))
    ack.resolve({ code: 0, data: { owner_id: '1001', quota_bytes: 2 * 1048576 } })
    await pending
    expect(view.storageSelectedUser.id).toBe('1002')
    expect(view.storageStatus.user_id).toBe('1002')
    expect(messages.success).not.toHaveBeenCalled()
  })
})
