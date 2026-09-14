-- CampusOS v1.1: owner-scoped assets and explicit rich-text attachment bindings.
-- Existing migrations are immutable.  This migration deliberately does not
-- backfill test-only legacy directory data; the v1.1 cutover is forward-only.

CREATE TABLE public.user_assets (
    id bigint NOT NULL,
    owner_user_id bigint NOT NULL,
    kind character varying(32) NOT NULL,
    original_name character varying(255) NOT NULL,
    storage_object_id bigint NOT NULL,
    mime_type character varying(160) NOT NULL,
    size_bytes bigint NOT NULL,
    status character varying(20) DEFAULT 'active'::character varying NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    trashed_at timestamp with time zone,
    deleted_at timestamp with time zone,
    CONSTRAINT user_assets_pkey PRIMARY KEY (id),
    CONSTRAINT uq_user_assets_storage_object UNIQUE (storage_object_id),
    CONSTRAINT chk_user_assets_kind CHECK (((kind)::text = ANY ((ARRAY['article_attachment'::character varying, 'richtext_image'::character varying])::text[]))),
    CONSTRAINT chk_user_assets_size CHECK ((size_bytes >= 0)),
    CONSTRAINT chk_user_assets_status CHECK (((status)::text = ANY ((ARRAY['active'::character varying, 'trashed'::character varying, 'quarantined'::character varying, 'deleted'::character varying])::text[]))),
    CONSTRAINT chk_user_assets_version CHECK ((version >= 1)),
    CONSTRAINT chk_user_assets_deleted_at CHECK (((((status)::text = 'deleted'::text) AND (deleted_at IS NOT NULL)) OR ((status)::text <> 'deleted'::text)))
);

ALTER TABLE ONLY public.user_assets
    ADD CONSTRAINT fk_user_assets_owner FOREIGN KEY (owner_user_id) REFERENCES public.users(id) ON DELETE RESTRICT;
ALTER TABLE ONLY public.user_assets
    ADD CONSTRAINT fk_user_assets_storage_object FOREIGN KEY (storage_object_id) REFERENCES public.storage_objects(id) ON DELETE RESTRICT;

CREATE INDEX idx_user_assets_owner_status_updated ON public.user_assets USING btree (owner_user_id, status, updated_at DESC, id DESC);
CREATE INDEX idx_user_assets_owner_kind_updated ON public.user_assets USING btree (owner_user_id, kind, updated_at DESC, id DESC);

CREATE TABLE public.richtext_article_attachments (
    id bigint NOT NULL,
    article_content_id bigint NOT NULL,
    asset_id bigint NOT NULL,
    display_name character varying(255) NOT NULL,
    display_order integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT richtext_article_attachments_pkey PRIMARY KEY (id),
    CONSTRAINT uq_richtext_article_attachment_asset UNIQUE (article_content_id, asset_id),
    CONSTRAINT uq_richtext_article_attachment_order UNIQUE (article_content_id, display_order),
    CONSTRAINT chk_richtext_article_attachment_name CHECK ((length(btrim((display_name)::text)) > 0)),
    CONSTRAINT chk_richtext_article_attachment_order CHECK ((display_order >= 0))
);

ALTER TABLE ONLY public.richtext_article_attachments
    ADD CONSTRAINT fk_richtext_article_attachments_article FOREIGN KEY (article_content_id) REFERENCES public.richtext_article_contents(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.richtext_article_attachments
    ADD CONSTRAINT fk_richtext_article_attachments_asset FOREIGN KEY (asset_id) REFERENCES public.user_assets(id) ON DELETE RESTRICT;

CREATE INDEX idx_richtext_article_attachments_article_order ON public.richtext_article_attachments USING btree (article_content_id, display_order, id);
CREATE INDEX idx_richtext_article_attachments_asset_article ON public.richtext_article_attachments USING btree (asset_id, article_content_id);
