-- CampusOS v1.0 baseline rollback.
-- `migrate down` only reaches this file after reference data and the v1 plugin
-- foundation have been rolled back. Migration metadata tables are intentionally
-- retained so the executor can record the rollback.
DO $$
DECLARE
    target record;
BEGIN
    FOR target IN
        SELECT schemaname, tablename
        FROM pg_tables
        WHERE schemaname = 'public'
          AND tablename NOT IN ('schema_migrations', 'schema_migration_locks')
        ORDER BY tablename
    LOOP
        EXECUTE format('DROP TABLE IF EXISTS %I.%I CASCADE', target.schemaname, target.tablename);
    END LOOP;
END $$;

DROP FUNCTION IF EXISTS public.campusos_guard_category_hierarchy() CASCADE;
DROP FUNCTION IF EXISTS public.campusos_guard_category_thread_type_policy() CASCADE;
DROP FUNCTION IF EXISTS public.campusos_guard_mutual_aid_detail() CASCADE;
DROP FUNCTION IF EXISTS public.campusos_guard_secondhand_detail() CASCADE;
DROP FUNCTION IF EXISTS public.sync_identity_admin_account_for_user(bigint, bigint) CASCADE;
DROP FUNCTION IF EXISTS public.sync_identity_admin_account_from_role() CASCADE;

