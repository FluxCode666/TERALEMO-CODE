//go:build integration

package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type enterpriseBillingFixture struct {
	enterprise *service.Enterprise
	department *service.EnterpriseDepartment
	member     *service.EnterpriseMember
	key        *service.APIKey
}

func newEnterpriseBillingFixture(t *testing.T) enterpriseBillingFixture {
	t.Helper()
	ctx := context.Background()
	client := testEntClient(t)
	e := enterpriseFixture(t, ctx)
	d := &service.EnterpriseDepartment{EnterpriseID: e.ID, Name: "Billing"}
	require.NoError(t, NewEnterpriseDepartmentRepository(client).Create(ctx, d))
	m := enterpriseMemberFixture(t, ctx, e.ID, &d.ID)
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: m.UserID, Name: "enterprise-billing", Key: "sk-ent-" + uuid.NewString()})
	_, err := integrationDB.ExecContext(ctx, `UPDATE enterprises SET balance = 10 WHERE id = $1`, e.ID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE users SET balance = 100 WHERE id = $1`, m.UserID)
	require.NoError(t, err)
	return enterpriseBillingFixture{e, d, m, key}
}

func (f enterpriseBillingFixture) command(cost float64) *service.UsageBillingCommand {
	return &service.UsageBillingCommand{RequestID: uuid.NewString(), APIKeyID: f.key.ID, UserID: f.member.UserID,
		EnterpriseID: f.enterprise.ID, EnterpriseDepartmentID: f.department.ID, EnterpriseMemberID: f.member.ID, EnterpriseBalanceCost: cost}
}

func TestUsageBillingEnterpriseAtomicDedupWindowsAndOverdraft(t *testing.T) {
	ctx := context.Background()
	f := newEnterpriseBillingFixture(t)
	repo := NewUsageBillingRepository(testEntClient(t), integrationDB)
	old := timezone.StartOfMonth(timezone.Now()).AddDate(0, -1, 0)
	_, err := integrationDB.ExecContext(ctx, `UPDATE enterprise_quotas SET daily_usage_usd=8, weekly_usage_usd=8, monthly_usage_usd=8,
		total_usage_usd=20, daily_window_start=$2, weekly_window_start=$2, monthly_window_start=$2 WHERE enterprise_id=$1`, f.enterprise.ID, old)
	require.NoError(t, err)
	cmd := f.command(1.25)
	first, err := repo.Apply(ctx, cmd)
	require.NoError(t, err)
	require.True(t, first.Applied)
	require.Nil(t, first.NewBalance)
	require.Equal(t, 8.75, *first.NewEnterpriseBalance)
	require.False(t, first.BalanceOverdrafted)
	replay, err := repo.Apply(ctx, cmd)
	require.NoError(t, err)
	require.False(t, replay.Applied)

	now := timezone.Now()
	rows, err := integrationDB.QueryContext(ctx, `SELECT daily_usage_usd, weekly_usage_usd, monthly_usage_usd, total_usage_usd,
		daily_window_start, weekly_window_start, monthly_window_start FROM enterprise_quotas WHERE enterprise_id=$1`, f.enterprise.ID)
	require.NoError(t, err)
	count := 0
	for rows.Next() {
		var daily, weekly, monthly, total float64
		var day, week, month time.Time
		require.NoError(t, rows.Scan(&daily, &weekly, &monthly, &total, &day, &week, &month))
		require.Equal(t, []float64{1.25, 1.25, 1.25, 21.25}, []float64{daily, weekly, monthly, total})
		require.True(t, day.Equal(timezone.StartOfDay(now)))
		require.True(t, week.Equal(timezone.StartOfWeek(now)))
		require.True(t, month.Equal(timezone.StartOfMonth(now)))
		count++
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	require.Equal(t, 3, count)

	second, err := repo.Apply(ctx, f.command(10))
	require.NoError(t, err)
	require.Equal(t, -1.25, *second.NewEnterpriseBalance)
	require.True(t, second.BalanceOverdrafted)
	var personal, enterprise, used float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance FROM users WHERE id=$1`, f.member.UserID).Scan(&personal))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance FROM enterprises WHERE id=$1`, f.enterprise.ID).Scan(&enterprise))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT daily_usage_usd FROM enterprise_quotas WHERE scope_type='member' AND scope_id=$1`, f.member.ID).Scan(&used))
	require.Equal(t, 100.0, personal)
	require.Equal(t, -1.25, enterprise)
	require.Equal(t, 11.25, used, "a current natural window accumulates without resetting")
}

