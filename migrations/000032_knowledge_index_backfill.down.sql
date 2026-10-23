LOCK TABLE knowledge_index_backfill_checkpoints IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM knowledge_index_backfill_checkpoints) THEN
        RAISE EXCEPTION 'knowledge backfill checkpoints exist; archive before rollback';
    END IF;
END $$;
DROP TABLE knowledge_index_backfill_checkpoints;
-- Existing targets, immutable requests, receipts, permissions and sequences stay.
