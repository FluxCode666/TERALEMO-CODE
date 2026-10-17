//go:build integration

package repository

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestEnterpriseBalanceAdjustmentLedgerAndPrecision(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	client := testEntClient(t)
	er := NewEnterpriseRepository(client)
	lr := NewEnterpriseBalanceLogRepository(client)
	e := enterpriseFixture(t, ctx)
	member := enterpriseMemberFixture(t, ctx, e.ID, nil)
	adjustment := service.EnterpriseBalanceAdjustment{Operation: service.EnterpriseBalanceOperationAdd, Amount: 0.12345678, OperatorUserID: &member.UserID, Notes: "first credit"}
	first, err := er.AdjustBalance(ctx, e.ID, adjustment)
	require.NoError(t, err)
	require.Zero(t, first.BalanceBefore)
	require.Equal(t, adjustment.Amount, first.BalanceAfter)
	require.Equal(t, adjustment.Amount, first.Amount)
	require.Equal(t, adjustment.Notes, first.Notes)
	adjustment.Amount = 0.00000001
	second, err := er.AdjustBalance(ctx, e.ID, adjustment)
	require.NoError(t, err)
	require.Equal(t, first.BalanceAfter, second.BalanceBefore)
	require.Equal(t, 0.12345679, second.BalanceAfter)
	adjustment.Operation = service.EnterpriseBalanceOperationSubtract
	adjustment.Amount = 0.02345679
	third, err := er.AdjustBalance(ctx, e.ID, adjustment)
	require.NoError(t, err)
	require.Equal(t, 0.1, third.BalanceAfter)
	balance, err := er.GetBalance(ctx, e.ID)
	require.NoError(t, err)
	require.Equal(t, 0.1, balance)
	details, err := er.GetByID(ctx, e.ID)
	require.NoError(t, err)
	require.Equal(t, 0.12345679, details.TotalRecharged)
	adjustment.Amount = 0.10000001
	_, err = er.AdjustBalance(ctx, e.ID, adjustment)
	require.ErrorIs(t, err, service.ErrEnterpriseBalanceAdjustmentInsufficient)
	logs, page, err := lr.List(ctx, e.ID, pagination.PaginationParams{Page: 1, PageSize: 2})
	require.NoError(t, err)
	require.EqualValues(t, 3, page.Total)
	require.Equal(t, 2, page.Pages)
	require.Len(t, logs, 2)
	require.Equal(t, third.ID, logs[0].ID)
	require.Equal(t, second.ID, logs[1].ID)
	require.NotEmpty(t, logs[0].OperatorEmail)
	logs, page, err = lr.List(ctx, e.ID, pagination.PaginationParams{Page: 2, PageSize: 2})
	require.NoError(t, err)
	require.EqualValues(t, 3, page.Total)
	require.Len(t, logs, 1)
	require.Equal(t, first.ID, logs[0].ID)
	_, err = er.GetBalance(ctx, 1<<62)
	require.ErrorIs(t, err, service.ErrEnterpriseNotFound)
}

func TestEnterpriseBalanceAdjustmentRejectsInvalidAmounts(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	er := NewEnterpriseRepository(testEntClient(t))
	e := enterpriseFixture(t, ctx)
	for _, amount := range []float64{0, -1, math.NaN(), math.Inf(1), math.Inf(-1), 0.000000001} {
		_, err := er.AdjustBalance(ctx, e.ID, service.EnterpriseBalanceAdjustment{Operation: service.EnterpriseBalanceOperationAdd, Amount: amount})
		require.Error(t, err)
	}
	_, err := er.AdjustBalance(ctx, e.ID, service.EnterpriseBalanceAdjustment{Operation: "multiply", Amount: 1})
	require.Error(t, err)
	_, err = er.AdjustBalance(ctx, 1<<62, service.EnterpriseBalanceAdjustment{Operation: service.EnterpriseBalanceOperationAdd, Amount: 1})
	require.ErrorIs(t, err, service.ErrEnterpriseNotFound)
	logs, page, err := NewEnterpriseBalanceLogRepository(testEntClient(t)).List(ctx, e.ID, pagination.DefaultPagination())
	require.NoError(t, err)
	require.Empty(t, logs)
	require.Zero(t, page.Total)
}