func TestUsageBillingEnterpriseMissingQuotaRollsBackBalanceAndDedup(t *testing.T) {
	ctx := context.Background()
	f := newEnterpriseBillingFixture(t)
	_, err := integrationDB.ExecContext(ctx, `DELETE FROM enterprise_quotas WHERE scope_type='department' AND scope_id=$1`, f.department.ID)
	require.NoError(t, err)
	cmd := f.command(1)
	_, err = NewUsageBillingRepository(testEntClient(t), integrationDB).Apply(ctx, cmd)
	require.ErrorIs(t, err, service.ErrBillingServiceUnavailable)
	var balance float64
	var dedup int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance FROM enterprises WHERE id=$1`, f.enterprise.ID).Scan(&balance))
	require.Equal(t, 10.0, balance)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id=$1 AND api_key_id=$2`, cmd.RequestID, cmd.APIKeyID).Scan(&dedup))
	require.Zero(t, dedup)
}

func TestUsageBillingEnterpriseWithoutDepartmentUsesTwoScopes(t *testing.T) {
	ctx := context.Background()
	f := newEnterpriseBillingFixture(t)
	member := enterpriseMemberFixture(t, ctx, f.enterprise.ID, nil)
	key := mustCreateApiKey(t, testEntClient(t), &service.APIKey{UserID: member.UserID, Name: "enterprise-no-department", Key: "sk-ent-" + uuid.NewString()})
	cmd := f.command(1)
	cmd.UserID, cmd.APIKeyID, cmd.EnterpriseMemberID, cmd.EnterpriseDepartmentID = member.UserID, key.ID, member.ID, 0
	result, err := NewUsageBillingRepository(testEntClient(t), integrationDB).Apply(ctx, cmd)
	require.NoError(t, err)
	require.Equal(t, 9.0, *result.NewEnterpriseBalance)
	var scopes int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM enterprise_quotas WHERE enterprise_id=$1 AND total_usage_usd=1`, f.enterprise.ID).Scan(&scopes))
	require.Equal(t, 2, scopes)
}

func TestUsageBillingEnterpriseSubscriptionLeavesEnterpriseWalletAndQuotasUntouched(t *testing.T) {
	ctx := context.Background()
	f := newEnterpriseBillingFixture(t)
	client := testEntClient(t)
	group := mustCreateGroup(t, client, &service.Group{Name: "ent-sub-" + uuid.NewString(), SubscriptionType: service.SubscriptionTypeSubscription})
	subscription := mustCreateSubscription(t, client, &service.UserSubscription{UserID: f.member.UserID, GroupID: group.ID})
	cmd := f.command(0)
	cmd.SubscriptionID, cmd.SubscriptionCost = &subscription.ID, 2.5
	result, err := NewUsageBillingRepository(client, integrationDB).Apply(ctx, cmd)
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.Nil(t, result.NewEnterpriseBalance)
	require.Nil(t, result.NewBalance)
	var balance, quotaUsage, subscriptionUsage float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance FROM enterprises WHERE id=$1`, f.enterprise.ID).Scan(&balance))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT SUM(total_usage_usd) FROM enterprise_quotas WHERE enterprise_id=$1`, f.enterprise.ID).Scan(&quotaUsage))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT daily_usage_usd FROM user_subscriptions WHERE id=$1`, subscription.ID).Scan(&subscriptionUsage))
	require.Equal(t, 10.0, balance)
	require.Zero(t, quotaUsage)
	require.Equal(t, 2.5, subscriptionUsage)
}

