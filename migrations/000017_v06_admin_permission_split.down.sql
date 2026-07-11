UPDATE permissions
SET deleted_at = NOW()
WHERE role_id = 1
  AND id BETWEEN 36 AND 55
  AND deleted_at IS NULL;
