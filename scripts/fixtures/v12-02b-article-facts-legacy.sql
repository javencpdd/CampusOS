INSERT INTO users(id,username,nickname,email,status,auth_version) VALUES(148001,'article-facts-legacy','Article Legacy','article-facts-legacy@example.test','active',1);
INSERT INTO categories(id,name,slug,node_kind,lifecycle_status) VALUES(148010,'Article facts legacy group','article-facts-legacy-group','group','active');
INSERT INTO categories(id,name,slug,parent_id,node_kind,lifecycle_status) VALUES(148011,'Article facts legacy board','article-facts-legacy-board',148010,'board','active');
INSERT INTO category_thread_type_policies(category_id,thread_type,enabled) VALUES(148011,'article',TRUE);
INSERT INTO threads(id,author_id,author_name,category_id,title,content,thread_type,publication_status,moderation_status,deletion_status,status)
VALUES(148101,148001,'Legacy',148011,'Live article','<p>Legacy original</p>','article','private','clear','active','private'),
      (148102,148001,'Legacy',148011,'Deleted article','<p>Deleted original</p>','article','draft','clear','trashed','archived');
INSERT INTO richtext_article_contents(id,thread_id,title,summary,cover_url,content_html,content_json,sanitized_html,status,created_by,updated_by,created_at,updated_at,deleted_at)
VALUES(148111,148101,'Live article','Legacy metadata','https://example.invalid/cover','<p>Legacy original</p>','{"value":9007199254740993,"legacy":true}','<p>Legacy original</p>','draft',148001,148001,'2026-01-01T00:00:00Z','2026-01-02T00:00:00Z',NULL),
      (148112,148102,'Deleted article','Deleted metadata','','<p>Deleted original</p>','{"deleted":true}','<p>Deleted original</p>','deleted',148001,148001,'2026-01-03T00:00:00Z','2026-01-04T00:00:00Z','2026-01-05T00:00:00Z');
UPDATE richtext_article_contents SET updated_by=NULL,updated_actor_kind='admin',updated_actor_id='adm.legacy.article' WHERE id=148111;
