-- Stop dependent commands first. Preserve original rows and nonce highwater.
DROP TRIGGER trg_user_spaces_facts_version ON public.user_spaces;
DROP TRIGGER trg_space_style_snapshots_facts_version ON public.user_space_style_snapshots;
DROP FUNCTION public.advance_personal_space_facts_version();
ALTER TABLE public.user_spaces DROP CONSTRAINT chk_user_spaces_facts_version, DROP COLUMN facts_version;
ALTER TABLE public.user_space_style_snapshots DROP CONSTRAINT chk_user_space_style_snapshots_facts_version, DROP COLUMN facts_version;
