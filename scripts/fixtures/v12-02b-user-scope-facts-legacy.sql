-- Owned four-table storage preservation fixture, including inactive history.
INSERT INTO users(id,username,nickname,email,status,auth_version) VALUES(151001,'scope-facts-legacy','Scope Legacy','scope-facts-legacy@example.test','active',1);
INSERT INTO categories(id,name,slug,node_kind,lifecycle_status) VALUES(151010,'Scope legacy group','scope-legacy-group','group','active');
INSERT INTO categories(id,name,slug,parent_id,node_kind,lifecycle_status) VALUES(151011,'Scope legacy board','scope-legacy-board',151010,'board','active');
INSERT INTO category_thread_type_policies(category_id,thread_type,enabled) VALUES(151011,'discussion',TRUE);
INSERT INTO roles(id,name,description,is_system,created_at,updated_at,deleted_at)
VALUES(151021,'scope_legacy_active','Original active role',FALSE,'2026-01-01T00:00:00Z','2026-01-02T00:00:00Z',NULL),
      (151022,'scope_legacy_deleted','Original deleted role',FALSE,'2026-01-01T00:00:00Z','2026-01-02T00:00:00Z','2026-01-03T00:00:00Z');
INSERT INTO permission_definitions(id,code,domain,resource,action,description,risk_level,allowed_scope_types,audit_level,deprecated_at)
VALUES(151031,'community.scopelegacy.pin','community','scopelegacy','pin','Original permission','medium','["global","category"]','required',NULL),
      (151032,'community.scopelegacy.old','community','scopelegacy','old','Deprecated permission','low','["global"]','standard','2026-01-03T00:00:00Z');
INSERT INTO role_permissions(id,role_id,permission_id,created_by,created_at,deleted_at)
VALUES(151041,151021,151031,'legacy-creator','2026-01-01T00:00:00Z',NULL),
      (151042,151022,151032,'legacy-creator','2026-01-01T00:00:00Z','2026-01-03T00:00:00Z');
INSERT INTO user_roles(id,user_id,role_id,scope_type,scope_id,created_at,deleted_at)
VALUES(151051,151001,151021,'category',151011,'2026-01-01T00:00:00Z',NULL),
      (151052,151001,151022,'global',NULL,'2026-01-01T00:00:00Z','2026-01-03T00:00:00Z');
