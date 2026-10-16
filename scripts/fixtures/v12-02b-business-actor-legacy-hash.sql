SELECT jsonb_build_object(
 'threads',(SELECT jsonb_agg(to_jsonb(t)-'moderation_actor_kind'-'moderation_actor_id' ORDER BY id) FROM threads t WHERE id=119201),
 'revisions',(SELECT jsonb_agg(to_jsonb(t)-'created_actor_kind'-'created_actor_id' ORDER BY id) FROM content_revisions t WHERE thread_id=119201),
 'cases',(SELECT jsonb_agg(to_jsonb(t)-'opened_actor_kind'-'opened_actor_id'-'resolved_actor_kind'-'resolved_actor_id' ORDER BY id) FROM content_moderation_cases t WHERE thread_id=119201),
 'actions',(SELECT jsonb_agg(to_jsonb(t)-'actor_kind'-'actor_ref_id' ORDER BY id) FROM content_moderation_actions t WHERE thread_id=119201),
 'richtext',(SELECT jsonb_agg(to_jsonb(t)-'updated_actor_kind'-'updated_actor_id'-'facts_version' ORDER BY id) FROM richtext_article_contents t WHERE thread_id=119201),
 'assets',(SELECT jsonb_agg(to_jsonb(t)-'actor_kind'-'actor_ref_id' ORDER BY id) FROM asset_lifecycle_audits t WHERE id IN(119701,119702)),
 'spaces',(SELECT jsonb_agg(to_jsonb(t)-'disabled_actor_kind'-'disabled_actor_id' ORDER BY id) FROM user_spaces t WHERE user_id=119101),
 'quotas',(SELECT jsonb_agg(to_jsonb(t)-'updated_actor_kind'-'updated_actor_id' ORDER BY user_id) FROM user_storage_quotas t WHERE user_id=119101)
)::text;
