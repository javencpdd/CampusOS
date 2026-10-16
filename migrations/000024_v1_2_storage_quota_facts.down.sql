-- Stop dependent commands first. Original quotas/actors/FKs and highwater remain.
DROP TRIGGER trg_user_storage_quotas_facts_version ON public.user_storage_quotas;
DROP FUNCTION public.advance_storage_quota_facts_version();
ALTER TABLE public.user_storage_quotas DROP CONSTRAINT chk_user_storage_quotas_facts_version, DROP COLUMN facts_version;
