-- Short-lived, server-side UI invocation contexts for first-party and future
-- governed plugin surfaces.  Values are opaque random identifiers; no token,
-- storage key, path, or byte payload is stored here.

CREATE TABLE public.plugin_ui_invocations (
    id bigint NOT NULL,
    user_id bigint NOT NULL,
    plugin_key character varying(120) NOT NULL,
    surface_id character varying(160) NOT NULL,
    article_content_id bigint NOT NULL,
    asset_id bigint,
    attachment_id bigint,
    presentation character varying(20) NOT NULL,
    purpose character varying(80) NOT NULL,
    context_digest character varying(128) DEFAULT ''::character varying NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    opened_at timestamp with time zone,
    revoked_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT plugin_ui_invocations_pkey PRIMARY KEY (id),
    CONSTRAINT chk_plugin_ui_invocations_presentation CHECK (((presentation)::text = ANY ((ARRAY['modal'::character varying, 'drawer'::character varying, 'fullscreen'::character varying, 'new-tab'::character varying])::text[]))),
    CONSTRAINT chk_plugin_ui_invocations_purpose CHECK ((length(btrim((purpose)::text)) > 0)),
    CONSTRAINT chk_plugin_ui_invocations_expiry CHECK ((expires_at > created_at))
);

ALTER TABLE ONLY public.plugin_ui_invocations
    ADD CONSTRAINT fk_plugin_ui_invocations_user FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.plugin_ui_invocations
    ADD CONSTRAINT fk_plugin_ui_invocations_article FOREIGN KEY (article_content_id) REFERENCES public.richtext_article_contents(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.plugin_ui_invocations
    ADD CONSTRAINT fk_plugin_ui_invocations_asset FOREIGN KEY (asset_id) REFERENCES public.user_assets(id) ON DELETE RESTRICT;
ALTER TABLE ONLY public.plugin_ui_invocations
    ADD CONSTRAINT fk_plugin_ui_invocations_attachment FOREIGN KEY (attachment_id) REFERENCES public.richtext_article_attachments(id) ON DELETE RESTRICT;

CREATE INDEX idx_plugin_ui_invocations_expiry ON public.plugin_ui_invocations USING btree (expires_at);
CREATE INDEX idx_plugin_ui_invocations_user_plugin_expiry ON public.plugin_ui_invocations USING btree (user_id, plugin_key, expires_at);
CREATE INDEX idx_plugin_ui_invocations_article ON public.plugin_ui_invocations USING btree (article_content_id);
CREATE INDEX idx_plugin_ui_invocations_asset ON public.plugin_ui_invocations USING btree (asset_id) WHERE asset_id IS NOT NULL;
CREATE INDEX idx_plugin_ui_invocations_attachment ON public.plugin_ui_invocations USING btree (attachment_id) WHERE attachment_id IS NOT NULL;
