CREATE SEQUENCE IF NOT EXISTS ai_gateway_epoch_seq AS bigint NO CYCLE;
CREATE TABLE ai_model_profiles (
 id text PRIMARY KEY CHECK (id IN ('remote-chat','local-chat')),
 revision bigint NOT NULL DEFAULT nextval('ai_gateway_epoch_seq') CHECK (revision>0),
 definition text NOT NULL CHECK (definition IN ('builtin.openai-compatible.text','builtin.local-openai.text')),
 model text NOT NULL CHECK (octet_length(model) BETWEEN 1 AND 256),
 endpoint text NOT NULL CHECK (octet_length(endpoint) BETWEEN 1 AND 2048),
 fingerprint text NOT NULL CHECK (fingerprint ~ '^[0-9a-f]{64}$'),
 secret_ref text NOT NULL CHECK (secret_ref ~ '^as_[0-9a-f]{32}$'),
 enabled boolean NOT NULL DEFAULT true,
 timeout_ms bigint NOT NULL CHECK (timeout_ms BETWEEN 100 AND 60000),
 max_tokens integer NOT NULL CHECK (max_tokens BETWEEN 1 AND 65536),
 max_concurrent integer NOT NULL CHECK (max_concurrent BETWEEN 1 AND 32)
);
CREATE TABLE ai_model_secrets (
 id text PRIMARY KEY CHECK (id ~ '^as_[0-9a-f]{32}$'),
 profile_id text NOT NULL REFERENCES ai_model_profiles(id) ON DELETE RESTRICT,
 key_id text NOT NULL CHECK (octet_length(key_id) BETWEEN 1 AND 64),
 nonce bytea NOT NULL CHECK (octet_length(nonce)=12),
 ciphertext bytea NOT NULL CHECK (octet_length(ciphertext) BETWEEN 16 AND 8192),
 endpoint text NOT NULL,
 purpose text NOT NULL CHECK (purpose='chat')
);
CREATE TABLE ai_model_bindings (
 name text PRIMARY KEY CHECK (name='chat'),
 profile_id text NOT NULL REFERENCES ai_model_profiles(id) ON DELETE RESTRICT,
 revision bigint NOT NULL DEFAULT nextval('ai_gateway_epoch_seq') CHECK (revision>0)
);
CREATE INDEX ai_secrets_profile ON ai_model_secrets(profile_id);
CREATE INDEX ai_bindings_profile ON ai_model_bindings(profile_id);
CREATE TABLE ai_model_budgets (
 profile_id text PRIMARY KEY REFERENCES ai_model_profiles(id) ON DELETE RESTRICT,
 quota bigint NOT NULL CHECK (quota BETWEEN 1 AND 1000000000),
 charged bigint NOT NULL DEFAULT 0 CHECK (charged>=0),
 reserved bigint NOT NULL DEFAULT 0 CHECK (reserved>=0),
 CHECK (charged+reserved<=quota)
);
CREATE TABLE ai_model_runs (
 id text PRIMARY KEY CHECK (id ~ '^ar_[0-9a-f]{32}$'),
 owner_id text NOT NULL CHECK (owner_id ~ '^[1-9][0-9]{0,18}$'),
 profile_id text NOT NULL REFERENCES ai_model_profiles(id) ON DELETE RESTRICT,
 profile_revision bigint NOT NULL CHECK (profile_revision>0),
 binding_revision bigint NOT NULL CHECK (binding_revision>0),
 instance_id text NOT NULL CHECK (instance_id ~ '^ai_[0-9a-f]{32}$'),
 state text NOT NULL CHECK (state IN ('reserved','dispatched','completed','failed','cancelled','unknown')),
 lease_until_ms bigint NOT NULL CHECK (lease_until_ms>0),
 charged_units integer CHECK (charged_units IN (0,1)),
 usage_known boolean NOT NULL DEFAULT false,
 prompt_tokens integer NOT NULL DEFAULT 0 CHECK (prompt_tokens>=0),
 completion_tokens integer NOT NULL DEFAULT 0 CHECK (completion_tokens>=0),
 total_tokens integer NOT NULL DEFAULT 0 CHECK (total_tokens>=0),
 created_at timestamptz NOT NULL DEFAULT now(),
 error_code text NOT NULL DEFAULT '' CHECK (error_code IN ('','cancelled','deadline','changed','provider','audit','expired','protection')),
 CHECK ((state IN ('reserved','dispatched') AND charged_units IS NULL) OR (state NOT IN ('reserved','dispatched') AND charged_units IS NOT NULL))
);
CREATE INDEX ai_runs_expired ON ai_model_runs(lease_until_ms,id) WHERE state IN ('reserved','dispatched');
CREATE INDEX ai_runs_owner ON ai_model_runs(owner_id,id);
CREATE INDEX ai_runs_profile ON ai_model_runs(profile_id);
CREATE TABLE ai_model_run_events (
 run_id text NOT NULL REFERENCES ai_model_runs(id) ON DELETE RESTRICT,
 ordinal integer NOT NULL CHECK (ordinal BETWEEN 1 AND 3),
 kind text NOT NULL CHECK (kind IN ('started','usage','completed','failed','cancelled')),
 PRIMARY KEY (run_id,ordinal),
 CHECK ((ordinal=1 AND kind='started') OR (ordinal=2 AND kind='usage') OR (ordinal=3 AND kind IN ('completed','failed','cancelled')))
);
CREATE FUNCTION protect_ai_run_event() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'AI run events are append only'; END $$;
CREATE TRIGGER ai_run_event_immutable BEFORE UPDATE OR DELETE ON ai_model_run_events FOR EACH ROW EXECUTE FUNCTION protect_ai_run_event();
CREATE TRIGGER ai_run_event_no_truncate BEFORE TRUNCATE ON ai_model_run_events FOR EACH STATEMENT EXECUTE FUNCTION protect_ai_run_event();
INSERT INTO permission_definitions(id,code,domain,resource,action,description,risk_level,allowed_scope_types,audit_level) VALUES
 (900000000000003501,'ai.ai.manage','ai','ai','manage','管理受管模型绑定与启停','high','["global"]','required');
INSERT INTO admin_role_permissions(role_id,permission_code,created_by) VALUES ('security_admin','ai.ai.manage','system:migration-35');