func TestUsageBillingEnterpriseConcurrentMembersHaveConsistentLockOrder(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	f := newEnterpriseBillingFixture(t)
	m2 := enterpriseMemberFixture(t, ctx, f.enterprise.ID, &f.department.ID)
	key2 := mustCreateApiKey(t, testEntClient(t), &service.APIKey{UserID: m2.UserID, Name: "enterprise-billing-2", Key: "sk-ent-" + uuid.NewString()})
	repo := NewUsageBillingRepository(testEntClient(t), integrationDB)
	start := make(chan struct{})
	errors := make(chan error, 16)
	var wg sync.WaitGroup
	for i := range 16 {
		cmd := f.command(0.25)
		if i%2 == 1 {
			cmd.UserID, cmd.APIKeyID, cmd.EnterpriseMemberID = m2.UserID, key2.ID, m2.ID
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := repo.Apply(ctx, cmd)
			errors <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err, "parallel members must settle without deadlocks")
	}
	var balance, enterpriseUsage, departmentUsage, memberUsage float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance FROM enterprises WHERE id=$1`, f.enterprise.ID).Scan(&balance))
	for _, scope := range []struct {
		kind string
		id   int64
		dest *float64
	}{
		{"enterprise", f.enterprise.ID, &enterpriseUsage}, {"department", f.department.ID, &departmentUsage}, {"member", f.member.ID, &memberUsage},
	} {
		require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT total_usage_usd FROM enterprise_quotas WHERE scope_type=$1 AND scope_id=$2`, scope.kind, scope.id).Scan(scope.dest))
	}
	require.Equal(t, 6.0, balance)
	require.Equal(t, 4.0, enterpriseUsage)
	require.Equal(t, 4.0, departmentUsage)
	require.Equal(t, 2.0, memberUsage)
}

func TestUsageBillingEnterpriseUsageLogAllInsertPaths(t *testing.T) {
	ctx := context.Background()
	f := newEnterpriseBillingFixture(t)
	client := testEntClient(t)
	account := mustCreateAccount(t, client, &service.Account{Name: "enterprise-log-" + uuid.NewString(), Type: service.AccountTypeAPIKey})
	repo := NewUsageLogRepository(client, integrationDB).(*usageLogRepository)
	for _, path := range []string{"single", "batch", "best-effort", "single-fallback"} {
		t.Run(path, func(t *testing.T) {
			log := &service.UsageLog{UserID: f.member.UserID, APIKeyID: f.key.ID, AccountID: account.ID, Model: "enterprise-model",
				EnterpriseID: &f.enterprise.ID, EnterpriseDepartmentID: &f.department.ID, RequestID: fmt.Sprintf("ent-log-%s-%s", path, uuid.NewString())}
			switch path {
			case "single":
				_, err := repo.createSingle(ctx, integrationDB, log)
				require.NoError(t, err)
			case "batch":
				_, err := repo.Create(ctx, log)
				require.NoError(t, err)
			case "best-effort":
				require.NoError(t, repo.CreateBestEffort(ctx, log))
			case "single-fallback":
				require.NoError(t, execUsageLogInsertNoResult(ctx, integrationDB, prepareUsageLogInsert(log)))
			}
			var enterpriseID, departmentID int64
			require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT enterprise_id, enterprise_department_id FROM usage_logs WHERE request_id=$1 AND api_key_id=$2`, log.RequestID, log.APIKeyID).Scan(&enterpriseID, &departmentID))
			require.Equal(t, f.enterprise.ID, enterpriseID)
			require.Equal(t, f.department.ID, departmentID)
		})
	}
}