func TestEnterpriseBalanceAdjustmentConcurrentSubtractNeverNegative(t *testing.T) {
	ctx := context.Background()
	er := NewEnterpriseRepository(testEntClient(t))
	e := enterpriseFixture(t, ctx)
	_, err := er.AdjustBalance(ctx, e.ID, service.EnterpriseBalanceAdjustment{Operation: service.EnterpriseBalanceOperationAdd, Amount: 5})
	require.NoError(t, err)
	const attempts = 16
	errorsCh := make(chan error, attempts)
	var wg sync.WaitGroup
	for range attempts {
		wg.Go(func() {
			_, err := er.AdjustBalance(ctx, e.ID, service.EnterpriseBalanceAdjustment{Operation: service.EnterpriseBalanceOperationSubtract, Amount: 1})
			errorsCh <- err
		})
	}
	wg.Wait()
	close(errorsCh)
	succeeded := 0
	for err := range errorsCh {
		if err == nil {
			succeeded++
		} else {
			require.True(t, errors.Is(err, service.ErrEnterpriseBalanceAdjustmentInsufficient), "unexpected concurrent error: %v", err)
		}
	}
	require.Equal(t, 5, succeeded)
	balance, err := er.GetBalance(ctx, e.ID)
	require.NoError(t, err)
	require.Zero(t, balance)
	logs, page, err := NewEnterpriseBalanceLogRepository(testEntClient(t)).List(ctx, e.ID, pagination.DefaultPagination())
	require.NoError(t, err)
	require.EqualValues(t, 6, page.Total)
	for _, log := range logs {
		require.GreaterOrEqual(t, log.BalanceAfter, 0.0)
	}
}

func TestEnterpriseBalanceAdjustmentOuterTransactionRollback(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	er := NewEnterpriseRepository(client)
	e := enterpriseFixture(t, ctx)
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	_, err := er.AdjustBalance(txCtx, e.ID, service.EnterpriseBalanceAdjustment{Operation: service.EnterpriseBalanceOperationAdd, Amount: 12})
	require.NoError(t, err)
	balance, err := er.GetBalance(txCtx, e.ID)
	require.NoError(t, err)
	require.Equal(t, 12.0, balance)
	outside, err := er.GetBalance(ctx, e.ID)
	require.NoError(t, err)
	require.Zero(t, outside)
	_, page, err := NewEnterpriseBalanceLogRepository(client).List(txCtx, e.ID, pagination.DefaultPagination())
	require.NoError(t, err)
	require.EqualValues(t, 1, page.Total)
	require.NoError(t, tx.Rollback())
	outside, err = er.GetBalance(ctx, e.ID)
	require.NoError(t, err)
	require.Zero(t, outside)
	details, err := er.GetByID(ctx, e.ID)
	require.NoError(t, err)
	require.Zero(t, details.TotalRecharged)
	_, page, err = NewEnterpriseBalanceLogRepository(client).List(ctx, e.ID, pagination.DefaultPagination())
	require.NoError(t, err)
	require.Zero(t, page.Total)
}

