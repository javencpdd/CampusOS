-- Owned disposable PostgreSQL only. Preserve legacy/User/independent Admin attribution.
INSERT INTO users(id,username,nickname,email,status,auth_version) VALUES
 (2549801,'quota-legacy-unknown','Quota legacy','quota-legacy-unknown@example.test','active',1),
 (2549802,'quota-legacy-user','Quota user','quota-legacy-user@example.test','active',1),
 (2549803,'quota-legacy-admin','Quota admin','quota-legacy-admin@example.test','suspended',1);
INSERT INTO admin_accounts(id,username,email,display_name)
 VALUES ('adm.quota.legacy','quota-legacy-admin','quota-admin@example.test','Quota administrator');
INSERT INTO user_storage_quotas(user_id,quota_bytes,updated_by,updated_actor_kind,updated_actor_id,created_at,updated_at) VALUES
 (2549801,1048576,NULL,'legacy_unknown','',TIMESTAMPTZ '2025-01-01 00:00:00+00',TIMESTAMPTZ '2026-01-01 00:00:00+00'),
 (2549802,2097152,2549802,'user','2549802',TIMESTAMPTZ '2025-02-01 00:00:00+00',TIMESTAMPTZ '2026-02-01 00:00:00+00'),
 (2549803,3145728,NULL,'admin','adm.quota.legacy',TIMESTAMPTZ '2025-03-01 00:00:00+00',TIMESTAMPTZ '2026-03-01 00:00:00+00');
