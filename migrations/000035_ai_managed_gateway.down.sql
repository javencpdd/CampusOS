LOCK TABLE ai_model_profiles,ai_model_secrets,ai_model_bindings,ai_model_budgets,ai_model_runs,ai_model_run_events IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM ai_model_profiles) OR EXISTS(SELECT 1 FROM ai_model_secrets) OR EXISTS(SELECT 1 FROM ai_model_bindings) OR EXISTS(SELECT 1 FROM ai_model_budgets) OR EXISTS(SELECT 1 FROM ai_model_runs) OR EXISTS(SELECT 1 FROM ai_model_run_events) THEN
  RAISE EXCEPTION 'managed AI data exists; rollback must not discard secrets, budgets or event history';
 END IF;
 IF EXISTS(SELECT 1 FROM admin_role_permissions WHERE permission_code='ai.ai.manage' AND (role_id<>'security_admin' OR created_by<>'system:migration-35')) OR EXISTS(SELECT 1 FROM role_permissions r JOIN permission_definitions p ON p.id=r.permission_id WHERE p.code='ai.ai.manage') THEN
  RAISE EXCEPTION 'managed AI permission has non-seed references';
 END IF;
END $$;
DELETE FROM admin_role_permissions WHERE permission_code='ai.ai.manage';
DELETE FROM permission_definitions WHERE code='ai.ai.manage';
DROP TABLE ai_model_run_events,ai_model_runs,ai_model_budgets,ai_model_bindings,ai_model_secrets,ai_model_profiles;
DROP FUNCTION protect_ai_run_event();
ALTER SEQUENCE ai_gateway_epoch_seq OWNED BY NONE;
