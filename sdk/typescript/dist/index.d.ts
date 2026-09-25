export declare const UI_CONTRACT_VERSION: "campusos.ui/v1";
export declare const CAPABILITY_CONTRACT_VERSION: "campusos.capability/v1";
export declare const EVENT_CONTRACT_VERSION: "campusos.event/v1";
export declare const PROCESS_CONTRACT_VERSION: "campusos.process/v1";
export type AuthorizationReason = "ALLOW" | "DENY_UNKNOWN_OPERATION" | "DENY_CAPABILITY_NOT_DECLARED" | "DENY_ADMIN_GRANT_MISSING" | "DENY_USER_CONSENT_MISSING" | "DENY_VERSION_MISMATCH" | "DENY_SCOPE_MISMATCH" | "DENY_PLUGIN_INACTIVE" | "DENY_DELEGATION_INVALID" | "DENY_SYSTEM_POLICY";
export interface CapabilityDescriptor {
    code: string;
    resource: string;
    action: string;
    scope: "self" | "system";
    risk: "low" | "medium" | "high";
    consent_required: boolean;
    data_classification: string;
    audit_level: string;
    description: string;
}
export interface CapabilityDeclaration {
    plugin_version_id: number;
    capability_code: string;
    purpose: string;
    risk_level: string;
    required: boolean;
    resource_scope: Record<string, unknown>;
}
export interface AuthorizationOverview {
    version: {
        id: number;
        plugin_name: string;
        version: string;
        permission_fingerprint: string;
    };
    declarations: CapabilityDeclaration[];
    catalog: CapabilityDescriptor[];
    admin_grants: Array<{
        capability_code: string;
        status: string;
        reason: string;
        policy_revision: number;
    }>;
    user_consents: Array<{
        capability_code: string;
        status: string;
        purpose_hash: string;
        policy_revision: number;
    }>;
}
export interface CampusOSErrorDetail {
    code: string;
    message: string;
    details?: unknown;
    request_id?: string;
    retryable: boolean;
}
export declare class CampusOSError extends Error {
    readonly status: number;
    readonly code: number;
    readonly machineCode: string;
    readonly msg: string;
    readonly requestId?: string;
    readonly retryable: boolean;
    readonly details?: unknown;
    readonly error: CampusOSErrorDetail;
    constructor(input: {
        status: number;
        legacyCode: number;
        machineCode: string;
        message: string;
        requestId?: string;
        retryable: boolean;
        details?: unknown;
    });
}
export declare function parseCampusOSError(payload: unknown, status?: number, fallback?: string): CampusOSError;
export type ActivationMode = "restart" | "plugin-restart" | "hot";
export type BackendState = "installed" | "starting" | "running" | "restarting" | "stopping" | "stopped" | "pending_restart" | "error";
export type FrontendState = "unloaded" | "loading" | "loaded" | "incompatible" | "error";
export type HealthState = "healthy" | "degraded" | "unavailable" | "unknown";
export interface RuntimeContext {
    plugin: string;
    revision: number;
    lifecycle: {
        scope: "system" | "user";
        backend_activation_mode: ActivationMode;
        frontend_activation_mode: "hot";
        backend_state: BackendState;
        frontend_state: FrontendState;
        health: HealthState;
        desired_enabled: boolean;
        pending_restart: boolean;
    };
}
export interface ActionContract {
    id: string;
    label: string;
    method: "GET" | "POST" | "PUT" | "PATCH" | "DELETE";
    path: `/${string}`;
    permission?: string;
    confirm?: boolean;
    audit?: boolean;
    body?: Record<string, unknown>;
}
export interface RuntimeManifest {
    contract_version: typeof UI_CONTRACT_VERSION;
    revision: number;
    current_theme?: string;
    plugins: Array<{
        name: string;
        version: string;
        runtime: string;
        lifecycle: RuntimeContext["lifecycle"];
        ui: Record<string, unknown>;
    }>;
}
export interface ResponsiveUI {
    supported_viewports?: Array<"mobile" | "tablet" | "desktop">;
    minimum_width?: number;
    mobile_behavior?: "responsive" | "unsupported";
    overflow_policy?: "internal-only" | "none";
}
export interface ManagedRecord {
    id: number;
    plugin_name: string;
    owner_type: "system" | "user";
    owner_id: string;
    collection: string;
    record_key: string;
    data: Record<string, unknown>;
    version: number;
    created_at: string;
    updated_at: string;
}
export interface ClientOptions {
    baseURL?: string;
    token: () => string | undefined;
    fetch?: typeof globalThis.fetch;
}
export declare class CampusExtensionClient {
    private readonly plugin;
    private readonly options;
    private readonly baseURL;
    private readonly request;
    constructor(plugin: string, options: ClientOptions);
    runtimeManifest(): Promise<RuntimeManifest>;
    authorization(): Promise<AuthorizationOverview>;
    setConsent(versionId: number, capability: string, granted: boolean, scope?: Record<string, unknown>): Promise<void>;
    invoke<T>(action: ActionContract): Promise<T>;
    listMyRecords(collection: string, page?: number, pageSize?: number): Promise<{
        items: ManagedRecord[];
        total: number;
    }>;
    createMyRecord(collection: string, input: {
        record_key?: string;
        data: Record<string, unknown>;
    }): Promise<ManagedRecord>;
    updateMyRecord(collection: string, recordKey: string, input: {
        version: number;
        data: Record<string, unknown>;
    }): Promise<ManagedRecord>;
    deleteMyRecord(collection: string, recordKey: string, version: number): Promise<void>;
    private headers;
}
