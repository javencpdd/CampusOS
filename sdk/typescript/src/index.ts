export const UI_CONTRACT_VERSION = "campusos.ui/v1" as const;
export const CAPABILITY_CONTRACT_VERSION = "campusos.capability/v1" as const;
export const EVENT_CONTRACT_VERSION = "campusos.event/v1" as const;
export const PROCESS_CONTRACT_VERSION = "campusos.process/v1" as const;

export type AuthorizationReason =
  | "ALLOW"
  | "DENY_UNKNOWN_OPERATION"
  | "DENY_CAPABILITY_NOT_DECLARED"
  | "DENY_ADMIN_GRANT_MISSING"
  | "DENY_USER_CONSENT_MISSING"
  | "DENY_VERSION_MISMATCH"
  | "DENY_SCOPE_MISMATCH"
  | "DENY_PLUGIN_INACTIVE"
  | "DENY_DELEGATION_INVALID"
  | "DENY_SYSTEM_POLICY";

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

export class CampusOSError extends Error {
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
  }) {
    super(input.message);
    this.name = "CampusOSError";
    this.status = input.status;
    this.code = input.legacyCode;
    this.machineCode = input.machineCode;
    this.msg = input.message;
    this.requestId = input.requestId;
    this.retryable = input.retryable;
    this.details = input.details;
    this.error = {
      code: input.machineCode,
      message: input.message,
      details: input.details,
      request_id: input.requestId,
      retryable: input.retryable,
    };
  }
}

const errorRecord = (value: unknown): Record<string, unknown> | undefined =>
  value !== null && typeof value === "object"
    ? (value as Record<string, unknown>)
    : undefined;

const errorString = (value: unknown): string | undefined =>
  typeof value === "string" && value.trim() ? value.trim() : undefined;

export function parseCampusOSError(
  payload: unknown,
  status = 0,
  fallback = "CampusOS request failed",
): CampusOSError {
  if (payload instanceof CampusOSError) return payload;
  const envelope = errorRecord(payload);
  const nested = errorRecord(envelope?.error);
  const message =
    errorString(nested?.message) ||
    errorString(envelope?.msg) ||
    errorString(envelope?.error) ||
    fallback;
  const requestId =
    errorString(nested?.request_id) || errorString(envelope?.request_id);
  const retryable =
    typeof nested?.retryable === "boolean"
      ? nested.retryable
      : status === 429 || status === 503 || status >= 500;
  return new CampusOSError({
    status,
    legacyCode: typeof envelope?.code === "number" ? envelope.code : 0,
    machineCode: errorString(nested?.code) || "request.failed",
    message,
    requestId,
    retryable,
    details: nested?.details,
  });
}

async function errorFromResponse(
  response: Response,
  fallback: string,
): Promise<CampusOSError> {
  let payload: unknown;
  try {
    const body = await response.text();
    payload = body ? JSON.parse(body) : undefined;
  } catch {
    payload = undefined;
  }
  return parseCampusOSError(payload, response.status, fallback);
}

export type ActivationMode = "restart" | "plugin-restart" | "hot";
export type BackendState =
  | "installed"
  | "starting"
  | "running"
  | "restarting"
  | "stopping"
  | "stopped"
  | "pending_restart"
  | "error";
export type FrontendState =
  "unloaded" | "loading" | "loaded" | "incompatible" | "error";
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

export class CampusExtensionClient {
  private readonly baseURL: string;
  private readonly request: typeof globalThis.fetch;
  constructor(
    private readonly plugin: string,
    private readonly options: ClientOptions,
  ) {
    if (!/^[a-z0-9][a-z0-9-]{1,62}$/.test(plugin))
      throw new Error("invalid plugin name");
    this.baseURL = (options.baseURL || "/api/v1").replace(/\/$/, "");
    this.request = options.fetch || globalThis.fetch.bind(globalThis);
  }

  async runtimeManifest(): Promise<RuntimeManifest> {
    const response = await this.request(`${this.baseURL}/ui/runtime-manifest`, {
      headers: this.headers(),
    });
    if (!response.ok)
      throw await errorFromResponse(response, "runtime manifest failed");
    const envelope = (await response.json()) as { data: RuntimeManifest };
    return envelope.data;
  }