func TestEnterpriseQuotaReadBatchLimitsAndReset(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	client := testEntClient(t)
	qr := NewEnterpriseQuotaRepository(client)
	e := enterpriseFixture(t, ctx)
	d := &service.EnterpriseDepartment{EnterpriseID: e.ID, Name: "Quota department"}
	require.NoError(t, NewEnterpriseDepartmentRepository(client).Create(ctx, d))
	m := enterpriseMemberFixture(t, ctx, e.ID, &d.ID)
	other := enterpriseFixture(t, ctx)
	foreign := enterpriseMemberFixture(t, ctx, other.ID, nil)
	quota, err := qr.GetByScope(ctx, e.ID, service.EnterpriseScopeMember, m.ID)
	require.NoError(t, err)
	require.NotNil(t, quota)
	oldStart := time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC)
	_, err = tx.Client().ExecContext(ctx, `UPDATE enterprise_quotas SET daily_usage_usd=1.5, weekly_usage_usd=2.5,
 monthly_usage_usd=3.5, total_usage_usd=4.5, daily_window_start=$2, weekly_window_start=$2,
 monthly_window_start=$2, weekly_limit_usd=50 WHERE id=$1`, quota.ID, oldStart)
	require.NoError(t, err)
	daily, monthly, total := 0.0, 100.0, 500.0
	quota.DailyLimitUSD, quota.MonthlyLimitUSD, quota.TotalLimitUSD = &daily, &monthly, &total
	// Caller usage fields are stale zeroes. Updating limits must preserve database usage.
	require.NoError(t, qr.UpdateLimits(ctx, quota))
	quota, err = qr.GetByScope(ctx, e.ID, service.EnterpriseScopeMember, m.ID)
	require.NoError(t, err)
	require.Equal(t, &daily, quota.DailyLimitUSD)
	require.Nil(t, quota.WeeklyLimitUSD)
	require.Equal(t, &monthly, quota.MonthlyLimitUSD)
	require.Equal(t, 1.5, quota.DailyUsageUSD)
	require.Equal(t, 2.5, quota.WeeklyUsageUSD)
	require.Equal(t, 3.5, quota.MonthlyUsageUSD)
	require.Equal(t, 4.5, quota.TotalUsageUSD)
	require.True(t, quota.MonthlyWindowStart.Equal(oldStart))
	items, err := qr.GetByScopes(ctx, e.ID, []service.EnterpriseQuotaScope{
		{ScopeType: service.EnterpriseScopeEnterprise, ScopeID: e.ID},
		{ScopeType: service.EnterpriseScopeDepartment, ScopeID: d.ID},
		{ScopeType: service.EnterpriseScopeMember, ScopeID: m.ID},
		{ScopeType: service.EnterpriseScopeMember, ScopeID: m.ID},
		{ScopeType: service.EnterpriseScopeMember, ScopeID: foreign.ID},
		{ScopeType: service.EnterpriseScopeMember, ScopeID: 1 << 62},
	})
	require.NoError(t, err)
	require.Len(t, items, 3)
	empty, err := qr.GetByScopes(ctx, e.ID, nil)
	require.NoError(t, err)
	require.Empty(t, empty)
	all, err := qr.List(ctx, e.ID, "")
	require.NoError(t, err)
	require.Len(t, all, 3)
	members, err := qr.List(ctx, e.ID, service.EnterpriseScopeMember)
	require.NoError(t, err)
	require.Len(t, members, 1)
	// A non-UTC input must still use the configured global timezone and natural month.
	now := time.Date(2026, 6, 1, 1, 15, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	require.NoError(t, qr.ResetUsage(ctx, e.ID, service.EnterpriseScopeMember, m.ID, []string{"daily", "weekly", "daily"}, now))
	quota, err = qr.GetByScope(ctx, e.ID, service.EnterpriseScopeMember, m.ID)
	require.NoError(t, err)
	require.Zero(t, quota.DailyUsageUSD)
	require.Zero(t, quota.WeeklyUsageUSD)
	require.Equal(t, 3.5, quota.MonthlyUsageUSD)
	require.Equal(t, 4.5, quota.TotalUsageUSD)
	require.True(t, quota.DailyWindowStart.Equal(timezone.StartOfDay(now)))
	require.True(t, quota.WeeklyWindowStart.Equal(timezone.StartOfWeek(now)))
	require.True(t, quota.MonthlyWindowStart.Equal(oldStart))
	require.NoError(t, qr.ResetUsage(ctx, e.ID, service.EnterpriseScopeMember, m.ID, []string{"monthly", "total"}, now))
	quota, err = qr.GetByScope(ctx, e.ID, service.EnterpriseScopeMember, m.ID)
	require.NoError(t, err)
	require.Zero(t, quota.MonthlyUsageUSD)
	require.Zero(t, quota.TotalUsageUSD)
	require.True(t, quota.MonthlyWindowStart.Equal(timezone.StartOfMonth(now)))
	require.Equal(t, time.May, quota.MonthlyWindowStart.Month(), "UTC natural month is May for this instant")
	require.Equal(t, &monthly, quota.MonthlyLimitUSD)
	_, err = qr.GetByScope(ctx, e.ID, service.EnterpriseScopeMember, foreign.ID)
	require.ErrorIs(t, err, service.ErrEnterpriseInvalidScope)
	_, err = qr.List(ctx, e.ID, "untrusted_column")
	require.ErrorIs(t, err, service.ErrEnterpriseInvalidScope)
	_, err = qr.GetByScopes(ctx, e.ID, []service.EnterpriseQuotaScope{{ScopeType: "arbitrary", ScopeID: m.ID}})
	require.ErrorIs(t, err, service.ErrEnterpriseInvalidScope)
	require.Error(t, qr.ResetUsage(ctx, e.ID, service.EnterpriseScopeMember, m.ID, []string{"yearly"}, now))
	require.Error(t, qr.ResetUsage(ctx, e.ID, service.EnterpriseScopeMember, m.ID, nil, now))
	negative := -1.0
	quota.DailyLimitUSD = &negative
	require.Error(t, qr.UpdateLimits(ctx, quota))
	quota.DailyLimitUSD = &daily
	require.NoError(t, NewEnterpriseMemberRepository(client).Delete(ctx, e.ID, m.ID))
	items, err = qr.GetByScopes(ctx, e.ID, []service.EnterpriseQuotaScope{{ScopeType: service.EnterpriseScopeMember, ScopeID: m.ID}})
	require.NoError(t, err)
	require.Empty(t, items)
	require.ErrorIs(t, qr.UpdateLimits(ctx, quota), service.ErrEnterpriseInvalidScope)
}

