package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func deductUsageBillingEnterpriseBalance(ctx context.Context, tx *sql.Tx, enterpriseID int64, amount float64) (float64, error) {
	var balance float64
	err := tx.QueryRowContext(ctx, `
		UPDATE enterprises
		SET balance = balance - $1, updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
		RETURNING balance
	`, amount, enterpriseID).Scan(&balance)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, service.ErrEnterpriseNotFound
	}
	return balance, err
}

// The materialized CTE acquires every required quota row lock in ascending id
// order before UPDATE can execute. Never depend on an UPDATE plan's row order.
// The cardinality guard makes missing quotas fail closed: no partial usage is
// applied, and the caller rolls back the enterprise balance deduction as well.
func incrementUsageBillingEnterpriseQuotas(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand) error {
	expected := 2
	if cmd.EnterpriseDepartmentID != 0 {
		expected++
	}
	now := timezone.Now()
	var updated int
	err := tx.QueryRowContext(ctx, `
		WITH locked AS MATERIALIZED (
			SELECT id FROM enterprise_quotas
			WHERE enterprise_id = $2 AND deleted_at IS NULL
				AND ((scope_type = 'enterprise' AND scope_id = $2)
					OR (scope_type = 'department' AND scope_id = $3 AND $3 <> 0)
					OR (scope_type = 'member' AND scope_id = $4))
			ORDER BY id FOR UPDATE
		), updated AS (
			UPDATE enterprise_quotas q SET
				daily_usage_usd = CASE WHEN q.daily_window_start IS NULL OR q.daily_window_start < $5 THEN 0 ELSE q.daily_usage_usd END + $1,
				weekly_usage_usd = CASE WHEN q.weekly_window_start IS NULL OR q.weekly_window_start < $6 THEN 0 ELSE q.weekly_usage_usd END + $1,
				monthly_usage_usd = CASE WHEN q.monthly_window_start IS NULL OR q.monthly_window_start < $7 THEN 0 ELSE q.monthly_usage_usd END + $1,
				total_usage_usd = q.total_usage_usd + $1,
				daily_window_start = CASE WHEN q.daily_window_start IS NULL OR q.daily_window_start < $5 THEN $5 ELSE q.daily_window_start END,
				weekly_window_start = CASE WHEN q.weekly_window_start IS NULL OR q.weekly_window_start < $6 THEN $6 ELSE q.weekly_window_start END,
				monthly_window_start = CASE WHEN q.monthly_window_start IS NULL OR q.monthly_window_start < $7 THEN $7 ELSE q.monthly_window_start END,
				updated_at = NOW()
			FROM locked
			WHERE q.id = locked.id AND (SELECT COUNT(*) FROM locked) = $8
			RETURNING q.id
		)
		SELECT COUNT(*) FROM updated
	`, cmd.EnterpriseBalanceCost, cmd.EnterpriseID, cmd.EnterpriseDepartmentID, cmd.EnterpriseMemberID,
		timezone.StartOfDay(now), timezone.StartOfWeek(now), timezone.StartOfMonth(now), expected).Scan(&updated)
	if err != nil {
		return err
	}
	if updated != expected {
		return fmt.Errorf("enterprise billing quota rows missing: enterprise_id=%d got=%d want=%d: %w", cmd.EnterpriseID, updated, expected, service.ErrBillingServiceUnavailable)
	}
	return nil
}
