// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
const fixture = vi.hoisted(() => ({
  api: { me: vi.fn(), refresh: vi.fn(), login: vi.fn(), logout: vi.fn() },
  peer: vi.fn(),
}))
vi.mock('../src/modules/identity/api', () => ({ authApi: fixture.api }))
vi.mock('../src/modules/identity/sessionCoordination', () => ({
  withSessionLock: (action: () => Promise<unknown>) => action(),
  peerAccessToken: fixture.peer,
  announceSessionChange: vi.fn(),
  shareCurrentSession: () => vi.fn(),
}))
import { useUserStore } from '../src/modules/identity/store'
import { getAccessToken, recoverAccessToken, setAccessToken } from '../src/modules/identity/session'
let store: ReturnType<typeof useUserStore>
const alice = { id: '1001', username: 'alice' }
const bob = { id: '1002', username: 'bob' }
beforeEach(() => {
  vi.clearAllMocks()
  setAccessToken(null)
  setActivePinia(createPinia())
  fixture.peer.mockResolvedValue(null)
  fixture.api.login.mockResolvedValue({ code: 0, data: { user: alice, access_token: 'alice-old' } })
  fixture.api.refresh.mockResolvedValue({ code: 0, data: { access_token: 'alice-new' } })
  fixture.api.me.mockResolvedValue({ code: 0, data: alice })
  fixture.api.logout.mockResolvedValue({ code: 0 })
  store = useUserStore()
})
afterEach(() => store.$dispose())
it('verifies a renewed token and shares one refresh across failed requests', async () => {
  await store.login('alice@example.test', 'owned')
  expect(await Promise.all([recoverAccessToken(), recoverAccessToken()])).toEqual([true, true])
  expect(fixture.api.refresh).toHaveBeenCalledTimes(1)
  expect(fixture.api.me).toHaveBeenCalledWith('alice-new')
  expect(store.user?.id).toBe(alice.id)
  expect(getAccessToken()).toBe('alice-new')
})
it('does not revive an account after logout when an older refresh completes late', async () => {
  await store.login('alice@example.test', 'owned')
  let finish!: (value: unknown) => void
  fixture.api.refresh.mockReturnValue(
    new Promise((resolve) => {
      finish = resolve
    }),
  )
  const pending = recoverAccessToken()
  await vi.waitFor(() => expect(fixture.api.refresh).toHaveBeenCalled())
  await store.logout()
  finish({ code: 0, data: { access_token: 'late-alice' } })
  expect(await pending).toBe(false)
  expect(store.isLoggedIn).toBe(false)
  expect(getAccessToken()).toBeNull()
  expect(await store.restore()).toBe(false)
})
it('does not overwrite a newer login with an older refresh response', async () => {
  await store.login('alice@example.test', 'owned')
  let finish!: (value: unknown) => void
  fixture.api.refresh.mockReturnValue(
    new Promise((resolve) => {
      finish = resolve
    }),
  )
  const pending = recoverAccessToken()
  await vi.waitFor(() => expect(fixture.api.refresh).toHaveBeenCalled())
  fixture.api.login.mockResolvedValue({ code: 0, data: { user: bob, access_token: 'bob-current' } })
  await store.login('bob@example.test', 'owned')
  finish({ code: 0, data: { access_token: 'late-alice' } })
  expect(await pending).toBe(false)
  expect(store.user?.id).toBe(bob.id)
  expect(getAccessToken()).toBe('bob-current')
})
it('keeps login on service errors but clears a confirmed invalid refresh session', async () => {
  await store.login('alice@example.test', 'owned')
  fixture.api.refresh.mockRejectedValue({ status: 503 })
  await expect(recoverAccessToken()).rejects.toMatchObject({ status: 503 })
  expect(getAccessToken()).toBe('alice-old')
  fixture.api.refresh.mockRejectedValue({ status: 401 })
  await expect(recoverAccessToken()).rejects.toMatchObject({ status: 401 })
  expect(store.isLoggedIn).toBe(false)
  expect(getAccessToken()).toBeNull()
})
it('reuses a same-origin peer token only after server verification without rotating cookies', async () => {
  fixture.peer.mockResolvedValue('peer-token')
  expect(await store.restore()).toBe(true)
  expect(fixture.api.me).toHaveBeenCalledWith('peer-token')
  expect(fixture.api.refresh).not.toHaveBeenCalled()
})
it('never adopts a peer or cookie token for another account during a pending user request', async () => {
  await store.login('alice@example.test', 'owned')
  fixture.peer.mockResolvedValue('bob-token')
  fixture.api.me.mockResolvedValue({ code: 0, data: bob })
  expect(await recoverAccessToken()).toBe(false)
  expect(store.user).toBeNull()
  expect(getAccessToken()).toBeNull()
  expect(fixture.api.refresh).not.toHaveBeenCalled()
})
