-- V12-01a: an audit actor is identified by its domain and opaque ID together.
-- Existing NULL actor IDs have no trustworthy origin, so retain that ambiguity.
ALTER TABLE public.authorization_audits
    ALTER COLUMN actor_id TYPE character varying(128) USING actor_id::text,
    ADD COLUMN actor_kind character varying(32);

UPDATE public.authorization_audits
SET actor_kind = CASE WHEN actor_id IS NULL THEN 'legacy_unknown' ELSE 'user' END;

ALTER TABLE public.authorization_audits
    ALTER COLUMN actor_kind SET NOT NULL,
    ADD CONSTRAINT chk_authorization_audits_actor CHECK (
        (actor_kind IN ('anonymous', 'legacy_unknown') AND actor_id IS NULL)
        OR
        (actor_kind IN ('user', 'admin', 'integration', 'plugin_instance', 'worker', 'system')
         AND actor_id IS NOT NULL
         AND actor_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]*$')
    );

DROP INDEX public.idx_authorization_audits_actor;
CREATE INDEX idx_authorization_audits_actor
    ON public.authorization_audits USING btree (actor_kind, actor_id, created_at DESC);
