-- V12-02d: independent admin identity domain baseline. Admin accounts,
-- credentials, roles and role bindings are their own stores with no foreign
-- keys to users/accounts; the same numeric ID or email never merges domains.
-- No administrator account, credential or session is seeded: the first
-- security administrator arrives only through the controlled bootstrap path.
CREATE TABLE public.admin_accounts (
    id VARCHAR(128) PRIMARY KEY,
    username VARCHAR(64) NOT NULL,
    email VARCHAR(320) NOT NULL,
    display_name VARCHAR(64) NOT NULL DEFAULT '',
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    status_reason VARCHAR(500) NOT NULL DEFAULT '',
    status_changed_by VARCHAR(128),
    status_changed_at TIMESTAMPTZ,
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at TIMESTAMPTZ,
    CONSTRAINT chk_admin_accounts_username CHECK (username ~ '^[a-z0-9][a-z0-9._-]{1,63}$'),
    CONSTRAINT chk_admin_accounts_email CHECK (position('@' IN email) > 1),
    CONSTRAINT chk_admin_accounts_status CHECK (status IN ('active', 'suspended', 'revoked')),
    CONSTRAINT chk_admin_accounts_revocation CHECK ((status = 'revoked' AND revoked_at IS NOT NULL) OR (status <> 'revoked' AND revoked_at IS NULL)),
    CONSTRAINT chk_admin_accounts_version CHECK (version >= 1),
    CONSTRAINT chk_admin_accounts_status_reason CHECK (char_length(status_reason) <= 500)
);
CREATE UNIQUE INDEX uq_admin_accounts_username ON public.admin_accounts (lower(username));
CREATE UNIQUE INDEX uq_admin_accounts_email ON public.admin_accounts (lower(email));

-- One current password credential per admin account; rotation is a CAS on
-- credential_version. TOTP and recovery stores arrive in their own slice.
CREATE TABLE public.admin_credentials (
    account_id VARCHAR(128) PRIMARY KEY REFERENCES public.admin_accounts(id) ON DELETE CASCADE,
    credential_hash VARCHAR(512) NOT NULL,
    credential_version BIGINT NOT NULL DEFAULT 1,
    password_changed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_admin_credentials_version CHECK (credential_version >= 1),
    CONSTRAINT chk_admin_credentials_hash CHECK (char_length(credential_hash) >= 32)
);

-- Admin-domain roles are reference data; security_admin marks the accounts
-- protected by the last-security-admin rule. No account is seeded.
CREATE TABLE public.admin_roles (
    id VARCHAR(64) PRIMARY KEY,
    name VARCHAR(64) NOT NULL,
    description VARCHAR(500) NOT NULL DEFAULT '',
    is_system BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_admin_roles_id CHECK (id ~ '^[a-z][a-z0-9_]{1,63}$')
);
INSERT INTO public.admin_roles (id, name, description, is_system) VALUES
    ('security_admin', '安全管理员', '管理域最高保护角色；最后一位有效安全管理员不可停用或撤销。', TRUE);

-- Role bindings join admin accounts to admin-domain roles only; the shared
-- permission catalogue constrains which principal kind may hold a code.
CREATE TABLE public.admin_role_bindings (
    id VARCHAR(128) PRIMARY KEY,
    account_id VARCHAR(128) NOT NULL REFERENCES public.admin_accounts(id) ON DELETE CASCADE,
    role_id VARCHAR(64) NOT NULL REFERENCES public.admin_roles(id) ON DELETE RESTRICT,
    granted_by VARCHAR(128) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at TIMESTAMPTZ,
    CONSTRAINT chk_admin_role_bindings_status CHECK (status IN ('active', 'revoked')),
    CONSTRAINT chk_admin_role_bindings_revocation CHECK ((status = 'revoked' AND revoked_at IS NOT NULL) OR (status <> 'revoked' AND revoked_at IS NULL)),
    CONSTRAINT chk_admin_role_bindings_version CHECK (version >= 1)
);
CREATE UNIQUE INDEX uq_admin_role_bindings_active ON public.admin_role_bindings (account_id, role_id) WHERE status = 'active';
CREATE INDEX idx_admin_role_bindings_role ON public.admin_role_bindings (role_id, status);
