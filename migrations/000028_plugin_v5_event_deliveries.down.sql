-- Serialize the emptiness check with writers so a concurrent committed
-- delivery cannot appear between the guard and DROP.
LOCK TABLE plugin_v5_event_deliveries IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM plugin_v5_event_deliveries) THEN RAISE EXCEPTION 'cannot roll back nonempty v5 event delivery queue'; END IF;
END $$;
DROP TABLE plugin_v5_event_deliveries;
DROP FUNCTION guard_plugin_v5_event_delivery();
