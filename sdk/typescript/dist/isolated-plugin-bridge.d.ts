export declare const PLUGIN_BRIDGE_VERSION = "campusos.bridge/v1";
export declare const MAX_PLUGIN_BRIDGE_MESSAGE_BYTES: number;
export declare const MAX_PLUGIN_RANGE_BYTES: number;
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
    method: "config.read" | "config.update" | "resource.describe" | "resource.readRange" | "backend.invoke" | "ui.requestSurface" | "records.read" | "records.write";
    params: Record<string, unknown>;
}
export interface PluginBridgeResponse {
    version: typeof PLUGIN_BRIDGE_VERSION;
    request_id: string;
    result?: unknown;
    error?: {
        code: string;
        message: string;
    };
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
export declare function normalizePluginOrigin(value: string): string | undefined;
export declare function validFrameIdentity(identity: PluginFrameIdentity, hostOrigin: string): boolean;
export declare function isMatchingReady(value: unknown, identity: PluginFrameIdentity): value is PluginBridgeReady;
export declare function isMatchingHandshake(value: unknown, challenge: string): value is PluginBridgeHandshake;
export declare function createPluginBridgeSession(identity: PluginFrameIdentity, challenge: string, channel: MessageChannelLike): PluginBridgeSession;
export declare function validatePluginBridgeRequest(value: unknown): PluginBridgeRequest | undefined;
export declare function dispatchPluginBridgeRequest(session: PluginBridgeSession, value: unknown, current: () => boolean, execute: (request: PluginBridgeRequest) => Promise<unknown>, seen: Set<string>, budget: {
    count: number;
}): Promise<void>;
export declare function pluginBridgeError(requestID: string, code: string, message: string): PluginBridgeResponse;
export declare function pluginBridgeResult(requestID: string, result: unknown): PluginBridgeResponse;
export declare function pluginBridgeTransferables(result: unknown): Transferable[];
