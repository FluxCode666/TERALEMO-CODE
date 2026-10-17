package repository

import (
	"context"
	"slices"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

func (r *enterpriseRepository) GetMemberContextByUserID(ctx context.Context, userID int64) (*service.EnterpriseMemberContext, error) {
	items, err := r.GetMemberContextsByUserIDs(ctx, []int64{userID})
	if err != nil {
		return nil, err
	}
	return items[userID], nil
}

// GetMemberContextsByUserIDs performs three queries regardless of batch size:
// memberships, pools, then the applicable whitelist scopes. Disabled records
// remain in the snapshot so authentication can explicitly deny their requests.
func (r *enterpriseRepository) GetMemberContextsByUserIDs(ctx context.Context, userIDs []int64) (map[int64]*service.EnterpriseMemberContext, error) {
	out := make(map[int64]*service.EnterpriseMemberContext)
	if len(userIDs) == 0 {
		return out, nil
	}
	client := clientFromContext(ctx, r.client)
	rows, err := client.QueryContext(ctx, `SELECT m.user_id, e.id, e.name, e.status, m.id, m.status, m.role, m.department_id
 FROM enterprise_members m JOIN enterprises e ON e.id = m.enterprise_id AND e.deleted_at IS NULL
 WHERE m.user_id = ANY($1::bigint[]) AND m.deleted_at IS NULL ORDER BY m.user_id`, pq.Array(userIDs))
	if err != nil {
		return nil, translatePersistenceError(err, nil, nil)
	}
	enterpriseSet := map[int64]struct{}{}
	enterpriseIDs, departmentIDs, memberIDs := []int64{}, []int64{}, []int64{}
	for rows.Next() {
		c := &service.EnterpriseMemberContext{GroupPoolIDs: []int64{}, GroupWhitelistIDs: []int64{}}
		var userID int64
		if err := rows.Scan(&userID, &c.EnterpriseID, &c.EnterpriseName, &c.EnterpriseStatus, &c.MemberID, &c.MemberStatus, &c.Role, &c.DepartmentID); err != nil {
			_ = rows.Close()
			return nil, err
		}
		out[userID] = c
		if _, ok := enterpriseSet[c.EnterpriseID]; !ok {
			enterpriseSet[c.EnterpriseID] = struct{}{}
			enterpriseIDs = append(enterpriseIDs, c.EnterpriseID)
		}
		if c.DepartmentID != nil {
			departmentIDs = append(departmentIDs, *c.DepartmentID)
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
	rows, err = client.QueryContext(ctx, `SELECT a.enterprise_id, a.group_id FROM enterprise_allowed_groups a
 JOIN groups g ON g.id = a.group_id AND g.deleted_at IS NULL WHERE a.enterprise_id = ANY($1::bigint[]) ORDER BY a.enterprise_id, a.group_id`, pq.Array(enterpriseIDs))
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
	rows, err = client.QueryContext(ctx, `SELECT w.enterprise_id, w.scope_type, w.scope_id, w.group_id FROM enterprise_group_whitelists w
 JOIN groups g ON g.id = w.group_id AND g.deleted_at IS NULL
 WHERE w.enterprise_id = ANY($1::bigint[]) AND ((w.scope_type = 'enterprise' AND w.scope_id = ANY($1::bigint[]))
 OR (w.scope_type = 'department' AND w.scope_id = ANY($2::bigint[])) OR (w.scope_type = 'member' AND w.scope_id = ANY($3::bigint[])))
 ORDER BY w.enterprise_id, w.scope_type, w.scope_id, w.group_id`, pq.Array(enterpriseIDs), pq.Array(departmentIDs), pq.Array(memberIDs))
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
		var groupID int64
		if err := rows.Scan(&key.enterpriseID, &key.scopeType, &key.scopeID, &groupID); err != nil {
			_ = rows.Close()
			return nil, err
		}
		whitelists[key] = append(whitelists[key], groupID)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, translatePersistenceError(err, nil, nil)
	}
	for _, c := range out {
		if ids := pools[c.EnterpriseID]; len(ids) > 0 {
			c.GroupPoolConfigured = true
			c.GroupPoolIDs = slices.Clone(ids)
		}
		keys := []scopeKey{{c.EnterpriseID, service.EnterpriseScopeEnterprise, c.EnterpriseID}, {c.EnterpriseID, service.EnterpriseScopeMember, c.MemberID}}
		if c.DepartmentID != nil {
			keys = append(keys, scopeKey{c.EnterpriseID, service.EnterpriseScopeDepartment, *c.DepartmentID})
		}
		for _, key := range keys {
			ids := whitelists[key]
			if len(ids) == 0 {
				continue
			}
			if !c.GroupWhitelistConfigured {
				c.GroupWhitelistConfigured = true
				c.GroupWhitelistIDs = slices.Clone(ids)
				continue
			}
			c.GroupWhitelistIDs = enterpriseIntersectSortedGroupIDs(c.GroupWhitelistIDs, ids)
		}
	}
	return out, nil
}

func enterpriseIntersectSortedGroupIDs(left, right []int64) []int64 {
	out := make([]int64, 0)
	for i, j := 0, 0; i < len(left) && j < len(right); {
		switch {
		case left[i] < right[j]:
			i++
		case left[i] > right[j]:
			j++
		default:
			out = append(out, left[i])
			i++
			j++
		}
	}
	return out
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
