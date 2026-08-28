-- 隔离演练使用；生产有引用数据时请停止功能并 forward-fix，不要直接执行 down。
DROP INDEX IF EXISTS idx_user_schedule_terms_academic_term;
DROP TABLE IF EXISTS user_schedule_terms;
