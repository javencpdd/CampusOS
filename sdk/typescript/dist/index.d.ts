export declare const UI_CONTRACT_VERSION: "campusos.ui/v1";
export type ActivationMode = 'restart' | 'plugin-restart' | 'hot';
export type BackendState = 'installed' | 'starting' | 'running' | 'restarting' | 'stopping' | 'stopped' | 'pending_restart' | 'error';
export type FrontendState = 'unloaded' | 'loading' | 'loaded' | 'incompatible' | 'error';
export type HealthState = 'healthy' | 'degraded' | 'unavailable' | 'unknown';
export interface RuntimeContext {
    plugin: string;
    revision: number;
    lifecycle: {
        scope: 'system' | 'user';
        backend_activation_mode: ActivationMode;
        frontend_activation_mode: 'hot';
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
    method: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE';
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
        lifecycle: RuntimeContext['lifecycle'];
        ui: Record<string, unknown>;
    }>;
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
    invoke<T>(action: ActionContract): Promise<T>;
    private headers;
}
