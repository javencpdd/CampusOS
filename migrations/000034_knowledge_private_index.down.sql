LOCK TABLE knowledge_private_sources,knowledge_private_tasks IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM knowledge_private_sources) OR EXISTS(SELECT 1 FROM knowledge_private_tasks) THEN
  RAISE EXCEPTION 'private selections exist; owner withdrawal and key retirement required before rollback';
 END IF;
END $$;
DROP TABLE knowledge_private_tasks,knowledge_private_sources;
DROP FUNCTION guard_knowledge_private_source();
-- Retain non-reused consent/lease high water, including across empty down/up.
ALTER SEQUENCE knowledge_private_epoch_seq OWNED BY NONE;
