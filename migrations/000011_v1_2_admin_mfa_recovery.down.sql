-- V12-02d rollback: admin MFA/recovery state is live; refuse a rollback that
-- would silently drop it, then remove the empty stores.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.admin_mfa_totp LIMIT 1) OR
       EXISTS (SELECT 1 FROM public.admin_mfa_tickets LIMIT 1) OR
       EXISTS (SELECT 1 FROM public.admin_recovery_tickets LIMIT 1) THEN
        RAISE EXCEPTION 'admin MFA/recovery data must be exported and removed before rollback'
            USING ERRCODE = '23514';
    END IF;
END;
$$;

DROP TABLE public.admin_recovery_tickets;
DROP TABLE public.admin_mfa_tickets;
DROP TABLE public.admin_mfa_totp;
