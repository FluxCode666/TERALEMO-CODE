package repository

import (
	"context"
	"slices"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type enterpriseGroupRepository struct{ client *dbent.Client }

func NewEnterpriseGroupRepository(client *dbent.Client) service.EnterpriseGroupRepository {
	return &enterpriseGroupRepository{client: client}
}

func enterpriseNormalizeGroupIDs(ids []int64) ([]int64, error) {
	ids = slices.Clone(ids)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	if len(ids) != 0 && ids[0] <= 0 {
		return nil, service.ErrEnterpriseGroupOutOfScope
	}
	return ids, nil
}

func enterpriseValidateGroupIDs(ctx context.Context, client *dbent.Client, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	var count int64
	err := enterpriseQueryOne(ctx, client, `SELECT COUNT(*) FROM groups WHERE id = ANY($1::bigint[]) AND deleted_at IS NULL`, []any{pq.Array(ids)}, func(s enterpriseScanner) error { return s.Scan(&count) })
	if err != nil {
		return translatePersistenceError(err, nil, nil)
	}
	if count != int64(len(ids)) {
		return service.ErrEnterpriseGroupOutOfScope
	}
	return nil
}

func (r *enterpriseGroupRepository) GetAllowedGroupIDs(ctx context.Context, enterpriseID int64) ([]int64, error) {
	client := clientFromContext(ctx, r.client)
	if err := enterpriseRequireEnterprise(ctx, client, enterpriseID, false); err != nil {
		return nil, err
	}
	rows, err := client.QueryContext(ctx, `SELECT a.group_id FROM enterprise_allowed_groups a JOIN groups g ON g.id = a.group_id AND g.deleted_at IS NULL WHERE a.enterprise_id = $1 ORDER BY a.group_id`, enterpriseID)
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

func (r *enterpriseGroupRepository) SetAllowedGroupIDs(ctx context.Context, enterpriseID int64, groupIDs []int64) error {
	ids, err := enterpriseNormalizeGroupIDs(groupIDs)
	if err != nil {
		return err
	}
	return enterpriseWithTx(ctx, r.client, func(ctx context.Context, client *dbent.Client) error {
		if err := enterpriseRequireEnterprise(ctx, client, enterpriseID, true); err != nil {
			return err
		}
		if err := enterpriseValidateGroupIDs(ctx, client, ids); err != nil {
			return err
		}
		if _, err := client.ExecContext(ctx, `DELETE FROM enterprise_allowed_groups WHERE enterprise_id = $1`, enterpriseID); err != nil {
			return translatePersistenceError(err, nil, nil)
		}
		if len(ids) == 0 {
			return nil
		}
		_, err := client.ExecContext(ctx, `INSERT INTO enterprise_allowed_groups (enterprise_id, group_id) SELECT $1, id FROM unnest($2::bigint[]) AS id`, enterpriseID, pq.Array(ids))
		return translatePersistenceError(err, nil, nil)
	})
}

func (r *enterpriseGroupRepository) GetWhitelist(ctx context.Context, enterpriseID int64, scopeType string, scopeID int64) (*service.EnterpriseGroupWhitelist, error) {
	client := clientFromContext(ctx, r.client)
	if err := enterpriseRequireScope(ctx, client, enterpriseID, scopeType, scopeID); err != nil {
		return nil, err
	}
	rows, err := client.QueryContext(ctx, `SELECT w.group_id FROM enterprise_group_whitelists w JOIN groups g ON g.id = w.group_id AND g.deleted_at IS NULL
 WHERE w.enterprise_id = $1 AND w.scope_type = $2 AND w.scope_id = $3 ORDER BY w.group_id`, enterpriseID, scopeType, scopeID)
	if err != nil {
		return nil, translatePersistenceError(err, nil, nil)
	}
	defer func() { _ = rows.Close() }()
	w := &service.EnterpriseGroupWhitelist{EnterpriseID: enterpriseID, ScopeType: scopeType, ScopeID: scopeID, GroupIDs: make([]int64, 0)}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		w.GroupIDs = append(w.GroupIDs, id)
	}
	if err := rows.Err(); err != nil {
		return nil, translatePersistenceError(err, nil, nil)
	}
	return w, nil
}

func (r *enterpriseGroupRepository) ListWhitelists(ctx context.Context, enterpriseID int64) ([]service.EnterpriseGroupWhitelist, error) {
	client := clientFromContext(ctx, r.client)
	if err := enterpriseRequireEnterprise(ctx, client, enterpriseID, false); err != nil {
		return nil, err
	}
	rows, err := client.QueryContext(ctx, `SELECT w.scope_type, w.scope_id, w.group_id FROM enterprise_group_whitelists w JOIN groups g ON g.id = w.group_id AND g.deleted_at IS NULL
 WHERE w.enterprise_id = $1 ORDER BY w.scope_type, w.scope_id, w.group_id`, enterpriseID)
	if err != nil {
		return nil, translatePersistenceError(err, nil, nil)
	}
	defer func() { _ = rows.Close() }()
	items := make([]service.EnterpriseGroupWhitelist, 0)
	for rows.Next() {
		var scopeType string
		var scopeID, groupID int64
		if err := rows.Scan(&scopeType, &scopeID, &groupID); err != nil {
			return nil, err
		}
		if len(items) == 0 || items[len(items)-1].ScopeType != scopeType || items[len(items)-1].ScopeID != scopeID {
			items = append(items, service.EnterpriseGroupWhitelist{EnterpriseID: enterpriseID, ScopeType: scopeType, ScopeID: scopeID, GroupIDs: []int64{}})
		}
		items[len(items)-1].GroupIDs = append(items[len(items)-1].GroupIDs, groupID)
	}
	return items, translatePersistenceError(rows.Err(), nil, nil)
}

func (r *enterpriseGroupRepository) SetWhitelist(ctx context.Context, enterpriseID int64, scopeType string, scopeID int64, groupIDs []int64) error {
	ids, err := enterpriseNormalizeGroupIDs(groupIDs)
	if err != nil {
		return err
	}
	return enterpriseWithTx(ctx, r.client, func(ctx context.Context, client *dbent.Client) error {
		if err := enterpriseRequireEnterprise(ctx, client, enterpriseID, true); err != nil {
			return err
		}
		if err := enterpriseRequireScope(ctx, client, enterpriseID, scopeType, scopeID); err != nil {
			return err
		}
		if err := enterpriseValidateGroupIDs(ctx, client, ids); err != nil {
			return err
		}
		if _, err := client.ExecContext(ctx, `DELETE FROM enterprise_group_whitelists WHERE enterprise_id = $1 AND scope_type = $2 AND scope_id = $3`, enterpriseID, scopeType, scopeID); err != nil {
			return translatePersistenceError(err, nil, nil)
		}
		if len(ids) == 0 {
			return nil
		}
		_, err := client.ExecContext(ctx, `INSERT INTO enterprise_group_whitelists (enterprise_id, scope_type, scope_id, group_id) SELECT $1, $2, $3, id FROM unnest($4::bigint[]) AS id`, enterpriseID, scopeType, scopeID, pq.Array(ids))
		return translatePersistenceError(err, nil, nil)
	})
}
