-- 修复早期种子数据中 admin@campusos.local 的错误 bcrypt 哈希。
-- 正确默认密码：Admin@123456

UPDATE accounts
SET credential = '$2a$10$fL4UMGXtNMprJEykAqvor.TJWB4MXECJUbrHs6dIFHW6TC8P2vhXS',
    verified = TRUE,
    updated_at = NOW()
WHERE type = 'email'
  AND identifier = 'admin@campusos.local'
  AND credential = '$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy'
  AND deleted_at IS NULL;
