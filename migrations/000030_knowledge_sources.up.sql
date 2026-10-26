-- Knowledge owns selection and reference metadata, never copies Community bodies.
CREATE SEQUENCE IF NOT EXISTS knowledge_facts_version_seq AS bigint NO CYCLE;
CREATE FUNCTION advance_knowledge_facts() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP = 'UPDATE' AND NEW.id <> OLD.id THEN RAISE EXCEPTION 'knowledge identity is immutable'; END IF;
 NEW.data := NEW.data - 'version';
 NEW.facts_version := nextval('knowledge_facts_version_seq');
 RETURN NEW;
END $$;
CREATE TABLE knowledge_collections (
 CONSTRAINT knowledge_collections_fields CHECK (data ?& ARRAY['title','status','created_by_admin_id'] AND NOT jsonb_path_exists(data, '$.* ? (@ == null)')),
 id text PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 96),
 data jsonb NOT NULL CHECK (jsonb_typeof(data)='object' AND data ? 'id' AND data->>'id'=id),
 facts_version bigint NOT NULL DEFAULT nextval('knowledge_facts_version_seq') CHECK (facts_version>0),
 
 CHECK (data->>'status' IN ('active','disabled')), CHECK (octet_length(data->>'title') BETWEEN 1 AND 1024)
);
CREATE TRIGGER knowledge_collections_facts BEFORE INSERT OR UPDATE ON knowledge_collections
 FOR EACH ROW EXECUTE FUNCTION advance_knowledge_facts();
CREATE TABLE knowledge_official_sources (
 CONSTRAINT knowledge_official_sources_fields CHECK (data ?& ARRAY['title','content','format','status','created_by_admin_id'] AND NOT jsonb_path_exists(data, '$.* ? (@ == null)')),
 id text PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 96),
 data jsonb NOT NULL CHECK (jsonb_typeof(data)='object' AND data ? 'id' AND data->>'id'=id),
 facts_version bigint NOT NULL DEFAULT nextval('knowledge_facts_version_seq') CHECK (facts_version>0),
 
 CHECK (data->>'status' IN ('draft','published','withdrawn')), CHECK (data->>'format' IN ('text','markdown')), CHECK (octet_length(data->>'title') BETWEEN 1 AND 1024), CHECK (octet_length(data->>'content') BETWEEN 1 AND 65536)
);
CREATE TRIGGER knowledge_official_sources_facts BEFORE INSERT OR UPDATE ON knowledge_official_sources
 FOR EACH ROW EXECUTE FUNCTION advance_knowledge_facts();
CREATE TABLE knowledge_source_selections (
 CONSTRAINT knowledge_source_selections_fields CHECK (data ?& ARRAY['collection_id','kind','source_id','active'] AND NOT jsonb_path_exists(data, '$.* ? (@ == null)')),
 id text PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 96),
 data jsonb NOT NULL CHECK (jsonb_typeof(data)='object' AND data ? 'id' AND data->>'id'=id),
 facts_version bigint NOT NULL DEFAULT nextval('knowledge_facts_version_seq') CHECK (facts_version>0),
 collection_id text GENERATED ALWAYS AS (data->>'collection_id') STORED NOT NULL REFERENCES knowledge_collections(id) ON DELETE RESTRICT,
 CHECK (data->>'kind' IN ('thread','board','official')), CHECK (jsonb_typeof(data->'active') = 'boolean'), CHECK (length(data->>'source_id') BETWEEN 1 AND 96), UNIQUE (id,collection_id), CHECK (data ? 'source_id')
);
CREATE TRIGGER knowledge_source_selections_facts BEFORE INSERT OR UPDATE ON knowledge_source_selections
 FOR EACH ROW EXECUTE FUNCTION advance_knowledge_facts();
