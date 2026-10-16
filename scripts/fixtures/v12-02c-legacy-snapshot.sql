-- Same complete legacy business rows before/up/down/up. New25 permission
-- seeds are excluded explicitly; all other existing columns stay in scope.
CREATE FUNCTION pg_temp.integration_legacy_snapshot() RETURNS jsonb LANGUAGE plpgsql AS $$
DECLARE item record; result jsonb := '{}'::jsonb; data jsonb; predicate text;
BEGIN
  FOR item IN SELECT tablename FROM pg_tables WHERE schemaname='public' AND tablename NOT IN('schema_migrations','schema_migration_locks','integration_principals','integration_credentials') ORDER BY tablename LOOP
    predicate := '';
    IF item.tablename='permission_definitions' THEN predicate := ' WHERE code NOT IN(''identity.integration.read'',''identity.integration.manage'')'; END IF;
    IF item.tablename='admin_role_permissions' THEN predicate := ' WHERE permission_code NOT IN(''identity.integration.read'',''identity.integration.manage'')'; END IF;
    EXECUTE format('SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text COLLATE "C"),''[]''::jsonb) FROM %I t%s',item.tablename,predicate) INTO data;
    result := result || jsonb_build_object(item.tablename,data);
  END LOOP;
  RETURN result;
END $$;
SELECT pg_temp.integration_legacy_snapshot()::text;
