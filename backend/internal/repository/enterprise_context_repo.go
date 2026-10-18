package repository

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *enterpriseRepository) GetMemberContextByUserID(ctx context.Context, userID int64) (*service.EnterpriseMemberContext, error) {
	items, err := r.GetMemberContextsByUserIDs(ctx, []int64{userID})
	if err != nil {
		return nil, err
	}
	return items[userID], nil
}

// GetMemberContextsByUserIDs performs four bounded queries regardless of batch size:
// memberships, pools, then the applicable whitelist scopes. Disabled records
// remain in the snapshot so authentication can explicitly deny their requests.
func (r *enterpriseRepository) GetMemberContextsByUserIDs(ctx context.Context, userIDs []int64) (map[int64]*service.EnterpriseMemberContext, error) {
	out := make(map[int64]*service.EnterpriseMemberContext)
	if len(userIDs) == 0 {
		return out, nil
	}
	client := clientFromContext(ctx, r.client)
	args := make([]any, 0, len(userIDs))
	userPlaceholders := enterpriseContextPlaceholders(userIDs, &args)
	rows, err := client.QueryContext(ctx, `SELECT m.user_id, e.id, e.name, e.status, m.id, m.status, m.role, m.department_id, e.group_pool_configured, COALESCE(d.path,'')
 FROM enterprise_members m JOIN enterprises e ON e.id = m.enterprise_id AND e.deleted_at IS NULL
 LEFT JOIN enterprise_departments d ON d.id=m.department_id AND d.enterprise_id=m.enterprise_id AND d.deleted_at IS NULL
 WHERE m.user_id IN (`+userPlaceholders+`) AND m.deleted_at IS NULL ORDER BY m.user_id`, args...)
	if err != nil {
		return nil, translatePersistenceError(err, nil, nil)
	}
	enterpriseSet := map[int64]struct{}{}
	enterpriseIDs, departmentIDs, memberIDs := []int64{}, []int64{}, []int64{}
	for rows.Next() {
		c := &service.EnterpriseMemberContext{GroupPoolIDs: []int64{}, GroupWhitelistIDs: []int64{}}
		var userID int64
		var path string
		if err := rows.Scan(&userID, &c.EnterpriseID, &c.EnterpriseName, &c.EnterpriseStatus, &c.MemberID, &c.MemberStatus, &c.Role, &c.DepartmentID, &c.GroupPoolConfigured, &path); err != nil {
			_ = rows.Close()
			return nil, err
		}
		out[userID] = c
		if _, ok := enterpriseSet[c.EnterpriseID]; !ok {
			enterpriseSet[c.EnterpriseID] = struct{}{}
			enterpriseIDs = append(enterpriseIDs, c.EnterpriseID)
		}
		if c.DepartmentID != nil {
			c.DepartmentPathIDs, err = service.ParseEnterpriseDepartmentPath(path)
			if err != nil {
				_ = rows.Close()
				return nil, err
			}
			c.DepartmentPathIDs = service.FrozenEnterpriseDepartmentIDs(c.DepartmentPathIDs, *c.DepartmentID)
			departmentIDs = append(departmentIDs, c.DepartmentPathIDs...)
		}
		memberIDs = append(memberIDs, c.MemberID)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, translatePersistenceError(err, nil, nil)
	}
	if len(out) == 0 {
		return out, nil
	}
	// Load current descendants once per enterprise batch. Scope IDs are never
	// derived from a client-supplied department or from current member placement.
	treeArgs := []any{}
	treePlaceholders := enterpriseContextPlaceholders(enterpriseIDs, &treeArgs)
	treeRows, err := client.QueryContext(ctx, `SELECT id, enterprise_id, path FROM enterprise_departments WHERE deleted_at IS NULL AND enterprise_id IN (`+treePlaceholders+`) ORDER BY enterprise_id,id`, treeArgs...)
	if err != nil {
		return nil, err
	}
	type treeNode struct {
		id, enterpriseID int64
		path             string
	}
	nodes := []treeNode{}
	for treeRows.Next() {
		var n treeNode
		if err := treeRows.Scan(&n.id, &n.enterpriseID, &n.path); err != nil {
			_ = treeRows.Close()
			return nil, err
		}
		if n.path == "" {
			n.path = fmt.Sprintf("/%d/", n.id)
		}
		nodes = append(nodes, n)
	}
	treeErr := treeRows.Err()
	_ = treeRows.Close()
	if treeErr != nil {
		return nil, treeErr
	}
	for _, c := range out {
		c.DepartmentSubtreeIDs = []int64{}
		if c.DepartmentID == nil {
			continue
		}
		prefix := service.EnterpriseDepartmentPath(c.DepartmentPathIDs)
		for _, n := range nodes {
			if n.enterpriseID == c.EnterpriseID && strings.HasPrefix(n.path, prefix) {
				c.DepartmentSubtreeIDs = append(c.DepartmentSubtreeIDs, n.id)
			}
		}
	}
	args = make([]any, 0, len(enterpriseIDs))
	enterprisePlaceholders := enterpriseContextPlaceholders(enterpriseIDs, &args)
	rows, err = client.QueryContext(ctx, `SELECT a.enterprise_id, a.group_id FROM enterprise_allowed_groups a
 JOIN groups g ON g.id = a.group_id AND g.deleted_at IS NULL WHERE a.enterprise_id IN (`+enterprisePlaceholders+`) ORDER BY a.enterprise_id, a.group_id`, args...)
	if err != nil {
		return nil, translatePersistenceError(err, nil, nil)
	}
	pools := make(map[int64][]int64)
	for rows.Next() {
		var enterpriseID, groupID int64
		if err := rows.Scan(&enterpriseID, &groupID); err != nil {
			_ = rows.Close()
			return nil, err
		}
		pools[enterpriseID] = append(pools[enterpriseID], groupID)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, translatePersistenceError(err, nil, nil)
	}
	departmentPlaceholders := enterpriseContextPlaceholders(departmentIDs, &args)
	memberPlaceholders := enterpriseContextPlaceholders(memberIDs, &args)
	rows, err = client.QueryContext(ctx, `SELECT s.enterprise_id, s.scope_type, s.scope_id, g.id FROM enterprise_group_whitelist_scopes s
 LEFT JOIN enterprise_group_whitelists w ON w.enterprise_id = s.enterprise_id AND w.scope_type = s.scope_type AND w.scope_id = s.scope_id
 LEFT JOIN groups g ON g.id = w.group_id AND g.deleted_at IS NULL
 WHERE s.enterprise_id IN (`+enterprisePlaceholders+`) AND ((s.scope_type = 'enterprise' AND s.scope_id IN (`+enterprisePlaceholders+`))
 OR (s.scope_type = 'department' AND s.scope_id IN (`+departmentPlaceholders+`)) OR (s.scope_type = 'member' AND s.scope_id IN (`+memberPlaceholders+`)))
 ORDER BY s.enterprise_id, s.scope_type, s.scope_id, w.group_id`, args...)
	if err != nil {
		return nil, translatePersistenceError(err, nil, nil)
	}
	type scopeKey struct {
		enterpriseID int64
		scopeType    string
		scopeID      int64
	}
	whitelists := make(map[scopeKey][]int64)
	for rows.Next() {
		var key scopeKey
		var groupID sql.NullInt64
		if err := rows.Scan(&key.enterpriseID, &key.scopeType, &key.scopeID, &groupID); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if _, ok := whitelists[key]; !ok {
			whitelists[key] = []int64{}
		}
		if groupID.Valid {
			whitelists[key] = append(whitelists[key], groupID.Int64)
		}
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, translatePersistenceError(err, nil, nil)
	}
	for _, c := range out {
		if ids := pools[c.EnterpriseID]; len(ids) > 0 {
			c.GroupPoolIDs = slices.Clone(ids)
		}
		keys := []scopeKey{{c.EnterpriseID, service.EnterpriseScopeEnterprise, c.EnterpriseID}, {c.EnterpriseID, service.EnterpriseScopeMember, c.MemberID}}
		for _, departmentID := range c.DepartmentPathIDs {
			keys = append(keys, scopeKey{c.EnterpriseID, service.EnterpriseScopeDepartment, departmentID})
		}
		for _, key := range keys {
			ids, configured := whitelists[key]
			if !configured {
				continue
			}
			c.ApplyGroupWhitelist(ids)
		}
	}
	return out, nil
}

// Parameterized IN keeps the same bounded query count and also works with the
// SQLite repository harness used for authentication projection tests.
func enterpriseContextPlaceholders(ids []int64, args *[]any) string {
	if len(ids) == 0 {
		return "NULL"
	}
	placeholders := make([]string, len(ids))
	for i, id := range ids {
		*args = append(*args, id)
		placeholders[i] = fmt.Sprintf("$%d", len(*args))
	}
	return strings.Join(placeholders, ",")
}

func (r *enterpriseRepository) ListMemberUserIDs(ctx context.Context, enterpriseID int64) ([]int64, error) {
	client := clientFromContext(ctx, r.client)
	if err := enterpriseRequireEnterprise(ctx, client, enterpriseID, false); err != nil {
		return nil, err
	}
	rows, err := client.QueryContext(ctx, `SELECT user_id FROM enterprise_members WHERE enterprise_id = $1 AND deleted_at IS NULL ORDER BY user_id`, enterpriseID)
	if err != nil {
		return nil, translatePersistenceError(err, nil, nil)
	}
	defer func() { _ = rows.Close() }()
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, translatePersistenceError(rows.Err(), nil, nil)
}
