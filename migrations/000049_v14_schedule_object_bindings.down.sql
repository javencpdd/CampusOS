DROP TABLE IF EXISTS user_schedule_preferences;

ALTER TABLE user_schedule_terms
    DROP COLUMN IF EXISTS current_object_id,
    DROP COLUMN IF EXISTS first_week_start,
    DROP COLUMN IF EXISTS version;
