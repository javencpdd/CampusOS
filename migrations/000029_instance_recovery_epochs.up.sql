-- Trusted offline recovery barrier. A fresh deployment has no recovery row.
-- Ordinary HTTP settings cannot edit this state; local maintenance owns it.
CREATE TABLE instance_recovery_epochs (
 singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
 epoch varchar(64) NOT NULL CHECK (epoch ~ '^[0-9a-f]{64}$'),
 archive_sha256 varchar(64) NOT NULL CHECK (archive_sha256 ~ '^[0-9a-f]{64}$'),
 phase varchar(16) NOT NULL CHECK (phase IN ('quarantined','reviewed')),
 inventory_sha256 varchar(64) NOT NULL DEFAULT '' CHECK (inventory_sha256='' OR inventory_sha256 ~ '^[0-9a-f]{64}$'),
 reviewed_by varchar(128) NOT NULL DEFAULT '',
 review_reason varchar(500) NOT NULL DEFAULT '',
 rotated_at timestamptz NOT NULL DEFAULT now(),
 reviewed_at timestamptz,
 egress_paused boolean NOT NULL DEFAULT true,
 audit jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(audit)='array' AND octet_length(audit::text)<=1048576),
 CHECK (phase<>'reviewed' OR (inventory_sha256<>'' AND reviewed_by<>'' AND review_reason<>'' AND reviewed_at IS NOT NULL))
);
