/** G1 target DTO. A value of this type is NOT proof of authentication or permission.
 * Only host verifiers construct authoritative contexts; never accept one from
 * a request body, plugin response, index or model as authorization facts.
 * String syntax and unknown fields are checked by principal-context-v1.schema.json.
 */
export const PRINCIPAL_CONTEXT_VERSION = "campusos.principal/v1" as const;

export type PrincipalKind =
  | "user"
  | "admin"
  | "integration"
  | "plugin_instance"
  | "worker";

export interface PrincipalRef<K extends PrincipalKind = PrincipalKind> {
  kind: K;
  id: string;
}

type NoDelegation = { effective_subject?: never; delegation_id?: never };
type UserDelegation =
  | NoDelegation
  | { effective_subject: PrincipalRef<"user">; delegation_id: string };

type AuthenticatedContext<
  K extends PrincipalKind,
  A extends string,
  S extends string,
> = {
  contract: typeof PRINCIPAL_CONTEXT_VERSION;
  actor: PrincipalRef<K>;
  credential_id: string;
  audience: A;
  authentication_strength: S;
};

/** Audience is the credential's purpose, not the destination route's label.
 * User and Admin with the same ID remain different subjects.
 * Delegation references are reloaded by the host, never trusted by value.
 */
export type PrincipalContextV1 =
  | ({
      contract: typeof PRINCIPAL_CONTEXT_VERSION;
      actor: { kind: "anonymous"; id?: never };
      audience: "public";
      authentication_strength: "none";
      credential_id?: never;
    } & NoDelegation)
  | (AuthenticatedContext<"user", "user", "password" | "mfa"> & NoDelegation)
  | (AuthenticatedContext<"admin", "admin", "password" | "mfa"> & NoDelegation)
  | (AuthenticatedContext<"integration", "integration_api", "credential"> & NoDelegation)
  | (AuthenticatedContext<"plugin_instance", "host_api", "workload"> & UserDelegation)
  | (AuthenticatedContext<"worker", "worker", "workload"> & UserDelegation);

/** Target contract errors; not yet emitted by the current HTTP middleware. */
export type PrincipalContextErrorCode =
  | "principal.context_invalid"
  | "principal.contract_unsupported";
