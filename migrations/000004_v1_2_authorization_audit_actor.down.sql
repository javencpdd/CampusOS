-- Preserve audit provenance: only rows representable by the v1.1 shape may roll back.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM public.authorization_audits
        WHERE actor_kind NOT IN ('user', 'anonymous', 'legacy_unknown')
           OR (actor_kind = 'user' AND NOT CASE
                WHEN actor_id ~ '^(0|[1-9][0-9]{0,18})$'
                THEN actor_id::numeric <= 9223372036854775807
                ELSE FALSE
              END)
    ) THEN
        RAISE EXCEPTION 'cannot roll back 000004 while non-legacy audit actors exist';
    END IF;
END $$;

DROP INDEX public.idx_authorization_audits_actor;
ALTER TABLE public.authorization_audits
    DROP CONSTRAINT chk_authorization_audits_actor,
    DROP COLUMN actor_kind,
    ALTER COLUMN actor_id TYPE bigint USING actor_id::bigint;
CREATE INDEX idx_authorization_audits_actor
    ON public.authorization_audits USING btree (actor_id, created_at DESC);
