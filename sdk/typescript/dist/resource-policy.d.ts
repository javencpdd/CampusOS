import type { PrincipalContextV1 } from "./principal";
export type ResourcePolicyActionV1 = "community.thread.edit" | "community.thread.take_down" | "community.post.delete" | "personal.document.read" | "knowledge.source.read";
export interface ResourcePolicyFactsV1 {
    kind: "thread" | "post" | "document" | "knowledge_source";
    id: string;
    owner_id?: string;
    board_id?: string;
    collection_id?: string;
    status: "active" | "draft" | "published" | "private" | "archived" | "deleted";
    publication_status?: "draft" | "published" | "private";
    origin_visible?: boolean;
    version: string;
}
export interface ResourcePolicyGrantV1 {
    subject_kind: "user" | "admin";
    subject_id: string;
    action: "community.thread.take_down" | "community.post.delete";
    board_id: string;
    expires_at_ms: number;
    status: "active" | "revoked";
    required_strength: "password" | "mfa";
}
export interface ResourcePolicyRequestV1 {
    contract: "campusos.resource-policy/v1";
    request_id: string;
    principal: PrincipalContextV1;
    action: ResourcePolicyActionV1;
    facts: ResourcePolicyFactsV1;
    grants: ResourcePolicyGrantV1[];
    evaluated_at_ms: number;
    policy_version: string;
}
export interface ResourcePolicyDecisionV1 {
    request_id: string;
    effect: "allow" | "deny";
    reason: string;
    facts_version: string;
    policy_version: string;
    obligations: Array<"recheck_facts" | "required_audit">;
}
export type ResourcePolicyErrorCodeV1 = "policy.invalid" | "policy.unknown_action" | "policy.principal_wrong_domain" | "policy.resource_unavailable" | "policy.scope_denied" | "policy.facts_changed";
