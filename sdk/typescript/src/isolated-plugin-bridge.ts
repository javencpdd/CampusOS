export const PLUGIN_BRIDGE_VERSION = "campusos.bridge/v1";
export const MAX_PLUGIN_BRIDGE_MESSAGE_BYTES = 64 * 1024;
export const MAX_PLUGIN_RANGE_BYTES = 1024 * 1024;

export type PluginAudience = "user" | "admin";

export interface PluginFrameIdentity {
  pluginKey: string;
  pluginVersion: string;
  surfaceID: string;
  audience: PluginAudience;
  origin: string;
}

export interface PluginBridgeRequest {
  version: typeof PLUGIN_BRIDGE_VERSION;
  request_id: string;
  method:
    | "config.read"
    | "config.update"
    | "resource.describe"
    | "resource.readRange"
    | "backend.invoke"
    | "ui.requestSurface"
    | "records.read"
    | "records.write";
  params: Record<string, unknown>;
}

export interface PluginBridgeResponse {
  version: typeof PLUGIN_BRIDGE_VERSION;
  request_id: string;
  result?: unknown;
  error?: { code: string; message: string };
}

export interface PluginBridgeReady {
  type: "campusos.bridge.ready";
  version: typeof PLUGIN_BRIDGE_VERSION;
  plugin_key: string;
  plugin_version: string;
  surface_id: string;
  audience: PluginAudience;
}

export interface PluginBridgeConnect {
  type: "campusos.bridge.connect";
  version: typeof PLUGIN_BRIDGE_VERSION;
  challenge: string;
  plugin_key: string;
  plugin_version: string;
  surface_id: string;
  audience: PluginAudience;
}

export interface PluginBridgeHandshake {
  type: "campusos.bridge.handshake";
  version: typeof PLUGIN_BRIDGE_VERSION;
  challenge: string;
}

export interface MessagePortLike {
  postMessage(message: unknown, transfer?: Transferable[]): void;
  start?: () => void;
  close(): void;
  onmessage: ((event: MessageEvent<unknown>) => void) | null;
}

export interface MessageChannelLike {
  port1: MessagePortLike;
  port2: MessagePortLike;
}

export interface PluginBridgeSession {
  readonly identity: PluginFrameIdentity;
  readonly challenge: string;
  readonly port: MessagePortLike;
  readonly expiresAt: number;
  active: boolean;
  close(): void;
}

const requestIDPattern = /^[A-Za-z0-9][A-Za-z0-9._-]{0,95}$/;
const allowedMethods = new Set<PluginBridgeRequest["method"]>([
  "config.read",
  "config.update",
  "resource.describe",
  "resource.readRange",
  "backend.invoke",
  "ui.requestSurface",
  "records.read",
  "records.write",
]);

export function normalizePluginOrigin(value: string): string | undefined {
  try {
    const origin = new URL(value).origin;
    return origin === "null" ? undefined : origin;
  } catch {
    return undefined;
  }
}

export function validFrameIdentity(
  identity: PluginFrameIdentity,
  hostOrigin: string,
): boolean {
  const origin = normalizePluginOrigin(identity.origin);
  return Boolean(
    origin &&
    origin !== normalizePluginOrigin(hostOrigin) &&
    /^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)+$/.test(identity.pluginKey) &&
    identity.pluginVersion.length > 0 &&
    /^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)*$/.test(identity.surfaceID) &&
    (identity.audience === "user" || identity.audience === "admin"),
  );
}

export function isMatchingReady(
  value: unknown,
  identity: PluginFrameIdentity,
): value is PluginBridgeReady {
  if (!isRecord(value)) return false;
  return (
    value.type === "campusos.bridge.ready" &&
    value.version === PLUGIN_BRIDGE_VERSION &&
    value.plugin_key === identity.pluginKey &&
    value.plugin_version === identity.pluginVersion &&
    value.surface_id === identity.surfaceID &&
    value.audience === identity.audience
  );
}

export function isMatchingHandshake(
  value: unknown,
  challenge: string,
): value is PluginBridgeHandshake {
  return (
    isRecord(value) &&
    value.type === "campusos.bridge.handshake" &&
    value.version === PLUGIN_BRIDGE_VERSION &&
    value.challenge === challenge
  );
}

export function createPluginBridgeSession(
  identity: PluginFrameIdentity,
  challenge: string,
  channel: MessageChannelLike,
): PluginBridgeSession {
  let active = false;
  const expiresAt = Date.now() + 10 * 60 * 1000;
  const session: PluginBridgeSession = {
    identity,
    challenge,
    port: channel.port1,
    expiresAt,
    get active() {
      return active && Date.now() < expiresAt;
    },
    set active(value: boolean) {
      active = value;
    },
    close() {
      active = false;
      channel.port1.onmessage = null;
      channel.port1.close();
    },
  };
  channel.port1.start?.();
  return session;
}

