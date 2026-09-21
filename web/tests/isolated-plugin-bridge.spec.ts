import { describe, expect, it } from 'vitest'
import {
  MAX_PLUGIN_RANGE_BYTES,
  PLUGIN_BRIDGE_VERSION,
  isMatchingReady,
  validatePluginBridgeRequest,
  validFrameIdentity,
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
