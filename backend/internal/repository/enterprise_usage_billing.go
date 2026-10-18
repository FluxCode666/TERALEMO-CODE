package repository

import (
	"context"
	"database/sql"
	"errors"
	"github.com/lib/pq"
	"log/slog"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func deductUsageBillingEnterpriseBalance(ctx context.Context, tx *sql.Tx, enterpriseID int64, amount float64) (float64, error) {
	var balance float64
	err := tx.QueryRowContext(ctx, `
		UPDATE enterprises
		SET balance = balance - $1, updated_at = NOW()
		WHERE id = $2
		RETURNING balance
	`, amount, enterpriseID).Scan(&balance)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, service.ErrEnterpriseNotFound
	}
	return balance, err
}

// The materialized CTE acquires every required quota row lock in ascending id
// order before UPDATE can execute. Never depend on an UPDATE plan's row order.
// Frozen scopes include soft-deleted archive rows. A missing row is an integrity
// incident, but must not undo payment for usage already consumed upstream.
func incrementUsageBillingEnterpriseQuotas(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand) (*service.EnterpriseBillingWindowStarts, error) {
	departmentIDs := service.FrozenEnterpriseDepartmentIDs(cmd.EnterpriseDepartmentPathIDs, cmd.EnterpriseDepartmentID)
	expected := 2 + len(departmentIDs)
	now := timezone.Now()
	windows := &service.EnterpriseBillingWindowStarts{
		Daily: timezone.StartOfDay(now), Weekly: timezone.StartOfWeek(now), Monthly: timezone.StartOfMonth(now),
	}
	var updated int
	err := tx.QueryRowContext(ctx, `
		WITH locked AS MATERIALIZED (
			SELECT id FROM enterprise_quotas
			WHERE enterprise_id = $2
				AND ((scope_type = 'enterprise' AND scope_id = $2)
					OR (scope_type = 'department' AND scope_id = ANY($3::bigint[]))
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
			WHERE q.id = locked.id
			RETURNING q.id
		)
		SELECT COUNT(*) FROM updated
	`, cmd.EnterpriseBalanceCost, cmd.EnterpriseID, pq.Array(departmentIDs), cmd.EnterpriseMemberID,
		windows.Daily, windows.Weekly, windows.Monthly).Scan(&updated)
	if err != nil {
		return nil, err
	}
	if updated != expected {
		slog.ErrorContext(ctx, "enterprise billing quota row count mismatch; enterprise payment retained",
			"request_id", cmd.RequestID, "api_key_id", cmd.APIKeyID,
			"enterprise_id", cmd.EnterpriseID, "department_id", cmd.EnterpriseDepartmentID, "member_id", cmd.EnterpriseMemberID,
			"department_path_ids", departmentIDs, "cost", cmd.EnterpriseBalanceCost, "updated_rows", updated, "expected_rows", expected)
	}
	return windows, nil
}
