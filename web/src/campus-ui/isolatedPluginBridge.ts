export const PLUGIN_BRIDGE_VERSION = 'campusos.bridge/v1'
export const MAX_PLUGIN_BRIDGE_MESSAGE_BYTES = 64 * 1024
export const MAX_PLUGIN_RANGE_BYTES = 1024 * 1024

export type PluginAudience = 'user' | 'admin'

export interface PluginFrameIdentity {
  pluginKey: string
  pluginVersion: string
  surfaceID: string
  audience: PluginAudience
  origin: string
}

export interface PluginBridgeRequest {
  version: typeof PLUGIN_BRIDGE_VERSION
  request_id: string
  method:
    | 'config.read'
    | 'config.update'
    | 'resource.describe'
    | 'resource.readRange'
    | 'backend.invoke'
    | 'ui.requestSurface'
    | 'records.read'
    | 'records.write'
  params: Record<string, unknown>
}

export interface PluginBridgeResponse {
  version: typeof PLUGIN_BRIDGE_VERSION
  request_id: string
  result?: unknown
  error?: { code: string; message: string }
}

export interface PluginBridgeReady {
  type: 'campusos.bridge.ready'
  version: typeof PLUGIN_BRIDGE_VERSION
  plugin_key: string
  plugin_version: string
  surface_id: string
  audience: PluginAudience
}

export interface PluginBridgeConnect {
  type: 'campusos.bridge.connect'
  version: typeof PLUGIN_BRIDGE_VERSION
  challenge: string
  plugin_key: string
  plugin_version: string
  surface_id: string
  audience: PluginAudience
}

export interface PluginBridgeHandshake {
  type: 'campusos.bridge.handshake'
  version: typeof PLUGIN_BRIDGE_VERSION
  challenge: string
}

export interface MessagePortLike {
  postMessage(message: unknown, transfer?: Transferable[]): void
  start?: () => void
  close(): void
  onmessage: ((event: MessageEvent<unknown>) => void) | null
}

export interface MessageChannelLike {
  port1: MessagePortLike
  port2: MessagePortLike
}

export interface PluginBridgeSession {
  readonly identity: PluginFrameIdentity
  readonly challenge: string
  readonly port: MessagePortLike
  active: boolean
  close(): void
}

const requestIDPattern = /^[A-Za-z0-9][A-Za-z0-9._-]{0,95}$/
const allowedMethods = new Set<PluginBridgeRequest['method']>([
  'config.read',
  'config.update',
  'resource.describe',
  'resource.readRange',
  'backend.invoke',
  'ui.requestSurface',
  'records.read',
  'records.write',
])

export function normalizePluginOrigin(value: string): string | undefined {
  try {
    const origin = new URL(value).origin
    return origin === 'null' ? undefined : origin
  } catch {
    return undefined
  }
}

export function validFrameIdentity(identity: PluginFrameIdentity, hostOrigin: string): boolean {
  const origin = normalizePluginOrigin(identity.origin)
  return Boolean(
    origin &&
    origin !== normalizePluginOrigin(hostOrigin) &&
    /^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)+$/.test(identity.pluginKey) &&
    identity.pluginVersion.length > 0 &&
    /^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)*$/.test(identity.surfaceID) &&
    (identity.audience === 'user' || identity.audience === 'admin'),
  )
}

export function isMatchingReady(value: unknown, identity: PluginFrameIdentity): value is PluginBridgeReady {
  if (!isRecord(value)) return false
  return (
    value.type === 'campusos.bridge.ready' &&
    value.version === PLUGIN_BRIDGE_VERSION &&
    value.plugin_key === identity.pluginKey &&
    value.plugin_version === identity.pluginVersion &&
    value.surface_id === identity.surfaceID &&
    value.audience === identity.audience
  )
}

export function isMatchingHandshake(value: unknown, challenge: string): value is PluginBridgeHandshake {
  return (
    isRecord(value) &&
    value.type === 'campusos.bridge.handshake' &&
    value.version === PLUGIN_BRIDGE_VERSION &&
    value.challenge === challenge
  )
}

export function createPluginBridgeSession(
  identity: PluginFrameIdentity,
  challenge: string,
  channel: MessageChannelLike,
): PluginBridgeSession {
  let active = false
  const session: PluginBridgeSession = {
    identity,
    challenge,
    port: channel.port1,
    get active() {
      return active
    },
    set active(value: boolean) {
      active = value
    },
    close() {
      active = false
      channel.port1.onmessage = null
      channel.port1.close()
    },
  }
  channel.port1.start?.()
  return session
}

export function validatePluginBridgeRequest(value: unknown): PluginBridgeRequest | undefined {
  if (!isRecord(value)) return undefined
  if (
    value.version !== PLUGIN_BRIDGE_VERSION ||
    typeof value.request_id !== 'string' ||
    !requestIDPattern.test(value.request_id) ||
    typeof value.method !== 'string' ||
    !allowedMethods.has(value.method as PluginBridgeRequest['method']) ||
    !isRecord(value.params) ||
    encodedSize(value) > MAX_PLUGIN_BRIDGE_MESSAGE_BYTES
  ) {
    return undefined
  }
  if (value.method === 'resource.readRange') {
    const offset = value.params.offset
    const length = value.params.length
    if (
      typeof value.params.handle !== 'string' ||
      value.params.handle.length === 0 ||
      !Number.isSafeInteger(offset) ||
      (offset as number) < 0 ||
      !Number.isSafeInteger(length) ||
      (length as number) < 1 ||
      (length as number) > MAX_PLUGIN_RANGE_BYTES
    ) {
      return undefined
    }
  }
  return value as unknown as PluginBridgeRequest
}

export function pluginBridgeError(requestID: string, code: string, message: string): PluginBridgeResponse {
  return { version: PLUGIN_BRIDGE_VERSION, request_id: requestID, error: { code, message } }
}

export function pluginBridgeResult(requestID: string, result: unknown): PluginBridgeResponse {
  return { version: PLUGIN_BRIDGE_VERSION, request_id: requestID, result }
}

export function pluginBridgeTransferables(result: unknown): Transferable[] {
  if (!isRecord(result)) return []
  const bytes = result.bytes
  return bytes instanceof ArrayBuffer ? [bytes] : []
}

function encodedSize(value: unknown): number {
  try {
    return new TextEncoder().encode(JSON.stringify(value)).byteLength
  } catch {
    return Number.MAX_SAFE_INTEGER
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