func TestEnterpriseAuditLogPaginationFilteringAndTransaction(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	e := enterpriseFixture(t, ctx)
	m := enterpriseMemberFixture(t, ctx, e.ID, nil)
	ar := NewEnterpriseAuditLogRepository(client)
	target := m.ID
	for i, action := range []string{service.EnterpriseAuditActionMemberAdd, service.EnterpriseAuditActionQuotaUpdate, service.EnterpriseAuditActionQuotaUpdate} {
		log := &service.EnterpriseAuditLog{EnterpriseID: e.ID, ActorUserID: m.UserID, ActorRole: service.EnterpriseRoleOwner, Action: action, TargetType: "member", TargetID: &target, Detail: map[string]any{"sequence": i, "after": map[string]any{"daily": 3.5}}}
		require.NoError(t, ar.Create(ctx, log))
		require.Positive(t, log.ID)
		require.False(t, log.CreatedAt.IsZero())
	}
	items, page, err := ar.List(ctx, e.ID, pagination.PaginationParams{Page: 1, PageSize: 1}, service.EnterpriseAuditActionQuotaUpdate)
	require.NoError(t, err)
	require.EqualValues(t, 2, page.Total)
	require.Equal(t, 2, page.Pages)
	require.Len(t, items, 1)
	require.Equal(t, float64(2), items[0].Detail["sequence"])
	require.NotEmpty(t, items[0].ActorEmail)
	require.Equal(t, &target, items[0].TargetID)
	items, page, err = ar.List(ctx, e.ID, pagination.PaginationParams{Page: 2, PageSize: 1}, service.EnterpriseAuditActionQuotaUpdate)
	require.NoError(t, err)
	require.EqualValues(t, 2, page.Total)
	require.Equal(t, float64(1), items[0].Detail["sequence"])
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	log := &service.EnterpriseAuditLog{EnterpriseID: e.ID, ActorUserID: m.UserID, ActorRole: service.EnterpriseRoleOwner, Action: service.EnterpriseAuditActionQuotaReset}
	require.NoError(t, ar.Create(txCtx, log))
	require.Equal(t, map[string]any{}, log.Detail)
	items, page, err = ar.List(txCtx, e.ID, pagination.DefaultPagination(), "")
	require.NoError(t, err)
	require.EqualValues(t, 4, page.Total)
	require.Equal(t, log.ID, items[0].ID)
	_, page, err = ar.List(ctx, e.ID, pagination.DefaultPagination(), "")
	require.NoError(t, err)
	require.EqualValues(t, 3, page.Total)
	require.NoError(t, tx.Rollback())
	_, page, err = ar.List(ctx, e.ID, pagination.DefaultPagination(), "")
	require.NoError(t, err)
	require.EqualValues(t, 3, page.Total)
}
