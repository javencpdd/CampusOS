-- V12-03b: host-owned package/catalog, separate installation/authority/config,
-- monotonic desired/observed generations and restartable durable operations.
-- DTO JSON is the single fact; generated columns enforce keys and relationships.
CREATE SEQUENCE IF NOT EXISTS plugin_v5_generations AS bigint NO CYCLE;
ALTER SEQUENCE plugin_v5_generations OWNED BY NONE;
CREATE TABLE plugin_packages (
 publisher varchar(63) COLLATE "C" NOT NULL CHECK (publisher ~ '^[a-z0-9][a-z0-9-]{1,62}$'),
 plugin_key varchar(63) COLLATE "C" NOT NULL CHECK (plugin_key ~ '^[a-z0-9][a-z0-9-]{1,62}$'),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (publisher,plugin_key)
);
CREATE TABLE plugin_package_releases (
 record_id text COLLATE "C" PRIMARY KEY CHECK (length(record_id) BETWEEN 3 AND 512),
 state jsonb NOT NULL CHECK ((jsonb_typeof(state)='object' AND octet_length(state::text)<=1048576) IS TRUE),
 publisher text COLLATE "C" GENERATED ALWAYS AS (state->>'publisher') STORED NOT NULL,
 plugin_key text COLLATE "C" GENERATED ALWAYS AS (state->>'plugin_key') STORED NOT NULL,
 version text COLLATE "C" GENERATED ALWAYS AS (state->>'version') STORED NOT NULL,
 CHECK (publisher ~ '^[a-z0-9][a-z0-9-]{1,62}$' AND plugin_key ~ '^[a-z0-9][a-z0-9-]{1,62}$'),
 CHECK (length(version)<=128 AND version ~ '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$'),
 UNIQUE (publisher,plugin_key,version),
 FOREIGN KEY (publisher,plugin_key) REFERENCES plugin_packages(publisher,plugin_key),
 FOREIGN KEY (publisher,plugin_key,version) REFERENCES plugin_package_identities(publisher,plugin_key,version),
 CHECK ((record_id=publisher||'/'||plugin_key||'@'||version AND state->>'package_digest' ~ '^sha256:[0-9a-f]{64}$' AND (state->>'size_bytes')::bigint BETWEEN 1 AND 67108864 AND jsonb_typeof(state->'manifest')='object' AND state->'manifest'->>'publisher'=publisher AND state->'manifest'->>'key'=plugin_key AND state->'manifest'->>'version'=version AND state->'manifest'->>'api_version'='campusos.plugin/v5' AND jsonb_typeof(state->'release')='object' AND (state->>'revision')::bigint>0 AND state->>'storage' IN('available','missing','quarantined','purged')) IS TRUE)
);
CREATE TABLE plugin_installations (
 record_id text COLLATE "C" PRIMARY KEY CHECK (length(record_id) BETWEEN 3 AND 512),
 state jsonb NOT NULL CHECK ((jsonb_typeof(state)='object' AND octet_length(state::text)<=1048576) IS TRUE),
 publisher text COLLATE "C" GENERATED ALWAYS AS (state->>'publisher') STORED NOT NULL,
 plugin_key text COLLATE "C" GENERATED ALWAYS AS (state->>'plugin_key') STORED NOT NULL,
 version text COLLATE "C" GENERATED ALWAYS AS (state->>'version') STORED NOT NULL,
 CHECK (publisher ~ '^[a-z0-9][a-z0-9-]{1,62}$' AND plugin_key ~ '^[a-z0-9][a-z0-9-]{1,62}$'),
 CHECK (length(version)<=128 AND version ~ '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$'),
 FOREIGN KEY (publisher,plugin_key,version) REFERENCES plugin_package_releases(publisher,plugin_key,version),
 revision bigint GENERATED ALWAYS AS ((state->>'revision')::bigint) STORED NOT NULL CHECK (revision>0),
 generation bigint GENERATED ALWAYS AS ((state->>'generation')::bigint) STORED NOT NULL CHECK (generation>0),
 desired_version text COLLATE "C" GENERATED ALWAYS AS (state->>'desired_version') STORED NOT NULL,
 FOREIGN KEY (publisher,plugin_key,desired_version) REFERENCES plugin_package_releases(publisher,plugin_key,version),
 CHECK ((state->>'desired' IN('enabled','disabled','uninstalled') AND state->>'observed' IN('pending','ready','failed','stopped') AND jsonb_typeof(state->'installed')='boolean' AND ((state->>'installed')::boolean=(state->>'desired'<>'uninstalled')) AND (state->>'observed_generation')::bigint BETWEEN 0 AND generation AND (state->>'active_generation')::bigint BETWEEN 0 AND generation AND (state->>'observed'<>'ready' OR ((state->>'installed')::boolean AND state->>'desired'='enabled' AND (state->>'observed_generation')::bigint=generation AND (state->>'active_generation')::bigint=generation AND version=desired_version))) IS TRUE),
 UNIQUE (publisher,plugin_key),
 CHECK ((state->>'owner_user_id'='' AND record_id=publisher||'/'||plugin_key||'#system') IS TRUE)
);
CREATE INDEX plugin_installations_release ON plugin_installations(publisher,plugin_key,version);
CREATE INDEX plugin_installations_desired_release ON plugin_installations(publisher,plugin_key,desired_version);
CREATE TABLE plugin_user_installations (
 record_id text COLLATE "C" PRIMARY KEY CHECK (length(record_id) BETWEEN 3 AND 512),
 state jsonb NOT NULL CHECK ((jsonb_typeof(state)='object' AND octet_length(state::text)<=1048576) IS TRUE),
 publisher text COLLATE "C" GENERATED ALWAYS AS (state->>'publisher') STORED NOT NULL,
 plugin_key text COLLATE "C" GENERATED ALWAYS AS (state->>'plugin_key') STORED NOT NULL,
 version text COLLATE "C" GENERATED ALWAYS AS (state->>'version') STORED NOT NULL,
 CHECK (publisher ~ '^[a-z0-9][a-z0-9-]{1,62}$' AND plugin_key ~ '^[a-z0-9][a-z0-9-]{1,62}$'),
 CHECK (length(version)<=128 AND version ~ '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$'),
 owner_user_id bigint GENERATED ALWAYS AS (NULLIF(state->>'owner_user_id','')::bigint) STORED,
 FOREIGN KEY (owner_user_id) REFERENCES users(id) ON DELETE RESTRICT,
 CHECK ((state->>'owner_user_id'='' OR state->>'owner_user_id' ~ '^[1-9][0-9]{0,18}$') IS TRUE),
 CHECK (owner_user_id IS NOT NULL),
 FOREIGN KEY (publisher,plugin_key,version) REFERENCES plugin_package_releases(publisher,plugin_key,version),
 revision bigint GENERATED ALWAYS AS ((state->>'revision')::bigint) STORED NOT NULL CHECK (revision>0),
 generation bigint GENERATED ALWAYS AS ((state->>'generation')::bigint) STORED NOT NULL CHECK (generation>0),
 desired_version text COLLATE "C" GENERATED ALWAYS AS (state->>'desired_version') STORED NOT NULL,
 FOREIGN KEY (publisher,plugin_key,desired_version) REFERENCES plugin_package_releases(publisher,plugin_key,version),
 CHECK ((state->>'desired' IN('enabled','disabled','uninstalled') AND state->>'observed' IN('pending','ready','failed','stopped') AND jsonb_typeof(state->'installed')='boolean' AND ((state->>'installed')::boolean=(state->>'desired'<>'uninstalled')) AND (state->>'observed_generation')::bigint BETWEEN 0 AND generation AND (state->>'active_generation')::bigint BETWEEN 0 AND generation AND (state->>'observed'<>'ready' OR ((state->>'installed')::boolean AND state->>'desired'='enabled' AND (state->>'observed_generation')::bigint=generation AND (state->>'active_generation')::bigint=generation AND version=desired_version))) IS TRUE),
 UNIQUE (publisher,plugin_key,owner_user_id),
 FOREIGN KEY (publisher,plugin_key) REFERENCES plugin_installations(publisher,plugin_key),
 CHECK (record_id=publisher||'/'||plugin_key||'#user-'||owner_user_id::text)
);
CREATE INDEX plugin_user_installations_owner ON plugin_user_installations(owner_user_id);
CREATE INDEX plugin_user_installations_release ON plugin_user_installations(publisher,plugin_key,version);
CREATE INDEX plugin_user_installations_desired_release ON plugin_user_installations(publisher,plugin_key,desired_version);
CREATE TABLE plugin_v5_admin_grants (
 record_id text COLLATE "C" PRIMARY KEY CHECK (length(record_id) BETWEEN 3 AND 512),
 state jsonb NOT NULL CHECK ((jsonb_typeof(state)='object' AND octet_length(state::text)<=1048576) IS TRUE),
 publisher text COLLATE "C" GENERATED ALWAYS AS (state->>'publisher') STORED NOT NULL,
 plugin_key text COLLATE "C" GENERATED ALWAYS AS (state->>'plugin_key') STORED NOT NULL,
 version text COLLATE "C" GENERATED ALWAYS AS (state->>'version') STORED NOT NULL,
 CHECK (publisher ~ '^[a-z0-9][a-z0-9-]{1,62}$' AND plugin_key ~ '^[a-z0-9][a-z0-9-]{1,62}$'),
 CHECK (length(version)<=128 AND version ~ '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$'),
 FOREIGN KEY (publisher,plugin_key,version) REFERENCES plugin_package_releases(publisher,plugin_key,version),
 revision bigint GENERATED ALWAYS AS ((state->>'revision')::bigint) STORED NOT NULL CHECK (revision>0),
 UNIQUE (publisher,plugin_key,version),
 CHECK ((record_id=publisher||'/'||plugin_key||'@'||version AND jsonb_typeof(state->'approved')='boolean' AND jsonb_typeof(state->'consumes')='array' AND jsonb_array_length(state->'consumes')<=256) IS TRUE)
);
CREATE TABLE plugin_v5_user_consents (
 record_id text COLLATE "C" PRIMARY KEY CHECK (length(record_id) BETWEEN 3 AND 512),
 state jsonb NOT NULL CHECK ((jsonb_typeof(state)='object' AND octet_length(state::text)<=1048576) IS TRUE),
 publisher text COLLATE "C" GENERATED ALWAYS AS (state->>'publisher') STORED NOT NULL,
 plugin_key text COLLATE "C" GENERATED ALWAYS AS (state->>'plugin_key') STORED NOT NULL,
 version text COLLATE "C" GENERATED ALWAYS AS (state->>'version') STORED NOT NULL,
 CHECK (publisher ~ '^[a-z0-9][a-z0-9-]{1,62}$' AND plugin_key ~ '^[a-z0-9][a-z0-9-]{1,62}$'),
 CHECK (length(version)<=128 AND version ~ '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$'),
 owner_user_id bigint GENERATED ALWAYS AS (NULLIF(state->>'owner_user_id','')::bigint) STORED,
 FOREIGN KEY (owner_user_id) REFERENCES users(id) ON DELETE RESTRICT,
 CHECK ((state->>'owner_user_id'='' OR state->>'owner_user_id' ~ '^[1-9][0-9]{0,18}$') IS TRUE),
 CHECK (owner_user_id IS NOT NULL),
 FOREIGN KEY (publisher,plugin_key,version) REFERENCES plugin_package_releases(publisher,plugin_key,version),
 revision bigint GENERATED ALWAYS AS ((state->>'revision')::bigint) STORED NOT NULL CHECK (revision>0),
 UNIQUE (publisher,plugin_key,version,owner_user_id),
 CHECK ((record_id=publisher||'/'||plugin_key||'@'||version||'#user-'||owner_user_id::text AND jsonb_typeof(state->'accepted')='boolean' AND state->>'purpose_hash' ~ '^sha256:[0-9a-f]{64}$' AND (state->>'grant_revision')::bigint>0) IS TRUE)
);
CREATE INDEX plugin_v5_user_consents_owner ON plugin_v5_user_consents(owner_user_id);
CREATE TABLE plugin_v5_configurations (
 record_id text COLLATE "C" PRIMARY KEY CHECK (length(record_id) BETWEEN 3 AND 512),
 state jsonb NOT NULL CHECK ((jsonb_typeof(state)='object' AND octet_length(state::text)<=1048576) IS TRUE),
 publisher text COLLATE "C" GENERATED ALWAYS AS (state->>'publisher') STORED NOT NULL,
 plugin_key text COLLATE "C" GENERATED ALWAYS AS (state->>'plugin_key') STORED NOT NULL,
 version text COLLATE "C" GENERATED ALWAYS AS (state->>'version') STORED NOT NULL,
 CHECK (publisher ~ '^[a-z0-9][a-z0-9-]{1,62}$' AND plugin_key ~ '^[a-z0-9][a-z0-9-]{1,62}$'),
 CHECK (length(version)<=128 AND version ~ '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$'),
 owner_user_id bigint GENERATED ALWAYS AS (NULLIF(state->>'owner_user_id','')::bigint) STORED,
 FOREIGN KEY (owner_user_id) REFERENCES users(id) ON DELETE RESTRICT,
 CHECK ((state->>'owner_user_id'='' OR state->>'owner_user_id' ~ '^[1-9][0-9]{0,18}$') IS TRUE),
 FOREIGN KEY (publisher,plugin_key,version) REFERENCES plugin_package_releases(publisher,plugin_key,version),
 revision bigint GENERATED ALWAYS AS ((state->>'revision')::bigint) STORED NOT NULL CHECK (revision>0),
 UNIQUE NULLS NOT DISTINCT(publisher,plugin_key,version,owner_user_id),
 CHECK ((record_id=publisher||'/'||plugin_key||'@'||version||'#'||CASE WHEN owner_user_id IS NULL THEN 'system' ELSE 'user-'||owner_user_id::text END AND jsonb_typeof(state->'values')='object' AND jsonb_typeof(state->'secret_refs')='object' AND octet_length((state->'values')::text)<=65536 AND octet_length((state->'secret_refs')::text)<=16384) IS TRUE)
);
CREATE INDEX plugin_v5_configurations_owner ON plugin_v5_configurations(owner_user_id);
CREATE TABLE plugin_operations (
 record_id text COLLATE "C" PRIMARY KEY CHECK (length(record_id) BETWEEN 3 AND 512),
 state jsonb NOT NULL CHECK ((jsonb_typeof(state)='object' AND octet_length(state::text)<=1048576) IS TRUE),
 publisher text COLLATE "C" GENERATED ALWAYS AS (state->>'publisher') STORED NOT NULL,
 plugin_key text COLLATE "C" GENERATED ALWAYS AS (state->>'plugin_key') STORED NOT NULL,
 version text COLLATE "C" GENERATED ALWAYS AS (state->>'version') STORED NOT NULL,
 CHECK (publisher ~ '^[a-z0-9][a-z0-9-]{1,62}$' AND plugin_key ~ '^[a-z0-9][a-z0-9-]{1,62}$'),
 CHECK (length(version)<=128 AND version ~ '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$'),
 FOREIGN KEY (publisher,plugin_key,version) REFERENCES plugin_package_releases(publisher,plugin_key,version),
 revision bigint GENERATED ALWAYS AS ((state->>'revision')::bigint) STORED NOT NULL CHECK (revision>0),
 actor_kind text GENERATED ALWAYS AS(state->>'actor_kind') STORED NOT NULL,
 actor_id text COLLATE "C" GENERATED ALWAYS AS(state->>'actor_id') STORED NOT NULL,
 idempotency_key text COLLATE "C" GENERATED ALWAYS AS(state->>'idempotency_key') STORED NOT NULL,
 UNIQUE (actor_kind,actor_id,idempotency_key),
 CHECK ((record_id=state->>'id' AND record_id ~ '^[0-9a-f]{32}$' AND actor_kind IN('user','admin','system') AND length(actor_id) BETWEEN 1 AND 128 AND length(idempotency_key) BETWEEN 1 AND 128 AND state->>'request_hash' ~ '^sha256:[0-9a-f]{64}$' AND state->>'kind' IN('import','install','enable','disable','upgrade','revoke','uninstall','grant','consent','configure','purge_release','retry') AND state->>'status' IN('queued','working','waiting','done','failed','superseded') AND (state->>'generation')::bigint>=0 AND (state->>'lease_generation')::bigint>=0 AND (state->>'lease_until_ms')::bigint>=0 AND (state->>'attempts')::integer>=0 AND length(state->>'error_code')<=128) IS TRUE)
);
CREATE INDEX plugin_operations_release ON plugin_operations(publisher,plugin_key,version);
CREATE INDEX plugin_operations_pending ON plugin_operations((CASE state->>'status' WHEN 'queued' THEN 0 WHEN 'working' THEN 1 ELSE 2 END),((state->>'lease_until_ms')::bigint),record_id) WHERE state->>'status' IN('queued','working','waiting');
CREATE FUNCTION guard_plugin_v5_state() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE known text;
BEGIN
 IF TG_OP='DELETE' OR TG_OP='TRUNCATE' THEN RAISE EXCEPTION 'v5 history is retained; use explicit purge state'; END IF;
 IF TG_TABLE_NAME='plugin_package_releases' THEN
   -- Generated columns are not computed until after BEFORE triggers.
   SELECT package_digest INTO known FROM plugin_package_identities WHERE publisher=NEW.state->>'publisher' AND plugin_key=NEW.state->>'plugin_key' AND version=NEW.state->>'version';
   IF known IS NULL OR known<>NEW.state->>'package_digest' THEN RAISE EXCEPTION 'release identity digest mismatch'; END IF;
   IF TG_OP='UPDATE' AND NEW.state IS DISTINCT FROM OLD.state AND (NEW.state->>'revision')::bigint<=(OLD.state->>'revision')::bigint THEN RAISE EXCEPTION 'release revision cannot regress'; END IF;
   IF TG_OP='UPDATE' AND (NEW.record_id<>OLD.record_id OR (NEW.state - 'storage' - 'release' - 'revision') IS DISTINCT FROM (OLD.state - 'storage' - 'release' - 'revision')) THEN RAISE EXCEPTION 'release identity is immutable'; END IF;
 ELSIF TG_OP='UPDATE' AND NEW.state IS DISTINCT FROM OLD.state THEN
   IF NEW.record_id<>OLD.record_id OR NEW.state->>'publisher'<>OLD.state->>'publisher' OR NEW.state->>'plugin_key'<>OLD.state->>'plugin_key' OR COALESCE(NEW.state->>'owner_user_id','')<>COALESCE(OLD.state->>'owner_user_id','') OR (NEW.state->>'revision')::bigint <= (OLD.state->>'revision')::bigint THEN RAISE EXCEPTION 'v5 state identity/revision changed incorrectly'; END IF;
   IF TG_TABLE_NAME IN('plugin_installations','plugin_user_installations') AND (NEW.state->>'generation')::bigint < (OLD.state->>'generation')::bigint THEN RAISE EXCEPTION 'installation generation cannot regress'; END IF;
   IF TG_TABLE_NAME='plugin_operations' AND (NEW.state - ARRAY['revision','status','error_code','lease_generation','lease_until_ms','attempts','preparation','cleanup_after']) IS DISTINCT FROM (OLD.state - ARRAY['revision','status','error_code','lease_generation','lease_until_ms','attempts','preparation','cleanup_after']) THEN RAISE EXCEPTION 'operation request is immutable'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER plugin_package_releases_guard BEFORE INSERT OR UPDATE OR DELETE ON plugin_package_releases FOR EACH ROW EXECUTE FUNCTION guard_plugin_v5_state();
