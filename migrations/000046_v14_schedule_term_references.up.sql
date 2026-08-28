-- v0.14：保留旧课表 JSON 的兼容读取，同时登记用户课表对受管学期的引用。
CREATE TABLE IF NOT EXISTS user_schedule_terms (
    user_id          BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    academic_term_id BIGINT NOT NULL REFERENCES academic_terms(id) ON DELETE RESTRICT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, academic_term_id)
);

CREATE INDEX IF NOT EXISTS idx_user_schedule_terms_academic_term
    ON user_schedule_terms (academic_term_id, user_id);
