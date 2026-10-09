-- V12-02a: identity delegation baseline separating execution rights from
-- delegatable ceilings for the two board governance actions
-- (community.thread.take_down, community.post.delete).
--
-- management rows witness an admin's identity.role.assign authority; bound
-- rows are an admin's delegable per-board ceilings; grant rows are the
-- delegated execution rights held by users. All windows are finite half-open
-- intervals; revoked is terminal and requires revoked_at.
CREATE TABLE public.identity_delegations (
    id VARCHAR(128) PRIMARY KEY,
    kind VARCHAR(16) NOT NULL,
    subject_kind VARCHAR(16) NOT NULL,
    subject_id VARCHAR(128) NOT NULL,
    action VARCHAR(128) NOT NULL,
    board_id VARCHAR(128),
    not_before TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    required_strength VARCHAR(16),
    delegable BOOLEAN NOT NULL DEFAULT false,
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    version BIGINT NOT NULL DEFAULT 1,
    created_by VARCHAR(128) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at TIMESTAMPTZ,
    CONSTRAINT chk_identity_delegations_id_format CHECK (id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    CONSTRAINT chk_identity_delegations_kind CHECK (kind IN ('management', 'bound', 'grant')),
    CONSTRAINT chk_identity_delegations_subject CHECK (
        (kind = 'grant' AND subject_kind = 'user')
        OR (kind IN ('management', 'bound') AND subject_kind = 'admin')
    ),
    CONSTRAINT chk_identity_delegations_subject_id_format CHECK (subject_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    CONSTRAINT chk_identity_delegations_action CHECK (
        (kind = 'management' AND action = 'identity.role.assign')
        OR (kind IN ('bound', 'grant') AND action IN ('community.thread.take_down', 'community.post.delete'))
    ),
    CONSTRAINT chk_identity_delegations_board_shape CHECK (
        (kind = 'management' AND board_id IS NULL)
        OR (kind IN ('bound', 'grant') AND board_id IS NOT NULL AND board_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$')
    ),
    CONSTRAINT chk_identity_delegations_strength_shape CHECK (
        (kind = 'management' AND required_strength IS NULL)
        OR (kind IN ('bound', 'grant') AND required_strength IN ('password', 'mfa'))
    ),
    CONSTRAINT chk_identity_delegations_delegable CHECK (delegable = (kind = 'bound')),
    CONSTRAINT chk_identity_delegations_window CHECK (not_before < expires_at),
    CONSTRAINT chk_identity_delegations_status CHECK (status IN ('active', 'suspended', 'revoked')),
    CONSTRAINT chk_identity_delegations_revocation CHECK (
        (status = 'revoked' AND revoked_at IS NOT NULL) OR (status <> 'revoked' AND revoked_at IS NULL)
    ),
    CONSTRAINT chk_identity_delegations_version CHECK (version >= 1)
);
-- Current governance evaluation: active grants by subject/action/board.
CREATE INDEX idx_identity_delegations_grant_current
    ON public.identity_delegations (subject_id, action, board_id, not_before, expires_at)
    WHERE kind = 'grant' AND status = 'active';
-- Admin authority and ceilings for proposal evaluation.
CREATE INDEX idx_identity_delegations_admin_current
    ON public.identity_delegations (subject_id, kind, status)
    WHERE kind IN ('management', 'bound');
-- Moderator listing joins the current grants of one user.
CREATE INDEX idx_identity_delegations_board_current
    ON public.identity_delegations (board_id, action)
    WHERE kind = 'grant' AND status = 'active';

-- Seed conversion from the disposable test dataset (owner-authorized):
-- active admin admissions become management authority; every active board gets
-- delegable bounds for each admin; existing moderator category assignments
-- become delegation grants. The seeded window is a one-year default chosen by
-- this migration; explicit term/strength management arrives with the Admin UI.
INSERT INTO public.identity_delegations
    (id, kind, subject_kind, subject_id, action, not_before, expires_at, created_by)
SELECT 'mgmt-' || aa.user_id, 'management', 'admin', aa.user_id::text, 'identity.role.assign',
       date_trunc('second', NOW()), date_trunc('second', NOW()) + INTERVAL '365 days', 'migration-000008'
FROM public.identity_admin_accounts aa
WHERE aa.status = 'active';

INSERT INTO public.identity_delegations
    (id, kind, subject_kind, subject_id, action, board_id, not_before, expires_at, required_strength, delegable, created_by)
SELECT 'bound-' || aa.user_id || '-' || c.id || '-' || a.action, 'bound', 'admin', aa.user_id::text,
       a.action, c.id::text,
       date_trunc('second', NOW()), date_trunc('second', NOW()) + INTERVAL '365 days', 'password', true, 'migration-000008'
FROM public.identity_admin_accounts aa
CROSS JOIN public.categories c
CROSS JOIN (VALUES ('community.thread.take_down'), ('community.post.delete')) AS a(action)
WHERE aa.status = 'active'
  AND c.node_kind = 'board' AND c.lifecycle_status = 'active' AND c.deleted_at IS NULL;

INSERT INTO public.identity_delegations
    (id, kind, subject_kind, subject_id, action, board_id, not_before, expires_at, required_strength, delegable, created_by)
SELECT 'grant-' || ur.id || '-' || a.action, 'grant', 'user', ur.user_id::text, a.action, ur.scope_id::text,
       date_trunc('second', NOW()), date_trunc('second', NOW()) + INTERVAL '365 days', 'password', false, 'migration-000008'
FROM public.user_roles ur
JOIN public.roles r ON r.id = ur.role_id AND r.name = 'moderator' AND r.deleted_at IS NULL
JOIN public.categories c ON c.id = ur.scope_id AND c.node_kind = 'board' AND c.deleted_at IS NULL
CROSS JOIN (VALUES ('community.thread.take_down'), ('community.post.delete')) AS a(action)
WHERE ur.scope_type = 'category' AND ur.scope_id IS NOT NULL AND ur.deleted_at IS NULL;

-- The two governance actions now come exclusively from delegation grants at
-- category scope; the moderator role retains only its non-delegated codes.
DELETE FROM public.role_permissions rp
USING public.roles r, public.permission_definitions pd
WHERE rp.role_id = r.id AND rp.permission_id = pd.id
  AND r.name = 'moderator'
  AND pd.code IN ('community.thread.take_down', 'community.post.delete');
