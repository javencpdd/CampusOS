LOCK TABLE knowledge_index_jobs,knowledge_index_documents IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM knowledge_index_jobs) OR EXISTS(SELECT 1 FROM knowledge_index_documents) THEN
  RAISE EXCEPTION 'index execution data exists; archive before rollback';
 END IF;
END $$;
DROP TABLE knowledge_index_documents,knowledge_index_jobs;
DROP FUNCTION advance_knowledge_index_facts();
-- Keep the non-reused epoch high water across empty-domain down/up.
ALTER SEQUENCE knowledge_index_epoch_seq OWNED BY NONE;
