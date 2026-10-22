package postgres

import "fmt"

// visibleScopeSQL reads the current membership and team policy in the same SQL
// statement as the list. A scope resolved before removal must not retain access.
func visibleScopeSQL(spaceArg, userArg string) string {
	return `visible_scope AS MATERIALIZED (SELECT s.id AS space_id,` + userArg + `::text AS user_id,s.kind,
 CASE WHEN s.kind='personal' THEN 'owner' ELSE coalesce(m.role,'') END AS role,
 CASE WHEN s.kind='personal' THEN 'isolated' ELSE t.policy END AS policy,
 CASE WHEN t.disabled THEN 'disabled' WHEN t.expires_at<=statement_timestamp() THEN 'expired' ELSE 'active' END AS status,
 (s.kind='team' AND t.expires_at<=statement_timestamp() AND coalesce(c.balance,0)=0) AS read_only
 FROM drama_catalog_v1.spaces s LEFT JOIN drama_catalog_v1.teams t ON t.space_id=s.id
 LEFT JOIN drama_catalog_v1.team_members m ON m.space_id=s.id AND m.user_id=` + userArg + `
 LEFT JOIN drama_catalog_v1.credit_accounts c ON c.account_id=s.id
 WHERE s.id=` + spaceArg + ` AND ` + userArg + `::text<>'' AND
 ((s.kind='personal' AND s.id=` + userArg + `) OR (s.kind='team' AND m.user_id IS NOT NULL)))`
}

// The expression is the SQL form of domain/spaces.Decide. Its inputs are only
// fixed query aliases/placeholders; callers must never interpolate user input.
// The same domain case table executes this exact expression in PostgreSQL.
func spaceAccessSQL(scope, space, owner, kind, sharePermission string) string {
	return fmt.Sprintf(`LEAST(CASE
 WHEN %[1]s.space_id='' OR %[1]s.user_id='' OR %[3]s='' OR %[2]s<>%[1]s.space_id THEN 0
 WHEN %[1]s.kind='personal' THEN CASE WHEN %[3]s=%[1]s.user_id THEN 2 ELSE 0 END
 WHEN %[1]s.kind<>'team' OR %[1]s.role NOT IN ('owner','admin','member') OR %[1]s.status='disabled' THEN 0
 WHEN %[1]s.role IN ('owner','admin') OR %[3]s=%[1]s.user_id THEN 2
 WHEN %[4]s='agent_session' THEN 0
 ELSE GREATEST(CASE %[1]s.policy WHEN 'read_all' THEN 1 WHEN 'edit_all' THEN 2 ELSE 0 END,
 CASE WHEN %[4]s IN ('project','canvas') THEN coalesce(%[5]s,0) ELSE 0 END)
 END,CASE WHEN %[1]s.read_only THEN 1 ELSE 2 END)`, scope, space, owner, kind, sharePermission)
}

func projectSharePermissionSQL(project, scope string) string {
	return `(SELECT max(sh.permission) FROM drama_catalog_v1.space_shares sh WHERE sh.space_id=` + project + `.space_id AND sh.resource_kind='project' AND sh.resource_id=` + project + `.id AND sh.grantee IN (` + scope + `.user_id,'*'))`
}
