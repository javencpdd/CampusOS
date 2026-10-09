import type { PrincipalContextV1, PrincipalRef } from "./principal";
/** G1 target contract only; trusted facts are constructed by the host. */
export declare const BOARD_DELEGATION_CONTRACT: "campusos.board-delegation/v1";
export type BoardGovernanceAction = "community.thread.take_down" | "community.post.delete";
export interface BoardGrantAtomV1 {
    action: BoardGovernanceAction;
    board_id: string;
    not_before: number;
    expires_at: number;
    required_strength: "password" | "mfa";
}
/** Execution alone cannot delegate. One complete bound must cover one atom. */
export interface BoardDelegationBoundV1 extends BoardGrantAtomV1 {
    id: string;
    actor: PrincipalRef<"admin">;
    status: "active" | "suspended" | "revoked";
    delegable: boolean;
}
export interface BoardDelegationRequestV1 {
    contract: typeof BOARD_DELEGATION_CONTRACT;
    request_id: string;
    facts_version: string;
    evaluated_at: number;
    principal: PrincipalContextV1;
    management: {
        actor: PrincipalRef<"admin">;
        action: "identity.role.assign";
        status: "active" | "suspended" | "revoked";
        not_before: number;
        expires_at: number;
    };
    recipient: {
        subject: PrincipalRef<"user">;
        status: "active" | "suspended" | "deleted";
    };
    boards: Array<{
        kind: "community.board";
        id: string;
        status: "active" | "archived";
    }>;
    bounds: BoardDelegationBoundV1[];
    candidate: {
        recipient: PrincipalRef<"user">;
        grants: BoardGrantAtomV1[];
    };
}
export type BoardDelegationDenyReason = "delegation.actor_denied" | "delegation.mfa_required" | "delegation.management_denied" | "delegation.recipient_denied" | "delegation.board_denied" | "delegation.outside_bounds";
/** All-or-nothing proposal; an allow is not a committed grant. */
export type BoardDelegationDecisionV1 = {
    contract: typeof BOARD_DELEGATION_CONTRACT;
    request_id: string;
    facts_version: string;
} & ({
    effect: "allow";
    reason: "delegation.allowed";
    witnesses: Array<{
        candidate_index: number;
        bound_id: string;
    }>;
    obligations: ["recheck_authority_in_transaction", "required_audit"];
} | {
    effect: "deny";
    reason: BoardDelegationDenyReason;
    witnesses: [];
    obligations: [];
});
export type BoardDelegationErrorCode = BoardDelegationDenyReason | "delegation.input_invalid" | "delegation.contract_unsupported" | "delegation.decision_mismatch" | "delegation.facts_changed";
