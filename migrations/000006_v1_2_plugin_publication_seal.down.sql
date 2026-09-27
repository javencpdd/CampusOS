-- Policy rollback only: keep published rows and permanently revoked tokens.
DROP TRIGGER trg_plugin_version_revoke_delegations ON public.plugin_versions;
DROP FUNCTION public.revoke_retired_plugin_delegations();

DROP TRIGGER trg_plugin_declaration_membership_guard ON public.plugin_capability_declarations;
DROP FUNCTION public.guard_plugin_declaration_membership();

DROP TRIGGER trg_plugin_version_publication_guard ON public.plugin_versions;
DROP FUNCTION public.guard_plugin_version_publication();
