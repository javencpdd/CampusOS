-- V12-01a: a published plugin version keeps the package and permission identity
-- that its grants and user consents were recorded against. Activation state,
-- signature review, release channel, and creator FK cleanup remain mutable.
CREATE FUNCTION public.reject_plugin_version_identity_update()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF ROW(NEW.id, NEW.plugin_id, NEW.version, NEW.package_digest,
           NEW.manifest_api_version, NEW.host_api_version,
           NEW.permission_fingerprint, NEW.manifest, NEW.created_at)
       IS DISTINCT FROM
       ROW(OLD.id, OLD.plugin_id, OLD.version, OLD.package_digest,
           OLD.manifest_api_version, OLD.host_api_version,
           OLD.permission_fingerprint, OLD.manifest, OLD.created_at) THEN
        RAISE EXCEPTION 'plugin version publication identity is immutable'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_plugin_version_identity_immutable
BEFORE UPDATE ON public.plugin_versions
FOR EACH ROW
EXECUTE FUNCTION public.reject_plugin_version_identity_update();

-- Declaration rows are authorization facts. Existing rows may not be
-- rewritten, including their scope or purpose after a consent decision.
CREATE FUNCTION public.reject_plugin_declaration_update()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF ROW(NEW.id, NEW.plugin_version_id, NEW.capability_code, NEW.purpose,
           NEW.risk_level, NEW.required, NEW.resource_scope,
           NEW.data_classification, NEW.created_at)
       IS DISTINCT FROM
       ROW(OLD.id, OLD.plugin_version_id, OLD.capability_code, OLD.purpose,
           OLD.risk_level, OLD.required, OLD.resource_scope,
           OLD.data_classification, OLD.created_at) THEN
        RAISE EXCEPTION 'plugin capability declaration is immutable'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_plugin_declaration_immutable
BEFORE UPDATE ON public.plugin_capability_declarations
FOR EACH ROW
EXECUTE FUNCTION public.reject_plugin_declaration_update();
