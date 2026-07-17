DROP TRIGGER IF EXISTS trg_secondhand_detail_guard ON secondhand_details;
DROP FUNCTION IF EXISTS campusos_guard_secondhand_detail();

DROP INDEX IF EXISTS idx_secondhand_details_created_by_updated;
DROP INDEX IF EXISTS idx_secondhand_details_status_updated;
DROP TABLE IF EXISTS secondhand_details;