CREATE TABLE knowledge_collection_sources (
 CONSTRAINT knowledge_collection_sources_fields CHECK (data ?& ARRAY['collection_id','selection_id','selection_version','kind','source_id','source_revision'] AND NOT jsonb_path_exists(data, '$.* ? (@ == null)')),
 id text PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 96),
 data jsonb NOT NULL CHECK (jsonb_typeof(data)='object' AND data ? 'id' AND data->>'id'=id),
 facts_version bigint NOT NULL DEFAULT nextval('knowledge_facts_version_seq') CHECK (facts_version>0),
 collection_id text GENERATED ALWAYS AS (data->>'collection_id') STORED NOT NULL REFERENCES knowledge_collections(id) ON DELETE RESTRICT, selection_id text GENERATED ALWAYS AS (data->>'selection_id') STORED NOT NULL,
 FOREIGN KEY (selection_id,collection_id) REFERENCES knowledge_source_selections(id,collection_id) ON DELETE RESTRICT, CHECK (data->>'kind' IN ('community.thread','richtext.article','knowledge.official')), CHECK (data->>'selection_version' ~ '^[1-9][0-9]*$'), CHECK (data->>'source_revision' ~ '^[0-9a-f]{64}$'), CHECK (data ? 'source_id')
);
CREATE TRIGGER knowledge_collection_sources_facts BEFORE INSERT OR UPDATE ON knowledge_collection_sources
 FOR EACH ROW EXECUTE FUNCTION advance_knowledge_facts();
CREATE TABLE knowledge_official_revisions (
 CONSTRAINT knowledge_official_revisions_fields CHECK (data ?& ARRAY['source_id','official_version','snapshot','changed_by_admin_id','changed_at_ms'] AND NOT jsonb_path_exists(data, '$.* ? (@ == null)')),
 id text PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 96),
 data jsonb NOT NULL CHECK (jsonb_typeof(data)='object' AND data ? 'id' AND data->>'id'=id),
 facts_version bigint NOT NULL DEFAULT nextval('knowledge_facts_version_seq') CHECK (facts_version>0),
 source_id text GENERATED ALWAYS AS (data->>'source_id') STORED NOT NULL REFERENCES knowledge_official_sources(id) ON DELETE RESTRICT,
 CHECK (data->>'official_version' ~ '^[1-9][0-9]*$'), CHECK (data ? 'official_version')
);
CREATE TRIGGER knowledge_official_revisions_facts BEFORE INSERT OR UPDATE ON knowledge_official_revisions
 FOR EACH ROW EXECUTE FUNCTION advance_knowledge_facts();
CREATE UNIQUE INDEX knowledge_selections_target ON knowledge_source_selections(collection_id,(data->>'kind'),(data->>'source_id'));
CREATE UNIQUE INDEX knowledge_members_target ON knowledge_collection_sources(selection_id,(data->>'kind'),(data->>'source_id'));
CREATE INDEX knowledge_members_selection_collection ON knowledge_collection_sources(selection_id,collection_id);
CREATE INDEX knowledge_members_collection ON knowledge_collection_sources(collection_id,id);
CREATE UNIQUE INDEX knowledge_revision_source ON knowledge_official_revisions(source_id,(data->>'official_version'));
CREATE FUNCTION protect_knowledge_revision() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'official revision history is append-only'; END $$;
CREATE TRIGGER knowledge_revision_immutable BEFORE UPDATE OR DELETE ON knowledge_official_revisions FOR EACH ROW EXECUTE FUNCTION protect_knowledge_revision();
CREATE TRIGGER knowledge_revision_no_truncate BEFORE TRUNCATE ON knowledge_official_revisions FOR EACH STATEMENT EXECUTE FUNCTION protect_knowledge_revision();
INSERT INTO permission_definitions(id,code,domain,resource,action,description,risk_level,allowed_scope_types,audit_level) VALUES
 (900000000000003001,'knowledge.collection.read','knowledge','collection','read','查看知识集合与官方资料管理目录','low','["global"]','standard'),
 (900000000000003002,'knowledge.collection.manage','knowledge','collection','manage','管理公开知识集合、规则、成员及官方资料','high','["global"]','required');
INSERT INTO admin_role_permissions(role_id,permission_code,created_by) VALUES
 ('security_admin','knowledge.collection.read','system:migration-30'),
 ('security_admin','knowledge.collection.manage','system:migration-30');
