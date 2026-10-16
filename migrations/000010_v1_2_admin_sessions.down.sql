-- V12-02d rollback: live admin sessions are user-visible state; refuse a
-- rollback that would silently drop them, then remove the empty store.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.admin_sessions LIMIT 1) THEN
        RAISE EXCEPTION 'admin sessions must be exported and revoked before rollback'
            USING ERRCODE = '23514';
    END IF;
END;
$$;

DROP TABLE public.admin_sessions;
