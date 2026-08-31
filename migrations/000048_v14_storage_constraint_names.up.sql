-- v0.14：兼容早期开发库中由 PostgreSQL 自动命名的对象账本约束。
-- 新空库的 000045 已直接使用目标名称；本 migration 只为已应用旧草案的开发库做无损前向统一。
DO $$
BEGIN
    IF to_regclass('public.user_storage_accounts') IS NOT NULL THEN
        IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname='user_storage_accounts_used_bytes_check') AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='chk_user_storage_accounts_used') THEN EXECUTE 'ALTER TABLE user_storage_accounts RENAME CONSTRAINT user_storage_accounts_used_bytes_check TO chk_user_storage_accounts_used'; END IF;
        IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname='user_storage_accounts_reserved_bytes_check') AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='chk_user_storage_accounts_reserved') THEN EXECUTE 'ALTER TABLE user_storage_accounts RENAME CONSTRAINT user_storage_accounts_reserved_bytes_check TO chk_user_storage_accounts_reserved'; END IF;
        IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname='user_storage_accounts_version_check') AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='chk_user_storage_accounts_version') THEN EXECUTE 'ALTER TABLE user_storage_accounts RENAME CONSTRAINT user_storage_accounts_version_check TO chk_user_storage_accounts_version'; END IF;
    END IF;
    IF to_regclass('public.storage_objects') IS NOT NULL THEN
        IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname='storage_objects_provider_check') AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='chk_storage_objects_provider') THEN EXECUTE 'ALTER TABLE storage_objects RENAME CONSTRAINT storage_objects_provider_check TO chk_storage_objects_provider'; END IF;
        IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname='storage_objects_size_bytes_check') AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='chk_storage_objects_size') THEN EXECUTE 'ALTER TABLE storage_objects RENAME CONSTRAINT storage_objects_size_bytes_check TO chk_storage_objects_size'; END IF;
        IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname='storage_objects_status_check') AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='chk_storage_objects_status') THEN EXECUTE 'ALTER TABLE storage_objects RENAME CONSTRAINT storage_objects_status_check TO chk_storage_objects_status'; END IF;
        IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname='storage_objects_version_check') AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='chk_storage_objects_version') THEN EXECUTE 'ALTER TABLE storage_objects RENAME CONSTRAINT storage_objects_version_check TO chk_storage_objects_version'; END IF;
    END IF;
    IF to_regclass('public.user_storage_reservations') IS NOT NULL THEN
        IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname='user_storage_reservations_reserved_bytes_check') AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='chk_user_storage_reservations_bytes') THEN EXECUTE 'ALTER TABLE user_storage_reservations RENAME CONSTRAINT user_storage_reservations_reserved_bytes_check TO chk_user_storage_reservations_bytes'; END IF;
        IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname='user_storage_reservations_status_check') AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='chk_user_storage_reservations_status') THEN EXECUTE 'ALTER TABLE user_storage_reservations RENAME CONSTRAINT user_storage_reservations_status_check TO chk_user_storage_reservations_status'; END IF;
    END IF;
END $$;
