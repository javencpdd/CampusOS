-- v14 administrator-governed academic terms. A term is a system directory
-- fact, not a Schedule-owned JSON convention. Historical schedule adoption is
-- deliberately deferred to 000046 so this migration remains additive.

CREATE TABLE IF NOT EXISTS academic_terms (
    id               BIGINT PRIMARY KEY,
    year             INTEGER NOT NULL,
    semester         VARCHAR(16) NOT NULL,
    first_week_start DATE NOT NULL,
    status           VARCHAR(16) NOT NULL DEFAULT 'open',
    is_default       BOOLEAN NOT NULL DEFAULT FALSE,
    version          BIGINT NOT NULL DEFAULT 1,
    created_by       BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    updated_by       BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    created_at       TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMP NOT NULL DEFAULT NOW(),
    closed_at        TIMESTAMP NULL,
    CONSTRAINT uk_academic_terms_year_semester UNIQUE (year, semester),
    CONSTRAINT chk_academic_terms_year CHECK (year BETWEEN 2000 AND 2200),
    CONSTRAINT chk_academic_terms_semester CHECK (semester IN ('spring', 'fall')),
    CONSTRAINT chk_academic_terms_first_week_monday CHECK (EXTRACT(ISODOW FROM first_week_start) = 1),
    CONSTRAINT chk_academic_terms_status CHECK (status IN ('open', 'closed')),
    CONSTRAINT chk_academic_terms_default_open CHECK ((NOT is_default) OR status = 'open'),
    CONSTRAINT chk_academic_terms_version CHECK (version >= 1),
    CONSTRAINT chk_academic_terms_closed_at CHECK (
        (status = 'closed' AND closed_at IS NOT NULL) OR (status = 'open' AND closed_at IS NULL)
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS uk_academic_terms_open_default
    ON academic_terms ((is_default))
    WHERE is_default = TRUE AND status = 'open';
CREATE INDEX IF NOT EXISTS idx_academic_terms_status_year
    ON academic_terms (status, year DESC, semester ASC);

INSERT INTO permission_definitions (id, code, domain, resource, action, description, risk_level, allowed_scope_types, audit_level, created_at, updated_at) VALUES
    (900000000000001060, 'schedule.academic_term.read', 'schedule', 'academic_term', 'read', '查看系统学期目录', 'medium', '["global"]'::jsonb, 'standard', NOW(), NOW()),
    (900000000000001061, 'schedule.academic_term.manage', 'schedule', 'academic_term', 'manage', '创建、修改、关闭、开放或设定默认学期', 'high', '["global"]'::jsonb, 'required', NOW(), NOW()),
    (900000000000001062, 'schedule.academic_term.delete', 'schedule', 'academic_term', 'delete', '删除未被课表引用的学期', 'high', '["global"]'::jsonb, 'required', NOW(), NOW())
ON CONFLICT (code) DO NOTHING;

INSERT INTO role_permissions (id, role_id, permission_id, created_by, created_at)
SELECT 900000000000209000 + ROW_NUMBER() OVER (ORDER BY pd.id), r.id, pd.id, 'v14-academic-term', NOW()
FROM roles r
INNER JOIN permission_definitions pd ON pd.code IN (
    'schedule.academic_term.read', 'schedule.academic_term.manage', 'schedule.academic_term.delete'
)
WHERE r.name = 'admin' AND r.deleted_at IS NULL
ON CONFLICT (role_id, permission_id) WHERE deleted_at IS NULL DO NOTHING;
