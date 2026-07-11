-- Replace broad role:manage checks on unrelated admin domains with explicit permissions.
INSERT INTO permissions (id, role_id, resource, action) VALUES
    (36, 1, 'plugin', 'read'),
    (37, 1, 'plugin', 'configure'),
    (38, 1, 'plugin', 'lifecycle'),
    (39, 1, 'plugin', 'install'),
    (40, 1, 'plugin', 'uninstall'),
    (41, 1, 'richtext', 'moderate'),
    (42, 1, 'ai', 'read'),
    (43, 1, 'integration', 'read'),
    (44, 1, 'metrics', 'read'),
    (45, 1, 'space', 'manage'),
    (46, 1, 'webhook', 'read'),
    (47, 1, 'webhook', 'write'),
    (48, 1, 'webhook', 'execute'),
    (49, 1, 'mcp', 'read'),
    (50, 1, 'mcp', 'call'),
    (51, 1, 'mcp', 'configure'),
    (52, 1, 'message', 'read'),
    (53, 1, 'message', 'write'),
    (54, 1, 'platform_log', 'read'),
    (55, 1, 'homepage', 'configure')
ON CONFLICT DO NOTHING;
