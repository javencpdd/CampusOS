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
('invalid posts.status', 'status', (SELECT count(*) FROM posts WHERE status NOT IN ('published', 'deleted'))),
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
