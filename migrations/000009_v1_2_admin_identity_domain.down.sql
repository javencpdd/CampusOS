-- V12-02d rollback: the admin identity domain baseline must be empty before
-- removal; live administrator accounts are never dropped silently.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.admin_accounts LIMIT 1) THEN
        RAISE EXCEPTION 'admin domain accounts must be exported and removed before rollback'
            USING ERRCODE = '23514';
    END IF;
END;
$$;

DROP TABLE public.admin_role_bindings;
DROP TABLE public.admin_credentials;
DROP TABLE public.admin_accounts;
DROP TABLE public.admin_roles;
