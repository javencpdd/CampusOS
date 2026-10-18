-- All pre26 business fields and sequence allocation facts, without excluding
-- Integration permission seeds or inferring the domains of historical actors.
CREATE FUNCTION pg_temp.v12_03b_legacy_snapshot() RETURNS jsonb LANGUAGE plpgsql AS $$
DECLARE item record; result jsonb := '{}'::jsonb; data jsonb;
BEGIN
  FOR item IN SELECT tablename FROM pg_tables WHERE schemaname='public'
    AND tablename NOT IN ('schema_migrations','schema_migration_locks','plugin_package_identities')
    ORDER BY tablename LOOP
    EXECUTE format('SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text COLLATE "C"),''[]''::jsonb) FROM %I t',item.tablename) INTO data;
    result := result || jsonb_build_object(item.tablename,data);
  END LOOP;
  SELECT coalesce(jsonb_agg(to_jsonb(s) ORDER BY sequencename),'[]'::jsonb) INTO data
    FROM pg_sequences s WHERE schemaname='public';
  RETURN jsonb_build_object('tables',result,'sequences',data);
END $$;
SELECT pg_temp.v12_03b_legacy_snapshot()::text;
