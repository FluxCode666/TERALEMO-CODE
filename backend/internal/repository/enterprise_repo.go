package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type enterpriseRepository struct{ client *dbent.Client }

func NewEnterpriseRepository(client *dbent.Client) service.EnterpriseRepository {
	return &enterpriseRepository{client: client}
}

const enterpriseSelectColumns = `e.id, e.name, e.status, e.balance, e.total_recharged, e.low_balance_threshold,
 e.contact_name, e.contact_email, e.contact_phone, e.notes, e.settings, e.group_pool_configured, e.created_at, e.updated_at, e.deleted_at,
 (SELECT COUNT(*) FROM enterprise_members m WHERE m.enterprise_id = e.id AND m.deleted_at IS NULL),
 (SELECT COUNT(*) FROM enterprise_departments d WHERE d.enterprise_id = e.id AND d.deleted_at IS NULL)`

func enterpriseScan(s enterpriseScanner, e *service.Enterprise) error {
	var settings []byte
	if err := s.Scan(&e.ID, &e.Name, &e.Status, &e.Balance, &e.TotalRecharged, &e.LowBalanceThreshold,
		&e.ContactName, &e.ContactEmail, &e.ContactPhone, &e.Notes, &settings, &e.GroupPoolConfigured, &e.CreatedAt, &e.UpdatedAt, &e.DeletedAt,
		&e.MemberCount, &e.DepartmentCount); err != nil {
		return err
	}
	return json.Unmarshal(settings, &e.Settings)
}

func (r *enterpriseRepository) Create(ctx context.Context, e *service.Enterprise) error {
	settings, err := json.Marshal(e.Settings)
	if err != nil {
		return err
	}
	if e.Status == "" {
		e.Status = service.EnterpriseStatusActive
	}
	err = enterpriseWithTx(ctx, r.client, func(ctx context.Context, client *dbent.Client) error {
		err := enterpriseQueryOne(ctx, client, `INSERT INTO enterprises (name, status, low_balance_threshold, contact_name, contact_email, contact_phone, notes, settings)
 VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id, created_at, updated_at`, []any{e.Name, e.Status, e.LowBalanceThreshold, e.ContactName, e.ContactEmail, e.ContactPhone, e.Notes, string(settings)}, func(s enterpriseScanner) error { return s.Scan(&e.ID, &e.CreatedAt, &e.UpdatedAt) })
		if err != nil {
			return translatePersistenceError(err, nil, service.ErrEnterpriseNameExists)
		}
		return enterpriseCreateQuota(ctx, client, e.ID, service.EnterpriseScopeEnterprise, e.ID)
	})
	if err == nil {
		e.Balance, e.TotalRecharged = 0, 0
	}
	return err
}

func (r *enterpriseRepository) GetByID(ctx context.Context, id int64) (*service.Enterprise, error) {
	client := clientFromContext(ctx, r.client)
	e := new(service.Enterprise)
	err := enterpriseQueryOne(ctx, client, `SELECT `+enterpriseSelectColumns+` FROM enterprises e WHERE e.id = $1 AND e.deleted_at IS NULL`, []any{id}, func(s enterpriseScanner) error { return enterpriseScan(s, e) })
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrEnterpriseNotFound, nil)
	}
	counts, err := enterpriseActiveOwnerCounts(ctx, client, []int64{id})
	if err != nil {
		return nil, err
	}
	e.ActiveOwnerCount = counts[id]
	return e, nil
}

func (r *enterpriseRepository) Update(ctx context.Context, e *service.Enterprise) error {
	settings, err := json.Marshal(e.Settings)
	if err != nil {
		return err
	}
	err = enterpriseQueryOne(ctx, clientFromContext(ctx, r.client), `UPDATE enterprises SET name = $2, status = $3, low_balance_threshold = $4,
 contact_name = $5, contact_email = $6, contact_phone = $7, notes = $8, settings = $9, updated_at = NOW()
 WHERE id = $1 AND deleted_at IS NULL RETURNING updated_at`, []any{e.ID, e.Name, e.Status, e.LowBalanceThreshold, e.ContactName, e.ContactEmail, e.ContactPhone, e.Notes, string(settings)}, func(s enterpriseScanner) error { return s.Scan(&e.UpdatedAt) })
	return translatePersistenceError(err, service.ErrEnterpriseNotFound, service.ErrEnterpriseNameExists)
}

