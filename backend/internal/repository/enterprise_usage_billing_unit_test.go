//go:build unit

package repository

import (
	"context"
	"database/sql"
	"math"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUsageBillingEnterpriseEffectsLocksWalletBeforeSortedQuotas(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	mock.ExpectBegin()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	mock.ExpectQuery(`(?s)UPDATE enterprises.*SET balance = balance - \$1.*RETURNING balance`).WithArgs(2.5, int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(-1.25))
	mock.ExpectQuery(`(?s)WITH locked AS MATERIALIZED.*ORDER BY id FOR UPDATE.*UPDATE enterprise_quotas.*CASE WHEN q.daily_window_start.*CASE WHEN q.weekly_window_start.*CASE WHEN q.monthly_window_start.*total_usage_usd = q.total_usage_usd \+ \$1.*SELECT COUNT\(\*\) FROM updated`).
		WithArgs(2.5, int64(1), int64(2), int64(3), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), 3).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))
	mock.ExpectCommit()
	result := &service.UsageBillingApplyResult{}
	err = (&usageBillingRepository{}).applyUsageBillingEffects(ctx, tx, &service.UsageBillingCommand{
		EnterpriseID: 1, EnterpriseDepartmentID: 2, EnterpriseMemberID: 3, EnterpriseBalanceCost: 2.5,
	}, result)
	require.NoError(t, err)
	require.Equal(t, -1.25, *result.NewEnterpriseBalance)
	require.True(t, result.BalanceOverdrafted)
	require.Nil(t, result.NewBalance)
	require.NoError(t, tx.Commit())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageBillingEnterpriseMissingQuotaFailsClosed(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	mock.ExpectBegin()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	mock.ExpectQuery(`WITH locked AS MATERIALIZED`).
		WithArgs(1.0, int64(1), int64(0), int64(3), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), 2).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectRollback()
	err = incrementUsageBillingEnterpriseQuotas(ctx, tx, &service.UsageBillingCommand{EnterpriseID: 1, EnterpriseMemberID: 3, EnterpriseBalanceCost: 1})
	require.ErrorIs(t, err, service.ErrBillingServiceUnavailable)
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageBillingEnterpriseMissingWallet(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	mock.ExpectBegin()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	mock.ExpectQuery(`UPDATE enterprises`).WithArgs(1.0, int64(1)).WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()
	_, err = deductUsageBillingEnterpriseBalance(ctx, tx, 1, 1)
	require.ErrorIs(t, err, service.ErrEnterpriseNotFound)
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageBillingEnterpriseRejectsPersonalWalletOrSubscriptionMix(t *testing.T) {
	for _, cmd := range []*service.UsageBillingCommand{
		{EnterpriseID: 1, EnterpriseMemberID: 2, BalanceCost: 1},
		{EnterpriseID: 1, EnterpriseMemberID: 2, EnterpriseBalanceCost: 1, BalanceCost: 1},
		{EnterpriseID: 1, EnterpriseMemberID: 2, EnterpriseBalanceCost: 1, SubscriptionCost: 1},
		{EnterpriseID: 1, EnterpriseBalanceCost: 1},
		{EnterpriseMemberID: 2, EnterpriseBalanceCost: 1},
		{EnterpriseID: 1, EnterpriseMemberID: 2, EnterpriseBalanceCost: -1},
		{EnterpriseID: 1, EnterpriseMemberID: 2, EnterpriseBalanceCost: math.NaN()},
		{EnterpriseID: 1, EnterpriseMemberID: 2, EnterpriseBalanceCost: math.Inf(1)},
		{EnterpriseID: 1, EnterpriseMemberID: 2, EnterpriseBalanceCost: math.Inf(-1)},
	} {
		require.ErrorIs(t, (&usageBillingRepository{}).applyUsageBillingEffects(context.Background(), nil, cmd, &service.UsageBillingApplyResult{}), service.ErrBillingServiceUnavailable)
	}
}
