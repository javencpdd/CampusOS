-- V12-01b: host-owned v5 configuration values and opaque Secret references.
-- The versioned definition is the immutable plugin_versions.manifest.configuration;
-- no plugin-controlled SQL or plaintext Secret is persisted here.
CREATE FUNCTION public.plugin_v5_config_values_valid(value_json jsonb)
RETURNS boolean LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE
    item record;
    item_count integer := 0;
BEGIN
    IF jsonb_typeof(value_json) <> 'object' OR octet_length(value_json::text) > 65536 THEN
        RETURN false;
    END IF;
    FOR item IN SELECT key, value FROM jsonb_each(value_json) LOOP
        item_count := item_count + 1;
        IF item_count > 32 OR item.key !~ '^[a-z][a-z0-9_.-]{0,63}$'
           OR jsonb_typeof(item.value) NOT IN ('string', 'number', 'boolean') THEN
            RETURN false;
        END IF;
        IF jsonb_typeof(item.value) = 'string' AND length(item.value #>> '{}') > 512 THEN
            RETURN false;
        END IF;
    END LOOP;
    RETURN true;
END;
$$;

CREATE FUNCTION public.plugin_v5_secret_refs_valid(refs_json jsonb)
RETURNS boolean LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE
    item record;
    item_count integer := 0;
    seen_refs text[] := ARRAY[]::text[];
    ref_value text;
BEGIN
    IF jsonb_typeof(refs_json) <> 'object' OR octet_length(refs_json::text) > 8192 THEN
        RETURN false;
    END IF;
    FOR item IN SELECT key, value FROM jsonb_each(refs_json) LOOP
        item_count := item_count + 1;
        IF item_count > 32 OR item.key !~ '^[a-z][a-z0-9_.-]{0,63}$'
           OR jsonb_typeof(item.value) <> 'string' THEN
            RETURN false;
        END IF;
        ref_value := item.value #>> '{}';
        IF ref_value !~ '^secret-ref:[a-z0-9-]{2,128}$' OR ref_value = ANY(seen_refs) THEN
            RETURN false;
        END IF;
        seen_refs := array_append(seen_refs, ref_value);
    END LOOP;
    RETURN true;
END;
$$;

CREATE TABLE public.plugin_configurations (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    plugin_version_id BIGINT NOT NULL REFERENCES public.plugin_versions(id) ON DELETE CASCADE,
    owner_user_id BIGINT REFERENCES public.users(id) ON DELETE CASCADE,
    definition_version VARCHAR(128) NOT NULL,
    revision BIGINT NOT NULL DEFAULT 1,
    values JSONB NOT NULL DEFAULT '{}'::jsonb,
    secret_refs JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_plugin_configurations_definition_version CHECK (definition_version ~ '^v[1-9][0-9]*$'),
    CONSTRAINT chk_plugin_configurations_revision CHECK (revision > 0),
    CONSTRAINT chk_plugin_configurations_values CHECK (public.plugin_v5_config_values_valid(values)),
    CONSTRAINT chk_plugin_configurations_secret_refs CHECK (public.plugin_v5_secret_refs_valid(secret_refs)),
    CONSTRAINT uq_plugin_configurations_scope UNIQUE NULLS NOT DISTINCT (plugin_version_id, owner_user_id)
);
CREATE INDEX idx_plugin_configurations_owner ON public.plugin_configurations(owner_user_id);
