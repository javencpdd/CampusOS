-- V12-02d rollback: restore the v1.1 user-role auto-admission trigger and its
-- helper exactly as the frozen baseline defined them.
CREATE FUNCTION public.sync_identity_admin_account_for_user(target_user_id bigint, preferred_assignment_id bigint) RETURNS void
    LANGUAGE plpgsql
    SET search_path = pg_catalog, public
    AS $$
DECLARE
    active_assignment_id BIGINT;
    active_credential_id BIGINT;
BEGIN
    PERFORM pg_advisory_xact_lock(130013, 39);

    SELECT ur.id, a.id
    INTO active_assignment_id, active_credential_id
    FROM user_roles ur
    INNER JOIN roles r
        ON r.id = ur.role_id
       AND r.name = 'admin'
       AND r.deleted_at IS NULL
    INNER JOIN accounts a
        ON a.user_id = ur.user_id
       AND a.type = 'email'
       AND a.deleted_at IS NULL
    WHERE ur.user_id = target_user_id
      AND ur.scope_type = 'global'
      AND ur.scope_id IS NULL
      AND ur.deleted_at IS NULL
    ORDER BY (ur.id = preferred_assignment_id) DESC, ur.created_at ASC, ur.id ASC
    LIMIT 1;

    IF FOUND THEN
        INSERT INTO identity_admin_accounts (
            id, user_id, credential_account_id, status, activation_source,
            activated_at, status_reason, status_changed_at, version, created_at, updated_at
        ) VALUES (
            active_assignment_id, target_user_id, active_credential_id, 'active',
            'role_assignment', NOW(), 'role_assignment', NOW(), 1, NOW(), NOW()
        )
        ON CONFLICT (user_id) DO UPDATE
        SET credential_account_id = EXCLUDED.credential_account_id,
            status = CASE
                WHEN identity_admin_accounts.status = 'suspended' THEN 'suspended'
                ELSE 'active'
            END,
            activation_source = CASE
                WHEN identity_admin_accounts.status = 'suspended' THEN identity_admin_accounts.activation_source
                ELSE EXCLUDED.activation_source
            END,
            activated_at = CASE
                WHEN identity_admin_accounts.status = 'revoked' THEN NOW()
                ELSE identity_admin_accounts.activated_at
            END,
            revoked_at = CASE
                WHEN identity_admin_accounts.status = 'suspended' THEN identity_admin_accounts.revoked_at
                ELSE NULL
            END,
            status_reason = CASE
                WHEN identity_admin_accounts.status = 'suspended' THEN identity_admin_accounts.status_reason
                WHEN identity_admin_accounts.status = 'revoked' THEN 'role_assignment'
                ELSE identity_admin_accounts.status_reason
            END,
            status_changed_by = CASE
                WHEN identity_admin_accounts.status = 'suspended' THEN identity_admin_accounts.status_changed_by
                WHEN identity_admin_accounts.status = 'revoked' THEN NULL
                ELSE identity_admin_accounts.status_changed_by
            END,
            status_changed_at = CASE
                WHEN identity_admin_accounts.status = 'suspended' THEN identity_admin_accounts.status_changed_at
                WHEN identity_admin_accounts.status = 'revoked' THEN NOW()
                ELSE identity_admin_accounts.status_changed_at
            END,
            updated_at = NOW(),
            version = identity_admin_accounts.version + 1;
        RETURN;
    END IF;

    UPDATE identity_admin_accounts
    SET status = 'revoked',
        revoked_at = COALESCE(revoked_at, NOW()),
        status_reason = 'admin_role_revoked',
        status_changed_by = NULL,
        status_changed_at = NOW(),
        updated_at = NOW(),
        version = version + 1
    WHERE user_id = target_user_id
      AND status <> 'revoked';
END;
$$;

CREATE FUNCTION public.sync_identity_admin_account_from_role() RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, public
    AS $$
DECLARE
    old_is_admin BOOLEAN := FALSE;
    new_is_admin BOOLEAN := FALSE;
BEGIN
    IF TG_OP IN ('UPDATE', 'DELETE') THEN
        SELECT EXISTS (
            SELECT 1 FROM roles WHERE id=OLD.role_id AND name='admin' AND deleted_at IS NULL
        ) INTO old_is_admin;
    END IF;
    IF TG_OP IN ('INSERT', 'UPDATE') THEN
        SELECT EXISTS (
            SELECT 1 FROM roles WHERE id=NEW.role_id AND name='admin' AND deleted_at IS NULL
        ) INTO new_is_admin;
    END IF;

    IF old_is_admin THEN
        PERFORM sync_identity_admin_account_for_user(OLD.user_id, OLD.id);
    END IF;
    IF new_is_admin AND (NOT old_is_admin OR TG_OP <> 'UPDATE' OR NEW.user_id <> OLD.user_id) THEN
        PERFORM sync_identity_admin_account_for_user(NEW.user_id, NEW.id);
    END IF;
    RETURN NULL;
END;
$$;

CREATE TRIGGER trg_sync_identity_admin_account_from_role AFTER INSERT OR DELETE OR UPDATE OF user_id, role_id, scope_type, scope_id, deleted_at ON public.user_roles FOR EACH ROW EXECUTE FUNCTION public.sync_identity_admin_account_from_role();
