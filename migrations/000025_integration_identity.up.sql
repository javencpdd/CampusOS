-- Identity-owned machine identities. Raw credentials never enter PostgreSQL.
CREATE SEQUENCE IF NOT EXISTS integration_facts_version_seq AS bigint NO CYCLE;
CREATE TABLE integration_principals (
    id varchar(128) PRIMARY KEY CHECK (id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    name varchar(120) NOT NULL CHECK (length(name)>0),
    status varchar(16) NOT NULL CHECK (status IN ('active','suspended','revoked')),
    facts_version bigint NOT NULL CHECK (facts_version>0),
    created_by_admin_id varchar(128) NOT NULL CHECK (created_by_admin_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE integration_credentials (
    id varchar(128) PRIMARY KEY CHECK (id ~ '^[a-f0-9]{16}$'),
    principal_id varchar(128) NOT NULL REFERENCES integration_principals(id) ON DELETE RESTRICT,
    prefix varchar(22) NOT NULL UNIQUE CHECK (prefix = 'cint1_' || id),
    digest bytea NOT NULL UNIQUE CHECK (octet_length(digest)=32),
    purpose varchar(32) NOT NULL CHECK (purpose='integration_api'),
    status varchar(16) NOT NULL CHECK (status IN ('active','revoked')),
    rules jsonb NOT NULL CHECK (jsonb_typeof(rules)='array' AND jsonb_array_length(rules)<=64 AND octet_length(rules::text)<=24576),
    not_before_ms bigint NOT NULL CHECK (not_before_ms>=0),
    expires_at_ms bigint NOT NULL CHECK (expires_at_ms>not_before_ms AND expires_at_ms<=9007199254740991),
    facts_version bigint NOT NULL CHECK (facts_version>0),
    created_by_admin_id varchar(128) NOT NULL CHECK (created_by_admin_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX idx_integration_credentials_principal ON integration_credentials(principal_id,id);

CREATE FUNCTION advance_integration_facts() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='UPDATE' THEN
        IF NEW.id IS DISTINCT FROM OLD.id OR NEW.created_by_admin_id IS DISTINCT FROM OLD.created_by_admin_id OR NEW.created_at IS DISTINCT FROM OLD.created_at OR OLD.status='revoked' THEN
            RAISE EXCEPTION 'immutable integration identity or revoked record';
        END IF;
        IF TG_TABLE_NAME='integration_credentials' THEN
            IF NEW.principal_id IS DISTINCT FROM OLD.principal_id OR NEW.prefix IS DISTINCT FROM OLD.prefix OR NEW.digest IS DISTINCT FROM OLD.digest OR NEW.purpose IS DISTINCT FROM OLD.purpose OR NEW.not_before_ms IS DISTINCT FROM OLD.not_before_ms OR NEW.expires_at_ms IS DISTINCT FROM OLD.expires_at_ms THEN
                RAISE EXCEPTION 'immutable integration credential';
            END IF;
        END IF;
    END IF;
    NEW.facts_version := nextval('integration_facts_version_seq');
    NEW.updated_at := clock_timestamp();
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_integration_principal_facts BEFORE INSERT OR UPDATE ON integration_principals FOR EACH ROW EXECUTE FUNCTION advance_integration_facts();
CREATE TRIGGER trg_integration_credential_facts BEFORE INSERT OR UPDATE ON integration_credentials FOR EACH ROW EXECUTE FUNCTION advance_integration_facts();

-- These are independent Admin execution permissions, never user role grants.
INSERT INTO permission_definitions(id,code,domain,resource,action,description,risk_level,allowed_scope_types,audit_level)
VALUES (900000000000002501,'identity.integration.read','identity','integration','read','查看独立机器集成及凭据元数据','medium','["global"]'::jsonb,'required'),
       (900000000000002502,'identity.integration.manage','identity','integration','manage','签发轮换撤销独立机器集成凭据','high','["global"]'::jsonb,'required');
INSERT INTO admin_role_permissions(role_id,permission_code,created_by)
VALUES ('security_admin','identity.integration.read','system:migration-25'),
       ('security_admin','identity.integration.manage','system:migration-25')
ON CONFLICT DO NOTHING;
