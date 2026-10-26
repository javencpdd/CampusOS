// @vitest-environment jsdom
import { beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
const api = vi.hoisted(() => ({ me: vi.fn(), refresh: vi.fn() }))
vi.mock('../src/modules/identity/api', () => ({ authApi: api }))
import { useUserStore } from '../src/modules/identity/store'
import { setAccessToken } from '../src/modules/identity/session'
beforeEach(() => {
  vi.clearAllMocks()
  setAccessToken(null)
  setActivePinia(createPinia())
})
it('authenticates an in-memory preview handoff without rotating the parent refresh family', async () => {
  api.me.mockResolvedValue({ code: 0, data: { id: '1001', username: 'alice' } })
  const store = useUserStore()
  store.updateAccessToken('current-handoff')
  expect(await store.restore()).toBe(true)
  expect(store.user?.id).toBe('1001')
  expect(api.refresh).not.toHaveBeenCalled()
})
it('discards an old handoff identity response after a local token change', async () => {
  let release!: (v: unknown) => void
  api.me.mockReturnValue(
    new Promise((r) => {
      release = r
    }),
  )
  const store = useUserStore()
  store.updateAccessToken('alice')
  const pending = store.restore()
  store.updateAccessToken('bob')
  release({ code: 0, data: { id: '1001', username: 'alice' } })
  expect(await pending).toBe(false)
  expect(store.user).toBeNull()
  expect(store.token).toBe('bob')
})
