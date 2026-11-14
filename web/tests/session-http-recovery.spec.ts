// @vitest-environment jsdom
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import api from '../src/shared/api/client'
import {
  clearAccessToken,
  getAccessToken,
  registerSessionRecovery,
  setAccessToken,
} from '../src/modules/identity/session'

const expired = vi.fn()
const recover = vi.fn()
const unauthorized = (config: InternalAxiosRequestConfig) =>
  new AxiosError(
    'unauthorized',
    'ERR_BAD_REQUEST',
    config,
    {},
    {
      status: 401,
      statusText: 'Unauthorized',
      data: { code: 401, msg: 'auth required' },
      headers: {},
      config,
    },
  )
beforeEach(() => {
  vi.clearAllMocks()
  setAccessToken('expired')
  window.addEventListener('campusos:identity-session-expired', expired)
  registerSessionRecovery(recover, clearAccessToken)
  recover.mockImplementation(async () => {
    setAccessToken('renewed', true)
    return true
  })
})
afterEach(() => window.removeEventListener('campusos:identity-session-expired', expired))
it('renews once and replays an expired read without redirecting or clearing identity', async () => {
  const adapter = vi.fn(async (config) => {
    if (config.headers.Authorization === 'Bearer expired') throw unauthorized(config)
    return { status: 200, data: { code: 0, data: 'ok' }, headers: {}, statusText: 'OK', config }
  })
  api.defaults.adapter = adapter
  expect(await api.get('/owned-read')).toEqual({ code: 0, data: 'ok' })
  expect(recover).toHaveBeenCalledTimes(1)
  expect(adapter).toHaveBeenCalledTimes(2)
  expect(getAccessToken()).toBe('renewed')
  expect(expired).not.toHaveBeenCalled()
})
it('shares one recovery across concurrent failed reads', async () => {
  let finish!: () => void
  recover.mockImplementation(
    () =>
      new Promise<boolean>((resolve) => {
        finish = () => {
          setAccessToken('renewed', true)
          resolve(true)
        }
      }),
  )
  api.defaults.adapter = async (config) => {
    if (config.headers.Authorization === 'Bearer expired') throw unauthorized(config)
    return { status: 200, data: { code: 0 }, headers: {}, statusText: 'OK', config }
  }
  const one = api.get('/one'),
    two = api.get('/two')
  await vi.waitFor(() => expect(recover).toHaveBeenCalledTimes(1))
  finish()
  await Promise.all([one, two])
  expect(expired).not.toHaveBeenCalled()
})
it('does not replay writes after recovering credentials', async () => {
  const adapter = vi.fn(async (config) => {
    throw unauthorized(config)
  })
  api.defaults.adapter = adapter
  await expect(api.post('/owned-command', { value: 1 })).rejects.toMatchObject({ status: 401 })
  expect(adapter).toHaveBeenCalledTimes(1)
  expect(getAccessToken()).toBe('renewed')
  expect(expired).not.toHaveBeenCalled()
})
it('ignores stale failures after a new login instead of retrying them as another account', async () => {
  let reject!: (reason: unknown) => void
  let sent!: InternalAxiosRequestConfig
  api.defaults.adapter = (config) => {
    sent = config
    return new Promise((_resolve, fail) => {
      reject = fail
    })
  }
  const pending = api.get('/alice-private-read')
  const checked = expect(pending).rejects.toMatchObject({ status: 401 })
  await vi.waitFor(() => expect(sent).toBeTruthy())
  setAccessToken('bob')
  reject(unauthorized(sent))
  await checked
  expect(getAccessToken()).toBe('bob')
  expect(recover).not.toHaveBeenCalled()
  expect(expired).not.toHaveBeenCalled()
})
it('cancels a queued replay if the identity changes before it is sent', async () => {
  const adapter = vi.fn(async (config) => {
    throw unauthorized(config)
  })
  api.defaults.adapter = adapter
  const interceptor = api.interceptors.request.use((config) => {
    if (config.sessionRetried) setAccessToken('bob')
    return config
  })
  try {
    await expect(api.get('/alice-private-read')).rejects.toMatchObject({ status: 0 })
    expect(adapter).toHaveBeenCalledTimes(1)
    expect(getAccessToken()).toBe('bob')
    expect(expired).not.toHaveBeenCalled()
  } finally {
    api.interceptors.request.eject(interceptor)
  }
})
it('keeps identity on transient recovery errors and terminates confirmed invalid sessions', async () => {
  api.defaults.adapter = async (config) => {
    throw unauthorized(config)
  }
  recover.mockRejectedValue({ status: 503 })
  await expect(api.get('/one')).rejects.toMatchObject({ status: 401 })
  expect(getAccessToken()).toBe('expired')
  expect(expired).not.toHaveBeenCalled()
  recover.mockResolvedValue(false)
  await expect(api.get('/two')).rejects.toMatchObject({ status: 401 })
  expect(getAccessToken()).toBeNull()
  expect(expired).toHaveBeenCalledTimes(1)
})
it('keeps bootstrap/probe requests out of recovery recursion and bounds replays', async () => {
  const adapter = vi.fn(async (config) => {
    throw unauthorized(config)
  })
  api.defaults.adapter = adapter
  await expect(api.post('/auth/login')).rejects.toMatchObject({ status: 401 })
  await expect(api.get('/auth/me', { sessionRecovery: false })).rejects.toMatchObject({ status: 401 })
  expect(recover).not.toHaveBeenCalled()
  await expect(api.get('/owned-read')).rejects.toMatchObject({ status: 401 })
  expect(adapter).toHaveBeenCalledTimes(4)
  expect(recover).toHaveBeenCalledTimes(1)
  expect(expired).not.toHaveBeenCalled()
})
it('does not redirect anonymous public requests on 401', async () => {
  clearAccessToken()
  api.defaults.adapter = async (config) => {
    throw unauthorized(config)
  }
  await expect(api.get('/public-read', { headers: new AxiosHeaders() })).rejects.toMatchObject({ status: 401 })
  expect(recover).not.toHaveBeenCalled()
  expect(expired).not.toHaveBeenCalled()
})
