-- 仅用于空隔离库演练；生产文档已写入后请使用 forward-fix。
DROP TABLE IF EXISTS personal_document_previews;
ALTER TABLE personal_documents DROP CONSTRAINT IF EXISTS fk_personal_documents_current_version;
DROP TABLE IF EXISTS personal_document_versions;
DROP TABLE IF EXISTS personal_documents;
