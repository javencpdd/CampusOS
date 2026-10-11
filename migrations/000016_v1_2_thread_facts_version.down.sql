-- Rollback preserves thread bodies, state and existing content revisions.
-- Public policy readers must be stopped or reverted before removing their
-- required version column. The allocation sequence deliberately survives:
-- down/up backfills must never reissue a previously observed facts version.
DROP TRIGGER IF EXISTS trg_threads_facts_version ON public.threads;
DROP FUNCTION IF EXISTS public.advance_thread_facts_version();
ALTER TABLE public.threads DROP CONSTRAINT IF EXISTS chk_threads_facts_version;
ALTER SEQUENCE IF EXISTS public.thread_facts_version_seq OWNED BY NONE;
ALTER TABLE public.threads DROP COLUMN IF EXISTS facts_version;
