-- Lock all new authority/projection tables before the empty-only check.
LOCK TABLE knowledge_collections,knowledge_official_sources,knowledge_official_revisions,knowledge_source_selections,knowledge_collection_sources IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM knowledge_collections) OR EXISTS(SELECT 1 FROM knowledge_official_sources) OR EXISTS(SELECT 1 FROM knowledge_official_revisions) OR EXISTS(SELECT 1 FROM knowledge_source_selections) OR EXISTS(SELECT 1 FROM knowledge_collection_sources) THEN
  RAISE EXCEPTION 'knowledge data exists; archive before rollback';
 END IF;
 IF EXISTS(SELECT 1 FROM admin_role_permissions WHERE permission_code IN ('knowledge.collection.read','knowledge.collection.manage') AND NOT(role_id='security_admin' AND created_by='system:migration-30')) OR EXISTS(SELECT 1 FROM role_permissions WHERE permission_id IN (900000000000003001,900000000000003002)) OR EXISTS(SELECT 1 FROM route_permission_bindings WHERE permission_id IN (900000000000003001,900000000000003002)) THEN
  RAISE EXCEPTION 'knowledge permission references exist; archive before rollback';
 END IF;
END $$;
DELETE FROM admin_role_permissions WHERE permission_code IN ('knowledge.collection.read','knowledge.collection.manage') AND role_id='security_admin' AND created_by='system:migration-30';
DELETE FROM permission_definitions WHERE id IN (900000000000003001,900000000000003002) AND code IN ('knowledge.collection.read','knowledge.collection.manage');
DROP TABLE knowledge_collection_sources;
DROP TABLE knowledge_source_selections;
DROP TABLE knowledge_official_revisions;
DROP TABLE knowledge_official_sources;
DROP TABLE knowledge_collections;
DROP FUNCTION protect_knowledge_revision();
DROP FUNCTION advance_knowledge_facts();
-- Preserve sequence high water across rollback/re-up.
