-- v0.14：私有个人文档、不可变版本与预览状态；源文件仍由 storage_objects 私有保存。
CREATE TABLE IF NOT EXISTS personal_documents (
    id BIGINT PRIMARY KEY,
    owner_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    name VARCHAR(255) NOT NULL,
    document_type VARCHAR(20) NOT NULL CONSTRAINT chk_personal_documents_type CHECK (document_type IN ('text','markdown','campusdoc','pdf','docx')),
    status VARCHAR(20) NOT NULL DEFAULT 'active' CONSTRAINT chk_personal_documents_status CHECK (status IN ('active','trashed')),
    current_version_id BIGINT NULL,
    version BIGINT NOT NULL DEFAULT 1 CONSTRAINT chk_personal_documents_version CHECK (version >= 1),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), deleted_at TIMESTAMPTZ NULL
);
CREATE TABLE IF NOT EXISTS personal_document_versions (
    id BIGINT PRIMARY KEY,
    document_id BIGINT NOT NULL REFERENCES personal_documents(id) ON DELETE RESTRICT,
    version_number INTEGER NOT NULL CONSTRAINT chk_personal_document_versions_number CHECK (version_number >= 1),
    source_object_id BIGINT NOT NULL REFERENCES storage_objects(id) ON DELETE RESTRICT,
    source_type VARCHAR(20) NOT NULL CONSTRAINT chk_personal_document_versions_type CHECK (source_type IN ('text','markdown','campusdoc','pdf','docx')),
    size_bytes BIGINT NOT NULL CONSTRAINT chk_personal_document_versions_size CHECK (size_bytes >= 0),
    sha256 VARCHAR(64) NOT NULL,
    restored_from_version_id BIGINT NULL REFERENCES personal_document_versions(id) ON DELETE RESTRICT,
    created_by BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uk_personal_document_versions_number UNIQUE (document_id,version_number)
);
ALTER TABLE personal_documents ADD CONSTRAINT fk_personal_documents_current_version FOREIGN KEY (current_version_id) REFERENCES personal_document_versions(id) ON DELETE RESTRICT;
CREATE INDEX IF NOT EXISTS idx_personal_documents_owner_status_updated ON personal_documents(owner_user_id,status,updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_personal_document_versions_document_number ON personal_document_versions(document_id,version_number DESC);
CREATE TABLE IF NOT EXISTS personal_document_previews (
    id BIGINT PRIMARY KEY, document_version_id BIGINT NOT NULL REFERENCES personal_document_versions(id) ON DELETE RESTRICT,
    preview_object_id BIGINT NULL REFERENCES storage_objects(id) ON DELETE RESTRICT,
    status VARCHAR(20) NOT NULL CONSTRAINT chk_personal_document_previews_status CHECK (status IN ('pending','processing','ready','failed','unsupported')),
    error_code VARCHAR(80) NOT NULL DEFAULT '', attempts INTEGER NOT NULL DEFAULT 0 CONSTRAINT chk_personal_document_previews_attempts CHECK (attempts >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uk_personal_document_previews_version UNIQUE (document_version_id)
);
