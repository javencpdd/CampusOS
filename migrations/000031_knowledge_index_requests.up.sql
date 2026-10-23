-- Metadata-only, immutable requests. No index executor/readiness is implied.
-- Logical target references deliberately allow an absent-target cleanup intent.
CREATE TABLE knowledge_index_tasks (
    id text PRIMARY KEY CHECK (id ~ '^[0-9a-f]{64}$'),
    target_kind text NOT NULL CHECK (target_kind IN ('collection','official')),
    target_id text NOT NULL CHECK (
        (target_kind='collection' AND target_id ~ '^kc_[0-9a-f]{32}$') OR
        (target_kind='official' AND target_id ~ '^ko_[0-9a-f]{32}$')
    ),
    target_version bigint NOT NULL CHECK (target_version >= 0),
    status text NOT NULL DEFAULT 'pending' CHECK (status='pending'),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (target_kind,target_id,target_version)
);
CREATE INDEX knowledge_index_tasks_pending ON knowledge_index_tasks(created_at,id);
CREATE FUNCTION protect_knowledge_index_request() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'knowledge index request is immutable'; END $$;
CREATE TRIGGER knowledge_index_request_immutable BEFORE UPDATE OR DELETE ON knowledge_index_tasks
    FOR EACH ROW EXECUTE FUNCTION protect_knowledge_index_request();
CREATE TRIGGER knowledge_index_request_no_truncate BEFORE TRUNCATE ON knowledge_index_tasks
    FOR EACH STATEMENT EXECUTE FUNCTION protect_knowledge_index_request();
