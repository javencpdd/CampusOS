-- V12-01a: activated_at is the durable publication boundary. Earlier
-- published rows may predate that convention; mark them without changing
-- their version, declaration, grant, or consent identity.
UPDATE public.plugin_versions
SET activated_at = COALESCE(created_at, NOW())
WHERE lifecycle_status IN ('active', 'retired') AND activated_at IS NULL;

CREATE FUNCTION public.guard_plugin_version_publication()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.lifecycle_status IN ('active', 'retired') AND NEW.activated_at IS NULL THEN
        RAISE EXCEPTION 'published plugin version requires activated_at'
            USING ERRCODE = '23514';
    END IF;
    IF TG_OP = 'UPDATE' AND OLD.activated_at IS NOT NULL
       AND (NEW.activated_at IS NULL OR NEW.lifecycle_status = 'staged') THEN
        RAISE EXCEPTION 'published plugin version cannot become unpublished'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_plugin_version_publication_guard
BEFORE INSERT OR UPDATE ON public.plugin_versions
FOR EACH ROW
EXECUTE FUNCTION public.guard_plugin_version_publication();

-- A published capability set is an authorization fact. New rows can be
-- assembled only while the version is staged and has never been activated.
-- If the version row is absent, the parent is being deleted and its existing
-- ON DELETE CASCADE may remove declarations during plugin uninstall.
CREATE FUNCTION public.guard_plugin_declaration_membership()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    parent_status varchar(16);
    parent_activated_at timestamptz;
BEGIN
    IF TG_OP = 'INSERT' THEN
        SELECT lifecycle_status, activated_at
        INTO parent_status, parent_activated_at
        FROM public.plugin_versions
        WHERE id = NEW.plugin_version_id
        FOR SHARE;
        IF NOT FOUND OR parent_status <> 'staged' OR parent_activated_at IS NOT NULL THEN
            RAISE EXCEPTION 'plugin capability declaration set is published'
                USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;

    SELECT lifecycle_status, activated_at
    INTO parent_status, parent_activated_at
    FROM public.plugin_versions
    WHERE id = OLD.plugin_version_id
    FOR SHARE;
    IF FOUND AND (parent_status <> 'staged' OR parent_activated_at IS NOT NULL) THEN
        RAISE EXCEPTION 'plugin capability declaration set is published'
            USING ERRCODE = '23514';
    END IF;
    RETURN OLD;
END;
$$;

CREATE TRIGGER trg_plugin_declaration_membership_guard
BEFORE INSERT OR DELETE ON public.plugin_capability_declarations
FOR EACH ROW
EXECUTE FUNCTION public.guard_plugin_declaration_membership();

-- Once an active version stops accepting work, every outstanding short-lived
-- delegation for that release is permanently revoked. Grants and consents
-- remain historical facts and must be rechecked if a version is reactivated.
CREATE FUNCTION public.revoke_retired_plugin_delegations()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    UPDATE public.plugin_delegations
    SET status = 'revoked', revoked_at = COALESCE(revoked_at, NOW())
    WHERE plugin_version_id = OLD.id AND status = 'active';
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_plugin_version_revoke_delegations
AFTER UPDATE OF lifecycle_status ON public.plugin_versions
FOR EACH ROW
WHEN (OLD.lifecycle_status = 'active' AND NEW.lifecycle_status <> 'active')
EXECUTE FUNCTION public.revoke_retired_plugin_delegations();
