import type { PrincipalContextV1, PrincipalRef } from "./principal";

/** G1 host-side target contract, not a public authorization endpoint. */
export const THREAD_PUBLIC_READ_CONTRACT = "campusos.policy.thread-public-read/v1" as const;
export const THREAD_PUBLIC_READ_POLICY = "community.thread.public_read/v1" as const;

export interface ThreadResourceRef {
  kind: "community.thread";
  id: string;
}

/** Constructed by Community from current authoritative rows, never by a client
 * or an index. All fields are required; no legacy status/default fallback.
 */
export interface ThreadPublicReadFactsV1 {
  resource: ThreadResourceRef;
  owner: PrincipalRef<"user">;
  board: { kind: "community.board"; id: string };
  facts_version: string;
  publication_status: "draft" | "published" | "private";
  moderation_status: "clear" | "pending" | "rejected" | "taken_down";
  deletion_status: "active" | "trashed" | "purged";
}

export interface ThreadPublicReadRequestV1 {
  contract: typeof THREAD_PUBLIC_READ_CONTRACT;
  request_id: string;
  policy: typeof THREAD_PUBLIC_READ_POLICY;
  principal: PrincipalContextV1;
  facts: ThreadPublicReadFactsV1;
}

type DecisionBinding = {
  contract: typeof THREAD_PUBLIC_READ_CONTRACT;
  request_id: string;
  policy: typeof THREAD_PUBLIC_READ_POLICY;
  resource: ThreadResourceRef;
  facts_version: string;
};

/** A decision applies only to this public-visibility predicate and this call.
 * It never replaces route/credential/board ACL/Scope/Grant/Consent checks.
 * Allow requires rechecking authoritative facts before returning content.
 */
export type ThreadPublicReadDecisionV1 = DecisionBinding & (
  | { effect: "allow"; reason: "policy.public_read"; obligations: ["recheck_current_facts"] }
  | { effect: "deny"; reason: "policy.resource_not_public"; obligations: [] }
);

export type ThreadPublicReadErrorCode =
  | "policy.input_invalid"
  | "policy.contract_unsupported"
  | "policy.resource_not_public"
  | "policy.decision_mismatch"
  | "policy.facts_changed";
