// G1 target resource/installation decision DTOs, not an installer or resource grant.
export interface PluginV5ResourceSet {
  network_targets: string[];
  storage_bytes: number;
  cpu_millis: number;
  memory_mb: number;
  max_concurrency: number;
  timeout_ms: number;
}
export interface PluginV5ResourceDecisionInput {
  contract: "campusos.plugin-host-resources/v1";
  requested: PluginV5ResourceSet;
  host_ceiling: PluginV5ResourceSet;
  admin_grant: PluginV5ResourceSet;
  grant_status: "granted" | "revoked";
  grant_expires_at_ms: number;
  now_ms: number;
}
export interface PluginV5HostFingerprint {
  source: string;
  dependencies: string;
  binary: string;
  frontends: string;
  schema: string;
}
export type PluginV5InstallMutationKind =
  | "plugin_package" | "plugin_config" | "plugin_data"
  | "installation_record" | "grant_record" | "operation_record" | "audit_record";
export interface PluginV5InstallPlan {
  contract: "campusos.plugin-install-plan/v1";
  plugin_key: string;
  mutations: Array<{ kind: PluginV5InstallMutationKind; namespace: string }>;
  host_before: PluginV5HostFingerprint;
  host_after: PluginV5HostFingerprint;
}
export type PluginV5ResourceInstallErrorCode =
  | "plugin.resource_request_invalid" | "plugin.resource_policy_unavailable"
  | "plugin.resource_grant_missing" | "plugin.resource_grant_expired"
  | "plugin.network_target_forbidden" | "plugin.install_mutation_forbidden"
  | "plugin.host_fingerprint_changed";
