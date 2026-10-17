package repository

import (
	"context"
	"fmt"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type enterpriseMemberRepository struct{ client *dbent.Client }

func NewEnterpriseMemberRepository(client *dbent.Client) service.EnterpriseMemberRepository {
	return &enterpriseMemberRepository{client: client}
}

const enterpriseMemberSelectColumns = `m.id, m.enterprise_id, m.user_id, m.department_id, m.role, m.status, m.title, m.joined_at,
 m.created_at, m.updated_at, m.deleted_at, COALESCE(u.email, ''), COALESCE(u.username, ''), COALESCE(u.status, ''), COALESCE(d.name, '')`
const enterpriseMemberJoins = ` FROM enterprise_members m JOIN enterprises e ON e.id = m.enterprise_id AND e.deleted_at IS NULL
 LEFT JOIN users u ON u.id = m.user_id AND u.deleted_at IS NULL
 LEFT JOIN enterprise_departments d ON d.id = m.department_id AND d.enterprise_id = m.enterprise_id AND d.deleted_at IS NULL`

func enterpriseMemberScan(s enterpriseScanner, m *service.EnterpriseMember) error {
	return s.Scan(&m.ID, &m.EnterpriseID, &m.UserID, &m.DepartmentID, &m.Role, &m.Status, &m.Title, &m.JoinedAt,
		&m.CreatedAt, &m.UpdatedAt, &m.DeletedAt, &m.Email, &m.Username, &m.UserStatus, &m.DepartmentName)
}

func (r *enterpriseMemberRepository) Create(ctx context.Context, m *service.EnterpriseMember) error {
	if m.Role == "" {
		m.Role = service.EnterpriseRoleMember
	}
	if m.Status == "" {
		m.Status = service.EnterpriseMemberStatusActive
	}
	return enterpriseWithTx(ctx, r.client, func(ctx context.Context, client *dbent.Client) error {
		if err := enterpriseRequireEnterprise(ctx, client, m.EnterpriseID, true); err != nil {
			return err
		}
		if m.DepartmentID != nil {
			if err := enterpriseRequireScope(ctx, client, m.EnterpriseID, service.EnterpriseScopeDepartment, *m.DepartmentID); err != nil {
				return err
			}
		}
		var userID int64
		var userRole string
		err := enterpriseQueryOne(ctx, client, `SELECT id, role FROM users WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, []any{m.UserID}, func(s enterpriseScanner) error { return s.Scan(&userID, &userRole) })
		if err != nil {
			return translatePersistenceError(err, service.ErrUserNotFound, nil)
		}
		if userRole == service.RoleAdmin {
			return service.ErrEnterpriseMemberAdminForbidden
		}
		query := `INSERT INTO enterprise_members (enterprise_id, user_id, department_id, role, status, title) VALUES ($1,$2,$3,$4,$5,$6) RETURNING id, joined_at, created_at, updated_at`
		args := []any{m.EnterpriseID, m.UserID, m.DepartmentID, m.Role, m.Status, m.Title}
		if !m.JoinedAt.IsZero() {
			query = `INSERT INTO enterprise_members (enterprise_id, user_id, department_id, role, status, title, joined_at) VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id, joined_at, created_at, updated_at`
			args = append(args, m.JoinedAt)
		}
		err = enterpriseQueryOne(ctx, client, query, args, func(s enterpriseScanner) error { return s.Scan(&m.ID, &m.JoinedAt, &m.CreatedAt, &m.UpdatedAt) })
		if err != nil {
			return translatePersistenceError(err, nil, service.ErrEnterpriseMemberExists)
		}
		return enterpriseCreateQuota(ctx, client, m.EnterpriseID, service.EnterpriseScopeMember, m.ID)
	})
}

func (r *enterpriseMemberRepository) GetByID(ctx context.Context, enterpriseID, memberID int64) (*service.EnterpriseMember, error) {
	m := new(service.EnterpriseMember)
	err := enterpriseQueryOne(ctx, clientFromContext(ctx, r.client), `SELECT `+enterpriseMemberSelectColumns+enterpriseMemberJoins+` WHERE m.enterprise_id = $1 AND m.id = $2 AND m.deleted_at IS NULL`, []any{enterpriseID, memberID}, func(s enterpriseScanner) error { return enterpriseMemberScan(s, m) })
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrEnterpriseMemberNotFound, nil)
	}
	return m, nil
}

func (r *enterpriseMemberRepository) GetByUserID(ctx context.Context, userID int64) (*service.EnterpriseMember, error) {
	m := new(service.EnterpriseMember)
	err := enterpriseQueryOne(ctx, clientFromContext(ctx, r.client), `SELECT `+enterpriseMemberSelectColumns+enterpriseMemberJoins+` WHERE m.user_id = $1 AND m.deleted_at IS NULL`, []any{userID}, func(s enterpriseScanner) error { return enterpriseMemberScan(s, m) })
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrEnterpriseMemberNotFound, nil)
	}
	return m, nil
}

func (r *enterpriseMemberRepository) Update(ctx context.Context, m *service.EnterpriseMember) error {
	return enterpriseWithTx(ctx, r.client, func(ctx context.Context, client *dbent.Client) error {
		if err := enterpriseRequireEnterprise(ctx, client, m.EnterpriseID, true); err != nil {
			return err
		}
		if m.DepartmentID != nil {
			if err := enterpriseRequireScope(ctx, client, m.EnterpriseID, service.EnterpriseScopeDepartment, *m.DepartmentID); err != nil {
				return err
			}
		}
		// The membership's user and enterprise identity are immutable. Moving to
		// another enterprise requires soft-delete followed by a new membership.
		err := enterpriseQueryOne(ctx, client, `UPDATE enterprise_members SET department_id = $3, role = $4, status = $5, title = $6, updated_at = NOW()
 WHERE enterprise_id = $1 AND id = $2 AND deleted_at IS NULL RETURNING updated_at`, []any{m.EnterpriseID, m.ID, m.DepartmentID, m.Role, m.Status, m.Title}, func(s enterpriseScanner) error { return s.Scan(&m.UpdatedAt) })
		return translatePersistenceError(err, service.ErrEnterpriseMemberNotFound, nil)
	})
}

func (r *enterpriseMemberRepository) Delete(ctx context.Context, enterpriseID, memberID int64) error {
	return enterpriseWithTx(ctx, r.client, func(ctx context.Context, client *dbent.Client) error {
		if err := enterpriseRequireEnterprise(ctx, client, enterpriseID, true); err != nil {
			return err
		}
		if err := enterpriseRequireScope(ctx, client, enterpriseID, service.EnterpriseScopeMember, memberID); err != nil {
			return err
		}
		if err := enterpriseDeleteScopeData(ctx, client, enterpriseID, service.EnterpriseScopeMember, memberID); err != nil {
			return err
		}
		_, err := client.ExecContext(ctx, `UPDATE enterprise_members SET deleted_at = NOW(), updated_at = NOW() WHERE enterprise_id = $1 AND id = $2 AND deleted_at IS NULL`, enterpriseID, memberID)
		return translatePersistenceError(err, nil, nil)
	})
}

func (r *enterpriseMemberRepository) List(ctx context.Context, enterpriseID int64, params pagination.PaginationParams, filters service.EnterpriseMemberListFilters) ([]service.EnterpriseMember, *pagination.PaginationResult, error) {
	client := clientFromContext(ctx, r.client)
	if err := enterpriseRequireEnterprise(ctx, client, enterpriseID, false); err != nil {
		return nil, nil, err
	}
	var where strings.Builder
	_, _ = where.WriteString(` WHERE m.enterprise_id = $1 AND m.deleted_at IS NULL`)
	args := []any{enterpriseID}
	if filters.Role != "" {
		enterpriseAddFilter(&where, &args, `m.role = $%d`, filters.Role)
	}
	if filters.Status != "" {
		enterpriseAddFilter(&where, &args, `m.status = $%d`, filters.Status)
	}
	if filters.DepartmentID != nil {
		if *filters.DepartmentID == 0 {
			_, _ = where.WriteString(` AND m.department_id IS NULL`)
		} else {
			enterpriseAddFilter(&where, &args, `m.department_id = $%d`, *filters.DepartmentID)
		}
	}
	if filters.Search != "" {
		args = append(args, "%"+filters.Search+"%")
		fmt.Fprintf(&where, ` AND (u.email ILIKE $%d OR u.username ILIKE $%d OR m.title ILIKE $%d)`, len(args), len(args), len(args))
	}
	var total int64
	if err := enterpriseQueryOne(ctx, client, `SELECT COUNT(*)`+enterpriseMemberJoins+where.String(), args, func(s enterpriseScanner) error { return s.Scan(&total) }); err != nil {
		return nil, nil, translatePersistenceError(err, nil, nil)
	}
	order := enterpriseListOrder(params, "m", map[string]string{"id": "id", "role": "role", "status": "status", "joined_at": "joined_at", "created_at": "created_at", "updated_at": "updated_at"}, "created_at")
	args = append(args, params.Limit(), params.Offset())
	query := fmt.Sprintf(`SELECT `+enterpriseMemberSelectColumns+enterpriseMemberJoins+`%s ORDER BY %s LIMIT $%d OFFSET $%d`, where.String(), order, len(args)-1, len(args))
	rows, err := client.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, translatePersistenceError(err, nil, nil)
	}
	defer func() { _ = rows.Close() }()
	items := make([]service.EnterpriseMember, 0)
	for rows.Next() {
		var m service.EnterpriseMember
		if err := enterpriseMemberScan(rows, &m); err != nil {
			return nil, nil, err
		}
		items = append(items, m)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, translatePersistenceError(err, nil, nil)
	}
	return items, paginationResultFromTotal(total, params), nil
}

func (r *enterpriseMemberRepository) CountActiveOwners(ctx context.Context, enterpriseID int64) (int64, error) {
	client := clientFromContext(ctx, r.client)
	// A management service can count and mutate owners in one caller-owned
	// transaction. Serialize those decisions on the same enterprise row used by
	// Update/Delete so two concurrent operations cannot both remove the last owner.
	if err := enterpriseRequireEnterprise(ctx, client, enterpriseID, true); err != nil {
		return 0, err
	}
	var count int64
	err := enterpriseQueryOne(ctx, client, `SELECT COUNT(*) FROM enterprise_members m JOIN users u ON u.id = m.user_id AND u.status = 'active' AND u.deleted_at IS NULL WHERE m.enterprise_id = $1 AND m.role = $2 AND m.status = $3 AND m.deleted_at IS NULL`, []any{enterpriseID, service.EnterpriseRoleOwner, service.EnterpriseMemberStatusActive}, func(s enterpriseScanner) error { return s.Scan(&count) })
	return count, translatePersistenceError(err, nil, nil)
}
