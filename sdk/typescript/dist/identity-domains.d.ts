export type IdentityKind = "user" | "admin";
export interface IdentityDomainDescriptor {
    issuer: string;
    audience: string;
    key_id: string;
    credential_store: string;
    session_store: string;
    recovery_store: string;
    role_store: string;
}
export interface IdentityDomainProbe {
    kind: IdentityKind;
    account_id: string;
    issuer: string;
    audience: string;
    key_id: string;
    credential_store: string;
    session_store: string;
    challenge_kind: "access" | "refresh" | "mfa" | "email" | "recovery";
    challenge_domain: IdentityKind;
    target_account_id: string;
    status: "active" | "suspended" | "revoked";
    activation_source: "direct" | "bootstrap" | "user_role";
}
export interface AdminDomainOperation {
    kind: "bootstrap" | "create" | "suspend" | "revoke" | "restore";
    actor_kind: IdentityKind | "system";
    actor_mfa: boolean;
    target_admin_id: string;
    target_is_security_admin: boolean;
    active_security_admins: number;
    bootstrap_enabled: boolean;
    audit_reason: string;
}
export interface IdentityDomainDecisionInput {
    contract: "campusos.identity-domains/v1";
    domains: Record<IdentityKind, IdentityDomainDescriptor>;
    request: IdentityDomainProbe;
    operation?: AdminDomainOperation;
}
export type IdentityDomainErrorCode = "identity.domain_invalid" | "identity.domain_not_isolated" | "identity.cross_domain_credential" | "identity.challenge_wrong_domain" | "identity.admin_auto_activation_forbidden" | "identity.account_inactive" | "identity.admin_bootstrap_forbidden" | "identity.admin_mfa_required" | "identity.last_security_admin" | "identity.admin_actor_required";
