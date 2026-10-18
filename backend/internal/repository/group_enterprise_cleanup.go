package repository

import (
	"context"
	dbent "github.com/Wei-Shaw/sub2api/ent"
)

func deleteEnterpriseGroupReferences(ctx context.Context, client *dbent.Client, groupID int64) error {
	for _, query := range []string{
		`DELETE FROM enterprise_allowed_groups WHERE group_id = $1`,
		`DELETE FROM enterprise_group_whitelists WHERE group_id = $1`,
	} {
		if _, err := client.ExecContext(ctx, query, groupID); err != nil {
			return translatePersistenceError(err, nil, nil)
		}
	}
	return nil
}

// GetEnterpriseGroupAffectedUserIDs must be called before deletion removes the
// references. Disabled members are included because their cached snapshots also
// need to reflect the new scope when they are re-enabled.
func (r *groupRepository) GetEnterpriseGroupAffectedUserIDs(ctx context.Context, groupID int64) ([]int64, error) {
	client := clientFromContext(ctx, r.client)
	rows, err := client.QueryContext(ctx, `SELECT DISTINCT m.user_id FROM enterprise_members m
 JOIN enterprises e ON e.id=m.enterprise_id AND e.deleted_at IS NULL
 WHERE m.deleted_at IS NULL AND m.enterprise_id IN (
 SELECT enterprise_id FROM enterprise_allowed_groups WHERE group_id=$1
 UNION SELECT enterprise_id FROM enterprise_group_whitelists WHERE group_id=$1)
 ORDER BY m.user_id`, groupID)
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
	return ids, rows.Err()
}
