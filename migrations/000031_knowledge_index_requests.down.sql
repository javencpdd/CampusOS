LOCK TABLE knowledge_index_tasks IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM knowledge_index_tasks) THEN
        RAISE EXCEPTION 'knowledge index requests exist; archive before rollback';
    END IF;
END $$;
DROP TABLE knowledge_index_tasks;
DROP FUNCTION protect_knowledge_index_request();
-- No previous business row, event receipt, sequence or permission is changed.
