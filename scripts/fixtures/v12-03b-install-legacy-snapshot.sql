-- Every pre27 business field and old sequence allocation, excluding only the
-- executor metadata and the eight new v5 tables/one new sequence.
CREATE FUNCTION pg_temp.v12_03b_install_legacy_snapshot() RETURNS jsonb LANGUAGE plpgsql AS $$
DECLARE item record; result jsonb := '{}'::jsonb; data jsonb;
BEGIN
 FOR item IN SELECT tablename FROM pg_tables WHERE schemaname='public'
 AND tablename NOT IN ('schema_migrations','schema_migration_locks','plugin_packages','plugin_package_releases','plugin_installations','plugin_user_installations','plugin_v5_admin_grants','plugin_v5_user_consents','plugin_v5_configurations','plugin_operations') ORDER BY tablename LOOP
 EXECUTE format('SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text COLLATE "C"),''[]''::jsonb) FROM %I t',item.tablename) INTO data;
 result:=result||jsonb_build_object(item.tablename,data);
 END LOOP;
 SELECT coalesce(jsonb_agg(to_jsonb(s) ORDER BY sequencename),'[]'::jsonb) INTO data FROM pg_sequences s WHERE schemaname='public' AND sequencename<>'plugin_v5_generations';
 RETURN jsonb_build_object('tables',result,'sequences',data);
END $$;
SELECT pg_temp.v12_03b_install_legacy_snapshot()::text;
