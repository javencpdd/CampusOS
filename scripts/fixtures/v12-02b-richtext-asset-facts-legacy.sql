-- Original lifecycle values and references must survive facts up/down/up.
INSERT INTO users(id,username,nickname,email,status,auth_version)
VALUES(156001,'asset-facts-legacy','Asset Legacy','asset-facts-legacy@example.test','active',1);
INSERT INTO categories(id,name,slug,node_kind,lifecycle_status)
VALUES(156010,'Asset legacy group','asset-legacy-group','group','active');
INSERT INTO categories(id,name,slug,parent_id,node_kind,lifecycle_status)
VALUES(156011,'Asset legacy board','asset-legacy-board',156010,'board','active');
INSERT INTO category_thread_type_policies(category_id,thread_type,enabled) VALUES(156011,'article',TRUE);
INSERT INTO threads(id,author_id,author_name,category_id,title,content,thread_type,publication_status,moderation_status,deletion_status,status)
VALUES(156101,156001,'Legacy',156011,'Asset original article','<p>Original</p>','article','private','clear','active','private');
INSERT INTO richtext_article_contents(id,thread_id,title,summary,content_html,content_json,sanitized_html,status,created_by,updated_by)
VALUES(156111,156101,'Asset original article','Original metadata','<p>Original</p>','{"legacy":9007199254740993}','<p>Original</p>','draft',156001,156001);
INSERT INTO storage_objects(id,owner_user_id,namespace,purpose,storage_key,original_name,mime_type,size_bytes,sha256,status,version,created_at,updated_at)
SELECT id,156001,'richtext','article_attachment','original/'||id::text,'original.pdf','application/pdf',123,'original-sha','ready',5,'2026-01-01T00:00:00Z','2026-01-02T00:00:00Z'
FROM (VALUES(156201),(156202),(156203),(156204)) x(id);
INSERT INTO user_assets(id,owner_user_id,kind,original_name,storage_object_id,mime_type,size_bytes,status,version,created_at,updated_at,trashed_at,deleted_at)
VALUES(156021,156001,'article_attachment','active original.pdf',156201,'application/pdf',123,'active',7,'2026-01-01T00:00:00Z','2026-01-02T00:00:00Z',NULL,NULL),
      (156022,156001,'article_attachment','trashed original.pdf',156202,'application/pdf',123,'trashed',9,'2026-01-01T00:00:00Z','2026-01-03T00:00:00Z','2026-01-03T00:00:00Z',NULL),
      (156023,156001,'article_attachment','quarantined original.pdf',156203,'application/pdf',123,'quarantined',11,'2026-01-01T00:00:00Z','2026-01-04T00:00:00Z',NULL,NULL),
      (156024,156001,'richtext_image','deleted original.png',156204,'image/png',123,'deleted',13,'2026-01-01T00:00:00Z','2026-01-05T00:00:00Z',NULL,'2026-01-05T00:00:00Z');
INSERT INTO richtext_article_attachments(id,article_content_id,asset_id,display_name,display_order,created_at,updated_at)
VALUES(156131,156111,156021,'First original.pdf',0,'2026-01-01T00:00:00Z','2026-01-02T00:00:00Z'),
      (156132,156111,156022,'Second original.pdf',1,'2026-01-03T00:00:00Z','2026-01-04T00:00:00Z');
