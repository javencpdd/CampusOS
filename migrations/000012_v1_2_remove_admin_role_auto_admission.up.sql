-- V12-02d: remove the user-role auto-admission coupling. Admin access is no
-- longer opened or closed by role assignment in any layer: the SQL trigger
-- and its helper are dropped, and application code stops syncing admission
-- from role changes. Existing admission rows are preserved untouched; the
-- independent admin identity domain (migration 000009+) is the only way to
-- create administrators from here on.
DROP TRIGGER IF EXISTS trg_sync_identity_admin_account_from_role ON public.user_roles;
DROP FUNCTION IF EXISTS public.sync_identity_admin_account_from_role();
DROP FUNCTION IF EXISTS public.sync_identity_admin_account_for_user(bigint, bigint);