  async authorization(): Promise<AuthorizationOverview> {
    const response = await this.request(
      `${this.baseURL}/plugin-authorizations/${encodeURIComponent(this.plugin)}`,
      { headers: this.headers() },
    );
    if (!response.ok)
      throw await errorFromResponse(response, "plugin authorization failed");
    return ((await response.json()) as { data: AuthorizationOverview }).data;
  }

  async setConsent(
    versionId: number,
    capability: string,
    granted: boolean,
    scope: Record<string, unknown> = { scope: "self" },
  ): Promise<void> {
    const response = await this.request(
      `${this.baseURL}/plugin-authorizations/${encodeURIComponent(this.plugin)}/versions/${versionId}/consents/${encodeURIComponent(capability)}`,
      {
        method: "PUT",
        headers: { ...this.headers(), "Content-Type": "application/json" },
        body: JSON.stringify({
          status: granted ? "granted" : "revoked",
          scope,
        }),
      },
    );
    if (!response.ok)
      throw await errorFromResponse(response, "plugin consent failed");
  }

  async invoke<T>(action: ActionContract): Promise<T> {
    if (!action.path.startsWith("/") || action.path.includes(".."))
      throw new Error("invalid action path");
    const response = await this.request(
      `${this.baseURL}/extensions/${encodeURIComponent(this.plugin)}${action.path}`,
      {
        method: action.method,
        headers: { ...this.headers(), "Content-Type": "application/json" },
        body:
          action.method === "GET"
            ? undefined
            : JSON.stringify(action.body || {}),
      },
    );
    if (!response.ok)
      throw await errorFromResponse(response, "extension action failed");
    return (await response.json()) as T;
  }

  async listMyRecords(
    collection: string,
    page = 1,
    pageSize = 20,
  ): Promise<{ items: ManagedRecord[]; total: number }> {
    const response = await this.request(
      `${this.baseURL}/plugin-market/${encodeURIComponent(this.plugin)}/records/${encodeURIComponent(collection)}?page=${page}&page_size=${pageSize}`,
      { headers: this.headers() },
    );
    if (!response.ok)
      throw await errorFromResponse(response, "plugin records failed");
    const envelope = (await response.json()) as {
      data: { items: ManagedRecord[]; total: number };
    };
    return envelope.data;
  }

  async createMyRecord(
    collection: string,
    input: { record_key?: string; data: Record<string, unknown> },
  ): Promise<ManagedRecord> {
    const response = await this.request(
      `${this.baseURL}/plugin-market/${encodeURIComponent(this.plugin)}/records/${encodeURIComponent(collection)}`,
      {
        method: "POST",
        headers: { ...this.headers(), "Content-Type": "application/json" },
        body: JSON.stringify(input),
      },
    );
    if (!response.ok)
      throw await errorFromResponse(response, "create plugin record failed");
    return ((await response.json()) as { data: ManagedRecord }).data;
  }

  async updateMyRecord(
    collection: string,
    recordKey: string,
    input: { version: number; data: Record<string, unknown> },
  ): Promise<ManagedRecord> {
    const response = await this.request(
      `${this.baseURL}/plugin-market/${encodeURIComponent(this.plugin)}/records/${encodeURIComponent(collection)}/${encodeURIComponent(recordKey)}`,
      {
        method: "PUT",
        headers: { ...this.headers(), "Content-Type": "application/json" },
        body: JSON.stringify(input),
      },
    );
    if (!response.ok)
      throw await errorFromResponse(response, "update plugin record failed");
    return ((await response.json()) as { data: ManagedRecord }).data;
  }

  async deleteMyRecord(
    collection: string,
    recordKey: string,
    version: number,
  ): Promise<void> {
    const response = await this.request(
      `${this.baseURL}/plugin-market/${encodeURIComponent(this.plugin)}/records/${encodeURIComponent(collection)}/${encodeURIComponent(recordKey)}?version=${version}`,
      { method: "DELETE", headers: this.headers() },
    );
    if (!response.ok)
      throw await errorFromResponse(response, "delete plugin record failed");
  }

  private headers(): Record<string, string> {
    const token = this.options.token();
    return token ? { Authorization: `Bearer ${token}` } : {};
  }
}

