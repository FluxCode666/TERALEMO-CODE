package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// enterpriseWithTx joins a caller's transaction so organization changes, quotas,
// and audit records can be committed together by the management service.
func enterpriseWithTx(ctx context.Context, client *dbent.Client, fn func(context.Context, *dbent.Client) error) error {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return fn(ctx, tx.Client())
	}
	tx, err := client.Tx(ctx)
	if err != nil {
		return translatePersistenceError(err, nil, nil)
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(dbent.NewTxContext(ctx, tx), tx.Client()); err != nil {
		return err
	}
	return translatePersistenceError(tx.Commit(), nil, nil)
}

type enterpriseScanner interface{ Scan(...any) error }

// QueryRow is intentionally implemented through the Ent client, including its
// transaction driver; opening a separate sql.DB would escape the caller's tx.
func enterpriseQueryOne(ctx context.Context, client *dbent.Client, query string, args []any, scan func(enterpriseScanner) error) error {
	rows, err := client.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return sql.ErrNoRows
	}
	if err := scan(rows); err != nil {
		return err
	}
	return rows.Err()
}

func enterpriseRequireEnterprise(ctx context.Context, client *dbent.Client, id int64, lock bool) error {
	query := `SELECT id FROM enterprises WHERE id = $1 AND deleted_at IS NULL`
	if lock {
		query += ` FOR UPDATE`
	}
	var found int64
	err := enterpriseQueryOne(ctx, client, query, []any{id}, func(s enterpriseScanner) error { return s.Scan(&found) })
	return translatePersistenceError(err, service.ErrEnterpriseNotFound, nil)
}

func enterpriseScopeNotFound(scopeType string) *infraerrors.ApplicationError {
	switch scopeType {
	case service.EnterpriseScopeEnterprise:
		return service.ErrEnterpriseNotFound
	case service.EnterpriseScopeDepartment:
		return service.ErrEnterpriseDepartmentNotFound
	case service.EnterpriseScopeMember:
		return service.ErrEnterpriseMemberNotFound
	default:
		return service.ErrEnterpriseInvalidScope
	}
}

func enterpriseRequireScope(ctx context.Context, client *dbent.Client, enterpriseID int64, scopeType string, scopeID int64) error {
	if err := enterpriseRequireEnterprise(ctx, client, enterpriseID, false); err != nil {
		return err
	}
	var table string
	switch scopeType {
	case service.EnterpriseScopeEnterprise:
		if scopeID != enterpriseID {
			return service.ErrEnterpriseInvalidScope
		}
		return nil
	case service.EnterpriseScopeDepartment:
		table = "enterprise_departments"
	case service.EnterpriseScopeMember:
		table = "enterprise_members"
	default:
		return service.ErrEnterpriseInvalidScope
	}
	var found int64
	err := enterpriseQueryOne(ctx, client, `SELECT id FROM `+table+` WHERE id = $1 AND enterprise_id = $2 AND deleted_at IS NULL`, []any{scopeID, enterpriseID}, func(s enterpriseScanner) error { return s.Scan(&found) })
	return translatePersistenceError(err, service.ErrEnterpriseInvalidScope, nil)
}

func enterpriseCreateQuota(ctx context.Context, client *dbent.Client, enterpriseID int64, scopeType string, scopeID int64) error {
	_, err := client.ExecContext(ctx, `INSERT INTO enterprise_quotas (enterprise_id, scope_type, scope_id) VALUES ($1, $2, $3)`, enterpriseID, scopeType, scopeID)
	return translatePersistenceError(err, nil, nil)
}

func enterpriseDeleteScopeData(ctx context.Context, client *dbent.Client, enterpriseID int64, scopeType string, scopeID int64) error {
	if _, err := client.ExecContext(ctx, `UPDATE enterprise_quotas SET deleted_at = NOW(), updated_at = NOW() WHERE enterprise_id = $1 AND scope_type = $2 AND scope_id = $3 AND deleted_at IS NULL`, enterpriseID, scopeType, scopeID); err != nil {
		return translatePersistenceError(err, nil, nil)
	}
	if _, err := client.ExecContext(ctx, `DELETE FROM enterprise_group_whitelists WHERE enterprise_id = $1 AND scope_type = $2 AND scope_id = $3`, enterpriseID, scopeType, scopeID); err != nil {
		return translatePersistenceError(err, nil, nil)
	}
	_, err := client.ExecContext(ctx, `DELETE FROM enterprise_group_whitelist_scopes WHERE enterprise_id = $1 AND scope_type = $2 AND scope_id = $3`, enterpriseID, scopeType, scopeID)
	return translatePersistenceError(err, nil, nil)
}

func enterpriseListOrder(params pagination.PaginationParams, alias string, allowed map[string]string, fallback string) string {
	field, ok := allowed[strings.ToLower(strings.TrimSpace(params.SortBy))]
	if !ok {
		field = fallback
	}
	direction := strings.ToUpper(params.NormalizedSortOrder(pagination.SortOrderDesc))
	order := alias + "." + field + " " + direction
	if field != "id" {
		order += ", " + alias + ".id " + direction
	}
	return order
}

func enterpriseAddFilter(where *strings.Builder, args *[]any, expression string, value any) {
	*args = append(*args, value)
	fmt.Fprintf(where, " AND "+expression, len(*args))
}
