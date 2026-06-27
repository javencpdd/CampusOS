-- 回滚种子数据：删除默认管理员
DO $$
BEGIN
    IF to_regclass('public.user_roles') IS NOT NULL THEN
        DELETE FROM user_roles WHERE user_id = 1000000000000000001;
    END IF;

    IF to_regclass('public.accounts') IS NOT NULL THEN
        DELETE FROM accounts WHERE user_id = 1000000000000000001;
    END IF;

    IF to_regclass('public.users') IS NOT NULL THEN
        DELETE FROM users WHERE id = 1000000000000000001;
    END IF;
END $$;
