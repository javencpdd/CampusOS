-- v12 C2 Campus Mutual Aid. The feature owns only structured business state;
-- Community continues to own the base thread, visibility, moderation and
-- recoverable deletion lifecycle.

CREATE TABLE IF NOT EXISTS mutual_aid_details (
    thread_id BIGINT PRIMARY KEY,
    aid_type VARCHAR(32) NOT NULL,
    aid_status VARCHAR(32) NOT NULL DEFAULT 'open',
    deadline TIMESTAMP NULL,
    location_scope VARCHAR(160) NOT NULL DEFAULT '',
    contact_mode VARCHAR(32) NOT NULL,
    version BIGINT NOT NULL DEFAULT 1,
    created_by BIGINT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_mutual_aid_details_thread
        FOREIGN KEY (thread_id) REFERENCES threads(id) ON DELETE RESTRICT,
    CONSTRAINT fk_mutual_aid_details_created_by
        FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT chk_mutual_aid_details_type
        CHECK (aid_type IN ('request', 'offer', 'volunteer', 'resource_share')),
    CONSTRAINT chk_mutual_aid_details_status
        CHECK (aid_status IN ('open', 'in_progress', 'resolved', 'closed')),
    CONSTRAINT chk_mutual_aid_details_contact_mode
        CHECK (contact_mode IN ('comment', 'in_app', 'email', 'other')),
    CONSTRAINT chk_mutual_aid_details_location_scope
        CHECK (char_length(location_scope) <= 160),
    CONSTRAINT chk_mutual_aid_details_version
        CHECK (version >= 1),
    CONSTRAINT chk_mutual_aid_details_deadline
        CHECK (deadline IS NULL OR deadline >= created_at)
);

CREATE INDEX IF NOT EXISTS idx_mutual_aid_details_status_updated
    ON mutual_aid_details(aid_status, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_mutual_aid_details_created_by_updated
    ON mutual_aid_details(created_by, updated_at DESC);

-- This trigger is the database backstop for the same contract enforced by
-- Community.CreateStructuredThread: only a mutual_aid Thread may receive a
-- detail, and the feature detail author cannot diverge from the Thread owner.
CREATE OR REPLACE FUNCTION campusos_guard_mutual_aid_detail()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    base_thread_type VARCHAR(32);
    base_author_id BIGINT;
BEGIN
    SELECT thread_type, author_id
    INTO base_thread_type, base_author_id
    FROM threads
    WHERE id = NEW.thread_id
    FOR KEY SHARE;

    IF NOT FOUND OR base_thread_type <> 'mutual_aid' THEN
        RAISE EXCEPTION 'mutual aid detail requires an existing mutual_aid thread';
    END IF;
    IF NEW.created_by <> base_author_id THEN
        RAISE EXCEPTION 'mutual aid detail created_by must match the thread author';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_mutual_aid_detail_guard ON mutual_aid_details;
CREATE TRIGGER trg_mutual_aid_detail_guard
BEFORE INSERT OR UPDATE OF thread_id, created_by
ON mutual_aid_details
FOR EACH ROW EXECUTE FUNCTION campusos_guard_mutual_aid_detail();
