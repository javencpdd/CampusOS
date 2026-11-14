-- Refuse to erase operator-created grants. Runtime files are always retained.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM admin_role_permissions WHERE permission_code IN ('platform.runtime_config.read','platform.runtime_config.manage') AND (role_id<>'security_admin' OR created_by<>'system:migration-36'))
 OR EXISTS(SELECT 1 FROM role_permissions r JOIN permission_definitions p ON p.id=r.permission_id WHERE p.code IN ('platform.runtime_config.read','platform.runtime_config.manage')) THEN
  RAISE EXCEPTION 'runtime configuration permission has non-seed references';
 END IF;
END $$;
DELETE FROM admin_role_permissions WHERE permission_code IN ('platform.runtime_config.read','platform.runtime_config.manage');
DELETE FROM permission_definitions WHERE code IN ('platform.runtime_config.read','platform.runtime_config.manage');
