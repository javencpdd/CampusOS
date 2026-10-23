-- Index execution/projections are separate from immutable 000031 requests.
CREATE SEQUENCE IF NOT EXISTS knowledge_index_epoch_seq AS bigint NO CYCLE;
CREATE FUNCTION advance_knowledge_index_facts() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' AND NEW.id<>OLD.id THEN RAISE EXCEPTION 'index identity is immutable'; END IF;
 NEW.data := NEW.data - 'version';
 NEW.facts_version := nextval('knowledge_index_epoch_seq');
 RETURN NEW;
END $$;
CREATE TABLE knowledge_index_jobs (
 id text PRIMARY KEY CHECK (id ~ '^kc_[0-9a-f]{32}$'),
 data jsonb NOT NULL CHECK (jsonb_typeof(data)='object' AND data->>'id'=id AND octet_length(data::text)<=8192),
 facts_version bigint NOT NULL DEFAULT nextval('knowledge_index_epoch_seq') CHECK (facts_version>0),
 state text GENERATED ALWAYS AS (data->>'state') STORED NOT NULL CHECK (state IN ('pending','processing','retry','idle','dead')),
 available_at_ms bigint GENERATED ALWAYS AS ((data->>'available_at_ms')::bigint) STORED NOT NULL CHECK (available_at_ms>=0),
 lease_until_ms bigint GENERATED ALWAYS AS ((data->>'lease_until_ms')::bigint) STORED NOT NULL CHECK (lease_until_ms>=0),
 lease_token bigint GENERATED ALWAYS AS ((data->>'lease_token')::bigint) STORED NOT NULL CHECK (lease_token>=0),
 CHECK (data ?& ARRAY['requested','active_request','run_generation','ready_generation','collection_version','phase','attempts','last_error']),
 CHECK ((data->>'requested')::bigint>0 AND (data->>'attempts')::int BETWEEN 0 AND 8),
 CHECK (data->>'phase' IN ('','members','sources','documents','purge')),
 CHECK (state<>'processing' OR (lease_until_ms>0 AND lease_token>0))
);
CREATE INDEX knowledge_index_job_due ON knowledge_index_jobs(available_at_ms,id);
CREATE TRIGGER knowledge_index_job_facts BEFORE INSERT OR UPDATE ON knowledge_index_jobs
 FOR EACH ROW EXECUTE FUNCTION advance_knowledge_index_facts();

CREATE TABLE knowledge_index_documents (
 id text PRIMARY KEY CHECK (id ~ '^[0-9a-f]{64}$'),
 data jsonb NOT NULL CHECK (jsonb_typeof(data)='object' AND data->>'id'=id AND octet_length(data::text)<=524288),
 facts_version bigint NOT NULL DEFAULT nextval('knowledge_index_epoch_seq') CHECK (facts_version>0),
 collection_id text GENERATED ALWAYS AS (data->>'collection_id') STORED NOT NULL CHECK (collection_id ~ '^kc_[0-9a-f]{32}$'),
 selection_id text GENERATED ALWAYS AS (data->>'selection_id') STORED NOT NULL CHECK (selection_id ~ '^ks_[0-9a-f]{32}$'),
 generation bigint GENERATED ALWAYS AS ((data->>'generation')::bigint) STORED NOT NULL CHECK (generation>0),
 CHECK (data ?& ARRAY['member_id','collection_version','selection_version','source','visibility_revision','parser_version','chunker_version','profile_fingerprint','title','url','chunks']),
 CHECK (jsonb_typeof(data->'chunks')='array' AND jsonb_array_length(data->'chunks') BETWEEN 1 AND 64),
 CHECK (data->'source'->>'kind' IN ('community.thread','richtext.article','knowledge.official')),
 CHECK (data->'source'->>'revision' ~ '^[0-9a-f]{64}$'),
 CHECK (data->>'visibility_revision' ~ '^[0-9a-f]{64}$'),
 CHECK (data->>'profile_fingerprint' ~ '^[0-9a-f]{64}$')
);
CREATE INDEX knowledge_index_document_page ON knowledge_index_documents(collection_id,id);
CREATE INDEX knowledge_index_document_source ON knowledge_index_documents((data->'source'->>'kind'),(data->'source'->>'id'));
CREATE TRIGGER knowledge_index_document_facts BEFORE INSERT OR UPDATE ON knowledge_index_documents
 FOR EACH ROW EXECUTE FUNCTION advance_knowledge_index_facts();
-- Logical references permit eventual cleanup of vanished collections/members.
-- No seed, permissions, source rows or old sequences are modified.
