package repository

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strconv"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type enterpriseBalanceLogRepository struct{ client *dbent.Client }

func NewEnterpriseBalanceLogRepository(client *dbent.Client) service.EnterpriseBalanceLogRepository {
	return &enterpriseBalanceLogRepository{client: client}
}

func (r *enterpriseRepository) GetBalance(ctx context.Context, enterpriseID int64) (float64, error) {
	rows, err := clientFromContext(ctx, r.client).QueryContext(ctx,
		`SELECT balance FROM enterprises WHERE id = $1 AND deleted_at IS NULL`, enterpriseID)
	if err != nil {
		return 0, translatePersistenceError(err, service.ErrEnterpriseNotFound, nil)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return 0, translatePersistenceError(err, nil, nil)
		}
		return 0, translatePersistenceError(sql.ErrNoRows, service.ErrEnterpriseNotFound, nil)
	}
	var balance float64
	if err := rows.Scan(&balance); err != nil {
		return 0, translatePersistenceError(err, nil, nil)
	}
	return balance, nil
}

func (r *enterpriseRepository) AdjustBalance(ctx context.Context, enterpriseID int64, adjustment service.EnterpriseBalanceAdjustment) (*service.EnterpriseBalanceLog, error) {
	if adjustment.Operation != service.EnterpriseBalanceOperationAdd && adjustment.Operation != service.EnterpriseBalanceOperationSubtract {
		return nil, fmt.Errorf("invalid enterprise balance operation %q", adjustment.Operation)
	}
	if adjustment.Amount <= 0 || math.IsNaN(adjustment.Amount) || math.IsInf(adjustment.Amount, 0) {
		return nil, fmt.Errorf("enterprise balance adjustment amount must be finite and positive")
	}
	amount := strconv.FormatFloat(adjustment.Amount, 'f', 8, 64)
	if amount == "0.00000000" {
		return nil, fmt.Errorf("enterprise balance adjustment amount must be positive at 8 decimal places")
	}
	var log service.EnterpriseBalanceLog
	err := enterpriseWithTx(ctx, r.client, func(txCtx context.Context, client *dbent.Client) error {
		if err := enterpriseRequireEnterprise(txCtx, client, enterpriseID, true); err != nil {
			return err
		}
		// PostgreSQL computes before/after at DECIMAL precision, and the CTE makes
		// the guarded update and its ledger entry indivisible even inside an outer transaction.
		rows, err := client.QueryContext(txCtx, `WITH adjusted AS (
 UPDATE enterprises SET
 balance = balance + CASE WHEN $2 = 'add' THEN $3::numeric(20,8) ELSE -$3::numeric(20,8) END,
 total_recharged = total_recharged + CASE WHEN $2 = 'add' THEN $3::numeric(20,8) ELSE 0 END,
 updated_at = NOW()
 WHERE id = $1 AND deleted_at IS NULL AND ($2 = 'add' OR balance >= $3::numeric(20,8))
 RETURNING id, balance - CASE WHEN $2 = 'add' THEN $3::numeric(20,8) ELSE -$3::numeric(20,8) END AS balance_before,
 balance AS balance_after
 ) INSERT INTO enterprise_balance_logs
 (enterprise_id, type, amount, balance_before, balance_after, operator_user_id, notes)
 SELECT id, $2, $3::numeric(20,8), balance_before, balance_after, $4, $5 FROM adjusted
 RETURNING id, enterprise_id, type, amount, balance_before, balance_after, operator_user_id, notes, created_at`,
			enterpriseID, adjustment.Operation, amount, adjustment.OperatorUserID, adjustment.Notes)
		if err != nil {
			return translatePersistenceError(err, service.ErrEnterpriseNotFound, nil)
		}
		defer func() { _ = rows.Close() }()
		if !rows.Next() {
			if err := rows.Err(); err != nil {
				return translatePersistenceError(err, nil, nil)
			}
			return service.ErrEnterpriseBalanceAdjustmentInsufficient
		}
		if err := rows.Scan(&log.ID, &log.EnterpriseID, &log.Type, &log.Amount, &log.BalanceBefore,
			&log.BalanceAfter, &log.OperatorUserID, &log.Notes, &log.CreatedAt); err != nil {
			return translatePersistenceError(err, nil, nil)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &log, nil
}

func (r *enterpriseBalanceLogRepository) List(ctx context.Context, enterpriseID int64, params pagination.PaginationParams) ([]service.EnterpriseBalanceLog, *pagination.PaginationResult, error) {
	client := clientFromContext(ctx, r.client)
	total, err := enterpriseBalanceLogCount(ctx, client, enterpriseID)
	if err != nil {
		return nil, nil, err
	}
	order := params.NormalizedSortOrder(pagination.SortOrderDesc)
	rows, err := client.QueryContext(ctx, `SELECT l.id, l.enterprise_id, l.type, l.amount,
 l.balance_before, l.balance_after, l.operator_user_id, COALESCE(u.email, ''), l.notes, l.created_at
 FROM enterprise_balance_logs l LEFT JOIN users u ON u.id = l.operator_user_id
 WHERE l.enterprise_id = $1 ORDER BY l.created_at `+order+`, l.id `+order+` LIMIT $2 OFFSET $3`, enterpriseID, params.Limit(), params.Offset())
	if err != nil {
		return nil, nil, translatePersistenceError(err, nil, nil)
	}
	defer func() { _ = rows.Close() }()
	items := make([]service.EnterpriseBalanceLog, 0)
	for rows.Next() {
		var log service.EnterpriseBalanceLog
		if err := rows.Scan(&log.ID, &log.EnterpriseID, &log.Type, &log.Amount, &log.BalanceBefore,
			&log.BalanceAfter, &log.OperatorUserID, &log.OperatorEmail, &log.Notes, &log.CreatedAt); err != nil {
			return nil, nil, translatePersistenceError(err, nil, nil)
		}
		items = append(items, log)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, translatePersistenceError(err, nil, nil)
	}
	return items, paginationResultFromTotal(total, params), nil
}

func enterpriseBalanceLogCount(ctx context.Context, client *dbent.Client, enterpriseID int64) (int64, error) {
	rows, err := client.QueryContext(ctx, `SELECT COUNT(*) FROM enterprise_balance_logs WHERE enterprise_id = $1`, enterpriseID)
	if err != nil {
		return 0, translatePersistenceError(err, nil, nil)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return 0, translatePersistenceError(rows.Err(), nil, nil)
	}
	var total int64
	if err := rows.Scan(&total); err != nil {
		return 0, translatePersistenceError(err, nil, nil)
	}
	return total, nil
}
