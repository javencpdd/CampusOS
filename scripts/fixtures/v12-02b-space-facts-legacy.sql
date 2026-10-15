INSERT INTO users(id,username,nickname,email,status,auth_version) VALUES
 (229901,'space-facts-legacy-public','Legacy public owner','space-facts-public@example.test','active',1),
 (229902,'space-facts-legacy-private','Legacy private owner','space-facts-private@example.test','active',1);
INSERT INTO user_spaces(id,user_id,title,bio,avatar,cover_image,theme,layout,visibility,sync_enabled,sync_categories,sync_tags,
 style_name,style_version,style_manifest,disabled_at,disabled_by,disabled_reason,disabled_actor_kind,disabled_actor_id,last_sync_at,last_sync_error,created_at,updated_at)
VALUES
 (2299011,229901,'Legacy public space','preserve public bio','/owned-avatar','/cover','legacy','blog','public',true,ARRAY['1','2'],ARRAY['tag'],
 'legacy-style','1','{"tokens":{"color":"blue"}}',NULL,'','','legacy_unknown','',TIMESTAMPTZ '2026-01-01 00:00:00+00','',TIMESTAMPTZ '2025-01-01 00:00:00+00',TIMESTAMPTZ '2026-01-01 00:00:00+00'),
 (2299021,229902,'Legacy private space','preserve private bio','','','default','blog','private',false,ARRAY[]::text[],ARRAY[]::text[],
 'private-style','2','{"components":[]}',TIMESTAMPTZ '2026-02-01 00:00:00+00','','retained governance','admin','admin:space-facts',NULL,'retry later',TIMESTAMPTZ '2025-02-01 00:00:00+00',TIMESTAMPTZ '2026-02-01 00:00:00+00');
INSERT INTO user_space_style_snapshots(id,user_id,snapshot_type,style_name,style_version,theme,layout,style_manifest,created_at)
VALUES (2299018,'229901','before_apply','older','0.5','legacy','blog','{"tokens":{"color":"red"}}',TIMESTAMPTZ '2025-12-01 00:00:00+00'),
 (2299028,'229902','before_restore_default','private','1','dark','cards','{"components":[]}',TIMESTAMPTZ '2025-12-02 00:00:00+00');
