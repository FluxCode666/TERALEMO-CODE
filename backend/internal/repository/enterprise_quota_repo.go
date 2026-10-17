package repository

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type enterpriseQuotaRepository struct{ client *dbent.Client }

func NewEnterpriseQuotaRepository(client *dbent.Client) service.EnterpriseQuotaRepository {
	return &enterpriseQuotaRepository{client: client}
}

const enterpriseQuotaColumns = `q.id, q.enterprise_id, q.scope_type, q.scope_id,
 q.daily_limit_usd, q.weekly_limit_usd, q.monthly_limit_usd, q.total_limit_usd,
 q.daily_usage_usd, q.weekly_usage_usd, q.monthly_usage_usd, q.total_usage_usd,
 q.daily_window_start, q.weekly_window_start, q.monthly_window_start,
 q.created_at, q.updated_at, q.deleted_at`

// The polymorphic scope must still exist and belong to the same enterprise.
const enterpriseQuotaActiveScope = `q.deleted_at IS NULL AND EXISTS (
 SELECT 1 FROM enterprises e WHERE e.id = q.enterprise_id AND e.deleted_at IS NULL)
 AND ((q.scope_type = 'enterprise' AND q.scope_id = q.enterprise_id)
 OR (q.scope_type = 'department' AND EXISTS (SELECT 1 FROM enterprise_departments d
 WHERE d.id = q.scope_id AND d.enterprise_id = q.enterprise_id AND d.deleted_at IS NULL))
 OR (q.scope_type = 'member' AND EXISTS (SELECT 1 FROM enterprise_members m
 WHERE m.id = q.scope_id AND m.enterprise_id = q.enterprise_id AND m.deleted_at IS NULL)))`

func (r *enterpriseQuotaRepository) GetByScope(ctx context.Context, enterpriseID int64, scopeType string, scopeID int64) (*service.EnterpriseQuota, error) {
	client := clientFromContext(ctx, r.client)
	if err := enterpriseRequireScope(ctx, client, enterpriseID, scopeType, scopeID); err != nil {
		return nil, err
	}
	items, err := enterpriseQuotaQuery(ctx, client, `SELECT `+enterpriseQuotaColumns+` FROM enterprise_quotas q
 WHERE q.enterprise_id = $1 AND q.scope_type = $2 AND q.scope_id = $3 AND `+enterpriseQuotaActiveScope,
		enterpriseID, scopeType, scopeID)
	if err != nil || len(items) == 0 {
		return nil, err
	}
	return &items[0], nil
}

func (r *enterpriseQuotaRepository) GetByScopes(ctx context.Context, enterpriseID int64, scopes []service.EnterpriseQuotaScope) ([]service.EnterpriseQuota, error) {
	client := clientFromContext(ctx, r.client)
	if len(scopes) == 0 {
		return []service.EnterpriseQuota{}, nil
	}
	if enterpriseID <= 0 {
		return nil, service.ErrEnterpriseInvalidScope
	}
	args := []any{enterpriseID}
	predicates := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		if !enterpriseQuotaValidScope(scope.ScopeType) || scope.ScopeID <= 0 || (scope.ScopeType == service.EnterpriseScopeEnterprise && scope.ScopeID != enterpriseID) {
			return nil, service.ErrEnterpriseInvalidScope
		}
		args = append(args, scope.ScopeType, scope.ScopeID)
		predicates = append(predicates, fmt.Sprintf("(q.scope_type = $%d AND q.scope_id = $%d)", len(args)-1, len(args)))
	}
	return enterpriseQuotaQuery(ctx, client, `SELECT `+enterpriseQuotaColumns+` FROM enterprise_quotas q
 WHERE q.enterprise_id = $1 AND (`+strings.Join(predicates, " OR ")+`) AND `+enterpriseQuotaActiveScope+` ORDER BY q.id`, args...)
}

func (r *enterpriseQuotaRepository) List(ctx context.Context, enterpriseID int64, scopeType string) ([]service.EnterpriseQuota, error) {
	client := clientFromContext(ctx, r.client)
	if err := enterpriseRequireEnterprise(ctx, client, enterpriseID, false); err != nil {
		return nil, err
	}
	if scopeType != "" && !enterpriseQuotaValidScope(scopeType) {
		return nil, service.ErrEnterpriseInvalidScope
	}
	return enterpriseQuotaQuery(ctx, client, `SELECT `+enterpriseQuotaColumns+` FROM enterprise_quotas q
 WHERE q.enterprise_id = $1 AND ($2 = '' OR q.scope_type = $2) AND `+enterpriseQuotaActiveScope+` ORDER BY q.id`, enterpriseID, scopeType)
}

