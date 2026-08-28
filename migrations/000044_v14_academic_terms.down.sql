-- Down is only for isolated migration drills. Production keeps term audit
-- evidence and uses a forward fix once user schedules reference these rows.

DELETE FROM role_permissions
WHERE created_by = 'v14-academic-term';

DELETE FROM permission_definitions
WHERE code IN (
    'schedule.academic_term.read', 'schedule.academic_term.manage', 'schedule.academic_term.delete'
);

DROP INDEX IF EXISTS idx_academic_terms_status_year;
DROP INDEX IF EXISTS uk_academic_terms_open_default;
DROP TABLE IF EXISTS academic_terms;
