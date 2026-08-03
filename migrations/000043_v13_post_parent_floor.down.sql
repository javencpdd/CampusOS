-- Rollback for 000043_v13_post_parent_floor.up.sql. Re-applying the up migration
-- rebuilds the snapshot column from the parent join, so dropping it is safe.

ALTER TABLE posts
    DROP COLUMN IF EXISTS parent_floor_number;
