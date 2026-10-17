package repository

import (
	"context"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type enterpriseDepartmentRepository struct{ client *dbent.Client }

func NewEnterpriseDepartmentRepository(client *dbent.Client) service.EnterpriseDepartmentRepository {
	return &enterpriseDepartmentRepository{client: client}
}

const enterpriseDepartmentSelectColumns = `d.id, d.enterprise_id, d.name, d.sort_order, d.created_at, d.updated_at, d.deleted_at,
 (SELECT COUNT(*) FROM enterprise_members m WHERE m.department_id = d.id AND m.enterprise_id = d.enterprise_id AND m.deleted_at IS NULL)`

func enterpriseDepartmentScan(s enterpriseScanner, d *service.EnterpriseDepartment) error {
	return s.Scan(&d.ID, &d.EnterpriseID, &d.Name, &d.SortOrder, &d.CreatedAt, &d.UpdatedAt, &d.DeletedAt, &d.MemberCount)
}

func (r *enterpriseDepartmentRepository) Create(ctx context.Context, d *service.EnterpriseDepartment) error {
	return enterpriseWithTx(ctx, r.client, func(ctx context.Context, client *dbent.Client) error {
		if err := enterpriseRequireEnterprise(ctx, client, d.EnterpriseID, true); err != nil {
			return err
		}
		err := enterpriseQueryOne(ctx, client, `INSERT INTO enterprise_departments (enterprise_id, name, sort_order) VALUES ($1,$2,$3) RETURNING id, created_at, updated_at`, []any{d.EnterpriseID, d.Name, d.SortOrder}, func(s enterpriseScanner) error { return s.Scan(&d.ID, &d.CreatedAt, &d.UpdatedAt) })
		if err != nil {
			return translatePersistenceError(err, nil, service.ErrEnterpriseDepartmentNameExists)
		}
		return enterpriseCreateQuota(ctx, client, d.EnterpriseID, service.EnterpriseScopeDepartment, d.ID)
	})
}

func (r *enterpriseDepartmentRepository) GetByID(ctx context.Context, enterpriseID, departmentID int64) (*service.EnterpriseDepartment, error) {
	d := new(service.EnterpriseDepartment)
	err := enterpriseQueryOne(ctx, clientFromContext(ctx, r.client), `SELECT `+enterpriseDepartmentSelectColumns+` FROM enterprise_departments d JOIN enterprises e ON e.id = d.enterprise_id AND e.deleted_at IS NULL WHERE d.enterprise_id = $1 AND d.id = $2 AND d.deleted_at IS NULL`, []any{enterpriseID, departmentID}, func(s enterpriseScanner) error { return enterpriseDepartmentScan(s, d) })
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrEnterpriseDepartmentNotFound, nil)
	}
	return d, nil
}

func (r *enterpriseDepartmentRepository) Update(ctx context.Context, d *service.EnterpriseDepartment) error {
	return enterpriseWithTx(ctx, r.client, func(ctx context.Context, client *dbent.Client) error {
		if err := enterpriseRequireEnterprise(ctx, client, d.EnterpriseID, true); err != nil {
			return err
		}
		err := enterpriseQueryOne(ctx, client, `UPDATE enterprise_departments SET name = $3, sort_order = $4, updated_at = NOW() WHERE enterprise_id = $1 AND id = $2 AND deleted_at IS NULL RETURNING updated_at`, []any{d.EnterpriseID, d.ID, d.Name, d.SortOrder}, func(s enterpriseScanner) error { return s.Scan(&d.UpdatedAt) })
		return translatePersistenceError(err, service.ErrEnterpriseDepartmentNotFound, service.ErrEnterpriseDepartmentNameExists)
	})
}

func (r *enterpriseDepartmentRepository) Delete(ctx context.Context, enterpriseID, departmentID int64) error {
	return enterpriseWithTx(ctx, r.client, func(ctx context.Context, client *dbent.Client) error {
		if err := enterpriseRequireEnterprise(ctx, client, enterpriseID, true); err != nil {
			return err
		}
		if err := enterpriseRequireScope(ctx, client, enterpriseID, service.EnterpriseScopeDepartment, departmentID); err != nil {
			return err
		}
		var count int64
		if err := enterpriseQueryOne(ctx, client, `SELECT COUNT(*) FROM enterprise_members WHERE enterprise_id = $1 AND department_id = $2 AND deleted_at IS NULL`, []any{enterpriseID, departmentID}, func(s enterpriseScanner) error { return s.Scan(&count) }); err != nil {
			return translatePersistenceError(err, nil, nil)
		}
		if count != 0 {
			return service.ErrEnterpriseDepartmentNotEmpty
		}
		if err := enterpriseDeleteScopeData(ctx, client, enterpriseID, service.EnterpriseScopeDepartment, departmentID); err != nil {
			return err
		}
		_, err := client.ExecContext(ctx, `UPDATE enterprise_departments SET deleted_at = NOW(), updated_at = NOW() WHERE enterprise_id = $1 AND id = $2 AND deleted_at IS NULL`, enterpriseID, departmentID)
		return translatePersistenceError(err, nil, nil)
	})
}

func (r *enterpriseDepartmentRepository) List(ctx context.Context, enterpriseID int64) ([]service.EnterpriseDepartment, error) {
	client := clientFromContext(ctx, r.client)
	if err := enterpriseRequireEnterprise(ctx, client, enterpriseID, false); err != nil {
		return nil, err
	}
	rows, err := client.QueryContext(ctx, `SELECT `+enterpriseDepartmentSelectColumns+` FROM enterprise_departments d WHERE d.enterprise_id = $1 AND d.deleted_at IS NULL ORDER BY d.sort_order ASC, d.id ASC`, enterpriseID)
	if err != nil {
		return nil, translatePersistenceError(err, nil, nil)
	}
	defer func() { _ = rows.Close() }()
	items := make([]service.EnterpriseDepartment, 0)
	for rows.Next() {
		var d service.EnterpriseDepartment
		if err := enterpriseDepartmentScan(rows, &d); err != nil {
			return nil, err
		}
		items = append(items, d)
	}
	return items, translatePersistenceError(rows.Err(), nil, nil)
}
