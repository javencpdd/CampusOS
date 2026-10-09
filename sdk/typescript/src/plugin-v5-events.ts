// G1 target event and lifecycle DTOs. Event payload contains only whitelisted metadata.
export interface PluginV5SafeEvent {
  contract: "campusos.plugin-event/v1";
  event_id: string;
  type: "plugin.lifecycle.changed/v1" | "community.thread.public.changed/v1" | "knowledge.source.public.changed/v1";
  revision: number;
  created_at_ms: number;
  expires_at_ms: number;
  payload: { resource_ref: string; state: "available" | "changed" | "removed" | "disabled" };
}
export interface PluginV5EventDelivery {
  now_ms: number;
  instance_generation: number;
  current_generation: number;
  subscribed: boolean;
  grant_active: boolean;
  runtime_ready: boolean;
  attempt: number;
  max_attempts: number;
  payload_bytes: number;
  max_bytes: number;
  core_transaction_committed: boolean;
}
export interface PluginV5EventDecisionInput {
  event: PluginV5SafeEvent;
  delivery: PluginV5EventDelivery;
}
export type PluginV5EventErrorCode =
  | "plugin.event_invalid" | "plugin.event_uncommitted" | "plugin.event_expired"
  | "plugin.event_stale_generation" | "plugin.event_subscription_missing"
  | "plugin.event_grant_revoked" | "plugin.event_runtime_unavailable"
  | "plugin.event_too_large" | "plugin.event_dead_letter";
export interface PluginV5LifecycleDecisionInput {
  contract: "campusos.plugin-lifecycle/v1";
  operation_id: string;
  operation: "install" | "enable" | "disable" | "upgrade" | "revoke" | "uninstall";
  phase: "prepare" | "switch" | "final";
  desired: "enabled" | "disabled" | "uninstalled";
  observed: "pending" | "ready" | "failed" | "stopped";
  current_generation: number;
  requested_generation: number;
  global_installed: boolean;
  grant_active: boolean;
  accepting_new_calls: boolean;
  published: boolean;
  inflight_calls: number;
  config_migration: boolean;
  private_data_mutation: boolean;
  snapshot_verified: boolean;
  drain_strategy: "drain" | "cancel";
}
export type PluginV5LifecycleErrorCode =
  | "plugin.lifecycle_invalid" | "plugin.lifecycle_generation_conflict"
  | "plugin.lifecycle_publication_forbidden" | "plugin.lifecycle_drain_incomplete"
  | "plugin.lifecycle_rollback_unverifiable" | "plugin.lifecycle_release_order";
