-- Deployment runtime editing is an independent Admin-only capability.
-- No settings or secrets are copied into PostgreSQL.
INSERT INTO permission_definitions(id,code,domain,resource,action,description,risk_level,allowed_scope_types,audit_level) VALUES
 (900000000000003601,'platform.runtime_config.read','platform','runtime_config','read','读取受管运行配置（凭据脱敏）','medium','["global"]','standard'),
 (900000000000003602,'platform.runtime_config.manage','platform','runtime_config','manage','保存受管配置并重启 API','high','["global"]','required');
INSERT INTO admin_role_permissions(role_id,permission_code,created_by) VALUES
 ('security_admin','platform.runtime_config.read','system:migration-36'),
 ('security_admin','platform.runtime_config.manage','system:migration-36');
