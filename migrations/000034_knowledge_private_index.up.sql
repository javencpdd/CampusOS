-- Private derived bytes are per-selection AEAD ciphertext; keys have separate
-- external custody. Logical original references confer no resource authority.
CREATE SEQUENCE IF NOT EXISTS knowledge_private_epoch_seq AS bigint NO CYCLE;
CREATE TABLE knowledge_private_sources (
 id text PRIMARY KEY CHECK (id ~ '^kp_[0-9a-f]{32}$'),
 owner_user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 kind text NOT NULL CHECK (kind IN ('personal_document','community_thread')),
 source_id bigint NOT NULL CHECK (source_id>0),
 revision text NOT NULL CHECK (revision ~ '^[0-9a-f]{64}$'),
 format text NOT NULL CHECK (format IN ('text','markdown','campusdoc')),
 consent_version bigint NOT NULL DEFAULT nextval('knowledge_private_epoch_seq') CHECK(consent_version>0),
 state text NOT NULL DEFAULT 'pending' CHECK(state IN('pending','ready')),
 nonce bytea NOT NULL CHECK(octet_length(nonce)=12),
 ciphertext bytea NOT NULL CHECK(octet_length(ciphertext) BETWEEN 16 AND 524288),
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE (owner_user_id,kind,source_id)
);
CREATE INDEX knowledge_private_sources_owner ON knowledge_private_sources(owner_user_id,id);
CREATE TABLE knowledge_private_tasks (
 source_id text PRIMARY KEY REFERENCES knowledge_private_sources(id) ON DELETE CASCADE,
 consent_version bigint NOT NULL CHECK(consent_version>0),
 state text NOT NULL DEFAULT 'pending' CHECK(state IN('pending','processing','retry','idle','dead')),
 attempts integer NOT NULL DEFAULT 0 CHECK(attempts BETWEEN 0 AND 5),
 lease_token bigint NOT NULL DEFAULT 0 CHECK(lease_token>=0),
 lease_until_ms bigint NOT NULL DEFAULT 0 CHECK(lease_until_ms>=0),
 available_at_ms bigint NOT NULL DEFAULT 0 CHECK(available_at_ms>=0),
 error_code text NOT NULL DEFAULT '' CHECK(error_code IN('','source_unavailable','key_unavailable','storage_unavailable'))
);
CREATE INDEX knowledge_private_tasks_due ON knowledge_private_tasks(available_at_ms,source_id);
-- Owner/source/consent/ciphertext are append/delete only, never recycled by a
-- worker changing technical readiness. Rekey or a changed source needs reselect.
CREATE FUNCTION guard_knowledge_private_source() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (to_jsonb(NEW)-'state') IS DISTINCT FROM (to_jsonb(OLD)-'state') THEN
  RAISE EXCEPTION 'private source identity and encrypted content are immutable';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER knowledge_private_source_immutable BEFORE UPDATE ON knowledge_private_sources
 FOR EACH ROW EXECUTE FUNCTION guard_knowledge_private_source();
