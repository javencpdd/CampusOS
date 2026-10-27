import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { bridgeVersion, PluginBridgeClient } from './bridge'

const identity = {
  pluginKey: 'campusos.pdf-viewer',
  pluginVersion: '3.0.0-dev.4',
  surfaceID: 'preview',
  audience: 'user' as const,
}
const hostOrigin = 'http://localhost:3000'

class Port {
  onmessage?: (event: { data: unknown }) => void
  start = vi.fn()
  postMessage = vi.fn()
  close = vi.fn()
}

function setup() {
  const listeners = new Set<(event: unknown) => void>()
  const parent = { postMessage: vi.fn() }
  vi.stubGlobal('window', {
    parent,
    setTimeout,
    clearTimeout,
    addEventListener: (_: string, listener: (event: unknown) => void) => listeners.add(listener),
    removeEventListener: (_: string, listener: (event: unknown) => void) => listeners.delete(listener),
  })
  const client = new PluginBridgeClient(identity, hostOrigin)
  const connect = (port: Port, overrides: Record<string, unknown> = {}) => {
    const event = {
      origin: hostOrigin,
      source: parent,
      ports: [port],
      data: {
        type: 'campusos.bridge.connect',
        version: bridgeVersion,
        challenge: 'challenge',
        plugin_key: identity.pluginKey,
        plugin_version: identity.pluginVersion,
        surface_id: identity.surfaceID,
        audience: identity.audience,
      },
      ...overrides,
    }
    for (const listener of [...listeners]) listener(event)
  }
  return { client, listeners, parent, connect }
}

beforeEach(() => {
  vi.useFakeTimers()
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('PDF plugin bridge', () => {
  it('requires the exact host and identity before accepting the transferred port', async () => {
    const { client, parent, connect, listeners } = setup()
    const pending = client.connect()
    expect(parent.postMessage).toHaveBeenCalledWith(
      expect.objectContaining({ type: 'campusos.bridge.ready' }),
      hostOrigin,
    )
    const port = new Port()
    connect(port, { origin: 'http://other.invalid' })
    connect(port, { source: {} })
    connect(port, {
      data: {
        type: 'campusos.bridge.connect',
        version: bridgeVersion,
        plugin_key: 'other',
      },
    })
    expect(port.start).not.toHaveBeenCalled()
    connect(port)
    await pending
    expect(listeners.size).toBe(0)
    expect(port.postMessage).toHaveBeenCalledWith({
      type: 'campusos.bridge.handshake',
      version: bridgeVersion,
      challenge: 'challenge',
    })
    expect(vi.getTimerCount()).toBe(0)
    client.close()
  })

  it('correlates responses and rejects pending requests when closed', async () => {
    const { client, connect } = setup()
    const connecting = client.connect()
    const port = new Port()
    connect(port)
    await connecting
    const result = client.request('resource.describe', { handle: 'opaque' })
    port.onmessage?.({
      data: { version: 'wrong', request_id: 'pdf-1', result: 'ignored' },
    })
    port.onmessage?.({
      data: {
        version: bridgeVersion,
        request_id: 'pdf-1',
        result: { size: 10 },
      },
    })
    await expect(result).resolves.toEqual({ size: 10 })
    const unfinished = client.request('resource.readRange', {})
    const rejected = expect(unfinished).rejects.toThrow('插件连接已关闭')
    client.close()
    await rejected
    expect(port.close).toHaveBeenCalledOnce()
    await expect(client.request('resource.describe', {})).rejects.toThrow('插件尚未连接宿主')
  })

  it('removes the handshake listener after timeout and rejects late connections', async () => {
    const { client, connect, listeners } = setup()
    const pending = expect(client.connect()).rejects.toThrow('连接宿主超时')
    await vi.advanceTimersByTimeAsync(10_000)
    await pending
    expect(listeners.size).toBe(0)
    const late = new Port()
    connect(late)
    expect(late.start).not.toHaveBeenCalled()
    await expect(client.request('resource.describe', {})).rejects.toThrow('插件尚未连接宿主')
  })

  it('cancels an unfinished handshake when the surface closes', async () => {
    const { client, listeners } = setup()
    const pending = expect(client.connect()).rejects.toThrow('插件连接已关闭')
    client.close()
    await pending
    expect(listeners.size).toBe(0)
    expect(vi.getTimerCount()).toBe(0)
  })
})
