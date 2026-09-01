-- Remove only immutable reference data introduced by 000003.
DELETE FROM role_permissions WHERE created_by = 'v1-baseline';
DELETE FROM permission_definitions WHERE id BETWEEN 900000000000000000 AND 900000000000009999;
DELETE FROM identity_challenge_policies WHERE id = 'email_verification';
DELETE FROM identity_mfa_policies WHERE id = 'admin';
DELETE FROM roles WHERE id IN (1, 2, 3, 4) AND is_system = TRUE;

