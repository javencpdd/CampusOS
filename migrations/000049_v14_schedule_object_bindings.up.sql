-- v0.14 S2: keep the legacy JSON readable while a user/term binding points
-- at the last successfully committed immutable schedule Object.
ALTER TABLE user_schedule_terms
    ADD COLUMN IF NOT EXISTS current_object_id BIGINT NULL REFERENCES storage_objects(id) ON DELETE RESTRICT,
    ADD COLUMN IF NOT EXISTS first_week_start DATE NULL,
    ADD COLUMN IF NOT EXISTS version BIGINT NOT NULL DEFAULT 1;

CREATE INDEX IF NOT EXISTS idx_user_schedule_terms_current_object
    ON user_schedule_terms (current_object_id)
    WHERE current_object_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS user_schedule_preferences (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    academic_term_id BIGINT NOT NULL REFERENCES academic_terms(id) ON DELETE RESTRICT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_user_schedule_preferences_term
    ON user_schedule_preferences (academic_term_id, user_id);