func (r *enterpriseRepository) Delete(ctx context.Context, id int64) error {
	return enterpriseWithTx(ctx, r.client, func(ctx context.Context, client *dbent.Client) error {
		if err := enterpriseRequireEnterprise(ctx, client, id, true); err != nil {
			return err
		}
		var count int64
		if err := enterpriseQueryOne(ctx, client, `SELECT COUNT(*) FROM enterprise_members WHERE enterprise_id = $1 AND deleted_at IS NULL`, []any{id}, func(s enterpriseScanner) error { return s.Scan(&count) }); err != nil {
			return translatePersistenceError(err, nil, nil)
		}
		if count != 0 {
			return service.ErrEnterpriseNotEmpty
		}
		queries := []string{
			`UPDATE enterprise_departments SET deleted_at = NOW(), updated_at = NOW() WHERE enterprise_id = $1 AND deleted_at IS NULL`,
			`UPDATE enterprise_quotas SET deleted_at = NOW(), updated_at = NOW() WHERE enterprise_id = $1 AND deleted_at IS NULL`,
			`DELETE FROM enterprise_allowed_groups WHERE enterprise_id = $1`,
			`DELETE FROM enterprise_group_whitelists WHERE enterprise_id = $1`,
			`DELETE FROM enterprise_group_whitelist_scopes WHERE enterprise_id = $1`,
			`UPDATE enterprises SET deleted_at = NOW(), updated_at = NOW() WHERE id = $1 AND deleted_at IS NULL`,
		}
		for _, query := range queries {
			if _, err := client.ExecContext(ctx, query, id); err != nil {
				return translatePersistenceError(err, nil, nil)
			}
		}
		return nil
	})
}

func (r *enterpriseRepository) List(ctx context.Context, params pagination.PaginationParams, filters service.EnterpriseListFilters) ([]service.Enterprise, *pagination.PaginationResult, error) {
	client := clientFromContext(ctx, r.client)
	var where strings.Builder
	_, _ = where.WriteString(` WHERE e.deleted_at IS NULL`)
	args := []any{}
	if filters.Status != "" {
		enterpriseAddFilter(&where, &args, `e.status = $%d`, filters.Status)
	}
	if filters.Search != "" {
		enterpriseAddFilter(&where, &args, `e.name ILIKE $%d`, "%"+filters.Search+"%")
	}
	var total int64
	if err := enterpriseQueryOne(ctx, client, `SELECT COUNT(*) FROM enterprises e`+where.String(), args, func(s enterpriseScanner) error { return s.Scan(&total) }); err != nil {
		return nil, nil, translatePersistenceError(err, nil, nil)
	}
	order := enterpriseListOrder(params, "e", map[string]string{"id": "id", "name": "name", "status": "status", "balance": "balance", "created_at": "created_at", "updated_at": "updated_at"}, "created_at")
	args = append(args, params.Limit(), params.Offset())
	query := fmt.Sprintf(`SELECT `+enterpriseSelectColumns+` FROM enterprises e%s ORDER BY %s LIMIT $%d OFFSET $%d`, where.String(), order, len(args)-1, len(args))
	rows, err := client.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, translatePersistenceError(err, nil, nil)
	}
	defer func() { _ = rows.Close() }()
	items := make([]service.Enterprise, 0)
	for rows.Next() {
		var e service.Enterprise
		if err := enterpriseScan(rows, &e); err != nil {
			return nil, nil, err
		}
		items = append(items, e)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, translatePersistenceError(err, nil, nil)
	}
	if err := rows.Close(); err != nil {
		return nil, nil, translatePersistenceError(err, nil, nil)
	}
	ids := make([]int64, len(items))
	for i := range items {
		ids[i] = items[i].ID
	}
	counts, err := enterpriseActiveOwnerCounts(ctx, client, ids)
	if err != nil {
		return nil, nil, err
	}
	for i := range items {
		items[i].ActiveOwnerCount = counts[items[i].ID]
	}
	return items, paginationResultFromTotal(total, params), nil
}

// Match CountActiveOwners without taking management locks. One grouped query
// covers the current list page, including enterprises with no available owner.
func enterpriseActiveOwnerCounts(ctx context.Context, client *dbent.Client, enterpriseIDs []int64) (map[int64]int, error) {
	counts := make(map[int64]int, len(enterpriseIDs))
	if len(enterpriseIDs) == 0 {
		return counts, nil
	}
	args := []any{service.EnterpriseRoleOwner, service.EnterpriseMemberStatusActive}
	placeholders := enterpriseContextPlaceholders(enterpriseIDs, &args)
	rows, err := client.QueryContext(ctx, `SELECT m.enterprise_id, COUNT(*) FROM enterprise_members m
 JOIN users u ON u.id = m.user_id AND u.status = 'active' AND u.deleted_at IS NULL
 WHERE m.role = $1 AND m.status = $2 AND m.deleted_at IS NULL
 AND m.enterprise_id IN (`+placeholders+`) GROUP BY m.enterprise_id`, args...)
	if err != nil {
		return nil, translatePersistenceError(err, nil, nil)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var enterpriseID int64
		var count int
		if err := rows.Scan(&enterpriseID, &count); err != nil {
			return nil, err
		}
		counts[enterpriseID] = count
	}
	return counts, translatePersistenceError(rows.Err(), nil, nil)
}
