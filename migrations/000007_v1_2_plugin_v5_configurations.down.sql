-- Refuse a rollback that would erase persisted configuration or Secret bindings.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.plugin_configurations LIMIT 1) THEN
        RAISE EXCEPTION 'plugin configuration data must be exported and removed before rollback'
            USING ERRCODE = '23514';
    END IF;
END;
$$;
DROP TABLE public.plugin_configurations;
DROP FUNCTION public.plugin_v5_secret_refs_valid(jsonb);
DROP FUNCTION public.plugin_v5_config_values_valid(jsonb);
