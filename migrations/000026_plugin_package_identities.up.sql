-- Host-owned v5 publication identity ledger. No package bytes, secrets,
-- installation/authorization state or old Manager identity conversion.
CREATE TABLE plugin_package_identities (
    publisher VARCHAR(63) COLLATE "C" NOT NULL,
    plugin_key VARCHAR(63) COLLATE "C" NOT NULL,
    version VARCHAR(128) COLLATE "C" NOT NULL,
    package_digest VARCHAR(71) COLLATE "C" NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (publisher, plugin_key, version),
    CONSTRAINT plugin_package_identity_publisher CHECK (publisher ~ '^[a-z0-9][a-z0-9-]{1,62}$'),
    CONSTRAINT plugin_package_identity_key CHECK (plugin_key ~ '^[a-z0-9][a-z0-9-]{1,62}$'),
    CONSTRAINT plugin_package_identity_version CHECK (version ~ '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$'),
    CONSTRAINT plugin_package_identity_digest CHECK (package_digest ~ '^sha256:[0-9a-f]{64}$')
);

CREATE FUNCTION reject_plugin_package_identity_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'plugin package identities are immutable' USING ERRCODE = '23514';
END $$;

CREATE TRIGGER plugin_package_identity_write_once
BEFORE UPDATE OR DELETE ON plugin_package_identities
FOR EACH ROW EXECUTE FUNCTION reject_plugin_package_identity_mutation();

CREATE TRIGGER plugin_package_identity_no_truncate
BEFORE TRUNCATE ON plugin_package_identities
FOR EACH STATEMENT EXECUTE FUNCTION reject_plugin_package_identity_mutation();