// Target v1.2 G1 data contract; does not enable a runtime authorization path.
export { PRINCIPAL_CONTEXT_VERSION } from "./principal";
export type { PrincipalKind, PrincipalRef, PrincipalContextV1, PrincipalContextErrorCode } from "./principal";

export { THREAD_PUBLIC_READ_CONTRACT, THREAD_PUBLIC_READ_POLICY } from "./thread-public-read-policy";
export type {
  ThreadResourceRef, ThreadPublicReadFactsV1, ThreadPublicReadRequestV1,
  ThreadPublicReadDecisionV1, ThreadPublicReadErrorCode,
} from "./thread-public-read-policy";

export { BOARD_DELEGATION_CONTRACT } from "./board-delegation";
export type {
  BoardGovernanceAction, BoardGrantAtomV1, BoardDelegationBoundV1, BoardDelegationRequestV1,
  BoardDelegationDenyReason, BoardDelegationDecisionV1, BoardDelegationErrorCode,
} from "./board-delegation";

export { PLUGIN_V5_API_VERSION } from "./plugin-v5-package-shape";
export type {
  PluginV5Runtime, PluginV5UiAudience, PluginV5ExtensionPoint, PluginV5UiArtifact,
  PluginV5BackendArtifact, PluginV5ProviderShape, PluginV5PackageShape,
  PluginV5ReleaseEnvelope, PluginV5ShapeErrorCode,
} from "./plugin-v5-package-shape";

export type {
  PluginV5ConsumeScope, PluginV5Consume, PluginV5Consumes,
  PluginV5HostCapability, PluginV5HostCatalog, PluginV5ConsumesErrorCode,
} from "./plugin-v5-consumes";

export type {
  PluginV5DataMode, PluginV5SchemaFileRef, PluginV5Provide,
  PluginV5ExtensionPointDescriptor, PluginV5ExtensionCatalog, PluginV5ProvidesErrorCode,
} from "./plugin-v5-provides";
export type {
  PluginV5ExtensionCallBase, PluginV5ExtensionResultBase,
  PluginV5PreviewRequest, PluginV5PreviewResponse,
  PluginV5ChatRequest, PluginV5ChatResponse,
  PluginV5KnowledgeSourceRequest, PluginV5KnowledgeSourceItem, PluginV5KnowledgeSourceResponse,
} from "./plugin-v5-provides";

export type {
  PluginV5ResourceSet, PluginV5ResourceDecisionInput, PluginV5HostFingerprint,
  PluginV5InstallMutationKind, PluginV5InstallPlan, PluginV5ResourceInstallErrorCode,
} from "./plugin-v5-resource-install";

export type {
  PluginV5ConfigField, PluginV5ConfigDefinition, PluginV5ConfigUpdate,
  PluginV5ConfigDecisionInput, PluginV5ConfigErrorCode,
} from "./plugin-v5-config";

export type {
  ResourcePolicyActionV1, ResourcePolicyFactsV1, ResourcePolicyGrantV1,
  ResourcePolicyRequestV1, ResourcePolicyDecisionV1, ResourcePolicyErrorCodeV1,
} from "./resource-policy";
export type { IdentityKind, IdentityDomainDescriptor, IdentityDomainProbe, AdminDomainOperation, IdentityDomainDecisionInput, IdentityDomainErrorCode } from "./identity-domains.js";
export type { PluginV5PrivateDataDeclaration, PluginV5ManifestBase, PluginV5Manifest, PluginV5ManifestErrorCode } from "./plugin-v5-manifest.js";
export type { PluginV5DispatchCall, PluginV5DispatchTrustedFacts, PluginV5DispatchDecisionInput, PluginV5DispatchErrorCode } from "./plugin-v5-dispatch.js";
export type { PluginV5SafeEvent, PluginV5EventDelivery, PluginV5EventDecisionInput, PluginV5EventErrorCode, PluginV5LifecycleDecisionInput, PluginV5LifecycleErrorCode } from "./plugin-v5-events.js";
export type { ScopeDelegationScope, ScopeDelegationAction, ScopeDelegationAtom, ScopeDelegationDecisionInput, ScopeDelegationErrorCode } from "./scope-delegation.js";
