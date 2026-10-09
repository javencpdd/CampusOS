// G1 bounded grant target; repository transactions and external principals arrive in later stages.
export type ScopeDelegationScope =
  | { kind: "public_collection"; ids: string[] }
  | { kind: "system_config"; ids: string[] }
  | { kind: "endpoint"; ids: string[] };
export type ScopeDelegationAction =
  | "knowledge.source.read" | "plugin.config.system.read"
  | "integration.webhook.invoke" | "personal.document.read";
export interface ScopeDelegationAtom {
  action: ScopeDelegationAction;
  scope: ScopeDelegationScope;
  not_before_ms: number;
  expires_at_ms: number;
  required_strength: "password" | "mfa";
}
export interface ScopeDelegationDecisionInput {
  contract: "campusos.scope-delegation/v1";
  actor: { kind: "user" | "admin"; id: string; authentication_strength: "password" | "mfa" };
  management: { action: "identity.role.assign" | "plugin.grant.manage" | "integration.grant.manage"; active: boolean; expires_at_ms: number };
  bound: ScopeDelegationAtom & { status: "active" | "suspended" | "revoked"; delegable: boolean };
  candidate: ScopeDelegationAtom & { recipient: { kind: "user" | "integration" | "plugin"; id: string } };
  now_ms: number;
  facts_version: string;
}
export type ScopeDelegationErrorCode =
  | "scope.input_invalid" | "scope.actor_denied" | "scope.management_denied"
  | "scope.non_delegable" | "scope.bound_inactive" | "scope.outside_bounds"
  | "scope.recipient_mismatch" | "scope.facts_changed";
