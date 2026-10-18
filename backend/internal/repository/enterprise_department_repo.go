package repository

import (
	"context"
	"fmt"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type enterpriseDepartmentRepository struct{ client *dbent.Client }

func NewEnterpriseDepartmentRepository(client *dbent.Client) service.EnterpriseDepartmentRepository {
	return &enterpriseDepartmentRepository{client: client}
}

const enterpriseDepartmentSelectColumns = `d.id, d.enterprise_id, d.name, d.sort_order, d.created_at, d.updated_at, d.deleted_at, d.parent_id, d.path, d.depth,
 (SELECT COUNT(*) FROM enterprise_members m WHERE m.department_id = d.id AND m.enterprise_id = d.enterprise_id AND m.deleted_at IS NULL),
 (SELECT COUNT(*) FROM enterprise_members m JOIN enterprise_departments child ON child.id=m.department_id AND child.enterprise_id=m.enterprise_id WHERE m.enterprise_id=d.enterprise_id AND m.deleted_at IS NULL AND child.deleted_at IS NULL AND (child.id=d.id OR child.path LIKE (COALESCE(NULLIF(d.path,''), '/' || CAST(d.id AS TEXT) || '/') || '%')))`

func enterpriseDepartmentScan(s enterpriseScanner, d *service.EnterpriseDepartment) error {
	err := s.Scan(&d.ID, &d.EnterpriseID, &d.Name, &d.SortOrder, &d.CreatedAt, &d.UpdatedAt, &d.DeletedAt, &d.ParentID, &d.Path, &d.Depth, &d.MemberCount, &d.TotalMemberCount)
	if err != nil {
		return err
	}
	if d.Path == "" {
		d.Path = fmt.Sprintf("/%d/", d.ID)
	}
	d.PathIDs, err = service.ParseEnterpriseDepartmentPath(d.Path)
	return err
}

func (r *enterpriseDepartmentRepository) Create(ctx context.Context, d *service.EnterpriseDepartment) error {
	return enterpriseWithTx(ctx, r.client, func(ctx context.Context, client *dbent.Client) error {
		if err := enterpriseRequireEnterprise(ctx, client, d.EnterpriseID, true); err != nil {
			return err
		}
		if d.ParentID != nil && *d.ParentID == 0 {
			d.ParentID = nil
		}
		prefix := "/"
		d.Depth = 1
		if d.ParentID != nil {
			parent, err := r.GetByID(ctx, d.EnterpriseID, *d.ParentID)
			if err != nil {
				return err
			}
			prefix = parent.Path
			d.Depth = parent.Depth + 1
		}
		if d.Depth > service.EnterpriseDepartmentMaxDepth {
			return service.ErrEnterpriseDepartmentDepthExceeded
		}
		err := enterpriseQueryOne(ctx, client, `INSERT INTO enterprise_departments (enterprise_id, parent_id, name, sort_order, path, depth) VALUES ($1,$2,$3,$4,'',$5) RETURNING id, created_at, updated_at`, []any{d.EnterpriseID, d.ParentID, d.Name, d.SortOrder, d.Depth}, func(s enterpriseScanner) error { return s.Scan(&d.ID, &d.CreatedAt, &d.UpdatedAt) })
		if err != nil {
			return translatePersistenceError(err, nil, service.ErrEnterpriseDepartmentNameExists)
		}
		d.Path = fmt.Sprintf("%s%d/", prefix, d.ID)
		if _, err = client.ExecContext(ctx, `UPDATE enterprise_departments SET path=$2 WHERE id=$1`, d.ID, d.Path); err != nil {
			return err
		}
		d.PathIDs, _ = service.ParseEnterpriseDepartmentPath(d.Path)
		return enterpriseCreateQuota(ctx, client, d.EnterpriseID, service.EnterpriseScopeDepartment, d.ID)
	})
}

func (r *enterpriseDepartmentRepository) GetByID(ctx context.Context, enterpriseID, departmentID int64) (*service.EnterpriseDepartment, error) {
	client := clientFromContext(ctx, r.client)
	d := new(service.EnterpriseDepartment)
	err := enterpriseQueryOne(ctx, client, `SELECT `+enterpriseDepartmentSelectColumns+` FROM enterprise_departments d JOIN enterprises e ON e.id=d.enterprise_id AND e.deleted_at IS NULL WHERE d.enterprise_id=$1 AND d.id=$2 AND d.deleted_at IS NULL`, []any{enterpriseID, departmentID}, func(s enterpriseScanner) error { return enterpriseDepartmentScan(s, d) })
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrEnterpriseDepartmentNotFound, nil)
	}
	args := []any{enterpriseID}
	placeholders := enterpriseContextPlaceholders(d.PathIDs, &args)
	rows, err := client.QueryContext(ctx, `SELECT id,name FROM enterprise_departments WHERE enterprise_id=$1 AND deleted_at IS NULL AND id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	names := map[int64]string{}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		names[id] = name
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	d.PathNames = []string{}
	for _, id := range d.PathIDs {
		d.PathNames = append(d.PathNames, names[id])
	}
	return d, nil
}

func (r *enterpriseDepartmentRepository) Update(ctx context.Context, d *service.EnterpriseDepartment) error {
	return enterpriseWithTx(ctx, r.client, func(ctx context.Context, client *dbent.Client) error {
		if err := enterpriseRequireEnterprise(ctx, client, d.EnterpriseID, true); err != nil {
			return err
		}
		old, err := r.GetByID(ctx, d.EnterpriseID, d.ID)
		if err != nil {
			return err
		}
		if d.ParentID != nil && *d.ParentID == 0 {
			d.ParentID = nil
		}
		newPath := fmt.Sprintf("/%d/", d.ID)
		newDepth := 1
		if d.ParentID != nil {
			parent, err := r.GetByID(ctx, d.EnterpriseID, *d.ParentID)
			if err != nil {
				return err
			}
			if strings.HasPrefix(parent.Path, old.Path) {
				return service.ErrEnterpriseDepartmentCycle
			}
			newPath = fmt.Sprintf("%s%d/", parent.Path, d.ID)
			newDepth = parent.Depth + 1
		}
		var deepest int
		if err = enterpriseQueryOne(ctx, client, `SELECT COALESCE(MAX(depth),$3) FROM enterprise_departments WHERE enterprise_id=$1 AND deleted_at IS NULL AND (id=$4 OR path LIKE $2)`, []any{d.EnterpriseID, old.Path + "%", old.Depth, d.ID}, func(s enterpriseScanner) error { return s.Scan(&deepest) }); err != nil {
			return err
		}
		delta := newDepth - old.Depth
		if deepest+delta > service.EnterpriseDepartmentMaxDepth {
			return service.ErrEnterpriseDepartmentDepthExceeded
		}
		err = enterpriseQueryOne(ctx, client, `UPDATE enterprise_departments SET name=$3,sort_order=$4,parent_id=$5,updated_at=NOW() WHERE enterprise_id=$1 AND id=$2 AND deleted_at IS NULL RETURNING updated_at`, []any{d.EnterpriseID, d.ID, d.Name, d.SortOrder, d.ParentID}, func(s enterpriseScanner) error { return s.Scan(&d.UpdatedAt) })
		if err != nil {
			return translatePersistenceError(err, service.ErrEnterpriseDepartmentNotFound, service.ErrEnterpriseDepartmentNameExists)
		}
		if old.Path != newPath {
			// The enterprise row is already locked, serializing moves and member
			// mutations. Quotas/whitelists keep their stable department identities.
			_, err = client.ExecContext(ctx, `UPDATE enterprise_departments SET path=CASE WHEN id=$6 THEN $3 ELSE $3 || SUBSTRING(path FROM $4::integer) END,depth=depth+$5,updated_at=NOW() WHERE enterprise_id=$1 AND deleted_at IS NULL AND (id=$6 OR path LIKE $2)`, d.EnterpriseID, old.Path+"%", newPath, len(old.Path)+1, delta, d.ID)
			if err != nil {
				return err
			}
		}
		d.Path = newPath
		d.Depth = newDepth
		d.PathIDs, _ = service.ParseEnterpriseDepartmentPath(newPath)
		return nil
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
		if err := enterpriseQueryOne(ctx, client, `SELECT (SELECT COUNT(*) FROM enterprise_members WHERE enterprise_id = $1 AND department_id = $2 AND deleted_at IS NULL) + (SELECT COUNT(*) FROM enterprise_departments WHERE enterprise_id=$1 AND parent_id=$2 AND deleted_at IS NULL)`, []any{enterpriseID, departmentID}, func(s enterpriseScanner) error { return s.Scan(&count) }); err != nil {
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
	if err := rows.Err(); err != nil {
		return nil, translatePersistenceError(err, nil, nil)
	}
	names := make(map[int64]string, len(items))
	for _, d := range items {
		names[d.ID] = d.Name
	}
	for i := range items {
		items[i].PathNames = []string{}
		for _, id := range items[i].PathIDs {
			items[i].PathNames = append(items[i].PathNames, names[id])
		}
	}
	return items, nil
}
