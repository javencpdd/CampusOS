-- Owned migration fixture only; never sourced by production migrations.
INSERT INTO users(id,username,nickname,email,status,auth_version) VALUES(119101,'actor-legacy-user','Actor Legacy','actor-legacy@example.test','active',1);
INSERT INTO categories(id,name,slug,node_kind,lifecycle_status) VALUES(119110,'Actor group','actor-group','group','active');
INSERT INTO categories(id,name,slug,parent_id,node_kind,lifecycle_status) VALUES(119111,'Actor board','actor-board',119110,'board','active');
INSERT INTO category_thread_type_policies(category_id,thread_type,enabled) VALUES(119111,'article',TRUE);
INSERT INTO threads(id,title,content,author_id,category_id,thread_type,current_revision,moderation_by) VALUES(119201,'Actor fixture','Original history',119101,119111,'article',1,119101);
INSERT INTO content_revisions(id,thread_id,version,title,content,content_format,action,created_by) VALUES(119301,119201,1,'Actor fixture','Original history','markdown','create',119101),(119302,119201,2,'Nullable actor','Original null','markdown','legacy',NULL);
INSERT INTO content_moderation_cases(id,thread_id,status,reason,opened_by,resolved_by,resolved_at) VALUES(119401,119201,'resolved_approve','Original history',119101,119101,NOW());
INSERT INTO content_moderation_actions(id,case_id,thread_id,action,reason,actor_id) VALUES(119501,119401,119201,'approve','Original history',119101);
INSERT INTO richtext_article_contents(id,thread_id,title,created_by,updated_by) VALUES(119601,119201,'Actor article',119101,119101);
INSERT INTO asset_lifecycle_audits(id,actor_user_id,actor_type,action,reason) VALUES(119701,119101,'admin','quarantined','Legacy User FK actor with old admin label'),(119702,NULL,'system','purged','Legacy system without a credential');
INSERT INTO user_spaces(id,user_id,disabled_by,disabled_reason) VALUES(119801,119101,'opaque.old-admin','Legacy text domain is unknown');
INSERT INTO user_storage_quotas(user_id,quota_bytes,updated_by) VALUES(119101,1073741824,119101);
