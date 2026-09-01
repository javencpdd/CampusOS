-- CampusOS v1.0 clean database baseline
-- Generated from the verified v0.14 final schema on 2026-09-01, then normalized for the v1 reset.
-- This baseline intentionally does not preserve compatibility with the former 000001-000049 chain.
-- All temporal instants use TIMESTAMPTZ; future schema changes must be appended as new migrations.

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

--
-- Name: campusos_guard_category_hierarchy(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.campusos_guard_category_hierarchy() RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, public
    AS $$
DECLARE
    parent_kind VARCHAR(16);
    parent_status VARCHAR(16);
BEGIN
    IF NEW.deleted_at IS NOT NULL THEN
        RETURN NEW;
    END IF;

    IF NEW.node_kind = 'group' AND NEW.parent_id IS NOT NULL THEN
        RAISE EXCEPTION 'category group must be a root node';
    END IF;

    IF NEW.parent_id IS NOT NULL THEN
        SELECT node_kind, lifecycle_status INTO parent_kind, parent_status
        FROM categories
        WHERE id = NEW.parent_id AND deleted_at IS NULL
        FOR KEY SHARE;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'category parent is unavailable';
        END IF;
        IF parent_kind <> 'group' THEN
            RAISE EXCEPTION 'category parent must be an active group';
        END IF;
        IF parent_status <> 'active' THEN
            RAISE EXCEPTION 'category parent is archived';
        END IF;
    END IF;

    IF NEW.node_kind <> 'group' AND EXISTS (
        SELECT 1 FROM categories child
        WHERE child.parent_id = NEW.id AND child.deleted_at IS NULL
    ) THEN
        RAISE EXCEPTION 'category board cannot own child nodes';
    END IF;

    IF NEW.lifecycle_status = 'archived' AND NEW.node_kind = 'group' AND EXISTS (
        SELECT 1 FROM categories child
        WHERE child.parent_id = NEW.id
          AND child.deleted_at IS NULL
          AND child.lifecycle_status = 'active'
    ) THEN
        RAISE EXCEPTION 'archive or move active child boards before archiving a group';
    END IF;

    RETURN NEW;
END;
$$;


--
-- Name: campusos_guard_category_thread_type_policy(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.campusos_guard_category_thread_type_policy() RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, public
    AS $$
DECLARE
    category_kind VARCHAR(16);
BEGIN
    SELECT node_kind INTO category_kind
    FROM categories
    WHERE id = NEW.category_id AND deleted_at IS NULL
    FOR KEY SHARE;
    IF NOT FOUND OR category_kind <> 'board' THEN
        RAISE EXCEPTION 'thread type policy requires an existing board category';
    END IF;
    RETURN NEW;
END;
$$;


--
-- Name: campusos_guard_mutual_aid_detail(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.campusos_guard_mutual_aid_detail() RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, public
    AS $$
DECLARE
    base_thread_type VARCHAR(32);
    base_author_id BIGINT;
BEGIN
    SELECT thread_type, author_id
    INTO base_thread_type, base_author_id
    FROM threads
    WHERE id = NEW.thread_id
    FOR KEY SHARE;

    IF NOT FOUND OR base_thread_type <> 'mutual_aid' THEN
        RAISE EXCEPTION 'mutual aid detail requires an existing mutual_aid thread';
    END IF;
    IF NEW.created_by <> base_author_id THEN
        RAISE EXCEPTION 'mutual aid detail created_by must match the thread author';
    END IF;
    RETURN NEW;
END;
$$;


--
-- Name: campusos_guard_secondhand_detail(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.campusos_guard_secondhand_detail() RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, public
    AS $$
DECLARE
    base_thread_type VARCHAR(32);
    base_author_id BIGINT;
BEGIN
    SELECT thread_type, author_id
    INTO base_thread_type, base_author_id
    FROM threads
    WHERE id = NEW.thread_id
    FOR KEY SHARE;

    IF NOT FOUND OR base_thread_type <> 'secondhand' THEN
        RAISE EXCEPTION 'secondhand detail requires an existing secondhand thread';
    END IF;
    IF NEW.created_by <> base_author_id THEN
        RAISE EXCEPTION 'secondhand detail created_by must match the thread author';
    END IF;
    RETURN NEW;
END;
$$;


--
-- Name: sync_identity_admin_account_for_user(bigint, bigint); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.sync_identity_admin_account_for_user(target_user_id bigint, preferred_assignment_id bigint) RETURNS void
    LANGUAGE plpgsql
    SET search_path = pg_catalog, public
    AS $$
DECLARE
    active_assignment_id BIGINT;
    active_credential_id BIGINT;
BEGIN
    -- Serialize role-triggered changes with explicit pause/restore commands.
    -- The aggregate is the set of effective management-plane administrators,
    -- not a single admission row.
    PERFORM pg_advisory_xact_lock(130013, 39);

    SELECT ur.id, a.id
    INTO active_assignment_id, active_credential_id
    FROM user_roles ur
    INNER JOIN roles r
        ON r.id = ur.role_id
       AND r.name = 'admin'
       AND r.deleted_at IS NULL
    INNER JOIN accounts a
        ON a.user_id = ur.user_id
       AND a.type = 'email'
       AND a.deleted_at IS NULL
    WHERE ur.user_id = target_user_id
      AND ur.scope_type = 'global'
      AND ur.scope_id IS NULL
      AND ur.deleted_at IS NULL
    ORDER BY (ur.id = preferred_assignment_id) DESC, ur.created_at ASC, ur.id ASC
    LIMIT 1;

    IF FOUND THEN
        INSERT INTO identity_admin_accounts (
            id, user_id, credential_account_id, status, activation_source,
            activated_at, status_reason, status_changed_at, version, created_at, updated_at
        ) VALUES (
            active_assignment_id, target_user_id, active_credential_id, 'active',
            'role_assignment', NOW(), 'role_assignment', NOW(), 1, NOW(), NOW()
        )
        ON CONFLICT (user_id) DO UPDATE
        SET credential_account_id = EXCLUDED.credential_account_id,
            status = CASE
                WHEN identity_admin_accounts.status = 'suspended' THEN 'suspended'
                ELSE 'active'
            END,
            activation_source = CASE
                WHEN identity_admin_accounts.status = 'suspended' THEN identity_admin_accounts.activation_source
                ELSE EXCLUDED.activation_source
            END,
            activated_at = CASE
                WHEN identity_admin_accounts.status = 'revoked' THEN NOW()
                ELSE identity_admin_accounts.activated_at
            END,
            revoked_at = CASE
                WHEN identity_admin_accounts.status = 'suspended' THEN identity_admin_accounts.revoked_at
                ELSE NULL
            END,
            status_reason = CASE
                WHEN identity_admin_accounts.status = 'suspended' THEN identity_admin_accounts.status_reason
                WHEN identity_admin_accounts.status = 'revoked' THEN 'role_assignment'
                ELSE identity_admin_accounts.status_reason
            END,
            status_changed_by = CASE
                WHEN identity_admin_accounts.status = 'suspended' THEN identity_admin_accounts.status_changed_by
                WHEN identity_admin_accounts.status = 'revoked' THEN NULL
                ELSE identity_admin_accounts.status_changed_by
            END,
            status_changed_at = CASE
                WHEN identity_admin_accounts.status = 'suspended' THEN identity_admin_accounts.status_changed_at
                WHEN identity_admin_accounts.status = 'revoked' THEN NOW()
                ELSE identity_admin_accounts.status_changed_at
            END,
            updated_at = NOW(),
            version = identity_admin_accounts.version + 1;
        RETURN;
    END IF;

    UPDATE identity_admin_accounts
    SET status = 'revoked',
        revoked_at = COALESCE(revoked_at, NOW()),
        status_reason = 'admin_role_revoked',
        status_changed_by = NULL,
        status_changed_at = NOW(),
        updated_at = NOW(),
        version = version + 1
    WHERE user_id = target_user_id
      AND status <> 'revoked';
END;
$$;


--
-- Name: sync_identity_admin_account_from_role(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.sync_identity_admin_account_from_role() RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, public
    AS $$
DECLARE
    old_is_admin BOOLEAN := FALSE;
    new_is_admin BOOLEAN := FALSE;
BEGIN
    IF TG_OP IN ('UPDATE', 'DELETE') THEN
        SELECT EXISTS (
            SELECT 1 FROM roles WHERE id=OLD.role_id AND name='admin' AND deleted_at IS NULL
        ) INTO old_is_admin;
    END IF;
    IF TG_OP IN ('INSERT', 'UPDATE') THEN
        SELECT EXISTS (
            SELECT 1 FROM roles WHERE id=NEW.role_id AND name='admin' AND deleted_at IS NULL
        ) INTO new_is_admin;
    END IF;

    IF old_is_admin THEN
        PERFORM sync_identity_admin_account_for_user(OLD.user_id, OLD.id);
    END IF;
    IF new_is_admin AND (NOT old_is_admin OR TG_OP <> 'UPDATE' OR NEW.user_id <> OLD.user_id) THEN
        PERFORM sync_identity_admin_account_for_user(NEW.user_id, NEW.id);
    END IF;
    RETURN NULL;
END;
$$;


SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: academic_terms; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.academic_terms (
    id bigint NOT NULL,
    year integer NOT NULL,
    semester character varying(16) NOT NULL,
    first_week_start date NOT NULL,
    status character varying(16) DEFAULT 'open'::character varying NOT NULL,
    is_default boolean DEFAULT false NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    created_by bigint,
    updated_by bigint,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    closed_at timestamp with time zone,
    CONSTRAINT chk_academic_terms_closed_at CHECK (((((status)::text = 'closed'::text) AND (closed_at IS NOT NULL)) OR (((status)::text = 'open'::text) AND (closed_at IS NULL)))),
    CONSTRAINT chk_academic_terms_default_open CHECK (((NOT is_default) OR ((status)::text = 'open'::text))),
    CONSTRAINT chk_academic_terms_first_week_monday CHECK ((EXTRACT(isodow FROM first_week_start) = (1)::numeric)),
    CONSTRAINT chk_academic_terms_semester CHECK (((semester)::text = ANY ((ARRAY['spring'::character varying, 'fall'::character varying])::text[]))),
    CONSTRAINT chk_academic_terms_status CHECK (((status)::text = ANY ((ARRAY['open'::character varying, 'closed'::character varying])::text[]))),
    CONSTRAINT chk_academic_terms_version CHECK ((version >= 1)),
    CONSTRAINT chk_academic_terms_year CHECK (((year >= 2000) AND (year <= 2200)))
);


--
-- Name: accounts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.accounts (
    id bigint NOT NULL,
    user_id bigint NOT NULL,
    type character varying(20) NOT NULL,
    identifier character varying(255) NOT NULL,
    credential character varying(512) NOT NULL,
    verified boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    identifier_normalized character varying(320) NOT NULL,
    verification_state character varying(32) NOT NULL,
    verified_at timestamp with time zone,
    verification_source character varying(64) DEFAULT ''::character varying NOT NULL,
    password_changed_at timestamp with time zone,
    credential_version bigint DEFAULT 1 NOT NULL,
    CONSTRAINT chk_accounts_credential_version CHECK ((credential_version >= 1)),
    CONSTRAINT chk_accounts_identifier_normalized CHECK ((((type)::text <> 'email'::text) OR ((identifier)::text = (identifier_normalized)::text))),
    CONSTRAINT chk_accounts_verification_state CHECK (((verification_state)::text = ANY ((ARRAY['unverified'::character varying, 'legacy_accepted'::character varying, 'verified'::character varying, 'system_managed'::character varying])::text[])))
);


--
-- Name: ai_call_logs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.ai_call_logs (
    id bigint NOT NULL,
    provider character varying(64) NOT NULL,
    model character varying(128) DEFAULT ''::character varying NOT NULL,
    source character varying(128) DEFAULT ''::character varying NOT NULL,
    status character varying(32) NOT NULL,
    duration_ms bigint DEFAULT 0 NOT NULL,
    prompt_tokens integer DEFAULT 0 NOT NULL,
    completion_tokens integer DEFAULT 0 NOT NULL,
    total_tokens integer DEFAULT 0 NOT NULL,
    error_message text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone
);


--
-- Name: api_keys; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.api_keys (
    id bigint NOT NULL,
    key character varying(64) NOT NULL,
    name character varying(128) NOT NULL,
    user_id bigint,
    plugin_name character varying(128),
    permissions jsonb DEFAULT '[]'::jsonb NOT NULL,
    is_active boolean DEFAULT true NOT NULL,
    last_used_at timestamp with time zone,
    expires_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone
);


--
-- Name: audit_logs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.audit_logs (
    id bigint NOT NULL,
    trace_id character varying(64) NOT NULL,
    actor_id bigint,
    actor_type character varying(20) NOT NULL,
    action character varying(32) NOT NULL,
    resource character varying(64) NOT NULL,
    resource_id character varying(64) NOT NULL,
    before_data jsonb,
    after_data jsonb,
    metadata jsonb DEFAULT '{}'::jsonb,
    ip_address character varying(45) DEFAULT ''::character varying,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: authorization_audits; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.authorization_audits (
    id bigint NOT NULL,
    request_id character varying(128) DEFAULT ''::character varying NOT NULL,
    actor_id bigint,
    permission_code character varying(160) DEFAULT ''::character varying NOT NULL,
    operation_code character varying(200) DEFAULT ''::character varying NOT NULL,
    scope_type character varying(32) DEFAULT ''::character varying NOT NULL,
    scope_id bigint,
    resource_type character varying(64) DEFAULT ''::character varying NOT NULL,
    resource_id character varying(128) DEFAULT ''::character varying NOT NULL,
    outcome character varying(16) NOT NULL,
    reason text DEFAULT ''::text NOT NULL,
    ip_address character varying(64) DEFAULT ''::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    command_id character varying(64),
    trace_id character varying(128),
    resource_version character varying(128),
    CONSTRAINT chk_authorization_audits_outcome CHECK (((outcome)::text = ANY ((ARRAY['allow'::character varying, 'deny'::character varying, 'error'::character varying])::text[])))
);


--
-- Name: builtin_feature_states; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.builtin_feature_states (
    feature_id character varying(128) NOT NULL,
    desired_enabled boolean NOT NULL,
    effective_enabled boolean NOT NULL,
    pending_restart boolean DEFAULT false NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    config jsonb DEFAULT '{}'::jsonb NOT NULL
);


--
-- Name: categories; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.categories (
    id bigint NOT NULL,
    name character varying(64) NOT NULL,
    slug character varying(64) NOT NULL,
    description character varying(500) DEFAULT ''::character varying,
    icon character varying(512) DEFAULT ''::character varying,
    parent_id bigint,
    sort_order integer DEFAULT 0 NOT NULL,
    thread_count bigint DEFAULT 0 NOT NULL,
    post_count bigint DEFAULT 0 NOT NULL,
    is_closed boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    default_tags text[] DEFAULT '{}'::text[] NOT NULL,
    node_kind character varying(16) DEFAULT 'board'::character varying NOT NULL,
    lifecycle_status character varying(16) DEFAULT 'active'::character varying NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    color character varying(9) DEFAULT ''::character varying NOT NULL,
    CONSTRAINT chk_categories_color CHECK ((((color)::text = ''::text) OR ((color)::text ~ '^#[0-9A-Fa-f]{6}([0-9A-Fa-f]{2})?$'::text))),
    CONSTRAINT chk_categories_counters CHECK (((thread_count >= 0) AND (post_count >= 0))),
    CONSTRAINT chk_categories_group_root CHECK ((((node_kind)::text = 'board'::text) OR (parent_id IS NULL))),
    CONSTRAINT chk_categories_lifecycle_status CHECK (((lifecycle_status)::text = ANY ((ARRAY['active'::character varying, 'archived'::character varying])::text[]))),
    CONSTRAINT chk_categories_node_kind CHECK (((node_kind)::text = ANY ((ARRAY['group'::character varying, 'board'::character varying])::text[]))),
    CONSTRAINT chk_categories_version CHECK ((version >= 1))
);


--
-- Name: category_thread_type_policies; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.category_thread_type_policies (
    category_id bigint NOT NULL,
    thread_type character varying(32) NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_category_thread_type_policy_enabled CHECK (((enabled = true) OR (enabled = false))),
    CONSTRAINT chk_category_thread_type_policy_type CHECK (((thread_type)::text = ANY ((ARRAY['discussion'::character varying, 'article'::character varying, 'mutual_aid'::character varying, 'secondhand'::character varying])::text[])))
);


--
-- Name: configurations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.configurations (
    id bigint NOT NULL,
    key character varying(255) NOT NULL,
    value text NOT NULL,
    type character varying(20) NOT NULL,
    description character varying(500) DEFAULT ''::character varying,
    category character varying(64) NOT NULL,
    is_secret boolean DEFAULT false NOT NULL,
    updated_by bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone
);


--
-- Name: content_moderation_actions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.content_moderation_actions (
    id bigint NOT NULL,
    case_id bigint,
    thread_id bigint NOT NULL,
    action character varying(64) NOT NULL,
    reason text DEFAULT ''::text NOT NULL,
    actor_id bigint,
    before_state character varying(96) DEFAULT ''::character varying NOT NULL,
    after_state character varying(96) DEFAULT ''::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: content_moderation_cases; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.content_moderation_cases (
    id bigint NOT NULL,
    thread_id bigint NOT NULL,
    status character varying(32) NOT NULL,
    reason text DEFAULT ''::text NOT NULL,
    opened_by bigint,
    resolved_by bigint,
    opened_at timestamp with time zone DEFAULT now() NOT NULL,
    resolved_at timestamp with time zone
);


--
-- Name: content_revisions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.content_revisions (
    id bigint NOT NULL,
    thread_id bigint NOT NULL,
    version integer NOT NULL,
    title character varying(255) NOT NULL,
    content text NOT NULL,
    content_format character varying(32) NOT NULL,
    tags text[] DEFAULT '{}'::text[] NOT NULL,
    action character varying(64) NOT NULL,
    reason text DEFAULT ''::text NOT NULL,
    created_by bigint,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: identity_account_recovery_cases; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.identity_account_recovery_cases (
    id bigint NOT NULL,
    public_id character varying(96) NOT NULL,
    user_id bigint NOT NULL,
    account_id bigint NOT NULL,
    target_email_normalized character varying(320) NOT NULL,
    challenge_id bigint NOT NULL,
    created_by bigint,
    proof_reference character varying(160) DEFAULT ''::character varying NOT NULL,
    status character varying(24) DEFAULT 'pending'::character varying NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    completed_at timestamp with time zone,
    cancelled_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_identity_recovery_case_completion CHECK (((((status)::text = 'completed'::text) AND (completed_at IS NOT NULL)) OR ((status)::text <> 'completed'::text))),
    CONSTRAINT chk_identity_recovery_case_status CHECK (((status)::text = ANY ((ARRAY['pending'::character varying, 'completed'::character varying, 'cancelled'::character varying, 'expired'::character varying])::text[]))),
    CONSTRAINT chk_identity_recovery_case_target_email CHECK (((target_email_normalized)::text = lower(btrim((target_email_normalized)::text))))
);


--
-- Name: identity_admin_accounts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.identity_admin_accounts (
    id bigint NOT NULL,
    user_id bigint NOT NULL,
    credential_account_id bigint NOT NULL,
    status character varying(20) DEFAULT 'active'::character varying NOT NULL,
    activation_source character varying(64) DEFAULT 'role_assignment'::character varying NOT NULL,
    activated_at timestamp with time zone DEFAULT now() NOT NULL,
    revoked_at timestamp with time zone,
    last_authenticated_at timestamp with time zone,
    version bigint DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    status_reason character varying(500) DEFAULT ''::character varying NOT NULL,
    status_changed_by bigint,
    status_changed_at timestamp with time zone,
    CONSTRAINT chk_identity_admin_account_revocation CHECK (((((status)::text = 'revoked'::text) AND (revoked_at IS NOT NULL)) OR (((status)::text <> 'revoked'::text) AND (revoked_at IS NULL)))),
    CONSTRAINT chk_identity_admin_account_status CHECK (((status)::text = ANY ((ARRAY['active'::character varying, 'suspended'::character varying, 'revoked'::character varying])::text[]))),
    CONSTRAINT chk_identity_admin_account_status_reason CHECK ((char_length((status_reason)::text) <= 500)),
    CONSTRAINT chk_identity_admin_account_version CHECK ((version >= 1))
);


--
-- Name: identity_challenge_policies; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.identity_challenge_policies (
    id character varying(64) NOT NULL,
    email_window_minutes integer NOT NULL,
    email_max_requests integer NOT NULL,
    ip_window_minutes integer NOT NULL,
    ip_max_requests integer NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    updated_by bigint,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_identity_challenge_policy_email_limit CHECK (((email_max_requests >= 1) AND (email_max_requests <= 100))),
    CONSTRAINT chk_identity_challenge_policy_email_window CHECK (((email_window_minutes >= 1) AND (email_window_minutes <= 1440))),
    CONSTRAINT chk_identity_challenge_policy_id CHECK (((id)::text = 'email_verification'::text)),
    CONSTRAINT chk_identity_challenge_policy_ip_limit CHECK (((ip_max_requests >= 1) AND (ip_max_requests <= 1000))),
    CONSTRAINT chk_identity_challenge_policy_ip_window CHECK (((ip_window_minutes >= 1) AND (ip_window_minutes <= 1440))),
    CONSTRAINT chk_identity_challenge_policy_version CHECK ((version >= 1))
);


--
-- Name: identity_challenge_rate_limits; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.identity_challenge_rate_limits (
    scope character varying(32) NOT NULL,
    subject_digest character varying(128) NOT NULL,
    window_started_at timestamp with time zone NOT NULL,
    request_count integer DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_identity_challenge_rate_count CHECK (((request_count >= 0) AND (request_count <= 10000))),
    CONSTRAINT chk_identity_challenge_rate_scope CHECK (((scope)::text = ANY ((ARRAY['email_minute'::character varying, 'email_day'::character varying, 'ip_hour'::character varying, 'email_window'::character varying, 'ip_window'::character varying])::text[])))
);


--
-- Name: identity_email_challenges; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.identity_email_challenges (
    id bigint NOT NULL,
    public_id character varying(96) NOT NULL,
    purpose character varying(32) NOT NULL,
    email_normalized character varying(320) NOT NULL,
    account_id bigint,
    key_id character varying(64) NOT NULL,
    nonce character varying(128) NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    attempt_count integer DEFAULT 0 NOT NULL,
    max_attempts integer DEFAULT 5 NOT NULL,
    verified_at timestamp with time zone,
    ticket_digest character varying(128),
    ticket_expires_at timestamp with time zone,
    consumed_at timestamp with time zone,
    invalidated_at timestamp with time zone,
    requested_ip_hash character varying(128) NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_identity_email_challenge_attempts CHECK (((attempt_count >= 0) AND ((max_attempts >= 1) AND (max_attempts <= 10)) AND (attempt_count <= max_attempts))),
    CONSTRAINT chk_identity_email_challenge_email_normalized CHECK (((email_normalized)::text = lower(btrim((email_normalized)::text)))),
    CONSTRAINT chk_identity_email_challenge_purpose CHECK (((purpose)::text = ANY ((ARRAY['registration'::character varying, 'email_binding'::character varying, 'password_reset'::character varying])::text[]))),
    CONSTRAINT chk_identity_email_challenge_ticket_state CHECK ((((ticket_digest IS NULL) AND (ticket_expires_at IS NULL)) OR ((ticket_digest IS NOT NULL) AND (ticket_expires_at IS NOT NULL) AND (verified_at IS NOT NULL))))
);


--
-- Name: identity_legacy_email_placeholders; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.identity_legacy_email_placeholders (
    id bigint NOT NULL,
    user_id bigint NOT NULL,
    placeholder_email character varying(320) NOT NULL,
    migration_source character varying(128) DEFAULT ''::character varying NOT NULL,
    resolved_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_identity_legacy_placeholder_email CHECK (((placeholder_email)::text = lower(btrim((placeholder_email)::text))))
);


--
-- Name: identity_mfa_policies; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.identity_mfa_policies (
    id character varying(32) NOT NULL,
    mode character varying(32) DEFAULT 'off'::character varying NOT NULL,
    grace_ends_at timestamp with time zone,
    version bigint DEFAULT 1 NOT NULL,
    updated_by bigint,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_identity_mfa_policy_grace CHECK (((((mode)::text = 'enrollment_grace'::text) AND (grace_ends_at IS NOT NULL)) OR (((mode)::text <> 'enrollment_grace'::text) AND (grace_ends_at IS NULL)))),
    CONSTRAINT chk_identity_mfa_policy_id CHECK (((id)::text = 'admin'::text)),
    CONSTRAINT chk_identity_mfa_policy_mode CHECK (((mode)::text = ANY ((ARRAY['off'::character varying, 'enrollment_grace'::character varying, 'required'::character varying])::text[]))),
    CONSTRAINT chk_identity_mfa_policy_version CHECK ((version >= 1))
);


--
-- Name: identity_mfa_recovery_codes; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.identity_mfa_recovery_codes (
    id bigint NOT NULL,
    user_id bigint NOT NULL,
    method_id bigint NOT NULL,
    code_digest character varying(64) NOT NULL,
    used_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_identity_mfa_recovery_digest CHECK (((code_digest)::text ~ '^[0-9a-f]{64}$'::text))
);


--
-- Name: identity_mfa_tickets; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.identity_mfa_tickets (
    id bigint NOT NULL,
    user_id bigint NOT NULL,
    audience character varying(16) NOT NULL,
    purpose character varying(16) NOT NULL,
    ticket_digest character varying(64) NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    consumed_at timestamp with time zone,
    attempts integer DEFAULT 0 NOT NULL,
    max_attempts integer DEFAULT 5 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_identity_mfa_ticket_attempts CHECK (((attempts >= 0) AND ((max_attempts >= 1) AND (max_attempts <= 10)))),
    CONSTRAINT chk_identity_mfa_ticket_audience CHECK (((audience)::text = ANY ((ARRAY['web'::character varying, 'admin'::character varying])::text[]))),
    CONSTRAINT chk_identity_mfa_ticket_digest CHECK (((ticket_digest)::text ~ '^[0-9a-f]{64}$'::text)),
    CONSTRAINT chk_identity_mfa_ticket_purpose CHECK (((purpose)::text = ANY ((ARRAY['login'::character varying, 'step_up'::character varying])::text[])))
);


--
-- Name: identity_mfa_totp_methods; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.identity_mfa_totp_methods (
    id bigint NOT NULL,
    user_id bigint NOT NULL,
    status character varying(16) NOT NULL,
    key_id character varying(96) NOT NULL,
    nonce text NOT NULL,
    ciphertext text NOT NULL,
    last_accepted_step bigint DEFAULT 0 NOT NULL,
    enrollment_expires_at timestamp with time zone NOT NULL,
    confirmed_at timestamp with time zone,
    disabled_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_identity_mfa_totp_confirmation CHECK ((((status)::text <> 'active'::text) OR (confirmed_at IS NOT NULL))),
    CONSTRAINT chk_identity_mfa_totp_envelope CHECK (((length((key_id)::text) > 0) AND (length(nonce) >= 8) AND (length(ciphertext) >= 16))),
    CONSTRAINT chk_identity_mfa_totp_status CHECK (((status)::text = ANY ((ARRAY['pending'::character varying, 'active'::character varying, 'disabled'::character varying])::text[]))),
    CONSTRAINT chk_identity_mfa_totp_step CHECK ((last_accepted_step >= 0))
);


--
-- Name: identity_reserved_identifiers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.identity_reserved_identifiers (
    identifier_type character varying(32) NOT NULL,
    identifier_normalized character varying(320) NOT NULL,
    reason character varying(128) NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_identity_reserved_identifier_normalized CHECK (((identifier_normalized)::text = lower(btrim((identifier_normalized)::text))))
);


--
-- Name: likes; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.likes (
    id bigint NOT NULL,
    user_id bigint NOT NULL,
    target_type character varying(20) NOT NULL,
    target_id bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    CONSTRAINT chk_likes_target_type CHECK (((target_type)::text = ANY ((ARRAY['thread'::character varying, 'post'::character varying])::text[])))
);


--
-- Name: mcp_audit_logs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.mcp_audit_logs (
    id bigint NOT NULL,
    user_id character varying(64) DEFAULT ''::character varying NOT NULL,
    tool character varying(128) NOT NULL,
    arguments jsonb DEFAULT '{}'::jsonb NOT NULL,
    success boolean DEFAULT false NOT NULL,
    error text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: message_bindings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.message_bindings (
    id bigint NOT NULL,
    user_id character varying(64) NOT NULL,
    platform character varying(64) NOT NULL,
    external_user_id character varying(128) NOT NULL,
    display_name character varying(128) DEFAULT ''::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone
);


--
-- Name: message_logs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.message_logs (
    id bigint NOT NULL,
    platform character varying(64) NOT NULL,
    conversation_id character varying(128) DEFAULT ''::character varying NOT NULL,
    sender_id character varying(128) DEFAULT ''::character varying NOT NULL,
    direction character varying(16) DEFAULT 'inbound'::character varying NOT NULL,
    message_type character varying(32) DEFAULT 'text'::character varying NOT NULL,
    content text DEFAULT ''::text NOT NULL,
    raw_payload jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: mutual_aid_details; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.mutual_aid_details (
    thread_id bigint NOT NULL,
    aid_type character varying(32) NOT NULL,
    aid_status character varying(32) DEFAULT 'open'::character varying NOT NULL,
    deadline timestamp with time zone,
    location_scope character varying(160) DEFAULT ''::character varying NOT NULL,
    contact_mode character varying(32) NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    created_by bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_mutual_aid_details_contact_mode CHECK (((contact_mode)::text = ANY ((ARRAY['comment'::character varying, 'in_app'::character varying, 'email'::character varying, 'other'::character varying])::text[]))),
    CONSTRAINT chk_mutual_aid_details_deadline CHECK (((deadline IS NULL) OR (deadline >= created_at))),
    CONSTRAINT chk_mutual_aid_details_location_scope CHECK ((char_length((location_scope)::text) <= 160)),
    CONSTRAINT chk_mutual_aid_details_status CHECK (((aid_status)::text = ANY ((ARRAY['open'::character varying, 'in_progress'::character varying, 'resolved'::character varying, 'closed'::character varying])::text[]))),
    CONSTRAINT chk_mutual_aid_details_type CHECK (((aid_type)::text = ANY ((ARRAY['request'::character varying, 'offer'::character varying, 'volunteer'::character varying, 'resource_share'::character varying])::text[]))),
    CONSTRAINT chk_mutual_aid_details_version CHECK ((version >= 1))
);


--
-- Name: notifications; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.notifications (
    id bigint NOT NULL,
    user_id bigint NOT NULL,
    type character varying(64) NOT NULL,
    title character varying(255) NOT NULL,
    content text DEFAULT ''::text,
    action_url character varying(512) DEFAULT ''::character varying,
    is_read boolean DEFAULT false NOT NULL,
    read_at timestamp with time zone,
    metadata jsonb DEFAULT '{}'::jsonb,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone
);


--
-- Name: outbox_consumer_receipts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.outbox_consumer_receipts (
    consumer_name character varying(160) NOT NULL,
    event_id character varying(64) NOT NULL,
    attempt integer DEFAULT 0 NOT NULL,
    delivered_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: permission_definitions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.permission_definitions (
    id bigint NOT NULL,
    code character varying(160) NOT NULL,
    domain character varying(64) NOT NULL,
    resource character varying(64) NOT NULL,
    action character varying(64) NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    risk_level character varying(16) DEFAULT 'low'::character varying NOT NULL,
    allowed_scope_types jsonb DEFAULT '["global"]'::jsonb NOT NULL,
    audit_level character varying(16) DEFAULT 'standard'::character varying NOT NULL,
    deprecated_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_permission_definition_audit CHECK (((audit_level)::text = ANY ((ARRAY['standard'::character varying, 'required'::character varying])::text[]))),
    CONSTRAINT chk_permission_definition_code CHECK (((code)::text ~ '^[a-z0-9_]+(\.[a-z0-9_]+){2,}$'::text)),
    CONSTRAINT chk_permission_definition_risk CHECK (((risk_level)::text = ANY ((ARRAY['low'::character varying, 'medium'::character varying, 'high'::character varying])::text[])))
);


--
-- Name: personal_document_previews; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.personal_document_previews (
    id bigint NOT NULL,
    document_version_id bigint NOT NULL,
    preview_object_id bigint,
    status character varying(20) NOT NULL,
    error_code character varying(80) DEFAULT ''::character varying NOT NULL,
    attempts integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_personal_document_previews_attempts CHECK ((attempts >= 0)),
    CONSTRAINT chk_personal_document_previews_status CHECK (((status)::text = ANY ((ARRAY['pending'::character varying, 'processing'::character varying, 'ready'::character varying, 'failed'::character varying, 'unsupported'::character varying])::text[])))
);


--
-- Name: personal_document_versions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.personal_document_versions (
    id bigint NOT NULL,
    document_id bigint NOT NULL,
    version_number integer NOT NULL,
    source_object_id bigint NOT NULL,
    source_type character varying(20) NOT NULL,
    size_bytes bigint NOT NULL,
    sha256 character varying(64) NOT NULL,
    restored_from_version_id bigint,
    created_by bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_personal_document_versions_number CHECK ((version_number >= 1)),
    CONSTRAINT chk_personal_document_versions_size CHECK ((size_bytes >= 0)),
    CONSTRAINT chk_personal_document_versions_type CHECK (((source_type)::text = ANY ((ARRAY['text'::character varying, 'markdown'::character varying, 'campusdoc'::character varying, 'pdf'::character varying, 'docx'::character varying])::text[])))
);


--
-- Name: personal_documents; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.personal_documents (
    id bigint NOT NULL,
    owner_user_id bigint NOT NULL,
    name character varying(255) NOT NULL,
    document_type character varying(20) NOT NULL,
    status character varying(20) DEFAULT 'active'::character varying NOT NULL,
    current_version_id bigint,
    version bigint DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    CONSTRAINT chk_personal_documents_status CHECK (((status)::text = ANY ((ARRAY['active'::character varying, 'trashed'::character varying])::text[]))),
    CONSTRAINT chk_personal_documents_type CHECK (((document_type)::text = ANY ((ARRAY['text'::character varying, 'markdown'::character varying, 'campusdoc'::character varying, 'pdf'::character varying, 'docx'::character varying])::text[]))),
    CONSTRAINT chk_personal_documents_version CHECK ((version >= 1))
);


--
-- Name: platform_command_audits; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.platform_command_audits (
    id character varying(64) NOT NULL,
    command_id character varying(64) NOT NULL,
    command_code character varying(160) NOT NULL,
    actor_id character varying(64),
    actor_type character varying(32),
    resource_type character varying(80),
    resource_id character varying(128),
    operation_code character varying(200),
    permission_code character varying(160),
    request_id character varying(128),
    trace_id character varying(128),
    event_id character varying(64),
    details jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: platform_compatibility_usage; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.platform_compatibility_usage (
    usage_key character varying(255) NOT NULL,
    usage_kind character varying(80) NOT NULL,
    detail jsonb DEFAULT '{}'::jsonb NOT NULL,
    first_seen timestamp with time zone DEFAULT now() NOT NULL,
    last_seen timestamp with time zone DEFAULT now() NOT NULL,
    usage_count bigint DEFAULT 1 NOT NULL
);


--
-- Name: platform_operation_runs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.platform_operation_runs (
    id character varying(96) NOT NULL,
    kind character varying(120) NOT NULL,
    subject_type character varying(80) NOT NULL,
    subject_id character varying(160) NOT NULL,
    status character varying(24) DEFAULT 'pending'::character varying NOT NULL,
    actor_id character varying(64),
    idempotency_key character varying(255),
    details jsonb DEFAULT '{}'::jsonb NOT NULL,
    error_message text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_platform_operation_status CHECK (((status)::text = ANY ((ARRAY['pending'::character varying, 'running'::character varying, 'compensating'::character varying, 'succeeded'::character varying, 'failed'::character varying])::text[])))
);


--
-- Name: platform_outbox; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.platform_outbox (
    id character varying(64) NOT NULL,
    event_type character varying(160) NOT NULL,
    schema_version character varying(64) DEFAULT 'v1'::character varying NOT NULL,
    aggregate_type character varying(80) DEFAULT ''::character varying NOT NULL,
    aggregate_id character varying(128) DEFAULT ''::character varying NOT NULL,
    payload jsonb DEFAULT '{}'::jsonb NOT NULL,
    headers jsonb DEFAULT '{}'::jsonb NOT NULL,
    status character varying(16) DEFAULT 'pending'::character varying NOT NULL,
    idempotency_key character varying(255),
    attempts integer DEFAULT 0 NOT NULL,
    max_attempts integer DEFAULT 8 NOT NULL,
    available_at timestamp with time zone DEFAULT now() NOT NULL,
    lease_owner character varying(160),
    lease_until timestamp with time zone,
    lease_generation bigint DEFAULT 0 NOT NULL,
    last_error text,
    dead_lettered_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_platform_outbox_attempts CHECK (((attempts >= 0) AND (max_attempts > 0))),
    CONSTRAINT chk_platform_outbox_status CHECK (((status)::text = ANY ((ARRAY['pending'::character varying, 'processing'::character varying, 'published'::character varying, 'retry'::character varying, 'dead'::character varying])::text[])))
);


--
-- Name: platform_outbox_attempts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.platform_outbox_attempts (
    id character varying(96) NOT NULL,
    event_id character varying(64) NOT NULL,
    consumer_name character varying(160) NOT NULL,
    worker_id character varying(160) NOT NULL,
    lease_generation bigint NOT NULL,
    attempt integer NOT NULL,
    status character varying(24) NOT NULL,
    error_message text,
    started_at timestamp with time zone DEFAULT now() NOT NULL,
    finished_at timestamp with time zone,
    CONSTRAINT chk_platform_outbox_attempt_status CHECK (((status)::text = ANY ((ARRAY['running'::character varying, 'succeeded'::character varying, 'retry'::character varying, 'dead'::character varying, 'skipped'::character varying, 'failed'::character varying])::text[])))
);


--
-- Name: platform_retention_runs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.platform_retention_runs (
    id character varying(96) NOT NULL,
    target character varying(80) NOT NULL,
    before_at timestamp with time zone NOT NULL,
    eligible_rows bigint DEFAULT 0 NOT NULL,
    mode character varying(24) DEFAULT 'dry-run'::character varying NOT NULL,
    status character varying(24) DEFAULT 'completed'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_platform_retention_run_mode CHECK (((mode)::text = 'dry-run'::text)),
    CONSTRAINT chk_platform_retention_run_status CHECK (((status)::text = ANY ((ARRAY['completed'::character varying, 'failed'::character varying])::text[])))
);


--
-- Name: platform_worker_leases; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.platform_worker_leases (
    worker_id character varying(160) NOT NULL,
    last_heartbeat_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: plugin_catalog_entries; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.plugin_catalog_entries (
    plugin_name character varying(128) NOT NULL,
    display_name character varying(255) DEFAULT ''::character varying NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    version character varying(64) NOT NULL,
    runtime character varying(32) NOT NULL,
    visibility character varying(16) DEFAULT 'draft'::character varying NOT NULL,
    package_checksum character varying(128) DEFAULT ''::character varying NOT NULL,
    risk_level character varying(16) DEFAULT ''::character varying NOT NULL,
    data_capabilities jsonb DEFAULT '[]'::jsonb NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    user_permissions jsonb DEFAULT '[]'::jsonb NOT NULL,
    experience jsonb DEFAULT '{}'::jsonb NOT NULL,
    CONSTRAINT chk_plugin_catalog_visibility CHECK (((visibility)::text = ANY ((ARRAY['draft'::character varying, 'published'::character varying, 'hidden'::character varying])::text[])))
);


--
-- Name: plugin_file_metadata; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.plugin_file_metadata (
    id bigint NOT NULL,
    plugin_name character varying(128) NOT NULL,
    owner_id character varying(64) NOT NULL,
    original_name text NOT NULL,
    stored_name character varying(255) NOT NULL,
    content_type character varying(255) NOT NULL,
    size_bytes bigint NOT NULL,
    storage_key text NOT NULL,
    retention character varying(32) DEFAULT 'user-deletable'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    CONSTRAINT chk_plugin_file_retention CHECK (((retention)::text = ANY ((ARRAY['retained'::character varying, 'user-deletable'::character varying])::text[]))),
    CONSTRAINT chk_plugin_file_size CHECK ((size_bytes >= 0))
);


--
-- Name: plugin_install_requests; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.plugin_install_requests (
    id bigint NOT NULL,
    plugin_name character varying(128) NOT NULL,
    user_id character varying(64) NOT NULL,
    message text DEFAULT ''::text NOT NULL,
    status character varying(16) DEFAULT 'pending'::character varying NOT NULL,
    reviewed_by character varying(64) DEFAULT ''::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    reviewed_at timestamp with time zone,
    CONSTRAINT chk_plugin_install_request_status CHECK (((status)::text = ANY ((ARRAY['pending'::character varying, 'approved'::character varying, 'rejected'::character varying])::text[])))
);


--
-- Name: plugin_logs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.plugin_logs (
    id bigint NOT NULL,
    plugin_name character varying(128) NOT NULL,
    level character varying(16) DEFAULT 'info'::character varying NOT NULL,
    message text NOT NULL,
    event_type character varying(128) DEFAULT ''::character varying,
    trace_id character varying(64) DEFAULT ''::character varying,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone
);


--
-- Name: plugin_market_audits; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.plugin_market_audits (
    id bigint NOT NULL,
    plugin_name character varying(128) NOT NULL,
    actor_id character varying(64) DEFAULT ''::character varying NOT NULL,
    action character varying(128) NOT NULL,
    outcome character varying(32) NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: plugin_permissions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.plugin_permissions (
    id bigint NOT NULL,
    plugin_name character varying(128) NOT NULL,
    permission_type character varying(64) NOT NULL,
    permission_value character varying(255) NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone
);


--
-- Name: plugin_records; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.plugin_records (
    id bigint NOT NULL,
    plugin_name character varying(128) NOT NULL,
    owner_type character varying(16) NOT NULL,
    owner_id character varying(64) NOT NULL,
    collection character varying(64) NOT NULL,
    record_key character varying(128) NOT NULL,
    data jsonb DEFAULT '{}'::jsonb NOT NULL,
    search_text text DEFAULT ''::text NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    CONSTRAINT chk_plugin_records_owner_type CHECK (((owner_type)::text = ANY ((ARRAY['system'::character varying, 'user'::character varying])::text[]))),
    CONSTRAINT chk_plugin_records_version CHECK ((version > 0))
);


--
-- Name: plugin_releases; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.plugin_releases (
    id bigint NOT NULL,
    plugin_name character varying(128) NOT NULL,
    version character varying(64) NOT NULL,
    checksum character varying(128) NOT NULL,
    signature_state character varying(32) NOT NULL,
    channel character varying(32) DEFAULT 'stable'::character varying NOT NULL,
    rollout_state character varying(32) DEFAULT 'pending'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: plugin_user_grants; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.plugin_user_grants (
    id bigint NOT NULL,
    plugin_name character varying(128) NOT NULL,
    user_id character varying(64) NOT NULL,
    version character varying(64) NOT NULL,
    permissions jsonb DEFAULT '[]'::jsonb NOT NULL,
    status character varying(16) NOT NULL,
    granted_at timestamp with time zone DEFAULT now() NOT NULL,
    revoked_at timestamp with time zone,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_plugin_user_grants_status CHECK (((status)::text = ANY ((ARRAY['enabled'::character varying, 'revoked'::character varying])::text[])))
);


--
-- Name: plugins; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.plugins (
    id bigint NOT NULL,
    name character varying(128) NOT NULL,
    display_name character varying(255) DEFAULT ''::character varying NOT NULL,
    version character varying(32) DEFAULT '0.0.0'::character varying NOT NULL,
    description text DEFAULT ''::text,
    author character varying(128) DEFAULT ''::character varying,
    runtime character varying(10) DEFAULT 'grpc'::character varying NOT NULL,
    manifest jsonb DEFAULT '{}'::jsonb NOT NULL,
    status character varying(20) DEFAULT 'installed'::character varying NOT NULL,
    api_key character varying(64) DEFAULT ''::character varying,
    config jsonb DEFAULT '{}'::jsonb,
    error_message text DEFAULT ''::text,
    installed_by character varying(128) DEFAULT 'system'::character varying,
    installed_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    checksum character varying(128) DEFAULT ''::character varying NOT NULL,
    package_size bigint DEFAULT 0 NOT NULL,
    last_preflight_at timestamp with time zone,
    backend_state character varying(32) DEFAULT 'installed'::character varying NOT NULL,
    frontend_state character varying(32) DEFAULT 'unloaded'::character varying NOT NULL,
    health_state character varying(32) DEFAULT 'unknown'::character varying NOT NULL,
    ui_revision bigint DEFAULT 0 NOT NULL,
    CONSTRAINT chk_plugins_backend_state CHECK (((backend_state)::text = ANY ((ARRAY['installed'::character varying, 'starting'::character varying, 'running'::character varying, 'restarting'::character varying, 'stopping'::character varying, 'stopped'::character varying, 'pending_restart'::character varying, 'error'::character varying])::text[]))),
    CONSTRAINT chk_plugins_frontend_state CHECK (((frontend_state)::text = ANY ((ARRAY['unloaded'::character varying, 'loading'::character varying, 'loaded'::character varying, 'incompatible'::character varying, 'error'::character varying])::text[]))),
    CONSTRAINT chk_plugins_health_state CHECK (((health_state)::text = ANY ((ARRAY['healthy'::character varying, 'degraded'::character varying, 'unavailable'::character varying, 'unknown'::character varying])::text[]))),
    CONSTRAINT chk_plugins_package_size CHECK ((package_size >= 0)),
    CONSTRAINT chk_plugins_runtime CHECK (((runtime)::text = ANY ((ARRAY['builtin'::character varying, 'grpc'::character varying, 'wasm'::character varying])::text[]))),
    CONSTRAINT chk_plugins_status CHECK (((status)::text = ANY ((ARRAY['installed'::character varying, 'enabled'::character varying, 'running'::character varying, 'stopped'::character varying, 'error'::character varying])::text[])))
);


--
-- Name: posts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.posts (
    id bigint NOT NULL,
    thread_id bigint NOT NULL,
    author_id bigint NOT NULL,
    author_name character varying(64) DEFAULT ''::character varying NOT NULL,
    parent_id bigint,
    content text NOT NULL,
    content_format character varying(20) DEFAULT 'markdown'::character varying NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    like_count bigint DEFAULT 0 NOT NULL,
    floor_number integer DEFAULT 0 NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    parent_floor_number integer DEFAULT 0 NOT NULL,
    CONSTRAINT chk_posts_counters CHECK (((like_count >= 0) AND (floor_number >= 0))),
    CONSTRAINT chk_posts_status CHECK (((status)::text = ANY ((ARRAY['published'::character varying, 'deleted'::character varying])::text[])))
);


--
-- Name: richtext_article_assets; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.richtext_article_assets (
    id bigint NOT NULL,
    thread_id bigint,
    article_content_id bigint,
    uploader_id bigint NOT NULL,
    file_url text NOT NULL,
    file_name character varying(255) DEFAULT ''::character varying NOT NULL,
    file_size bigint DEFAULT 0 NOT NULL,
    mime_type character varying(100) DEFAULT ''::character varying NOT NULL,
    width integer DEFAULT 0 NOT NULL,
    height integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: richtext_article_contents; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.richtext_article_contents (
    id bigint NOT NULL,
    thread_id bigint NOT NULL,
    title character varying(255) NOT NULL,
    summary text DEFAULT ''::text NOT NULL,
    cover_url text DEFAULT ''::text NOT NULL,
    content_html text DEFAULT ''::text NOT NULL,
    content_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    sanitized_html text DEFAULT ''::text NOT NULL,
    status character varying(32) DEFAULT 'draft'::character varying NOT NULL,
    created_by bigint NOT NULL,
    updated_by bigint,
    published_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    CONSTRAINT chk_richtext_article_status CHECK (((status)::text = ANY ((ARRAY['draft'::character varying, 'published'::character varying, 'pending_review'::character varying, 'offline'::character varying, 'trashed'::character varying, 'deleted'::character varying])::text[])))
);


--
-- Name: role_permissions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.role_permissions (
    id bigint NOT NULL,
    role_id bigint NOT NULL,
    permission_id bigint NOT NULL,
    created_by character varying(64) DEFAULT ''::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone
);


--
-- Name: roles; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.roles (
    id bigint NOT NULL,
    name character varying(32) NOT NULL,
    description character varying(255) DEFAULT ''::character varying,
    is_system boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone
);


--
-- Name: route_operations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.route_operations (
    id bigint NOT NULL,
    operation_code character varying(200) NOT NULL,
    module_owner character varying(128) NOT NULL,
    method character varying(12) NOT NULL,
    path_template text NOT NULL,
    audience character varying(32) NOT NULL,
    legacy_aliases jsonb DEFAULT '[]'::jsonb NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_route_operation_code CHECK (((operation_code)::text ~ '^[a-z0-9_]+(\.[a-z0-9_]+){2,}$'::text))
);


--
-- Name: route_permission_bindings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.route_permission_bindings (
    id bigint NOT NULL,
    route_operation_id bigint NOT NULL,
    permission_id bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone
);


--
-- Name: secondhand_details; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.secondhand_details (
    thread_id bigint NOT NULL,
    price_minor bigint NOT NULL,
    currency character(3) DEFAULT 'CNY'::bpchar NOT NULL,
    item_condition character varying(32) NOT NULL,
    trade_method character varying(32) NOT NULL,
    trade_status character varying(32) DEFAULT 'available'::character varying NOT NULL,
    location_scope character varying(160) DEFAULT ''::character varying NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    created_by bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_secondhand_details_condition CHECK (((item_condition)::text = ANY ((ARRAY['new'::character varying, 'like_new'::character varying, 'good'::character varying, 'fair'::character varying])::text[]))),
    CONSTRAINT chk_secondhand_details_currency CHECK ((currency = 'CNY'::bpchar)),
    CONSTRAINT chk_secondhand_details_location_scope CHECK ((char_length((location_scope)::text) <= 160)),
    CONSTRAINT chk_secondhand_details_price_minor CHECK ((price_minor >= 0)),
    CONSTRAINT chk_secondhand_details_trade_method CHECK (((trade_method)::text = ANY ((ARRAY['in_person'::character varying, 'campus_dropoff'::character varying, 'other'::character varying])::text[]))),
    CONSTRAINT chk_secondhand_details_trade_status CHECK (((trade_status)::text = ANY ((ARRAY['available'::character varying, 'reserved'::character varying, 'sold'::character varying, 'closed'::character varying])::text[]))),
    CONSTRAINT chk_secondhand_details_version CHECK ((version >= 1))
);


--
-- Name: sessions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.sessions (
    id bigint NOT NULL,
    user_id bigint NOT NULL,
    device_id character varying(128) DEFAULT ''::character varying,
    device_name character varying(128) DEFAULT ''::character varying,
    device_type character varying(20) DEFAULT 'web'::character varying,
    user_agent character varying(512) DEFAULT ''::character varying,
    last_active_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    refresh_token_digest character varying(64) NOT NULL,
    token_family_id character varying(32) NOT NULL,
    rotated_from_id character varying(32),
    rotated_to_id character varying(32),
    ip_hash character varying(64) DEFAULT ''::character varying NOT NULL,
    revoked_at timestamp with time zone,
    revoke_reason character varying(64) DEFAULT ''::character varying NOT NULL,
    authentication_strength character varying(16) DEFAULT 'password'::character varying NOT NULL,
    mfa_authenticated_at timestamp with time zone,
    CONSTRAINT chk_sessions_authentication_strength CHECK (((authentication_strength)::text = ANY ((ARRAY['password'::character varying, 'mfa'::character varying])::text[]))),
    CONSTRAINT ck_sessions_refresh_token_digest_shape CHECK (((refresh_token_digest)::text ~ '^[0-9a-f]{64}$'::text))
);


--
-- Name: storage_objects; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.storage_objects (
    id bigint NOT NULL,
    owner_user_id bigint NOT NULL,
    namespace character varying(80) NOT NULL,
    purpose character varying(120) NOT NULL,
    provider character varying(32) DEFAULT 'local'::character varying NOT NULL,
    storage_key character varying(255) NOT NULL,
    original_name character varying(255) NOT NULL,
    mime_type character varying(160) NOT NULL,
    size_bytes bigint DEFAULT 0 NOT NULL,
    sha256 character varying(64) DEFAULT ''::character varying NOT NULL,
    status character varying(20) NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    CONSTRAINT chk_storage_objects_deleted_at CHECK (((((status)::text = 'deleted'::text) AND (deleted_at IS NOT NULL)) OR ((status)::text <> 'deleted'::text))),
    CONSTRAINT chk_storage_objects_provider CHECK (((provider)::text = 'local'::text)),
    CONSTRAINT chk_storage_objects_ready_payload CHECK ((((status)::text <> 'ready'::text) OR (((storage_key)::text <> ''::text) AND (size_bytes >= 0) AND ((sha256)::text <> ''::text)))),
    CONSTRAINT chk_storage_objects_size CHECK ((size_bytes >= 0)),
    CONSTRAINT chk_storage_objects_status CHECK (((status)::text = ANY ((ARRAY['pending'::character varying, 'ready'::character varying, 'deleting'::character varying, 'deleted'::character varying, 'quarantined'::character varying, 'missing'::character varying])::text[]))),
    CONSTRAINT chk_storage_objects_version CHECK ((version >= 1))
);


--
-- Name: tags; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.tags (
    id bigint NOT NULL,
    name character varying(32) NOT NULL,
    slug character varying(64) NOT NULL,
    description character varying(255) DEFAULT ''::character varying,
    color character varying(7) DEFAULT '#007bff'::character varying,
    thread_count bigint DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone
);


--
-- Name: threads; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.threads (
    id bigint NOT NULL,
    title character varying(255) NOT NULL,
    content text NOT NULL,
    content_format character varying(20) DEFAULT 'markdown'::character varying NOT NULL,
    author_id bigint NOT NULL,
    author_name character varying(64) DEFAULT ''::character varying NOT NULL,
    category_id bigint NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    is_pinned boolean DEFAULT false NOT NULL,
    is_locked boolean DEFAULT false NOT NULL,
    is_highlighted boolean DEFAULT false NOT NULL,
    view_count bigint DEFAULT 0 NOT NULL,
    reply_count bigint DEFAULT 0 NOT NULL,
    like_count bigint DEFAULT 0 NOT NULL,
    last_post_id bigint,
    last_post_at timestamp with time zone,
    tags text[] DEFAULT '{}'::text[],
    metadata jsonb DEFAULT '{}'::jsonb,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    publication_status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    moderation_status character varying(20) DEFAULT 'clear'::character varying NOT NULL,
    deletion_status character varying(20) DEFAULT 'active'::character varying NOT NULL,
    moderation_reason text DEFAULT ''::text NOT NULL,
    moderation_by bigint,
    moderation_at timestamp with time zone,
    current_revision integer DEFAULT 0 NOT NULL,
    thread_type character varying(32) DEFAULT 'discussion'::character varying NOT NULL,
    CONSTRAINT chk_threads_counters CHECK (((view_count >= 0) AND (reply_count >= 0) AND (like_count >= 0))),
    CONSTRAINT chk_threads_deletion_status CHECK (((deletion_status)::text = ANY ((ARRAY['active'::character varying, 'trashed'::character varying, 'purged'::character varying])::text[]))),
    CONSTRAINT chk_threads_moderation_status CHECK (((moderation_status)::text = ANY ((ARRAY['clear'::character varying, 'pending'::character varying, 'rejected'::character varying, 'taken_down'::character varying])::text[]))),
    CONSTRAINT chk_threads_publication_status CHECK (((publication_status)::text = ANY ((ARRAY['draft'::character varying, 'published'::character varying, 'private'::character varying])::text[]))),
    CONSTRAINT chk_threads_status CHECK (((status)::text = ANY ((ARRAY['draft'::character varying, 'pending_review'::character varying, 'published'::character varying, 'private'::character varying, 'archived'::character varying])::text[]))),
    CONSTRAINT chk_threads_thread_type CHECK (((thread_type)::text = ANY ((ARRAY['discussion'::character varying, 'article'::character varying, 'mutual_aid'::character varying, 'secondhand'::character varying])::text[])))
);


--
-- Name: user_roles; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_roles (
    id bigint NOT NULL,
    user_id bigint NOT NULL,
    role_id bigint NOT NULL,
    scope_type character varying(20) DEFAULT 'global'::character varying,
    scope_id bigint,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    CONSTRAINT chk_user_roles_scope_shape CHECK (((deleted_at IS NOT NULL) OR COALESCE((((((scope_type)::text = 'global'::text) AND (scope_id IS NULL)) OR (((scope_type)::text = 'category'::text) AND (scope_id IS NOT NULL) AND (scope_id > 0))) AND ((role_id <> 2) OR ((scope_type)::text = 'category'::text))), false)))
);


--
-- Name: user_schedule_preferences; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_schedule_preferences (
    user_id bigint NOT NULL,
    academic_term_id bigint NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: user_schedule_terms; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_schedule_terms (
    user_id bigint NOT NULL,
    academic_term_id bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    current_object_id bigint,
    first_week_start date,
    version bigint DEFAULT 1 NOT NULL
);


--
-- Name: user_space_contents; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_space_contents (
    id bigint NOT NULL,
    user_id bigint NOT NULL,
    thread_id bigint NOT NULL,
    title character varying(255) NOT NULL,
    excerpt text DEFAULT ''::text NOT NULL,
    author_name character varying(64) DEFAULT ''::character varying NOT NULL,
    category_id bigint NOT NULL,
    tags text[] DEFAULT '{}'::text[] NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    thread_created_at timestamp with time zone NOT NULL,
    thread_updated_at timestamp with time zone NOT NULL,
    synced_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone
);


--
-- Name: user_space_style_snapshots; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_space_style_snapshots (
    id bigint NOT NULL,
    user_id character varying(64) NOT NULL,
    snapshot_type character varying(32) DEFAULT 'before_apply'::character varying NOT NULL,
    style_name character varying(64) DEFAULT ''::character varying NOT NULL,
    style_version character varying(32) DEFAULT ''::character varying NOT NULL,
    theme character varying(64) DEFAULT 'default'::character varying NOT NULL,
    layout character varying(64) DEFAULT 'blog'::character varying NOT NULL,
    style_manifest jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: user_spaces; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_spaces (
    id bigint NOT NULL,
    user_id bigint NOT NULL,
    title character varying(120) DEFAULT ''::character varying NOT NULL,
    bio character varying(500) DEFAULT ''::character varying NOT NULL,
    avatar character varying(512) DEFAULT ''::character varying NOT NULL,
    cover_image character varying(512) DEFAULT ''::character varying NOT NULL,
    theme character varying(64) DEFAULT 'default'::character varying NOT NULL,
    layout character varying(64) DEFAULT 'blog'::character varying NOT NULL,
    visibility character varying(20) DEFAULT 'public'::character varying NOT NULL,
    sync_enabled boolean DEFAULT true NOT NULL,
    sync_categories text[] DEFAULT '{}'::text[] NOT NULL,
    sync_tags text[] DEFAULT '{}'::text[] NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    style_name character varying(128) DEFAULT ''::character varying NOT NULL,
    style_version character varying(32) DEFAULT ''::character varying NOT NULL,
    style_manifest jsonb DEFAULT '{}'::jsonb NOT NULL,
    disabled_at timestamp with time zone,
    disabled_by character varying(64) DEFAULT ''::character varying NOT NULL,
    disabled_reason text DEFAULT ''::text NOT NULL,
    last_sync_at timestamp with time zone,
    last_sync_error text DEFAULT ''::text NOT NULL
);


--
-- Name: user_storage_accounts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_storage_accounts (
    user_id bigint NOT NULL,
    used_bytes bigint DEFAULT 0 NOT NULL,
    reserved_bytes bigint DEFAULT 0 NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_user_storage_accounts_reserved CHECK ((reserved_bytes >= 0)),
    CONSTRAINT chk_user_storage_accounts_used CHECK ((used_bytes >= 0)),
    CONSTRAINT chk_user_storage_accounts_version CHECK ((version >= 1))
);


--
-- Name: user_storage_quotas; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_storage_quotas (
    user_id bigint NOT NULL,
    quota_bytes bigint NOT NULL,
    updated_by bigint,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT user_storage_quotas_quota_bytes_check CHECK ((quota_bytes > 0))
);


--
-- Name: user_storage_reservations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_storage_reservations (
    id bigint NOT NULL,
    user_id bigint NOT NULL,
    object_id bigint NOT NULL,
    reserved_bytes bigint NOT NULL,
    status character varying(20) NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_user_storage_reservations_bytes CHECK ((reserved_bytes >= 0)),
    CONSTRAINT chk_user_storage_reservations_status CHECK (((status)::text = ANY ((ARRAY['pending'::character varying, 'committed'::character varying, 'released'::character varying, 'expired'::character varying])::text[])))
);


--
-- Name: users; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.users (
    id bigint NOT NULL,
    username character varying(32) NOT NULL,
    nickname character varying(64) NOT NULL,
    email character varying(255) NOT NULL,
    avatar character varying(512) DEFAULT ''::character varying,
    bio character varying(500) DEFAULT ''::character varying,
    status character varying(20) DEFAULT 'active'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    auth_version bigint DEFAULT 1 NOT NULL,
    must_change_password boolean DEFAULT false NOT NULL,
    CONSTRAINT chk_users_auth_version CHECK ((auth_version >= 1)),
    CONSTRAINT chk_users_status CHECK (((status)::text = ANY ((ARRAY['active'::character varying, 'suspended'::character varying, 'deactivated'::character varying])::text[])))
);


--
-- Name: webhook_deliveries; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.webhook_deliveries (
    id bigint NOT NULL,
    endpoint_id bigint NOT NULL,
    event_id character varying(128) DEFAULT ''::character varying NOT NULL,
    event_type character varying(128) DEFAULT ''::character varying NOT NULL,
    target_url text DEFAULT ''::text NOT NULL,
    status character varying(32) DEFAULT 'pending'::character varying NOT NULL,
    attempts integer DEFAULT 0 NOT NULL,
    response_status integer DEFAULT 0 NOT NULL,
    error_message text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    outbox_event_id character varying(64),
    delivery_key character varying(255),
    next_attempt_at timestamp with time zone,
    dead_lettered_at timestamp with time zone
);


--
-- Name: webhook_endpoints; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.webhook_endpoints (
    id bigint NOT NULL,
    name character varying(128) NOT NULL,
    url text NOT NULL,
    secret character varying(255) DEFAULT ''::character varying NOT NULL,
    events text[] DEFAULT '{}'::text[] NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    max_retries integer DEFAULT 2 NOT NULL,
    timeout_ms integer DEFAULT 5000 NOT NULL,
    created_by character varying(64) DEFAULT ''::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    max_concurrent integer DEFAULT 2 NOT NULL,
    rate_limit_per_minute integer DEFAULT 60 NOT NULL,
    CONSTRAINT chk_webhook_endpoint_max_concurrent CHECK (((max_concurrent >= 1) AND (max_concurrent <= 16))),
    CONSTRAINT chk_webhook_endpoint_rate_limit CHECK (((rate_limit_per_minute >= 1) AND (rate_limit_per_minute <= 600)))
);


--
-- Name: academic_terms academic_terms_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.academic_terms
    ADD CONSTRAINT academic_terms_pkey PRIMARY KEY (id);


--
-- Name: accounts accounts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.accounts
    ADD CONSTRAINT accounts_pkey PRIMARY KEY (id);


--
-- Name: ai_call_logs ai_call_logs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ai_call_logs
    ADD CONSTRAINT ai_call_logs_pkey PRIMARY KEY (id);


--
-- Name: api_keys api_keys_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.api_keys
    ADD CONSTRAINT api_keys_pkey PRIMARY KEY (id);


--
-- Name: audit_logs audit_logs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audit_logs
    ADD CONSTRAINT audit_logs_pkey PRIMARY KEY (id);


--
-- Name: authorization_audits authorization_audits_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.authorization_audits
    ADD CONSTRAINT authorization_audits_pkey PRIMARY KEY (id);


--
-- Name: builtin_feature_states builtin_feature_states_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.builtin_feature_states
    ADD CONSTRAINT builtin_feature_states_pkey PRIMARY KEY (feature_id);


--
-- Name: categories categories_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.categories
    ADD CONSTRAINT categories_pkey PRIMARY KEY (id);


--
-- Name: category_thread_type_policies category_thread_type_policies_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.category_thread_type_policies
    ADD CONSTRAINT category_thread_type_policies_pkey PRIMARY KEY (category_id, thread_type);


--
-- Name: configurations configurations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.configurations
    ADD CONSTRAINT configurations_pkey PRIMARY KEY (id);


--
-- Name: content_moderation_actions content_moderation_actions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.content_moderation_actions
    ADD CONSTRAINT content_moderation_actions_pkey PRIMARY KEY (id);


--
-- Name: content_moderation_cases content_moderation_cases_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.content_moderation_cases
    ADD CONSTRAINT content_moderation_cases_pkey PRIMARY KEY (id);


--
-- Name: content_revisions content_revisions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.content_revisions
    ADD CONSTRAINT content_revisions_pkey PRIMARY KEY (id);


--
-- Name: identity_account_recovery_cases identity_account_recovery_cases_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_account_recovery_cases
    ADD CONSTRAINT identity_account_recovery_cases_pkey PRIMARY KEY (id);


--
-- Name: identity_admin_accounts identity_admin_accounts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_admin_accounts
    ADD CONSTRAINT identity_admin_accounts_pkey PRIMARY KEY (id);


--
-- Name: identity_challenge_policies identity_challenge_policies_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_challenge_policies
    ADD CONSTRAINT identity_challenge_policies_pkey PRIMARY KEY (id);


--
-- Name: identity_challenge_rate_limits identity_challenge_rate_limits_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_challenge_rate_limits
    ADD CONSTRAINT identity_challenge_rate_limits_pkey PRIMARY KEY (scope, subject_digest, window_started_at);


--
-- Name: identity_email_challenges identity_email_challenges_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_email_challenges
    ADD CONSTRAINT identity_email_challenges_pkey PRIMARY KEY (id);


--
-- Name: identity_legacy_email_placeholders identity_legacy_email_placeholders_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_legacy_email_placeholders
    ADD CONSTRAINT identity_legacy_email_placeholders_pkey PRIMARY KEY (id);


--
-- Name: identity_mfa_policies identity_mfa_policies_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_mfa_policies
    ADD CONSTRAINT identity_mfa_policies_pkey PRIMARY KEY (id);


--
-- Name: identity_mfa_recovery_codes identity_mfa_recovery_codes_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_mfa_recovery_codes
    ADD CONSTRAINT identity_mfa_recovery_codes_pkey PRIMARY KEY (id);


--
-- Name: identity_mfa_tickets identity_mfa_tickets_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_mfa_tickets
    ADD CONSTRAINT identity_mfa_tickets_pkey PRIMARY KEY (id);


--
-- Name: identity_mfa_totp_methods identity_mfa_totp_methods_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_mfa_totp_methods
    ADD CONSTRAINT identity_mfa_totp_methods_pkey PRIMARY KEY (id);


--
-- Name: identity_reserved_identifiers identity_reserved_identifiers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_reserved_identifiers
    ADD CONSTRAINT identity_reserved_identifiers_pkey PRIMARY KEY (identifier_type, identifier_normalized);


--
-- Name: likes likes_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.likes
    ADD CONSTRAINT likes_pkey PRIMARY KEY (id);


--
-- Name: mcp_audit_logs mcp_audit_logs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.mcp_audit_logs
    ADD CONSTRAINT mcp_audit_logs_pkey PRIMARY KEY (id);


--
-- Name: message_bindings message_bindings_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.message_bindings
    ADD CONSTRAINT message_bindings_pkey PRIMARY KEY (id);


--
-- Name: message_logs message_logs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.message_logs
    ADD CONSTRAINT message_logs_pkey PRIMARY KEY (id);


--
-- Name: mutual_aid_details mutual_aid_details_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.mutual_aid_details
    ADD CONSTRAINT mutual_aid_details_pkey PRIMARY KEY (thread_id);


--
-- Name: notifications notifications_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.notifications
    ADD CONSTRAINT notifications_pkey PRIMARY KEY (id);


--
-- Name: outbox_consumer_receipts outbox_consumer_receipts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.outbox_consumer_receipts
    ADD CONSTRAINT outbox_consumer_receipts_pkey PRIMARY KEY (consumer_name, event_id);


--
-- Name: permission_definitions permission_definitions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.permission_definitions
    ADD CONSTRAINT permission_definitions_pkey PRIMARY KEY (id);


--
-- Name: personal_document_previews personal_document_previews_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.personal_document_previews
    ADD CONSTRAINT personal_document_previews_pkey PRIMARY KEY (id);


--
-- Name: personal_document_versions personal_document_versions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.personal_document_versions
    ADD CONSTRAINT personal_document_versions_pkey PRIMARY KEY (id);


--
-- Name: personal_documents personal_documents_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.personal_documents
    ADD CONSTRAINT personal_documents_pkey PRIMARY KEY (id);


--
-- Name: platform_command_audits platform_command_audits_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.platform_command_audits
    ADD CONSTRAINT platform_command_audits_pkey PRIMARY KEY (id);


--
-- Name: platform_compatibility_usage platform_compatibility_usage_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.platform_compatibility_usage
    ADD CONSTRAINT platform_compatibility_usage_pkey PRIMARY KEY (usage_key);


--
-- Name: platform_operation_runs platform_operation_runs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.platform_operation_runs
    ADD CONSTRAINT platform_operation_runs_pkey PRIMARY KEY (id);


--
-- Name: platform_outbox_attempts platform_outbox_attempts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.platform_outbox_attempts
    ADD CONSTRAINT platform_outbox_attempts_pkey PRIMARY KEY (id);


--
-- Name: platform_outbox platform_outbox_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.platform_outbox
    ADD CONSTRAINT platform_outbox_pkey PRIMARY KEY (id);


--
-- Name: platform_retention_runs platform_retention_runs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.platform_retention_runs
    ADD CONSTRAINT platform_retention_runs_pkey PRIMARY KEY (id);


--
-- Name: platform_worker_leases platform_worker_leases_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.platform_worker_leases
    ADD CONSTRAINT platform_worker_leases_pkey PRIMARY KEY (worker_id);


--
-- Name: plugin_catalog_entries plugin_catalog_entries_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.plugin_catalog_entries
    ADD CONSTRAINT plugin_catalog_entries_pkey PRIMARY KEY (plugin_name);


--
-- Name: plugin_file_metadata plugin_file_metadata_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.plugin_file_metadata
    ADD CONSTRAINT plugin_file_metadata_pkey PRIMARY KEY (id);


--
-- Name: plugin_install_requests plugin_install_requests_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.plugin_install_requests
    ADD CONSTRAINT plugin_install_requests_pkey PRIMARY KEY (id);


--
-- Name: plugin_logs plugin_logs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.plugin_logs
    ADD CONSTRAINT plugin_logs_pkey PRIMARY KEY (id);


--
-- Name: plugin_market_audits plugin_market_audits_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.plugin_market_audits
    ADD CONSTRAINT plugin_market_audits_pkey PRIMARY KEY (id);


--
-- Name: plugin_permissions plugin_permissions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.plugin_permissions
    ADD CONSTRAINT plugin_permissions_pkey PRIMARY KEY (id);


--
-- Name: plugin_records plugin_records_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.plugin_records
    ADD CONSTRAINT plugin_records_pkey PRIMARY KEY (id);


--
-- Name: plugin_releases plugin_releases_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.plugin_releases
    ADD CONSTRAINT plugin_releases_pkey PRIMARY KEY (id);


--
-- Name: plugin_user_grants plugin_user_grants_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.plugin_user_grants
    ADD CONSTRAINT plugin_user_grants_pkey PRIMARY KEY (id);


--
-- Name: plugins plugins_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.plugins
    ADD CONSTRAINT plugins_pkey PRIMARY KEY (id);


--
-- Name: posts posts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.posts
    ADD CONSTRAINT posts_pkey PRIMARY KEY (id);


--
-- Name: richtext_article_assets richtext_article_assets_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.richtext_article_assets
    ADD CONSTRAINT richtext_article_assets_pkey PRIMARY KEY (id);


--
-- Name: richtext_article_contents richtext_article_contents_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.richtext_article_contents
    ADD CONSTRAINT richtext_article_contents_pkey PRIMARY KEY (id);


--
-- Name: richtext_article_contents richtext_article_contents_thread_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.richtext_article_contents
    ADD CONSTRAINT richtext_article_contents_thread_id_key UNIQUE (thread_id);


--
-- Name: role_permissions role_permissions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.role_permissions
    ADD CONSTRAINT role_permissions_pkey PRIMARY KEY (id);


--
-- Name: roles roles_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.roles
    ADD CONSTRAINT roles_pkey PRIMARY KEY (id);


--
-- Name: route_operations route_operations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.route_operations
    ADD CONSTRAINT route_operations_pkey PRIMARY KEY (id);


--
-- Name: route_permission_bindings route_permission_bindings_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.route_permission_bindings
    ADD CONSTRAINT route_permission_bindings_pkey PRIMARY KEY (id);


--
-- Name: secondhand_details secondhand_details_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.secondhand_details
    ADD CONSTRAINT secondhand_details_pkey PRIMARY KEY (thread_id);


--
-- Name: sessions sessions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sessions
    ADD CONSTRAINT sessions_pkey PRIMARY KEY (id);


--
-- Name: storage_objects storage_objects_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storage_objects
    ADD CONSTRAINT storage_objects_pkey PRIMARY KEY (id);


--
-- Name: tags tags_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tags
    ADD CONSTRAINT tags_pkey PRIMARY KEY (id);


--
-- Name: threads threads_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.threads
    ADD CONSTRAINT threads_pkey PRIMARY KEY (id);


--
-- Name: academic_terms uk_academic_terms_year_semester; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.academic_terms
    ADD CONSTRAINT uk_academic_terms_year_semester UNIQUE (year, semester);


--
-- Name: content_revisions uk_content_revisions_thread_version; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.content_revisions
    ADD CONSTRAINT uk_content_revisions_thread_version UNIQUE (thread_id, version);


--
-- Name: identity_mfa_recovery_codes uk_identity_mfa_recovery_digest; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_mfa_recovery_codes
    ADD CONSTRAINT uk_identity_mfa_recovery_digest UNIQUE (code_digest);


--
-- Name: identity_mfa_tickets uk_identity_mfa_ticket_digest; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_mfa_tickets
    ADD CONSTRAINT uk_identity_mfa_ticket_digest UNIQUE (ticket_digest);


--
-- Name: personal_document_previews uk_personal_document_previews_version; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.personal_document_previews
    ADD CONSTRAINT uk_personal_document_previews_version UNIQUE (document_version_id);


--
-- Name: personal_document_versions uk_personal_document_versions_number; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.personal_document_versions
    ADD CONSTRAINT uk_personal_document_versions_number UNIQUE (document_id, version_number);


--
-- Name: storage_objects uk_storage_objects_provider_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storage_objects
    ADD CONSTRAINT uk_storage_objects_provider_key UNIQUE (provider, storage_key);


--
-- Name: user_storage_reservations uk_user_storage_reservations_object; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_storage_reservations
    ADD CONSTRAINT uk_user_storage_reservations_object UNIQUE (object_id);


--
-- Name: user_roles user_roles_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_roles
    ADD CONSTRAINT user_roles_pkey PRIMARY KEY (id);


--
-- Name: user_schedule_preferences user_schedule_preferences_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_schedule_preferences
    ADD CONSTRAINT user_schedule_preferences_pkey PRIMARY KEY (user_id);


--
-- Name: user_schedule_terms user_schedule_terms_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_schedule_terms
    ADD CONSTRAINT user_schedule_terms_pkey PRIMARY KEY (user_id, academic_term_id);


--
-- Name: user_space_contents user_space_contents_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_space_contents
    ADD CONSTRAINT user_space_contents_pkey PRIMARY KEY (id);


--
-- Name: user_space_style_snapshots user_space_style_snapshots_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_space_style_snapshots
    ADD CONSTRAINT user_space_style_snapshots_pkey PRIMARY KEY (id);


--
-- Name: user_spaces user_spaces_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_spaces
    ADD CONSTRAINT user_spaces_pkey PRIMARY KEY (id);


--
-- Name: user_storage_accounts user_storage_accounts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_storage_accounts
    ADD CONSTRAINT user_storage_accounts_pkey PRIMARY KEY (user_id);


--
-- Name: user_storage_quotas user_storage_quotas_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_storage_quotas
    ADD CONSTRAINT user_storage_quotas_pkey PRIMARY KEY (user_id);


--
-- Name: user_storage_reservations user_storage_reservations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_storage_reservations
    ADD CONSTRAINT user_storage_reservations_pkey PRIMARY KEY (id);


--
-- Name: users users_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);


--
-- Name: webhook_deliveries webhook_deliveries_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.webhook_deliveries
    ADD CONSTRAINT webhook_deliveries_pkey PRIMARY KEY (id);


--
-- Name: webhook_endpoints webhook_endpoints_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.webhook_endpoints
    ADD CONSTRAINT webhook_endpoints_pkey PRIMARY KEY (id);


--
-- Name: idx_academic_terms_status_year; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_academic_terms_status_year ON public.academic_terms USING btree (status, year DESC, semester);


--
-- Name: idx_accounts_user_email_active; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_accounts_user_email_active ON public.accounts USING btree (user_id, identifier_normalized) WHERE (((type)::text = 'email'::text) AND (deleted_at IS NULL));


--
-- Name: idx_accounts_user_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_accounts_user_id ON public.accounts USING btree (user_id) WHERE (deleted_at IS NULL);


--
-- Name: idx_accounts_verification_state; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_accounts_verification_state ON public.accounts USING btree (verification_state) WHERE (((type)::text = 'email'::text) AND (deleted_at IS NULL));


--
-- Name: idx_ai_call_logs_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_ai_call_logs_created ON public.ai_call_logs USING btree (created_at DESC) WHERE (deleted_at IS NULL);


--
-- Name: idx_ai_call_logs_provider_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_ai_call_logs_provider_created ON public.ai_call_logs USING btree (provider, created_at DESC) WHERE (deleted_at IS NULL);


--
-- Name: idx_ai_call_logs_source; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_ai_call_logs_source ON public.ai_call_logs USING btree (source) WHERE ((deleted_at IS NULL) AND ((source)::text <> ''::text));


--
-- Name: idx_ai_call_logs_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_ai_call_logs_status ON public.ai_call_logs USING btree (status) WHERE (deleted_at IS NULL);


--
-- Name: idx_api_keys_expires_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_api_keys_expires_at ON public.api_keys USING btree (expires_at) WHERE ((deleted_at IS NULL) AND (is_active = true));


--
-- Name: idx_api_keys_plugin; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_api_keys_plugin ON public.api_keys USING btree (plugin_name) WHERE (deleted_at IS NULL);


--
-- Name: idx_api_keys_user_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_api_keys_user_id ON public.api_keys USING btree (user_id) WHERE (deleted_at IS NULL);


--
-- Name: idx_audit_logs_actor_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_audit_logs_actor_id ON public.audit_logs USING btree (actor_id);


--
-- Name: idx_audit_logs_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_audit_logs_created_at ON public.audit_logs USING btree (created_at DESC);


--
-- Name: idx_audit_logs_resource; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_audit_logs_resource ON public.audit_logs USING btree (resource, resource_id);


--
-- Name: idx_audit_logs_trace_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_audit_logs_trace_id ON public.audit_logs USING btree (trace_id);


--
-- Name: idx_authorization_audits_actor; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_authorization_audits_actor ON public.authorization_audits USING btree (actor_id, created_at DESC);


--
-- Name: idx_authorization_audits_command; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_authorization_audits_command ON public.authorization_audits USING btree (command_id, created_at DESC) WHERE (command_id IS NOT NULL);


--
-- Name: idx_authorization_audits_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_authorization_audits_created ON public.authorization_audits USING btree (created_at DESC);


--
-- Name: idx_categories_default_tags; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_categories_default_tags ON public.categories USING gin (default_tags) WHERE (deleted_at IS NULL);


--
-- Name: idx_categories_lifecycle_kind; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_categories_lifecycle_kind ON public.categories USING btree (lifecycle_status, node_kind, sort_order, created_at) WHERE (deleted_at IS NULL);


--
-- Name: idx_categories_parent_active; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_categories_parent_active ON public.categories USING btree (parent_id, sort_order, created_at) WHERE ((deleted_at IS NULL) AND ((lifecycle_status)::text = 'active'::text));


--
-- Name: idx_categories_parent_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_categories_parent_id ON public.categories USING btree (parent_id) WHERE (deleted_at IS NULL);


--
-- Name: idx_categories_sort_order; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_categories_sort_order ON public.categories USING btree (sort_order) WHERE (deleted_at IS NULL);


--
-- Name: idx_category_thread_type_policies_enabled; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_category_thread_type_policies_enabled ON public.category_thread_type_policies USING btree (category_id, thread_type) WHERE (enabled = true);


--
-- Name: idx_configurations_category; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_configurations_category ON public.configurations USING btree (category) WHERE (deleted_at IS NULL);


--
-- Name: idx_content_moderation_actions_thread_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_content_moderation_actions_thread_created ON public.content_moderation_actions USING btree (thread_id, created_at DESC);


--
-- Name: idx_content_moderation_cases_open; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_content_moderation_cases_open ON public.content_moderation_cases USING btree (thread_id, opened_at DESC) WHERE (resolved_at IS NULL);


--
-- Name: idx_content_revisions_thread_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_content_revisions_thread_created ON public.content_revisions USING btree (thread_id, created_at DESC);


--
-- Name: idx_identity_admin_accounts_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_identity_admin_accounts_status ON public.identity_admin_accounts USING btree (status, updated_at DESC);


--
-- Name: idx_identity_admin_accounts_status_changed; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_identity_admin_accounts_status_changed ON public.identity_admin_accounts USING btree (status, status_changed_at DESC, user_id);


--
-- Name: idx_identity_challenge_rate_expiry; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_identity_challenge_rate_expiry ON public.identity_challenge_rate_limits USING btree (window_started_at, updated_at);


--
-- Name: idx_identity_email_challenge_delivery; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_identity_email_challenge_delivery ON public.identity_email_challenges USING btree (id, expires_at) WHERE ((verified_at IS NULL) AND (invalidated_at IS NULL));


--
-- Name: idx_identity_email_challenge_email_purpose_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_identity_email_challenge_email_purpose_created ON public.identity_email_challenges USING btree (email_normalized, purpose, created_at DESC);


--
-- Name: idx_identity_email_challenge_expiry; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_identity_email_challenge_expiry ON public.identity_email_challenges USING btree (expires_at, ticket_expires_at) WHERE (consumed_at IS NULL);


--
-- Name: idx_identity_legacy_placeholder_email; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_identity_legacy_placeholder_email ON public.identity_legacy_email_placeholders USING btree (placeholder_email) WHERE (resolved_at IS NULL);


--
-- Name: idx_identity_mfa_recovery_user_unused; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_identity_mfa_recovery_user_unused ON public.identity_mfa_recovery_codes USING btree (user_id, created_at DESC) WHERE (used_at IS NULL);


--
-- Name: idx_identity_mfa_tickets_expiry; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_identity_mfa_tickets_expiry ON public.identity_mfa_tickets USING btree (expires_at) WHERE (consumed_at IS NULL);


--
-- Name: idx_identity_mfa_totp_user_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_identity_mfa_totp_user_status ON public.identity_mfa_totp_methods USING btree (user_id, status, created_at DESC);


--
-- Name: idx_identity_recovery_case_expiry; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_identity_recovery_case_expiry ON public.identity_account_recovery_cases USING btree (status, expires_at) WHERE ((status)::text = 'pending'::text);


--
-- Name: idx_identity_recovery_case_user_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_identity_recovery_case_user_status ON public.identity_account_recovery_cases USING btree (user_id, status, created_at DESC);


--
-- Name: idx_likes_target; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_likes_target ON public.likes USING btree (target_type, target_id) WHERE (deleted_at IS NULL);


--
-- Name: idx_mcp_audit_logs_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_mcp_audit_logs_created ON public.mcp_audit_logs USING btree (created_at DESC);


--
-- Name: idx_mcp_audit_logs_tool; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_mcp_audit_logs_tool ON public.mcp_audit_logs USING btree (tool, created_at DESC);


--
-- Name: idx_message_logs_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_message_logs_created ON public.message_logs USING btree (created_at DESC);


--
-- Name: idx_message_logs_platform_conversation; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_message_logs_platform_conversation ON public.message_logs USING btree (platform, conversation_id, created_at DESC);


--
-- Name: idx_mutual_aid_details_created_by_updated; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_mutual_aid_details_created_by_updated ON public.mutual_aid_details USING btree (created_by, updated_at DESC);


--
-- Name: idx_mutual_aid_details_status_updated; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_mutual_aid_details_status_updated ON public.mutual_aid_details USING btree (aid_status, updated_at DESC);


--
-- Name: idx_notifications_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_notifications_created_at ON public.notifications USING btree (created_at DESC) WHERE (deleted_at IS NULL);


--
-- Name: idx_notifications_user_read; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_notifications_user_read ON public.notifications USING btree (user_id, is_read) WHERE (deleted_at IS NULL);


--
-- Name: idx_outbox_consumer_receipts_event; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_outbox_consumer_receipts_event ON public.outbox_consumer_receipts USING btree (event_id, delivered_at DESC);


--
-- Name: idx_personal_document_versions_document_number; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_personal_document_versions_document_number ON public.personal_document_versions USING btree (document_id, version_number DESC);


--
-- Name: idx_personal_documents_owner_status_updated; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_personal_documents_owner_status_updated ON public.personal_documents USING btree (owner_user_id, status, updated_at DESC);


--
-- Name: idx_platform_command_audits_actor; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_platform_command_audits_actor ON public.platform_command_audits USING btree (actor_id, created_at DESC);


--
-- Name: idx_platform_command_audits_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_platform_command_audits_created ON public.platform_command_audits USING btree (created_at DESC);


--
-- Name: idx_platform_compatibility_last_seen; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_platform_compatibility_last_seen ON public.platform_compatibility_usage USING btree (last_seen DESC);


--
-- Name: idx_platform_operation_runs_subject; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_platform_operation_runs_subject ON public.platform_operation_runs USING btree (kind, subject_type, subject_id, created_at DESC);


--
-- Name: idx_platform_outbox_attempts_event; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_platform_outbox_attempts_event ON public.platform_outbox_attempts USING btree (event_id, started_at DESC);


--
-- Name: idx_platform_outbox_claim; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_platform_outbox_claim ON public.platform_outbox USING btree (status, available_at, created_at) WHERE ((status)::text = ANY ((ARRAY['pending'::character varying, 'retry'::character varying])::text[]));


--
-- Name: idx_platform_outbox_dead; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_platform_outbox_dead ON public.platform_outbox USING btree (dead_lettered_at DESC) WHERE ((status)::text = 'dead'::text);


--
-- Name: idx_platform_retention_runs_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_platform_retention_runs_created ON public.platform_retention_runs USING btree (created_at DESC);


--
-- Name: idx_plugin_catalog_visibility; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_plugin_catalog_visibility ON public.plugin_catalog_entries USING btree (visibility, plugin_name);


--
-- Name: idx_plugin_file_metadata_owner; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_plugin_file_metadata_owner ON public.plugin_file_metadata USING btree (plugin_name, owner_id, created_at DESC) WHERE (deleted_at IS NULL);


--
-- Name: idx_plugin_install_requests_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_plugin_install_requests_status ON public.plugin_install_requests USING btree (status, created_at DESC);


--
-- Name: idx_plugin_logs_level; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_plugin_logs_level ON public.plugin_logs USING btree (level) WHERE (deleted_at IS NULL);


--
-- Name: idx_plugin_logs_plugin_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_plugin_logs_plugin_created ON public.plugin_logs USING btree (plugin_name, created_at DESC) WHERE (deleted_at IS NULL);


--
-- Name: idx_plugin_logs_trace_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_plugin_logs_trace_id ON public.plugin_logs USING btree (trace_id) WHERE ((deleted_at IS NULL) AND ((trace_id)::text <> ''::text));


--
-- Name: idx_plugin_market_audits_plugin_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_plugin_market_audits_plugin_created ON public.plugin_market_audits USING btree (plugin_name, created_at DESC);


--
-- Name: idx_plugin_records_owner_collection_updated; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_plugin_records_owner_collection_updated ON public.plugin_records USING btree (plugin_name, owner_type, owner_id, collection, updated_at DESC) WHERE (deleted_at IS NULL);


--
-- Name: idx_plugin_user_grants_user; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_plugin_user_grants_user ON public.plugin_user_grants USING btree (user_id, status, updated_at DESC);


--
-- Name: idx_plugins_api_key; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_plugins_api_key ON public.plugins USING btree (api_key) WHERE ((deleted_at IS NULL) AND ((api_key)::text <> ''::text));


--
-- Name: idx_plugins_runtime_state; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_plugins_runtime_state ON public.plugins USING btree (backend_state, frontend_state, health_state) WHERE (deleted_at IS NULL);


--
-- Name: idx_plugins_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_plugins_status ON public.plugins USING btree (status) WHERE (deleted_at IS NULL);


--
-- Name: idx_posts_author_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_posts_author_id ON public.posts USING btree (author_id) WHERE (deleted_at IS NULL);


--
-- Name: idx_posts_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_posts_created_at ON public.posts USING btree (created_at) WHERE (deleted_at IS NULL);


--
-- Name: idx_posts_parent_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_posts_parent_id ON public.posts USING btree (parent_id) WHERE ((deleted_at IS NULL) AND (parent_id IS NOT NULL));


--
-- Name: idx_posts_thread_floor; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_posts_thread_floor ON public.posts USING btree (thread_id, floor_number) WHERE (deleted_at IS NULL);


--
-- Name: idx_richtext_article_created_by; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_richtext_article_created_by ON public.richtext_article_contents USING btree (created_by) WHERE (deleted_at IS NULL);


--
-- Name: idx_richtext_article_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_richtext_article_status ON public.richtext_article_contents USING btree (status) WHERE (deleted_at IS NULL);


--
-- Name: idx_richtext_article_thread_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_richtext_article_thread_id ON public.richtext_article_contents USING btree (thread_id) WHERE (deleted_at IS NULL);


--
-- Name: idx_richtext_assets_article_content_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_richtext_assets_article_content_id ON public.richtext_article_assets USING btree (article_content_id) WHERE (article_content_id IS NOT NULL);


--
-- Name: idx_richtext_assets_thread_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_richtext_assets_thread_id ON public.richtext_article_assets USING btree (thread_id) WHERE (thread_id IS NOT NULL);


--
-- Name: idx_richtext_assets_uploader_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_richtext_assets_uploader_id ON public.richtext_article_assets USING btree (uploader_id);


--
-- Name: idx_route_operations_owner; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_route_operations_owner ON public.route_operations USING btree (module_owner, audience);


--
-- Name: idx_secondhand_details_created_by_updated; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_secondhand_details_created_by_updated ON public.secondhand_details USING btree (created_by, updated_at DESC);


--
-- Name: idx_secondhand_details_status_updated; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_secondhand_details_status_updated ON public.secondhand_details USING btree (trade_status, updated_at DESC);


--
-- Name: idx_sessions_expires_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sessions_expires_at ON public.sessions USING btree (expires_at) WHERE (deleted_at IS NULL);


--
-- Name: idx_sessions_token_family; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sessions_token_family ON public.sessions USING btree (token_family_id) WHERE (deleted_at IS NULL);


--
-- Name: idx_sessions_user_active; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sessions_user_active ON public.sessions USING btree (user_id, last_active_at DESC) WHERE (deleted_at IS NULL);


--
-- Name: idx_storage_objects_owner_id_desc; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_storage_objects_owner_id_desc ON public.storage_objects USING btree (owner_user_id, id DESC);


--
-- Name: idx_storage_objects_owner_namespace_purpose_id_desc; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_storage_objects_owner_namespace_purpose_id_desc ON public.storage_objects USING btree (owner_user_id, namespace, purpose, id DESC);


--
-- Name: idx_storage_objects_status_updated_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_storage_objects_status_updated_at ON public.storage_objects USING btree (status, updated_at);


--
-- Name: idx_threads_author_visibility_v10; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_threads_author_visibility_v10 ON public.threads USING btree (author_id, deletion_status, updated_at DESC) WHERE (deleted_at IS NULL);


--
-- Name: idx_threads_category_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_threads_category_id ON public.threads USING btree (category_id) WHERE (deleted_at IS NULL);


--
-- Name: idx_threads_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_threads_created_at ON public.threads USING btree (created_at DESC) WHERE (deleted_at IS NULL);


--
-- Name: idx_threads_is_pinned; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_threads_is_pinned ON public.threads USING btree (is_pinned) WHERE ((deleted_at IS NULL) AND (is_pinned = true));


--
-- Name: idx_threads_last_post_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_threads_last_post_at ON public.threads USING btree (last_post_at DESC) WHERE (deleted_at IS NULL);


--
-- Name: idx_threads_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_threads_status ON public.threads USING btree (status) WHERE (deleted_at IS NULL);


--
-- Name: idx_threads_tags; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_threads_tags ON public.threads USING gin (tags) WHERE (deleted_at IS NULL);


--
-- Name: idx_threads_thread_type_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_threads_thread_type_created ON public.threads USING btree (thread_type, created_at DESC) WHERE (deleted_at IS NULL);


--
-- Name: idx_threads_visibility_v10; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_threads_visibility_v10 ON public.threads USING btree (publication_status, moderation_status, deletion_status, created_at DESC) WHERE (deleted_at IS NULL);


--
-- Name: idx_user_roles_scope_lookup; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_roles_scope_lookup ON public.user_roles USING btree (user_id, scope_type, scope_id, role_id) WHERE (deleted_at IS NULL);


--
-- Name: idx_user_schedule_preferences_term; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_schedule_preferences_term ON public.user_schedule_preferences USING btree (academic_term_id, user_id);


--
-- Name: idx_user_schedule_terms_academic_term; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_schedule_terms_academic_term ON public.user_schedule_terms USING btree (academic_term_id, user_id);


--
-- Name: idx_user_schedule_terms_current_object; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_schedule_terms_current_object ON public.user_schedule_terms USING btree (current_object_id) WHERE (current_object_id IS NOT NULL);


--
-- Name: idx_user_space_contents_category; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_space_contents_category ON public.user_space_contents USING btree (category_id) WHERE (deleted_at IS NULL);


--
-- Name: idx_user_space_contents_tags; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_space_contents_tags ON public.user_space_contents USING gin (tags) WHERE (deleted_at IS NULL);


--
-- Name: idx_user_space_contents_user_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_space_contents_user_created ON public.user_space_contents USING btree (user_id, thread_created_at DESC) WHERE (deleted_at IS NULL);


--
-- Name: idx_user_space_style_snapshots_user_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_space_style_snapshots_user_created ON public.user_space_style_snapshots USING btree (user_id, created_at DESC);


--
-- Name: idx_user_spaces_disabled; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_spaces_disabled ON public.user_spaces USING btree (disabled_at) WHERE (deleted_at IS NULL);


--
-- Name: idx_user_spaces_style_name; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_spaces_style_name ON public.user_spaces USING btree (style_name) WHERE ((deleted_at IS NULL) AND ((style_name)::text <> ''::text));


--
-- Name: idx_user_spaces_sync_categories; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_spaces_sync_categories ON public.user_spaces USING gin (sync_categories) WHERE (deleted_at IS NULL);


--
-- Name: idx_user_spaces_sync_tags; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_spaces_sync_tags ON public.user_spaces USING gin (sync_tags) WHERE (deleted_at IS NULL);


--
-- Name: idx_user_spaces_updated_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_spaces_updated_at ON public.user_spaces USING btree (updated_at DESC) WHERE (deleted_at IS NULL);


--
-- Name: idx_user_spaces_visibility; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_spaces_visibility ON public.user_spaces USING btree (visibility) WHERE (deleted_at IS NULL);


--
-- Name: idx_user_storage_quotas_updated_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_storage_quotas_updated_at ON public.user_storage_quotas USING btree (updated_at DESC);


--
-- Name: idx_user_storage_reservations_pending_expiry; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_storage_reservations_pending_expiry ON public.user_storage_reservations USING btree (status, expires_at) WHERE ((status)::text = 'pending'::text);


--
-- Name: idx_users_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_users_created_at ON public.users USING btree (created_at DESC) WHERE (deleted_at IS NULL);


--
-- Name: idx_users_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_users_status ON public.users USING btree (status) WHERE (deleted_at IS NULL);


--
-- Name: idx_webhook_deliveries_endpoint_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_webhook_deliveries_endpoint_created ON public.webhook_deliveries USING btree (endpoint_id, created_at DESC);


--
-- Name: idx_webhook_deliveries_retry; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_webhook_deliveries_retry ON public.webhook_deliveries USING btree (status, next_attempt_at) WHERE ((status)::text = ANY ((ARRAY['pending'::character varying, 'retry'::character varying])::text[]));


--
-- Name: idx_webhook_deliveries_status_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_webhook_deliveries_status_created ON public.webhook_deliveries USING btree (status, created_at DESC);


--
-- Name: idx_webhook_endpoints_enabled; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_webhook_endpoints_enabled ON public.webhook_endpoints USING btree (enabled) WHERE (deleted_at IS NULL);


--
-- Name: uk_academic_terms_open_default; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_academic_terms_open_default ON public.academic_terms USING btree (is_default) WHERE ((is_default = true) AND ((status)::text = 'open'::text));


--
-- Name: uk_accounts_email_normalized; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_accounts_email_normalized ON public.accounts USING btree (identifier_normalized) WHERE (((type)::text = 'email'::text) AND (deleted_at IS NULL));


--
-- Name: uk_accounts_type_identifier; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_accounts_type_identifier ON public.accounts USING btree (type, identifier) WHERE (deleted_at IS NULL);


--
-- Name: uk_api_keys_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_api_keys_key ON public.api_keys USING btree (key) WHERE (deleted_at IS NULL);


--
-- Name: uk_categories_slug; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_categories_slug ON public.categories USING btree (slug) WHERE (deleted_at IS NULL);


--
-- Name: uk_configurations_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_configurations_key ON public.configurations USING btree (key) WHERE (deleted_at IS NULL);


--
-- Name: uk_identity_admin_accounts_credential; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_identity_admin_accounts_credential ON public.identity_admin_accounts USING btree (credential_account_id);


--
-- Name: uk_identity_admin_accounts_user; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_identity_admin_accounts_user ON public.identity_admin_accounts USING btree (user_id);


--
-- Name: uk_identity_email_challenge_public_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_identity_email_challenge_public_id ON public.identity_email_challenges USING btree (public_id);


--
-- Name: uk_identity_legacy_placeholder_user; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_identity_legacy_placeholder_user ON public.identity_legacy_email_placeholders USING btree (user_id) WHERE (resolved_at IS NULL);


--
-- Name: uk_identity_mfa_totp_active_user; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_identity_mfa_totp_active_user ON public.identity_mfa_totp_methods USING btree (user_id) WHERE ((status)::text = 'active'::text);


--
-- Name: uk_identity_mfa_totp_pending_user; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_identity_mfa_totp_pending_user ON public.identity_mfa_totp_methods USING btree (user_id) WHERE ((status)::text = 'pending'::text);


--
-- Name: uk_identity_recovery_case_challenge; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_identity_recovery_case_challenge ON public.identity_account_recovery_cases USING btree (challenge_id);


--
-- Name: uk_identity_recovery_case_public_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_identity_recovery_case_public_id ON public.identity_account_recovery_cases USING btree (public_id);


--
-- Name: uk_likes_user_target; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_likes_user_target ON public.likes USING btree (user_id, target_type, target_id) WHERE (deleted_at IS NULL);


--
-- Name: uk_message_bindings_platform_external; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_message_bindings_platform_external ON public.message_bindings USING btree (platform, external_user_id) WHERE (deleted_at IS NULL);


--
-- Name: uk_permission_definitions_code; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_permission_definitions_code ON public.permission_definitions USING btree (code);


--
-- Name: uk_platform_operation_idempotency; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_platform_operation_idempotency ON public.platform_operation_runs USING btree (idempotency_key) WHERE (idempotency_key IS NOT NULL);


--
-- Name: uk_platform_outbox_idempotency; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_platform_outbox_idempotency ON public.platform_outbox USING btree (idempotency_key) WHERE (idempotency_key IS NOT NULL);


--
-- Name: uk_plugin_install_request_pending; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_plugin_install_request_pending ON public.plugin_install_requests USING btree (plugin_name, user_id) WHERE ((status)::text = 'pending'::text);


--
-- Name: uk_plugin_permissions; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_plugin_permissions ON public.plugin_permissions USING btree (plugin_name, permission_type, permission_value) WHERE (deleted_at IS NULL);


--
-- Name: uk_plugin_records_active_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_plugin_records_active_key ON public.plugin_records USING btree (plugin_name, owner_type, owner_id, collection, record_key) WHERE (deleted_at IS NULL);


--
-- Name: uk_plugin_releases_version; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_plugin_releases_version ON public.plugin_releases USING btree (plugin_name, version, checksum);


--
-- Name: uk_plugin_user_grants_identity; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_plugin_user_grants_identity ON public.plugin_user_grants USING btree (plugin_name, user_id);


--
-- Name: uk_plugins_name; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_plugins_name ON public.plugins USING btree (name) WHERE (deleted_at IS NULL);


--
-- Name: uk_role_permissions_active; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_role_permissions_active ON public.role_permissions USING btree (role_id, permission_id) WHERE (deleted_at IS NULL);


--
-- Name: uk_roles_name; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_roles_name ON public.roles USING btree (name) WHERE (deleted_at IS NULL);


--
-- Name: uk_route_operations_code; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_route_operations_code ON public.route_operations USING btree (operation_code);


--
-- Name: uk_route_permission_bindings_active; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_route_permission_bindings_active ON public.route_permission_bindings USING btree (route_operation_id, permission_id) WHERE (deleted_at IS NULL);


--
-- Name: uk_sessions_refresh_token_digest; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_sessions_refresh_token_digest ON public.sessions USING btree (refresh_token_digest);


--
-- Name: uk_tags_name; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_tags_name ON public.tags USING btree (name) WHERE (deleted_at IS NULL);


--
-- Name: uk_tags_slug; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_tags_slug ON public.tags USING btree (slug) WHERE (deleted_at IS NULL);


--
-- Name: uk_user_roles; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_user_roles ON public.user_roles USING btree (user_id, role_id, scope_type, scope_id) WHERE (deleted_at IS NULL);


--
-- Name: uk_user_roles_global_active; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_user_roles_global_active ON public.user_roles USING btree (user_id, role_id, scope_type) WHERE ((deleted_at IS NULL) AND (scope_id IS NULL));


--
-- Name: uk_user_space_contents_thread_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_user_space_contents_thread_id ON public.user_space_contents USING btree (thread_id) WHERE (deleted_at IS NULL);


--
-- Name: uk_user_spaces_user_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_user_spaces_user_id ON public.user_spaces USING btree (user_id) WHERE (deleted_at IS NULL);


--
-- Name: uk_users_email; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_users_email ON public.users USING btree (email) WHERE (deleted_at IS NULL);


--
-- Name: uk_users_username; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_users_username ON public.users USING btree (username) WHERE (deleted_at IS NULL);


--
-- Name: uk_webhook_deliveries_delivery_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_webhook_deliveries_delivery_key ON public.webhook_deliveries USING btree (delivery_key) WHERE (delivery_key IS NOT NULL);


--
-- Name: categories trg_categories_hierarchy_guard; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_categories_hierarchy_guard BEFORE INSERT OR UPDATE OF parent_id, node_kind, lifecycle_status, deleted_at ON public.categories FOR EACH ROW EXECUTE FUNCTION public.campusos_guard_category_hierarchy();


--
-- Name: category_thread_type_policies trg_category_thread_type_policy_guard; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_category_thread_type_policy_guard BEFORE INSERT OR UPDATE OF category_id, thread_type ON public.category_thread_type_policies FOR EACH ROW EXECUTE FUNCTION public.campusos_guard_category_thread_type_policy();


--
-- Name: mutual_aid_details trg_mutual_aid_detail_guard; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_mutual_aid_detail_guard BEFORE INSERT OR UPDATE OF thread_id, created_by ON public.mutual_aid_details FOR EACH ROW EXECUTE FUNCTION public.campusos_guard_mutual_aid_detail();


--
-- Name: secondhand_details trg_secondhand_detail_guard; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_secondhand_detail_guard BEFORE INSERT OR UPDATE OF thread_id, created_by ON public.secondhand_details FOR EACH ROW EXECUTE FUNCTION public.campusos_guard_secondhand_detail();


--
-- Name: user_roles trg_sync_identity_admin_account_from_role; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_sync_identity_admin_account_from_role AFTER INSERT OR DELETE OR UPDATE OF user_id, role_id, scope_type, scope_id, deleted_at ON public.user_roles FOR EACH ROW EXECUTE FUNCTION public.sync_identity_admin_account_from_role();


--
-- Name: academic_terms academic_terms_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.academic_terms
    ADD CONSTRAINT academic_terms_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: academic_terms academic_terms_updated_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.academic_terms
    ADD CONSTRAINT academic_terms_updated_by_fkey FOREIGN KEY (updated_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: accounts fk_accounts_user; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.accounts
    ADD CONSTRAINT fk_accounts_user FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: categories fk_categories_parent; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.categories
    ADD CONSTRAINT fk_categories_parent FOREIGN KEY (parent_id) REFERENCES public.categories(id) ON DELETE RESTRICT;


--
-- Name: category_thread_type_policies fk_category_thread_type_policy_category; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.category_thread_type_policies
    ADD CONSTRAINT fk_category_thread_type_policy_category FOREIGN KEY (category_id) REFERENCES public.categories(id) ON DELETE RESTRICT;


--
-- Name: configurations fk_configurations_updated_by; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.configurations
    ADD CONSTRAINT fk_configurations_updated_by FOREIGN KEY (updated_by) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: identity_admin_accounts fk_identity_admin_account_credential; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_admin_accounts
    ADD CONSTRAINT fk_identity_admin_account_credential FOREIGN KEY (credential_account_id) REFERENCES public.accounts(id) ON DELETE RESTRICT;


--
-- Name: identity_admin_accounts fk_identity_admin_account_status_changed_by; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_admin_accounts
    ADD CONSTRAINT fk_identity_admin_account_status_changed_by FOREIGN KEY (status_changed_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: identity_admin_accounts fk_identity_admin_account_user; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_admin_accounts
    ADD CONSTRAINT fk_identity_admin_account_user FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: identity_challenge_policies fk_identity_challenge_policy_updated_by; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_challenge_policies
    ADD CONSTRAINT fk_identity_challenge_policy_updated_by FOREIGN KEY (updated_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: identity_email_challenges fk_identity_email_challenge_account; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_email_challenges
    ADD CONSTRAINT fk_identity_email_challenge_account FOREIGN KEY (account_id) REFERENCES public.accounts(id) ON DELETE RESTRICT;


--
-- Name: identity_legacy_email_placeholders fk_identity_legacy_placeholder_user; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_legacy_email_placeholders
    ADD CONSTRAINT fk_identity_legacy_placeholder_user FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: identity_mfa_policies fk_identity_mfa_policy_updated_by; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_mfa_policies
    ADD CONSTRAINT fk_identity_mfa_policy_updated_by FOREIGN KEY (updated_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: identity_mfa_recovery_codes fk_identity_mfa_recovery_method; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_mfa_recovery_codes
    ADD CONSTRAINT fk_identity_mfa_recovery_method FOREIGN KEY (method_id) REFERENCES public.identity_mfa_totp_methods(id) ON DELETE RESTRICT;


--
-- Name: identity_mfa_recovery_codes fk_identity_mfa_recovery_user; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_mfa_recovery_codes
    ADD CONSTRAINT fk_identity_mfa_recovery_user FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: identity_mfa_tickets fk_identity_mfa_ticket_user; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_mfa_tickets
    ADD CONSTRAINT fk_identity_mfa_ticket_user FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: identity_mfa_totp_methods fk_identity_mfa_totp_user; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_mfa_totp_methods
    ADD CONSTRAINT fk_identity_mfa_totp_user FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: identity_account_recovery_cases fk_identity_recovery_case_account; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_account_recovery_cases
    ADD CONSTRAINT fk_identity_recovery_case_account FOREIGN KEY (account_id) REFERENCES public.accounts(id) ON DELETE RESTRICT;


--
-- Name: identity_account_recovery_cases fk_identity_recovery_case_challenge; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_account_recovery_cases
    ADD CONSTRAINT fk_identity_recovery_case_challenge FOREIGN KEY (challenge_id) REFERENCES public.identity_email_challenges(id) ON DELETE RESTRICT;


--
-- Name: identity_account_recovery_cases fk_identity_recovery_case_created_by; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_account_recovery_cases
    ADD CONSTRAINT fk_identity_recovery_case_created_by FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: identity_account_recovery_cases fk_identity_recovery_case_user; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_account_recovery_cases
    ADD CONSTRAINT fk_identity_recovery_case_user FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: likes fk_likes_user; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.likes
    ADD CONSTRAINT fk_likes_user FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: mutual_aid_details fk_mutual_aid_details_created_by; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.mutual_aid_details
    ADD CONSTRAINT fk_mutual_aid_details_created_by FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: mutual_aid_details fk_mutual_aid_details_thread; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.mutual_aid_details
    ADD CONSTRAINT fk_mutual_aid_details_thread FOREIGN KEY (thread_id) REFERENCES public.threads(id) ON DELETE RESTRICT;


--
-- Name: notifications fk_notifications_user; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.notifications
    ADD CONSTRAINT fk_notifications_user FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: outbox_consumer_receipts fk_outbox_consumer_receipt_event; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.outbox_consumer_receipts
    ADD CONSTRAINT fk_outbox_consumer_receipt_event FOREIGN KEY (event_id) REFERENCES public.platform_outbox(id) ON DELETE CASCADE;


--
-- Name: personal_documents fk_personal_documents_current_version; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.personal_documents
    ADD CONSTRAINT fk_personal_documents_current_version FOREIGN KEY (current_version_id) REFERENCES public.personal_document_versions(id) ON DELETE RESTRICT;


--
-- Name: platform_command_audits fk_platform_command_audit_event; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.platform_command_audits
    ADD CONSTRAINT fk_platform_command_audit_event FOREIGN KEY (event_id) REFERENCES public.platform_outbox(id) ON DELETE SET NULL;


--
-- Name: platform_outbox_attempts fk_platform_outbox_attempt_event; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.platform_outbox_attempts
    ADD CONSTRAINT fk_platform_outbox_attempt_event FOREIGN KEY (event_id) REFERENCES public.platform_outbox(id) ON DELETE CASCADE;


--
-- Name: posts fk_posts_author; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.posts
    ADD CONSTRAINT fk_posts_author FOREIGN KEY (author_id) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: posts fk_posts_parent; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.posts
    ADD CONSTRAINT fk_posts_parent FOREIGN KEY (parent_id) REFERENCES public.posts(id) ON DELETE RESTRICT;


--
-- Name: posts fk_posts_thread; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.posts
    ADD CONSTRAINT fk_posts_thread FOREIGN KEY (thread_id) REFERENCES public.threads(id) ON DELETE RESTRICT;


--
-- Name: richtext_article_assets fk_richtext_assets_content; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.richtext_article_assets
    ADD CONSTRAINT fk_richtext_assets_content FOREIGN KEY (article_content_id) REFERENCES public.richtext_article_contents(id) ON DELETE RESTRICT;


--
-- Name: richtext_article_assets fk_richtext_assets_thread; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.richtext_article_assets
    ADD CONSTRAINT fk_richtext_assets_thread FOREIGN KEY (thread_id) REFERENCES public.threads(id) ON DELETE RESTRICT;


--
-- Name: richtext_article_assets fk_richtext_assets_uploader; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.richtext_article_assets
    ADD CONSTRAINT fk_richtext_assets_uploader FOREIGN KEY (uploader_id) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: richtext_article_contents fk_richtext_contents_created_by; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.richtext_article_contents
    ADD CONSTRAINT fk_richtext_contents_created_by FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: richtext_article_contents fk_richtext_contents_thread; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.richtext_article_contents
    ADD CONSTRAINT fk_richtext_contents_thread FOREIGN KEY (thread_id) REFERENCES public.threads(id) ON DELETE RESTRICT;


--
-- Name: richtext_article_contents fk_richtext_contents_updated_by; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.richtext_article_contents
    ADD CONSTRAINT fk_richtext_contents_updated_by FOREIGN KEY (updated_by) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: route_permission_bindings fk_route_permission_definition; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.route_permission_bindings
    ADD CONSTRAINT fk_route_permission_definition FOREIGN KEY (permission_id) REFERENCES public.permission_definitions(id) ON DELETE RESTRICT;


--
-- Name: route_permission_bindings fk_route_permission_operation; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.route_permission_bindings
    ADD CONSTRAINT fk_route_permission_operation FOREIGN KEY (route_operation_id) REFERENCES public.route_operations(id) ON DELETE CASCADE;


--
-- Name: secondhand_details fk_secondhand_details_created_by; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.secondhand_details
    ADD CONSTRAINT fk_secondhand_details_created_by FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: secondhand_details fk_secondhand_details_thread; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.secondhand_details
    ADD CONSTRAINT fk_secondhand_details_thread FOREIGN KEY (thread_id) REFERENCES public.threads(id) ON DELETE RESTRICT;


--
-- Name: sessions fk_sessions_user; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sessions
    ADD CONSTRAINT fk_sessions_user FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: threads fk_threads_author; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.threads
    ADD CONSTRAINT fk_threads_author FOREIGN KEY (author_id) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: threads fk_threads_category; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.threads
    ADD CONSTRAINT fk_threads_category FOREIGN KEY (category_id) REFERENCES public.categories(id) ON DELETE RESTRICT;


--
-- Name: user_roles fk_user_roles_role; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_roles
    ADD CONSTRAINT fk_user_roles_role FOREIGN KEY (role_id) REFERENCES public.roles(id) ON DELETE RESTRICT;


--
-- Name: user_roles fk_user_roles_user; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_roles
    ADD CONSTRAINT fk_user_roles_user FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: user_space_contents fk_user_space_contents_category; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_space_contents
    ADD CONSTRAINT fk_user_space_contents_category FOREIGN KEY (category_id) REFERENCES public.categories(id) ON DELETE RESTRICT;


--
-- Name: user_space_contents fk_user_space_contents_thread; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_space_contents
    ADD CONSTRAINT fk_user_space_contents_thread FOREIGN KEY (thread_id) REFERENCES public.threads(id) ON DELETE RESTRICT;


--
-- Name: user_space_contents fk_user_space_contents_user; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_space_contents
    ADD CONSTRAINT fk_user_space_contents_user FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: user_spaces fk_user_spaces_user; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_spaces
    ADD CONSTRAINT fk_user_spaces_user FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: role_permissions fk_v10_role_permissions_permission; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.role_permissions
    ADD CONSTRAINT fk_v10_role_permissions_permission FOREIGN KEY (permission_id) REFERENCES public.permission_definitions(id) ON DELETE RESTRICT;


--
-- Name: role_permissions fk_v10_role_permissions_role; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.role_permissions
    ADD CONSTRAINT fk_v10_role_permissions_role FOREIGN KEY (role_id) REFERENCES public.roles(id) ON DELETE RESTRICT;


--
-- Name: webhook_deliveries fk_webhook_deliveries_endpoint; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.webhook_deliveries
    ADD CONSTRAINT fk_webhook_deliveries_endpoint FOREIGN KEY (endpoint_id) REFERENCES public.webhook_endpoints(id) ON DELETE RESTRICT;


--
-- Name: webhook_deliveries fk_webhook_deliveries_outbox; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.webhook_deliveries
    ADD CONSTRAINT fk_webhook_deliveries_outbox FOREIGN KEY (outbox_event_id) REFERENCES public.platform_outbox(id) ON DELETE SET NULL;


--
-- Name: personal_document_previews personal_document_previews_document_version_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.personal_document_previews
    ADD CONSTRAINT personal_document_previews_document_version_id_fkey FOREIGN KEY (document_version_id) REFERENCES public.personal_document_versions(id) ON DELETE RESTRICT;


--
-- Name: personal_document_previews personal_document_previews_preview_object_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.personal_document_previews
    ADD CONSTRAINT personal_document_previews_preview_object_id_fkey FOREIGN KEY (preview_object_id) REFERENCES public.storage_objects(id) ON DELETE RESTRICT;


--
-- Name: personal_document_versions personal_document_versions_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.personal_document_versions
    ADD CONSTRAINT personal_document_versions_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: personal_document_versions personal_document_versions_document_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.personal_document_versions
    ADD CONSTRAINT personal_document_versions_document_id_fkey FOREIGN KEY (document_id) REFERENCES public.personal_documents(id) ON DELETE RESTRICT;


--
-- Name: personal_document_versions personal_document_versions_restored_from_version_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.personal_document_versions
    ADD CONSTRAINT personal_document_versions_restored_from_version_id_fkey FOREIGN KEY (restored_from_version_id) REFERENCES public.personal_document_versions(id) ON DELETE RESTRICT;


--
-- Name: personal_document_versions personal_document_versions_source_object_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.personal_document_versions
    ADD CONSTRAINT personal_document_versions_source_object_id_fkey FOREIGN KEY (source_object_id) REFERENCES public.storage_objects(id) ON DELETE RESTRICT;


--
-- Name: personal_documents personal_documents_owner_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.personal_documents
    ADD CONSTRAINT personal_documents_owner_user_id_fkey FOREIGN KEY (owner_user_id) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: storage_objects storage_objects_owner_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storage_objects
    ADD CONSTRAINT storage_objects_owner_user_id_fkey FOREIGN KEY (owner_user_id) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: user_schedule_preferences user_schedule_preferences_academic_term_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_schedule_preferences
    ADD CONSTRAINT user_schedule_preferences_academic_term_id_fkey FOREIGN KEY (academic_term_id) REFERENCES public.academic_terms(id) ON DELETE RESTRICT;


--
-- Name: user_schedule_preferences user_schedule_preferences_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_schedule_preferences
    ADD CONSTRAINT user_schedule_preferences_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: user_schedule_terms user_schedule_terms_academic_term_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_schedule_terms
    ADD CONSTRAINT user_schedule_terms_academic_term_id_fkey FOREIGN KEY (academic_term_id) REFERENCES public.academic_terms(id) ON DELETE RESTRICT;


--
-- Name: user_schedule_terms user_schedule_terms_current_object_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_schedule_terms
    ADD CONSTRAINT user_schedule_terms_current_object_id_fkey FOREIGN KEY (current_object_id) REFERENCES public.storage_objects(id) ON DELETE RESTRICT;


--
-- Name: user_schedule_terms user_schedule_terms_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_schedule_terms
    ADD CONSTRAINT user_schedule_terms_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: user_storage_accounts user_storage_accounts_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_storage_accounts
    ADD CONSTRAINT user_storage_accounts_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: user_storage_quotas user_storage_quotas_updated_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_storage_quotas
    ADD CONSTRAINT user_storage_quotas_updated_by_fkey FOREIGN KEY (updated_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: user_storage_quotas user_storage_quotas_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_storage_quotas
    ADD CONSTRAINT user_storage_quotas_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: user_storage_reservations user_storage_reservations_object_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_storage_reservations
    ADD CONSTRAINT user_storage_reservations_object_id_fkey FOREIGN KEY (object_id) REFERENCES public.storage_objects(id) ON DELETE CASCADE;


--
-- Name: user_storage_reservations user_storage_reservations_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_storage_reservations
    ADD CONSTRAINT user_storage_reservations_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


-- Foreign-key maintenance indexes. PostgreSQL does not create indexes on the
-- referencing side automatically; every remaining FK is covered by a leading
-- index so parent update/delete checks never require a full child-table scan.
CREATE INDEX idx_academic_terms_created_by ON public.academic_terms (created_by);
CREATE INDEX idx_academic_terms_updated_by ON public.academic_terms (updated_by);
CREATE INDEX idx_configurations_updated_by ON public.configurations (updated_by);
CREATE INDEX idx_identity_recovery_account ON public.identity_account_recovery_cases (account_id);
CREATE INDEX idx_identity_recovery_created_by ON public.identity_account_recovery_cases (created_by);
CREATE INDEX idx_identity_admin_status_changed_by ON public.identity_admin_accounts (status_changed_by);
CREATE INDEX idx_identity_challenge_policy_updated_by ON public.identity_challenge_policies (updated_by);
CREATE INDEX idx_identity_email_challenges_account ON public.identity_email_challenges (account_id);
CREATE INDEX idx_identity_mfa_policy_updated_by ON public.identity_mfa_policies (updated_by);
CREATE INDEX idx_identity_mfa_recovery_method ON public.identity_mfa_recovery_codes (method_id);
CREATE INDEX idx_identity_mfa_tickets_user ON public.identity_mfa_tickets (user_id);
CREATE INDEX idx_personal_document_previews_preview_object ON public.personal_document_previews (preview_object_id);
CREATE INDEX idx_personal_document_versions_created_by ON public.personal_document_versions (created_by);
CREATE INDEX idx_personal_document_versions_restored_from ON public.personal_document_versions (restored_from_version_id);
CREATE INDEX idx_personal_document_versions_source_object ON public.personal_document_versions (source_object_id);
CREATE INDEX idx_personal_documents_current_version ON public.personal_documents (current_version_id);
CREATE INDEX idx_platform_command_audits_event ON public.platform_command_audits (event_id);
CREATE INDEX idx_richtext_article_contents_updated_by ON public.richtext_article_contents (updated_by);
CREATE INDEX idx_role_permissions_permission ON public.role_permissions (permission_id);
CREATE INDEX idx_route_permission_bindings_permission ON public.route_permission_bindings (permission_id);
CREATE INDEX idx_user_roles_role ON public.user_roles (role_id);
CREATE INDEX idx_user_storage_quotas_updated_by ON public.user_storage_quotas (updated_by);
CREATE INDEX idx_user_storage_reservations_user ON public.user_storage_reservations (user_id);
CREATE INDEX idx_webhook_deliveries_outbox ON public.webhook_deliveries (outbox_event_id);


--
-- PostgreSQL database dump complete
--
