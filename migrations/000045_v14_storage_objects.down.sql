-- Isolated rollback for a pristine v0.14 migration drill only. Do not use this
-- after production objects exist; production recovery is forward-fix plus
-- reconciliation so metadata and private files remain auditable.
DROP TABLE IF EXISTS user_storage_reservations;
DROP TABLE IF EXISTS storage_objects;
DROP TABLE IF EXISTS user_storage_accounts;
