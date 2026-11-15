// @vitest-environment jsdom
import { beforeEach, expect, it, vi } from 'vitest'
import { loginRedirect } from '../src/modules/identity/loginRedirect'
import { ensureAccessToken, registerSessionRestorer, setAccessToken } from '../src/modules/identity/session'
import router, { webAuthGuard } from '../src/router'
beforeEach(() => {
  setAccessToken(null)
  registerSessionRestorer(async () => false)
})
it('waits for cookie restoration before deciding whether to display login', async () => {
  let finish!: (value: boolean) => void
  registerSessionRestorer(
    () =>
      new Promise((resolve) => {
        finish = resolve
      }),
  )
  let completed = false
  const guard = webAuthGuard({ path: '/login', meta: {} } as any, {} as any, vi.fn())
  void Promise.resolve(guard).then(() => {
    completed = true
  })
  await Promise.resolve()
  expect(completed).toBe(false)
  setAccessToken('restored')
  finish(true)
  expect(await guard).toEqual({ path: '/', replace: true })
})
it('uses verified restoration even if a handoff token is present', async () => {
  setAccessToken('unverified-handoff')
  const restore = vi.fn().mockResolvedValue(false)
  registerSessionRestorer(restore)
  expect(await ensureAccessToken()).toBe(false)
  expect(restore).toHaveBeenCalledTimes(1)
})
it('protects an extension deep link before its dynamic plugin route is registered', async () => {
  const target = router.resolve('/extensions/campusos.rss-publisher/ui')
  expect(target.meta.requiresAuth).toBe(true)
  expect(await webAuthGuard(target, {} as any, vi.fn())).toEqual({
    path: '/login',
    query: { redirect: target.fullPath },
  })
})
it('returns only safe internal paths after login and prevents loops/open redirects', () => {
  expect(loginRedirect('/extensions/campusos.rss-publisher/ui')).toBe('/extensions/campusos.rss-publisher/ui')
  expect(loginRedirect('/threads/create?draft=1')).toBe('/threads/create?draft=1')
  for (const value of [
    'https://other.test',
    '//other.test',
    '/\\other.test',
    '/login',
    '/login?redirect=/login',
    '/bad\r\nx',
    ['//other.test'],
    null,
  ])
    expect(loginRedirect(value)).toBe('/')
})
