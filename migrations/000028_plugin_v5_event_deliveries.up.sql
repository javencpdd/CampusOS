--03c: independent metadata-only delivery queue. Never copy a core payload here.
CREATE TABLE plugin_v5_event_deliveries (
 id text COLLATE "C" PRIMARY KEY CHECK (id ~ '^[0-9a-f]{32}$'),
 state jsonb NOT NULL CHECK (jsonb_typeof(state)='object' AND octet_length(state::text)<=8192),
 source_event_id text COLLATE "C" GENERATED ALWAYS AS(state->>'source_event_id') STORED NOT NULL,
 instance_id text COLLATE "C" GENERATED ALWAYS AS(state->>'instance_id') STORED NOT NULL,
 publisher text COLLATE "C" GENERATED ALWAYS AS(state->>'publisher') STORED NOT NULL,
 plugin_key text COLLATE "C" GENERATED ALWAYS AS(state->>'plugin_key') STORED NOT NULL,
 version text COLLATE "C" GENERATED ALWAYS AS(state->>'version') STORED NOT NULL,
 instance_generation bigint GENERATED ALWAYS AS((state->>'instance_generation')::bigint) STORED NOT NULL CHECK (instance_generation>0),
 FOREIGN KEY (publisher,plugin_key,version) REFERENCES plugin_package_releases(publisher,plugin_key,version),
 UNIQUE (source_event_id,instance_id,instance_generation),
 CHECK ((state-'id'-'publisher'-'plugin_key'-'version'-'source_event_id'-'instance_id'-'owner_user_id'-'instance_generation'-'event'-'status'-'attempts'-'lease_generation'-'lease_until_ms'-'next_at_ms'-'error_code')='{}'::jsonb),
 CHECK ((jsonb_typeof(state->'event')='object' AND jsonb_typeof(state->'event'->'payload')='object' AND jsonb_typeof(state->'attempts')='number' AND jsonb_typeof(state->'lease_until_ms')='number' AND jsonb_typeof(state->'next_at_ms')='number' AND jsonb_typeof(state->'instance_generation')='string' AND jsonb_typeof(state->'lease_generation')='string' AND jsonb_typeof(state->'event'->'revision')='number' AND jsonb_typeof(state->'event'->'created_at_ms')='number' AND jsonb_typeof(state->'event'->'expires_at_ms')='number' AND state->'event'->>'event_id'=source_event_id) IS TRUE),
 CHECK ((id=state->>'id' AND source_event_id ~ '^[0-9]{1,20}$' AND instance_id ~ '^[0-9a-f]{32}$' AND state->>'owner_user_id' ~ '^([1-9][0-9]{0,18})?$' AND state->>'status' IN('queued','working','retry','done','dead') AND (state->>'attempts')::int BETWEEN 0 AND 5 AND (state->>'lease_generation')::bigint>=0 AND (state->>'lease_until_ms')::bigint>=0 AND (state->>'next_at_ms')::bigint>=0 AND length(state->>'error_code')<=128) IS TRUE),
 CHECK ((state->'event'->>'contract'='campusos.plugin-event/v1' AND state->'event'->>'type' IN('plugin.lifecycle.changed/v1','community.thread.public.changed/v1','knowledge.source.public.changed/v1') AND length(state->'event'->>'event_id') BETWEEN 1 AND 128 AND (state->'event'->>'revision')::bigint>0 AND (state->'event'->>'created_at_ms')::bigint>0 AND (state->'event'->>'expires_at_ms')::bigint>(state->'event'->>'created_at_ms')::bigint AND (state->'event'->>'expires_at_ms')::bigint<=(state->'event'->>'created_at_ms')::bigint+300000 AND ((state->'event')-'contract'-'type'-'event_id'-'revision'-'created_at_ms'-'expires_at_ms'-'payload')='{}'::jsonb AND state->'event'->'payload'->>'resource_ref' ~ '^[a-z][a-z0-9.]+:[a-zA-Z0-9_-]{1,128}$' AND state->'event'->'payload'->>'state' IN('available','changed','removed','disabled') AND ((state->'event'->'payload')-'resource_ref'-'state')='{}'::jsonb AND octet_length((state->'event')::text)<=4096) IS TRUE)
);
CREATE INDEX plugin_v5_event_deliveries_release ON plugin_v5_event_deliveries(publisher,plugin_key,version);
CREATE INDEX plugin_v5_event_deliveries_due ON plugin_v5_event_deliveries(((state->>'next_at_ms')::bigint),id) WHERE state->>'status' IN('queued','working','retry');
CREATE FUNCTION guard_plugin_v5_event_delivery() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP IN('DELETE','TRUNCATE') THEN RAISE EXCEPTION 'v5 event evidence is retained'; END IF;
 IF (NEW.state-'status'-'attempts'-'lease_generation'-'lease_until_ms'-'next_at_ms'-'error_code') IS DISTINCT FROM (OLD.state-'status'-'attempts'-'lease_generation'-'lease_until_ms'-'next_at_ms'-'error_code') OR NEW.id<>OLD.id THEN RAISE EXCEPTION 'v5 event intent is immutable'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER plugin_v5_event_delivery_guard BEFORE UPDATE OR DELETE ON plugin_v5_event_deliveries FOR EACH ROW EXECUTE FUNCTION guard_plugin_v5_event_delivery();
CREATE TRIGGER plugin_v5_event_delivery_truncate BEFORE TRUNCATE ON plugin_v5_event_deliveries FOR EACH STATEMENT EXECUTE FUNCTION guard_plugin_v5_event_delivery();
