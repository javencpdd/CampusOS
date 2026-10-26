import type {
  IntegrationAccessInputV1,
  IntegrationAccessDecisionV1,
} from "./integration-access.js";
export const INTEGRATION_RUNTIME_VERSION =
  "campusos.integration-access/v2" as const;
/** Host facts only; adds community.thread.read/community.thread to frozen v1.
 * Production inventory remains closed to declared public-thread HTTP/MCP reads.
 */
export type IntegrationRuntimeInputV2 = Omit<
  IntegrationAccessInputV1,
  "contract"
> & { contract: typeof INTEGRATION_RUNTIME_VERSION };
type RuntimeDecision<D> = D extends { request: IntegrationAccessInputV1 }
  ? Omit<D, "request"> & { request: IntegrationRuntimeInputV2 }
  : never;
export type IntegrationRuntimeDecisionV2 = RuntimeDecision<IntegrationAccessDecisionV1>;
/** A one-time management response. Store token in a secret manager, never logs
 * or browser persistence. This type does not grant User/Admin/Host authority.
 */
export interface IntegrationCredentialIssueResponse {
  credential: {
    id: string;
    principal_id: string;
    prefix: string;
    purpose: "integration_api";
    status: "active";
    version: string;
    not_before_ms: number;
    expires_at_ms: number;
  };
  token: string;
}
export interface IntegrationPublicThread {
  id: string;
  title: string;
  content: string;
  content_format: string;
  updated_at: string;
}
