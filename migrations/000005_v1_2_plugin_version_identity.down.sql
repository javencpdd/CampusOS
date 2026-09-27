-- Reversible policy rollback: rows and decisions are preserved.
DROP TRIGGER trg_plugin_declaration_immutable ON public.plugin_capability_declarations;
DROP FUNCTION public.reject_plugin_declaration_update();

DROP TRIGGER trg_plugin_version_identity_immutable ON public.plugin_versions;
DROP FUNCTION public.reject_plugin_version_identity_update();
