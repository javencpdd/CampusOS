-- V12-02d follow-up rollback: drop the supporting index added by 000015.
DROP INDEX IF EXISTS public.idx_admin_recovery_tickets_account;
