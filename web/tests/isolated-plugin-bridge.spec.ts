import { describe, expect, it, vi } from 'vitest'
import {
  MAX_PLUGIN_RANGE_BYTES,
  PLUGIN_BRIDGE_VERSION,
  isMatchingReady,
  validatePluginBridgeRequest,
  validFrameIdentity,
  dispatchPluginBridgeRequest,
  createPluginBridgeSession,
  type PluginFrameIdentity,
} from '@/campus-ui/isolatedPluginBridge'

const identity: PluginFrameIdentity = {
  pluginKey: 'campusos.pdf-viewer',
  pluginVersion: '2.0.0-dev.1',
  surfaceID: 'preview',
  audience: 'user',
  origin: 'https://plugin.example.test',
}

describe('isolatedPluginBridge', () => {
  it('requires a distinct trustworthy plugin origin and matching ready identity', () => {
    expect(validFrameIdentity(identity, 'https://app.example.test')).toBe(true)
    expect(validFrameIdentity({ ...identity, origin: 'https://app.example.test' }, 'https://app.example.test')).toBe(
      false,
    )
    expect(
      isMatchingReady(
        {
          type: 'campusos.bridge.ready',
          version: PLUGIN_BRIDGE_VERSION,
          plugin_key: identity.pluginKey,
          plugin_version: identity.pluginVersion,
          surface_id: identity.surfaceID,
          audience: identity.audience,
        },
        identity,
      ),
    ).toBe(true)
    expect(
      isMatchingReady(
        { type: 'campusos.bridge.ready', version: PLUGIN_BRIDGE_VERSION, plugin_key: 'other.plugin' },
        identity,
      ),
    ).toBe(false)
  })

  it('rejects unknown bridge methods and range overreads', () => {
    expect(
      validatePluginBridgeRequest({
        version: PLUGIN_BRIDGE_VERSION,
        request_id: 'range-1',
        method: 'resource.readRange',
        params: { handle: 'host-issued-handle', offset: 0, length: MAX_PLUGIN_RANGE_BYTES },
      }),
    ).toBeDefined()
    expect(
      validatePluginBridgeRequest({
        version: PLUGIN_BRIDGE_VERSION,
        request_id: 'range-2',
        method: 'resource.readRange',
        params: { handle: 'host-issued-handle', offset: 0, length: MAX_PLUGIN_RANGE_BYTES + 1 },
      }),
    ).toBeUndefined()
    expect(
      validatePluginBridgeRequest({
        version: PLUGIN_BRIDGE_VERSION,
        request_id: 'file-1',
        method: 'filesystem.read',
        params: { path: '/etc/passwd' },
      }),
    ).toBeUndefined()
  })
})

it('discards late private results after port replacement, revocation and closure', async () => {
  for (const fence of ['replace', 'identity', 'close']) {
    const oldPort = { postMessage: vi.fn(), close: vi.fn(), onmessage: null },
      newPort = { postMessage: vi.fn(), close: vi.fn(), onmessage: null }
    const old = createPluginBridgeSession(identity, 'old', { port1: oldPort, port2: newPort })
    old.active = true
    let current = true,
      done!: (v: unknown) => void
    const execute = vi.fn(
      () =>
        new Promise((r) => {
          done = r
        }),
    )
    const pending = dispatchPluginBridgeRequest(
      old,
      { version: PLUGIN_BRIDGE_VERSION, request_id: 'one', method: 'resource.describe', params: { handle: 'current' } },
      () => current,
      execute,
      new Set(),
      { count: 0 },
    )
    if (fence === 'close') old.close()
    else current = false
    done({ private: 'old owner bytes' })
    await pending
    expect(execute).toHaveBeenCalledOnce()
    expect(oldPort.postMessage).not.toHaveBeenCalled()
    expect(newPort.postMessage).not.toHaveBeenCalled()
  }
})
it('rejects duplicate request IDs, surplus fields and excessive concurrent requests before dispatch', async () => {
  const port = { postMessage: vi.fn(), close: vi.fn(), onmessage: null },
    session = createPluginBridgeSession(identity, 'challenge', { port1: port, port2: port })
  session.active = true
  const execute = vi.fn().mockResolvedValue({ size: 10 }),
    seen = new Set<string>(),
    budget = { count: 0 }
  const request = {
    version: PLUGIN_BRIDGE_VERSION,
    request_id: 'one',
    method: 'resource.describe',
    params: { handle: 'current' },
  }
  await dispatchPluginBridgeRequest(session, request, () => true, execute, seen, budget)
  await dispatchPluginBridgeRequest(session, request, () => true, execute, seen, budget)
  await dispatchPluginBridgeRequest(
    session,
    { ...request, request_id: 'two', params: { handle: 'current', url: 'https://attacker.test' } },
    () => true,
    execute,
    seen,
    budget,
  )
  budget.count = 32
  await dispatchPluginBridgeRequest(session, { ...request, request_id: 'three' }, () => true, execute, seen, budget)
  expect(execute).toHaveBeenCalledOnce()
  expect(port.postMessage.mock.calls.slice(1).map(([r]) => r.request_id)).toEqual(['one', 'two', 'three'])
  expect(port.postMessage.mock.calls.slice(1).every(([r]) => r.error?.code === 'bridge.invalid_request')).toBe(true)
})
