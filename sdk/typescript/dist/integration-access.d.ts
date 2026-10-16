import type { PrincipalContextV1 } from "./principal";
/** Host-only whitelist facts. These types neither authenticate credentials nor
 * authorize original content. Every successful entry still needs owner ACLs.
 */
export declare const INTEGRATION_ACCESS_VERSION: "campusos.integration-access/v1";
export interface IntegrationAccessTargetV1 {
    channel: string;
    endpoint: string;
    action: string;
    resource: {
        kind: string;
        id: string;
    };
}
export interface IntegrationAccessEndpointV1 {
    channel: string;
    id: string;
    action: string;
    resource_kind: string;
    enabled: boolean;
    integration_allowed: boolean;
}
export interface IntegrationAccessRuleV1 {
    id: string;
    channel: string;
    endpoint: string;
    action: string;
    resource_kind: string;
    resource_ids: readonly string[];
    status: "active" | "suspended" | "revoked";
    not_before_ms: number;
    expires_at_ms: number;
}
export interface IntegrationAccessInputV1 {
    contract: typeof INTEGRATION_ACCESS_VERSION;
    request_id: string;
    principal: PrincipalContextV1;
    target: IntegrationAccessTargetV1;
    facts: {
        integration_id: string;
        credential_id: string;
        principal_status: "active" | "suspended" | "revoked";
        credential_status: "active" | "revoked";
        credential_audience: string;
        not_before_ms: number;
        expires_at_ms: number;
        facts_version: string;
        endpoint: IntegrationAccessEndpointV1;
        rules: readonly IntegrationAccessRuleV1[];
    };
    now_ms: number;
}
export type IntegrationAccessDenyReasonV1 = "integration.actor_denied" | "integration.principal_inactive" | "integration.credential_denied" | "integration.channel_denied" | "integration.scope_denied" | "integration.endpoint_denied" | "integration.whitelist_denied";
/** Private, same-call decision; never publish it as a reusable authorization. */
export type IntegrationAccessDecisionV1 = {
    request: IntegrationAccessInputV1;
} & ({
    effect: "allow";
    reason: "integration.allowed";
    rule_id: string;
} | {
    effect: "deny";
    reason: IntegrationAccessDenyReasonV1;
    rule_id?: never;
});
export type IntegrationAccessErrorCodeV1 = IntegrationAccessDenyReasonV1 | "integration.input_invalid" | "integration.decision_mismatch" | "integration.facts_changed";
