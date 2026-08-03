-- Snapshot the parent reply floor number on each reply. Replies are soft-deleted
-- (deleted_at) and list contracts hide deleted rows, so without a snapshot the
-- quoted "回复：第 N 楼" label cannot be resolved once the parent reply is deleted
-- or sits outside the current page.

ALTER TABLE posts
    ADD COLUMN IF NOT EXISTS parent_floor_number INTEGER NOT NULL DEFAULT 0;

-- Backfill existing replies from their parent, including soft-deleted parents.
UPDATE posts child
SET parent_floor_number = parent.floor_number
FROM posts parent
WHERE child.parent_id = parent.id
  AND child.parent_floor_number = 0;