export function validatePluginBridgeRequest(
  value: unknown,
): PluginBridgeRequest | undefined {
  if (!isRecord(value)) return undefined;
  if (
    value.version !== PLUGIN_BRIDGE_VERSION ||
    typeof value.request_id !== "string" ||
    !requestIDPattern.test(value.request_id) ||
    typeof value.method !== "string" ||
    !allowedMethods.has(value.method as PluginBridgeRequest["method"]) ||
    !isRecord(value.params) ||
    Object.keys(value).some(
      (key) => !["version", "request_id", "method", "params"].includes(key),
    ) ||
    encodedSize(value) > MAX_PLUGIN_BRIDGE_MESSAGE_BYTES
  ) {
    return undefined;
  }
  if (
    value.method === "resource.describe" ||
    value.method === "resource.readRange"
  ) {
    const keys =
      value.method === "resource.describe"
        ? ["handle"]
        : ["handle", "offset", "length"];
    if (
      Object.keys(value.params).some((key) => !keys.includes(key)) ||
      typeof value.params.handle !== "string" ||
      !/^[A-Za-z0-9_.:-]{1,256}$/.test(value.params.handle)
    )
      return undefined;
  }
  if (value.method === "resource.readRange") {
    const offset = value.params.offset;
    const length = value.params.length;
    if (
      typeof value.params.handle !== "string" ||
      value.params.handle.length === 0 ||
      !Number.isSafeInteger(offset) ||
      (offset as number) < 0 ||
      !Number.isSafeInteger(length) ||
      (length as number) < 1 ||
      (length as number) > MAX_PLUGIN_RANGE_BYTES
    ) {
      return undefined;
    }
  }
  return value as unknown as PluginBridgeRequest;
}

// Capture the port, host handler and identity predicate before awaiting. A late
// response can never use the mutable session belonging to another iframe.
export async function dispatchPluginBridgeRequest(
  session: PluginBridgeSession,
  value: unknown,
  current: () => boolean,
  execute: (request: PluginBridgeRequest) => Promise<unknown>,
  seen: Set<string>,
  budget: { count: number },
): Promise<void> {
  if (!session.active || !current()) return;
  const request = validatePluginBridgeRequest(value);
  if (
    !request ||
    seen.has(request.request_id) ||
    seen.size >= 4096 ||
    budget.count >= 32
  ) {
    session.port.postMessage(
      pluginBridgeError(
        isRecord(value) &&
          typeof value.request_id === "string" &&
          requestIDPattern.test(value.request_id)
          ? value.request_id
          : "invalid",
        "bridge.invalid_request",
        "插件请求格式、编号或并发数量无效。",
      ),
    );
    return;
  }
  seen.add(request.request_id);
  budget.count++;
  let timeout: ReturnType<typeof setTimeout> | undefined;
  try {
    const result = await Promise.race([
      execute(request),
      new Promise<never>((_, reject) => {
        timeout = setTimeout(() => reject(new Error("timeout")), 30000);
      }),
    ]);
    if (session.active && current())
      session.port.postMessage(
        pluginBridgeResult(request.request_id, result),
        pluginBridgeTransferables(result),
      );
  } catch {
    if (session.active && current())
      session.port.postMessage(
        pluginBridgeError(
          request.request_id,
          "bridge.denied",
          "当前操作未获授权或资源已不可用。",
        ),
      );
  } finally {
    if (timeout) clearTimeout(timeout);
    budget.count--;
  }
}

export function pluginBridgeError(
  requestID: string,
  code: string,
  message: string,
): PluginBridgeResponse {
  return {
    version: PLUGIN_BRIDGE_VERSION,
    request_id: requestID,
    error: { code, message },
  };
}

export function pluginBridgeResult(
  requestID: string,
  result: unknown,
): PluginBridgeResponse {
  return { version: PLUGIN_BRIDGE_VERSION, request_id: requestID, result };
}

export function pluginBridgeTransferables(result: unknown): Transferable[] {
  if (!isRecord(result)) return [];
  const bytes = result.bytes;
  return bytes instanceof ArrayBuffer ? [bytes] : [];
}

function encodedSize(value: unknown): number {
  try {
    return new TextEncoder().encode(JSON.stringify(value)).byteLength;
  } catch {
    return Number.MAX_SAFE_INTEGER;
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
