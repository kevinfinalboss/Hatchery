-- Per-server permissions of org members with role 'member' (owner/admin have everything).
-- gameserver '*' means every server of the org, including ones created later.
CREATE TABLE member_permissions (
    org_id     BIGINT NOT NULL,
    user_id    BIGINT NOT NULL,
    gameserver TEXT   NOT NULL,
    permission TEXT   NOT NULL,
    PRIMARY KEY (org_id, user_id, gameserver, permission),
    FOREIGN KEY (org_id, user_id) REFERENCES memberships (org_id, user_id) ON DELETE CASCADE
);

-- Existing members keep exactly what the 'member' role gave them before this table existed.
INSERT INTO member_permissions (org_id, user_id, gameserver, permission)
SELECT m.org_id, m.user_id, '*', p.permission
FROM memberships m
CROSS JOIN (
    SELECT 'console.read' AS permission
    UNION ALL SELECT 'console.write'
    UNION ALL SELECT 'power'
    UNION ALL SELECT 'files.read'
    UNION ALL SELECT 'files.write'
    UNION ALL SELECT 'backups.read'
) p
WHERE m.role = 'member';
