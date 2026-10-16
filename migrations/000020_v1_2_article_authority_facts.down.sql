-- All article content, metadata and typed actor history remain intact.
DROP TRIGGER trg_article_facts_version ON public.richtext_article_contents;
DROP FUNCTION public.advance_article_facts_version();
ALTER SEQUENCE public.article_facts_version_seq OWNED BY NONE;
ALTER TABLE public.richtext_article_contents
    DROP CONSTRAINT chk_richtext_article_facts_version,
    DROP COLUMN facts_version;
