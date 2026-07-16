CREATE TEMP TABLE campusos_audit_results (
    check_name TEXT PRIMARY KEY,
    category TEXT NOT NULL,
    violations BIGINT NOT NULL
);

INSERT INTO campusos_audit_results VALUES
('accounts.user_id orphan', 'orphan', (SELECT count(*) FROM accounts a LEFT JOIN users u ON u.id = a.user_id WHERE u.id IS NULL)),
('sessions.user_id orphan', 'orphan', (SELECT count(*) FROM sessions s LEFT JOIN users u ON u.id = s.user_id WHERE u.id IS NULL)),
('threads.author_id orphan', 'orphan', (SELECT count(*) FROM threads t LEFT JOIN users u ON u.id = t.author_id WHERE u.id IS NULL)),
('threads.category_id orphan', 'orphan', (SELECT count(*) FROM threads t LEFT JOIN categories c ON c.id = t.category_id WHERE c.id IS NULL)),
('posts.thread_id orphan', 'orphan', (SELECT count(*) FROM posts p LEFT JOIN threads t ON t.id = p.thread_id WHERE t.id IS NULL)),
('posts.author_id orphan', 'orphan', (SELECT count(*) FROM posts p LEFT JOIN users u ON u.id = p.author_id WHERE u.id IS NULL)),
('active user_roles user orphan', 'orphan', (SELECT count(*) FROM user_roles ur LEFT JOIN users u ON u.id = ur.user_id WHERE ur.deleted_at IS NULL AND u.id IS NULL)),
('active user_roles role orphan', 'orphan', (SELECT count(*) FROM user_roles ur LEFT JOIN roles r ON r.id = ur.role_id WHERE ur.deleted_at IS NULL AND r.id IS NULL)),
('duplicate active username', 'duplicate', (SELECT count(*) FROM (SELECT username FROM users WHERE deleted_at IS NULL GROUP BY username HAVING count(*) > 1) d)),
('duplicate active email', 'duplicate', (SELECT count(*) FROM (SELECT email FROM users WHERE deleted_at IS NULL GROUP BY email HAVING count(*) > 1) d)),
('invalid users.status', 'status', (SELECT count(*) FROM users WHERE status NOT IN ('active', 'suspended', 'deactivated'))),
('invalid threads.status', 'status', (SELECT count(*) FROM threads WHERE status NOT IN ('draft', 'pending_review', 'published', 'private', 'archived'))),
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
