CREATE TEMP TABLE campusos_audit_results (
    check_name TEXT PRIMARY KEY,
    category TEXT NOT NULL,
    violations BIGINT NOT NULL
);

INSERT INTO campusos_audit_results VALUES
('accounts.user_id orphan', 'orphan', (SELECT count(*) FROM accounts a LEFT JOIN users u ON u.id = a.user_id WHERE u.id IS NULL)),
('admin account user orphan', 'orphan', (SELECT count(*) FROM identity_admin_accounts aa LEFT JOIN users u ON u.id = aa.user_id WHERE u.id IS NULL)),
('admin account credential orphan', 'orphan', (SELECT count(*) FROM identity_admin_accounts aa LEFT JOIN accounts a ON a.id = aa.credential_account_id WHERE a.id IS NULL)),
('admin account credential owner mismatch', 'identity', (SELECT count(*) FROM identity_admin_accounts aa INNER JOIN accounts a ON a.id = aa.credential_account_id WHERE a.user_id <> aa.user_id)),
('invalid admin account state', 'identity', (SELECT count(*) FROM identity_admin_accounts WHERE status NOT IN ('active', 'suspended', 'revoked') OR version < 1 OR ((status = 'revoked') <> (revoked_at IS NOT NULL)))),
('global admin role missing admin account record', 'identity', (
    SELECT count(*)
    FROM user_roles ur
    INNER JOIN roles r ON r.id=ur.role_id AND r.name='admin' AND r.deleted_at IS NULL
    LEFT JOIN identity_admin_accounts aa ON aa.user_id=ur.user_id
    WHERE ur.scope_type='global' AND ur.scope_id IS NULL AND ur.deleted_at IS NULL AND aa.user_id IS NULL
)),
('active admin account missing global admin role', 'identity', (
    SELECT count(*)
    FROM identity_admin_accounts aa
    WHERE aa.status='active' AND NOT EXISTS (
        SELECT 1 FROM user_roles ur
        INNER JOIN roles r ON r.id=ur.role_id AND r.name='admin' AND r.deleted_at IS NULL
        WHERE ur.user_id=aa.user_id AND ur.scope_type='global' AND ur.scope_id IS NULL AND ur.deleted_at IS NULL
    )
)),
('active admin account has inactive identity', 'identity', (
    SELECT count(*)
    FROM identity_admin_accounts aa
    LEFT JOIN users u ON u.id=aa.user_id
    LEFT JOIN accounts a ON a.id=aa.credential_account_id AND a.user_id=aa.user_id
    WHERE aa.status='active' AND (u.id IS NULL OR u.deleted_at IS NOT NULL OR u.status <> 'active' OR a.id IS NULL OR a.deleted_at IS NOT NULL)
)),
('sessions.user_id orphan', 'orphan', (SELECT count(*) FROM sessions s LEFT JOIN users u ON u.id = s.user_id WHERE u.id IS NULL)),
('active session missing refresh digest', 'identity', (SELECT count(*) FROM sessions WHERE deleted_at IS NULL AND revoked_at IS NULL AND (refresh_token_digest IS NULL OR refresh_token_digest !~ '^[0-9a-f]{64}$'))),
('session stores raw refresh token', 'identity', (SELECT count(*) FROM sessions WHERE refresh_token IS NOT NULL)),
('session missing token family', 'identity', (SELECT count(*) FROM sessions WHERE deleted_at IS NULL AND (token_family_id IS NULL OR btrim(token_family_id) = ''))),
('active session expires before creation', 'identity', (SELECT count(*) FROM sessions WHERE deleted_at IS NULL AND expires_at <= created_at)),
('threads.author_id orphan', 'orphan', (SELECT count(*) FROM threads t LEFT JOIN users u ON u.id = t.author_id WHERE u.id IS NULL)),
('threads.category_id orphan', 'orphan', (SELECT count(*) FROM threads t LEFT JOIN categories c ON c.id = t.category_id WHERE c.id IS NULL)),
('posts.thread_id orphan', 'orphan', (SELECT count(*) FROM posts p LEFT JOIN threads t ON t.id = p.thread_id WHERE t.id IS NULL)),
('posts.author_id orphan', 'orphan', (SELECT count(*) FROM posts p LEFT JOIN users u ON u.id = p.author_id WHERE u.id IS NULL)),
('active user_roles user orphan', 'orphan', (SELECT count(*) FROM user_roles ur LEFT JOIN users u ON u.id = ur.user_id WHERE ur.deleted_at IS NULL AND u.id IS NULL)),
('active user_roles role orphan', 'orphan', (SELECT count(*) FROM user_roles ur LEFT JOIN roles r ON r.id = ur.role_id WHERE ur.deleted_at IS NULL AND r.id IS NULL)),
('duplicate active username', 'duplicate', (SELECT count(*) FROM (SELECT username FROM users WHERE deleted_at IS NULL GROUP BY username HAVING count(*) > 1) d)),
('duplicate active email', 'duplicate', (SELECT count(*) FROM (SELECT email FROM users WHERE deleted_at IS NULL GROUP BY email HAVING count(*) > 1) d)),
('duplicate active email normalized', 'duplicate', (SELECT count(*) FROM (SELECT lower(btrim(email)) FROM users WHERE deleted_at IS NULL GROUP BY lower(btrim(email)) HAVING count(*) > 1) d)),
('duplicate active account email normalized', 'duplicate', (SELECT count(*) FROM (SELECT identifier_normalized FROM accounts WHERE type = 'email' AND deleted_at IS NULL GROUP BY identifier_normalized HAVING count(*) > 1) d)),
('multiple active email accounts per user', 'duplicate', (SELECT count(*) FROM (SELECT user_id FROM accounts WHERE type = 'email' AND deleted_at IS NULL GROUP BY user_id HAVING count(*) > 1) d)),
('users/accounts active email mismatch', 'identity', (SELECT count(*) FROM users u JOIN accounts a ON a.user_id = u.id AND a.type = 'email' AND a.deleted_at IS NULL WHERE u.deleted_at IS NULL AND lower(btrim(u.email)) <> a.identifier_normalized)),
('invalid users.status', 'status', (SELECT count(*) FROM users WHERE status NOT IN ('active', 'suspended', 'deactivated'))),
('invalid users.auth_version', 'identity', (SELECT count(*) FROM users WHERE auth_version < 1)),
('invalid accounts.verification_state', 'identity', (SELECT count(*) FROM accounts WHERE verification_state NOT IN ('unverified', 'legacy_accepted', 'verified', 'system_managed'))),
('invalid accounts.identifier_normalized', 'identity', (SELECT count(*) FROM accounts WHERE type = 'email' AND (identifier_normalized <> lower(btrim(identifier)) OR identifier <> identifier_normalized))),
('identity challenge account orphan', 'orphan', (SELECT count(*) FROM identity_email_challenges c LEFT JOIN accounts a ON a.id = c.account_id WHERE c.account_id IS NOT NULL AND a.id IS NULL)),
('invalid identity challenge purpose', 'identity', (SELECT count(*) FROM identity_email_challenges WHERE purpose NOT IN ('registration', 'email_binding', 'password_reset'))),
('invalid identity challenge normalized email', 'identity', (SELECT count(*) FROM identity_email_challenges WHERE email_normalized <> lower(btrim(email_normalized)))),
('invalid identity challenge attempt state', 'identity', (SELECT count(*) FROM identity_email_challenges WHERE attempt_count < 0 OR max_attempts NOT BETWEEN 1 AND 10 OR attempt_count > max_attempts)),
('invalid identity challenge ticket state', 'identity', (SELECT count(*) FROM identity_email_challenges WHERE (ticket_digest IS NULL) <> (ticket_expires_at IS NULL) OR (ticket_digest IS NOT NULL AND verified_at IS NULL))),
('invalid identity challenge rate state', 'identity', (SELECT count(*) FROM identity_challenge_rate_limits WHERE scope NOT IN ('email_minute', 'email_day', 'ip_hour', 'email_window', 'ip_window') OR request_count < 0 OR request_count > 10000)),
('invalid identity challenge policy', 'identity', (SELECT count(*) FROM identity_challenge_policies WHERE id <> 'email_verification' OR email_window_minutes NOT BETWEEN 1 AND 1440 OR email_max_requests NOT BETWEEN 1 AND 100 OR ip_window_minutes NOT BETWEEN 1 AND 1440 OR ip_max_requests NOT BETWEEN 1 AND 1000 OR version < 1)),
('reserved identifier used by challenge', 'identity', (SELECT count(*) FROM identity_email_challenges c INNER JOIN identity_reserved_identifiers r ON r.identifier_type = 'email' AND r.identifier_normalized = c.email_normalized)),
('identity recovery case user orphan', 'orphan', (SELECT count(*) FROM identity_account_recovery_cases c LEFT JOIN users u ON u.id = c.user_id WHERE u.id IS NULL)),
('identity recovery case account orphan', 'orphan', (SELECT count(*) FROM identity_account_recovery_cases c LEFT JOIN accounts a ON a.id = c.account_id WHERE a.id IS NULL)),
('identity recovery case challenge orphan', 'orphan', (SELECT count(*) FROM identity_account_recovery_cases c LEFT JOIN identity_email_challenges ch ON ch.id = c.challenge_id WHERE ch.id IS NULL)),
('identity recovery case creator orphan', 'orphan', (SELECT count(*) FROM identity_account_recovery_cases c LEFT JOIN users u ON u.id = c.created_by WHERE c.created_by IS NOT NULL AND u.id IS NULL)),
('invalid identity recovery case state', 'identity', (SELECT count(*) FROM identity_account_recovery_cases WHERE status NOT IN ('pending', 'completed', 'cancelled', 'expired') OR target_email_normalized <> lower(btrim(target_email_normalized)))),
('invalid identity recovery completion state', 'identity', (SELECT count(*) FROM identity_account_recovery_cases WHERE (status = 'completed' AND completed_at IS NULL) OR (status <> 'completed' AND completed_at IS NOT NULL))),
('invalid categories.node_kind', 'community', (SELECT count(*) FROM categories WHERE node_kind NOT IN ('group', 'board'))),
('invalid categories.lifecycle_status', 'community', (SELECT count(*) FROM categories WHERE lifecycle_status NOT IN ('active', 'archived'))),
('invalid categories.version', 'community', (SELECT count(*) FROM categories WHERE version < 1)),
('invalid categories.color', 'community', (SELECT count(*) FROM categories WHERE color <> '' AND color !~ '^#[0-9A-Fa-f]{6}([0-9A-Fa-f]{2})?$')),
('category group has parent', 'community', (SELECT count(*) FROM categories WHERE deleted_at IS NULL AND node_kind = 'group' AND parent_id IS NOT NULL)),
('category board owns children', 'community', (SELECT count(*) FROM categories parent WHERE parent.deleted_at IS NULL AND parent.node_kind = 'board' AND EXISTS (SELECT 1 FROM categories child WHERE child.parent_id = parent.id AND child.deleted_at IS NULL))),
('category child parent is not active group', 'community', (SELECT count(*) FROM categories child LEFT JOIN categories parent ON parent.id = child.parent_id AND parent.deleted_at IS NULL WHERE child.deleted_at IS NULL AND child.parent_id IS NOT NULL AND (parent.id IS NULL OR parent.node_kind <> 'group' OR parent.lifecycle_status <> 'active'))),
('archived group has active child board', 'community', (SELECT count(*) FROM categories parent WHERE parent.deleted_at IS NULL AND parent.node_kind = 'group' AND parent.lifecycle_status = 'archived' AND EXISTS (SELECT 1 FROM categories child WHERE child.parent_id = parent.id AND child.deleted_at IS NULL AND child.node_kind = 'board' AND child.lifecycle_status = 'active'))),
('category thread type policy category orphan', 'orphan', (SELECT count(*) FROM category_thread_type_policies policy LEFT JOIN categories category ON category.id = policy.category_id WHERE category.id IS NULL)),
('category thread type policy targets non-board', 'community', (SELECT count(*) FROM category_thread_type_policies policy INNER JOIN categories category ON category.id = policy.category_id WHERE category.deleted_at IS NOT NULL OR category.node_kind <> 'board')),
('active board missing thread type policy', 'community', (SELECT count(*) FROM categories category WHERE category.deleted_at IS NULL AND category.node_kind = 'board' AND NOT EXISTS (SELECT 1 FROM category_thread_type_policies policy WHERE policy.category_id = category.id AND policy.enabled = TRUE))),
('invalid threads.status', 'status', (SELECT count(*) FROM threads WHERE status NOT IN ('draft', 'pending_review', 'published', 'private', 'archived'))),
('invalid threads.thread_type', 'community', (SELECT count(*) FROM threads WHERE thread_type NOT IN ('discussion', 'article', 'mutual_aid', 'secondhand'))),
('mutual aid detail thread orphan', 'orphan', (SELECT count(*) FROM mutual_aid_details detail LEFT JOIN threads thread ON thread.id = detail.thread_id WHERE thread.id IS NULL)),
('mutual aid detail creator orphan', 'orphan', (SELECT count(*) FROM mutual_aid_details detail LEFT JOIN users owner ON owner.id = detail.created_by WHERE owner.id IS NULL)),
('mutual aid detail type mismatch', 'community', (SELECT count(*) FROM mutual_aid_details detail INNER JOIN threads thread ON thread.id = detail.thread_id WHERE thread.thread_type <> 'mutual_aid' OR detail.created_by <> thread.author_id)),
('invalid mutual aid detail state', 'community', (SELECT count(*) FROM mutual_aid_details WHERE aid_type NOT IN ('request', 'offer', 'volunteer', 'resource_share') OR aid_status NOT IN ('open', 'in_progress', 'resolved', 'closed') OR contact_mode NOT IN ('comment', 'in_app', 'email', 'other') OR version < 1 OR char_length(location_scope) > 160 OR (deadline IS NOT NULL AND deadline < created_at))),
('secondhand detail thread orphan', 'orphan', (SELECT count(*) FROM secondhand_details detail LEFT JOIN threads thread ON thread.id = detail.thread_id WHERE thread.id IS NULL)),
('secondhand detail creator orphan', 'orphan', (SELECT count(*) FROM secondhand_details detail LEFT JOIN users owner ON owner.id = detail.created_by WHERE owner.id IS NULL)),
('secondhand detail type mismatch', 'community', (SELECT count(*) FROM secondhand_details detail INNER JOIN threads thread ON thread.id = detail.thread_id WHERE thread.thread_type <> 'secondhand' OR detail.created_by <> thread.author_id)),
('invalid secondhand detail state', 'community', (SELECT count(*) FROM secondhand_details WHERE price_minor < 0 OR currency <> 'CNY' OR item_condition NOT IN ('new', 'like_new', 'good', 'fair') OR trade_method NOT IN ('in_person', 'campus_dropoff', 'other') OR trade_status NOT IN ('available', 'reserved', 'sold', 'closed') OR version < 1 OR char_length(location_scope) > 160)),
('invalid threads.publication_status', 'status', (SELECT count(*) FROM threads WHERE publication_status NOT IN ('draft', 'published', 'private'))),
('invalid threads.moderation_status', 'status', (SELECT count(*) FROM threads WHERE moderation_status NOT IN ('clear', 'pending', 'rejected', 'taken_down'))),
('invalid threads.deletion_status', 'status', (SELECT count(*) FROM threads WHERE deletion_status NOT IN ('active', 'trashed', 'purged'))),
('deleted thread not purged in governance state', 'status', (SELECT count(*) FROM threads WHERE deleted_at IS NOT NULL AND deletion_status <> 'purged')),
('invalid posts.status', 'status', (SELECT count(*) FROM posts WHERE status NOT IN ('published', 'deleted'))),
('content_revisions.thread_id orphan', 'orphan', (SELECT count(*) FROM content_revisions cr LEFT JOIN threads t ON t.id = cr.thread_id WHERE t.id IS NULL)),
('content_moderation_cases.thread_id orphan', 'orphan', (SELECT count(*) FROM content_moderation_cases mc LEFT JOIN threads t ON t.id = mc.thread_id WHERE t.id IS NULL)),
('content_moderation_actions.thread_id orphan', 'orphan', (SELECT count(*) FROM content_moderation_actions ma LEFT JOIN threads t ON t.id = ma.thread_id WHERE t.id IS NULL)),
('content_moderation_actions.case_id orphan', 'orphan', (SELECT count(*) FROM content_moderation_actions ma LEFT JOIN content_moderation_cases mc ON mc.id = ma.case_id WHERE ma.case_id IS NOT NULL AND mc.id IS NULL)),
('active role_permissions role orphan', 'orphan', (SELECT count(*) FROM role_permissions rp LEFT JOIN roles r ON r.id = rp.role_id WHERE rp.deleted_at IS NULL AND r.id IS NULL)),
('active role_permissions definition orphan', 'orphan', (SELECT count(*) FROM role_permissions rp LEFT JOIN permission_definitions pd ON pd.id = rp.permission_id WHERE rp.deleted_at IS NULL AND pd.id IS NULL)),
('active route_permission_bindings operation orphan', 'orphan', (SELECT count(*) FROM route_permission_bindings rb LEFT JOIN route_operations ro ON ro.id = rb.route_operation_id WHERE rb.deleted_at IS NULL AND ro.id IS NULL)),
('active route_permission_bindings definition orphan', 'orphan', (SELECT count(*) FROM route_permission_bindings rb LEFT JOIN permission_definitions pd ON pd.id = rb.permission_id WHERE rb.deleted_at IS NULL AND pd.id IS NULL)),
('negative content counter', 'counter', (
    SELECT (SELECT count(*) FROM categories WHERE thread_count < 0 OR post_count < 0)
         + (SELECT count(*) FROM threads WHERE view_count < 0 OR reply_count < 0 OR like_count < 0)
         + (SELECT count(*) FROM posts WHERE like_count < 0 OR floor_number < 0)
));

SELECT jsonb_pretty(jsonb_build_object(
    'generated_at', now(),
    'database', current_database(),
    'checks', jsonb_agg(to_jsonb(campusos_audit_results) ORDER BY category, check_name),
    'total_violations', sum(violations)
)) AS database_audit
FROM campusos_audit_results;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM campusos_audit_results WHERE violations > 0) THEN
        RAISE EXCEPTION 'database audit failed; one or more checks reported violations';
    END IF;
END $$;
