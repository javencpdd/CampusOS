-- v12 C3 Campus Secondhand. The feature owns only structured listing and
-- transaction-progress facts; Community continues to own the base thread,
-- visibility, moderation and recoverable deletion lifecycle.

CREATE TABLE IF NOT EXISTS secondhand_details (
    thread_id BIGINT PRIMARY KEY,
    price_minor BIGINT NOT NULL,
    currency CHAR(3) NOT NULL DEFAULT 'CNY',
    item_condition VARCHAR(32) NOT NULL,
    trade_method VARCHAR(32) NOT NULL,
    trade_status VARCHAR(32) NOT NULL DEFAULT 'available',
    location_scope VARCHAR(160) NOT NULL DEFAULT '',
    version BIGINT NOT NULL DEFAULT 1,
    created_by BIGINT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_secondhand_details_thread
        FOREIGN KEY (thread_id) REFERENCES threads(id) ON DELETE RESTRICT,
    CONSTRAINT fk_secondhand_details_created_by
        FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT chk_secondhand_details_price_minor
        CHECK (price_minor >= 0),
    CONSTRAINT chk_secondhand_details_currency
        CHECK (currency = 'CNY'),
    CONSTRAINT chk_secondhand_details_condition
        CHECK (item_condition IN ('new', 'like_new', 'good', 'fair')),
    CONSTRAINT chk_secondhand_details_trade_method
        CHECK (trade_method IN ('in_person', 'campus_dropoff', 'other')),
    CONSTRAINT chk_secondhand_details_trade_status
        CHECK (trade_status IN ('available', 'reserved', 'sold', 'closed')),
    CONSTRAINT chk_secondhand_details_location_scope
        CHECK (char_length(location_scope) <= 160),
    CONSTRAINT chk_secondhand_details_version
        CHECK (version >= 1)
);

CREATE INDEX IF NOT EXISTS idx_secondhand_details_status_updated
    ON secondhand_details(trade_status, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_secondhand_details_created_by_updated
    ON secondhand_details(created_by, updated_at DESC);

-- This trigger is the database backstop for the same contract enforced by
-- Community.CreateStructuredThread: only a secondhand Thread may receive a
-- Detail, and the feature Detail author cannot diverge from the Thread owner.
CREATE OR REPLACE FUNCTION campusos_guard_secondhand_detail()
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

    IF NOT FOUND OR base_thread_type <> 'secondhand' THEN
        RAISE EXCEPTION 'secondhand detail requires an existing secondhand thread';
    END IF;
    IF NEW.created_by <> base_author_id THEN
        RAISE EXCEPTION 'secondhand detail created_by must match the thread author';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_secondhand_detail_guard ON secondhand_details;
CREATE TRIGGER trg_secondhand_detail_guard
BEFORE INSERT OR UPDATE OF thread_id, created_by
ON secondhand_details
FOR EACH ROW EXECUTE FUNCTION campusos_guard_secondhand_detail();