CREATE TRIGGER plugin_package_releases_no_truncate BEFORE TRUNCATE ON plugin_package_releases FOR EACH STATEMENT EXECUTE FUNCTION guard_plugin_v5_state();
CREATE TRIGGER plugin_installations_guard BEFORE INSERT OR UPDATE OR DELETE ON plugin_installations FOR EACH ROW EXECUTE FUNCTION guard_plugin_v5_state();
CREATE TRIGGER plugin_installations_no_truncate BEFORE TRUNCATE ON plugin_installations FOR EACH STATEMENT EXECUTE FUNCTION guard_plugin_v5_state();
CREATE TRIGGER plugin_user_installations_guard BEFORE INSERT OR UPDATE OR DELETE ON plugin_user_installations FOR EACH ROW EXECUTE FUNCTION guard_plugin_v5_state();
CREATE TRIGGER plugin_user_installations_no_truncate BEFORE TRUNCATE ON plugin_user_installations FOR EACH STATEMENT EXECUTE FUNCTION guard_plugin_v5_state();
CREATE TRIGGER plugin_v5_admin_grants_guard BEFORE INSERT OR UPDATE OR DELETE ON plugin_v5_admin_grants FOR EACH ROW EXECUTE FUNCTION guard_plugin_v5_state();
CREATE TRIGGER plugin_v5_admin_grants_no_truncate BEFORE TRUNCATE ON plugin_v5_admin_grants FOR EACH STATEMENT EXECUTE FUNCTION guard_plugin_v5_state();
CREATE TRIGGER plugin_v5_user_consents_guard BEFORE INSERT OR UPDATE OR DELETE ON plugin_v5_user_consents FOR EACH ROW EXECUTE FUNCTION guard_plugin_v5_state();
CREATE TRIGGER plugin_v5_user_consents_no_truncate BEFORE TRUNCATE ON plugin_v5_user_consents FOR EACH STATEMENT EXECUTE FUNCTION guard_plugin_v5_state();
CREATE TRIGGER plugin_v5_configurations_guard BEFORE INSERT OR UPDATE OR DELETE ON plugin_v5_configurations FOR EACH ROW EXECUTE FUNCTION guard_plugin_v5_state();
CREATE TRIGGER plugin_v5_configurations_no_truncate BEFORE TRUNCATE ON plugin_v5_configurations FOR EACH STATEMENT EXECUTE FUNCTION guard_plugin_v5_state();
CREATE TRIGGER plugin_operations_guard BEFORE INSERT OR UPDATE OR DELETE ON plugin_operations FOR EACH ROW EXECUTE FUNCTION guard_plugin_v5_state();
CREATE TRIGGER plugin_operations_no_truncate BEFORE TRUNCATE ON plugin_operations FOR EACH STATEMENT EXECUTE FUNCTION guard_plugin_v5_state();
