-- Removing a live barrier could revive old credentials. Empty-only rollback.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM instance_recovery_epochs) THEN
  RAISE EXCEPTION 'instance recovery evidence exists; rollback would remove the authentication barrier';
 END IF;
END $$;
DROP TABLE instance_recovery_epochs;
