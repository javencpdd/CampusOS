DROP TRIGGER IF EXISTS trg_mutual_aid_detail_guard ON mutual_aid_details;
DROP FUNCTION IF EXISTS campusos_guard_mutual_aid_detail();

DROP INDEX IF EXISTS idx_mutual_aid_details_created_by_updated;
DROP INDEX IF EXISTS idx_mutual_aid_details_status_updated;
DROP TABLE IF EXISTS mutual_aid_details;