func (r *enterpriseQuotaRepository) UpdateLimits(ctx context.Context, quota *service.EnterpriseQuota) error {
	if quota == nil {
		return service.ErrEnterpriseInvalidScope
	}
	for _, limit := range []*float64{quota.DailyLimitUSD, quota.WeeklyLimitUSD, quota.MonthlyLimitUSD, quota.TotalLimitUSD} {
		if limit != nil && (*limit < 0 || math.IsNaN(*limit) || math.IsInf(*limit, 0)) {
			return fmt.Errorf("enterprise quota limits must be finite and non-negative")
		}
	}
	return enterpriseWithTx(ctx, r.client, func(txCtx context.Context, client *dbent.Client) error {
		if err := enterpriseRequireEnterprise(txCtx, client, quota.EnterpriseID, true); err != nil {
			return err
		}
		if err := enterpriseRequireScope(txCtx, client, quota.EnterpriseID, quota.ScopeType, quota.ScopeID); err != nil {
			return err
		}
		// Limits are full replacements; usage and window columns are intentionally absent.
		rows, err := client.QueryContext(txCtx, `UPDATE enterprise_quotas SET
 daily_limit_usd = $4, weekly_limit_usd = $5, monthly_limit_usd = $6, total_limit_usd = $7, updated_at = NOW()
 WHERE enterprise_id = $1 AND scope_type = $2 AND scope_id = $3 AND deleted_at IS NULL
 RETURNING id, created_at, updated_at`, quota.EnterpriseID, quota.ScopeType, quota.ScopeID,
			quota.DailyLimitUSD, quota.WeeklyLimitUSD, quota.MonthlyLimitUSD, quota.TotalLimitUSD)
		if err != nil {
			return translatePersistenceError(err, enterpriseScopeNotFound(quota.ScopeType), nil)
		}
		defer rows.Close()
		if !rows.Next() {
			if err := rows.Err(); err != nil {
				return translatePersistenceError(err, nil, nil)
			}
			return translatePersistenceError(sql.ErrNoRows, enterpriseScopeNotFound(quota.ScopeType), nil)
		}
		return translatePersistenceError(rows.Scan(&quota.ID, &quota.CreatedAt, &quota.UpdatedAt), nil, nil)
	})
}

func (r *enterpriseQuotaRepository) ResetUsage(ctx context.Context, enterpriseID int64, scopeType string, scopeID int64, windows []string, now time.Time) error {
	if len(windows) == 0 {
		return fmt.Errorf("enterprise quota reset requires at least one window")
	}
	seen := make(map[string]bool, len(windows))
	sets := []string{"updated_at = NOW()"}
	args := []any{enterpriseID, scopeType, scopeID}
	for _, window := range windows {
		if seen[window] {
			continue
		}
		seen[window] = true
		var start time.Time
		switch window {
		case service.EnterpriseQuotaWindowDaily:
			start = timezone.StartOfDay(now)
		case service.EnterpriseQuotaWindowWeekly:
			start = timezone.StartOfWeek(now)
		case service.EnterpriseQuotaWindowMonthly:
			start = timezone.StartOfMonth(now)
		case service.EnterpriseQuotaWindowTotal:
			sets = append(sets, "total_usage_usd = 0")
			continue
		default:
			return fmt.Errorf("invalid enterprise quota reset window %q", window)
		}
		args = append(args, start)
		// window is validated above, so the column names cannot contain caller SQL.
		sets = append(sets, window+"_usage_usd = 0", fmt.Sprintf("%s_window_start = $%d", window, len(args)))
	}
	return enterpriseWithTx(ctx, r.client, func(txCtx context.Context, client *dbent.Client) error {
		if err := enterpriseRequireEnterprise(txCtx, client, enterpriseID, true); err != nil {
			return err
		}
		if err := enterpriseRequireScope(txCtx, client, enterpriseID, scopeType, scopeID); err != nil {
			return err
		}
		result, err := client.ExecContext(txCtx, `UPDATE enterprise_quotas SET `+strings.Join(sets, ", ")+`
 WHERE enterprise_id = $1 AND scope_type = $2 AND scope_id = $3 AND deleted_at IS NULL`, args...)
		if err != nil {
			return translatePersistenceError(err, enterpriseScopeNotFound(scopeType), nil)
		}
		count, err := result.RowsAffected()
		if err != nil {
			return translatePersistenceError(err, nil, nil)
		}
		if count == 0 {
			return translatePersistenceError(sql.ErrNoRows, enterpriseScopeNotFound(scopeType), nil)
		}
		return nil
	})
}

func enterpriseQuotaValidScope(scopeType string) bool {
	return scopeType == service.EnterpriseScopeEnterprise || scopeType == service.EnterpriseScopeDepartment || scopeType == service.EnterpriseScopeMember
}

func enterpriseQuotaQuery(ctx context.Context, client *dbent.Client, query string, args ...any) ([]service.EnterpriseQuota, error) {
	rows, err := client.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, translatePersistenceError(err, nil, nil)
	}
	defer rows.Close()
	items := make([]service.EnterpriseQuota, 0)
	for rows.Next() {
		var quota service.EnterpriseQuota
		if err := rows.Scan(&quota.ID, &quota.EnterpriseID, &quota.ScopeType, &quota.ScopeID,
			&quota.DailyLimitUSD, &quota.WeeklyLimitUSD, &quota.MonthlyLimitUSD, &quota.TotalLimitUSD,
			&quota.DailyUsageUSD, &quota.WeeklyUsageUSD, &quota.MonthlyUsageUSD, &quota.TotalUsageUSD,
			&quota.DailyWindowStart, &quota.WeeklyWindowStart, &quota.MonthlyWindowStart,
			&quota.CreatedAt, &quota.UpdatedAt, &quota.DeletedAt); err != nil {
			return nil, translatePersistenceError(err, nil, nil)
		}
		items = append(items, quota)
	}
	return items, translatePersistenceError(rows.Err(), nil, nil)
}
