//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestEnterpriseUsageSnapshotIsolationAndPreaggregates(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)
	e := enterpriseFixture(t, ctx)
	other := enterpriseFixture(t, ctx)
	dr := NewEnterpriseDepartmentRepository(client)
	a := &service.EnterpriseDepartment{EnterpriseID: e.ID, Name: "A"}
	b := &service.EnterpriseDepartment{EnterpriseID: e.ID, Name: "B"}
	require.NoError(t, dr.Create(ctx, a))
	require.NoError(t, dr.Create(ctx, b))
	memberA := enterpriseMemberFixture(t, ctx, e.ID, &a.ID)
	memberB := enterpriseMemberFixture(t, ctx, e.ID, &b.ID)
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: memberA.UserID, Key: "sk-enterprise-usage-snapshot", Name: "display-only"})
	account := mustCreateAccount(t, client, &service.Account{Name: "private-account"})
	group := mustCreateGroup(t, client, &service.Group{Name: "usage-snapshot-group"})
	now := time.Now().UTC()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	for _, v := range []struct {
		enterprise, department, user int64
		cost                         float64
	}{{e.ID, a.ID, memberA.UserID, 1}, {e.ID, b.ID, memberB.UserID, 2}, {other.ID, 0, memberA.UserID, 100}, {0, 0, memberA.UserID, 1000}} {
		log := &service.UsageLog{UserID: v.user, APIKeyID: key.ID, AccountID: account.ID, GroupID: &group.ID, Model: "gpt-enterprise", InputTokens: 3, OutputTokens: 4, ActualCost: v.cost, CreatedAt: now}
		_, err := repo.Create(ctx, log)
		require.NoError(t, err)
		if v.enterprise != 0 {
			var dept any
			if v.department != 0 {
				dept = v.department
			}
			_, err = tx.ExecContext(ctx, "UPDATE usage_logs SET enterprise_id=$1,enterprise_department_id=$2 WHERE id=$3", v.enterprise, dept, log.ID)
			require.NoError(t, err)
		}
	}
	// Poison the all-site preaggregation. Enterprise reads must still sum raw snapshots.
	_, err := tx.ExecContext(ctx, `INSERT INTO usage_dashboard_daily(bucket_date,total_requests,actual_cost) VALUES($1,99999,99999) ON CONFLICT(bucket_date) DO UPDATE SET total_requests=99999,actual_cost=99999`, start)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `INSERT INTO usage_dashboard_hourly(bucket_start,total_requests,actual_cost) VALUES($1,99999,99999) ON CONFLICT(bucket_start) DO UPDATE SET total_requests=99999,actual_cost=99999`, now.Truncate(time.Hour))
	require.NoError(t, err)
	filters := usagestats.UsageLogFilters{EnterpriseID: e.ID, StartTime: &start, EndTime: &end}
	require.False(t, shouldUseFastUsageLogTotal(filters))
	for _, granularity := range []string{"day", "hour"} {
		trend, err := repo.GetUsageTrendWithUsageFilters(ctx, start, end, granularity, filters)
		require.NoError(t, err)
		require.Len(t, trend, 1)
		require.EqualValues(t, 2, trend[0].Requests)
		require.InDelta(t, 3, trend[0].ActualCost, 1e-9)
	}
	all, page, err := repo.ListWithFilters(ctx, pagination.DefaultPagination(), filters)
	require.NoError(t, err)
	require.Len(t, all, 2)
	require.EqualValues(t, 2, page.Total)
	list, page, err := repo.ListEnterpriseUsageLogs(ctx, pagination.DefaultPagination(), filters)
	require.NoError(t, err)
	require.Len(t, list, 2)
	require.EqualValues(t, 2, page.Total)
	require.Equal(t, "display-only", list[0].APIKey.Name)
	stats, err := repo.GetStatsWithFilters(ctx, filters)
	require.NoError(t, err)
	require.EqualValues(t, 2, stats.TotalRequests)
	require.InDelta(t, 3, stats.TotalActualCost, 1e-9)
	model, err := repo.GetModelStatsWithUsageFiltersBySource(ctx, start, end, filters, usagestats.ModelSourceRequested)
	require.NoError(t, err)
	require.Len(t, model, 1)
	require.EqualValues(t, 2, model[0].Requests)
	groups, err := repo.GetGroupStatsWithUsageFilters(ctx, start, end, filters)
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.EqualValues(t, 2, groups[0].Requests)
	// Managers retain department A history after a member moves or leaves.
	memberA.DepartmentID = &b.ID
	require.NoError(t, NewEnterpriseMemberRepository(client).Update(ctx, memberA))
	require.NoError(t, NewEnterpriseMemberRepository(client).Delete(ctx, e.ID, memberA.ID))
	filters.EnterpriseDepartmentID = a.ID
	list, _, err = repo.ListEnterpriseUsageLogs(ctx, pagination.DefaultPagination(), filters)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.EqualValues(t, memberA.UserID, list[0].User.ID)
	require.Equal(t, &a.ID, list[0].DepartmentID)
	stats, err = repo.GetStatsWithFilters(ctx, filters)
	require.NoError(t, err)
	require.EqualValues(t, 1, stats.TotalRequests)
	require.InDelta(t, 1, stats.TotalActualCost, 1e-9)
	trend, err := repo.GetUsageTrendWithUsageFilters(ctx, start, end, "day", filters)
	require.NoError(t, err)
	require.Len(t, trend, 1)
	require.EqualValues(t, 1, trend[0].Requests)
	model, err = repo.GetModelStatsWithUsageFiltersBySource(ctx, start, end, filters, usagestats.ModelSourceRequested)
	require.NoError(t, err)
	require.Len(t, model, 1)
	require.EqualValues(t, 1, model[0].Requests)
	groups, err = repo.GetGroupStatsWithUsageFilters(ctx, start, end, filters)
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.EqualValues(t, 1, groups[0].Requests)
	members, err := repo.GetEnterpriseMemberUsage(ctx, filters)
	require.NoError(t, err)
	require.Len(t, members, 1)
	require.EqualValues(t, memberA.UserID, members[0].UserID)
	require.Equal(t, &a.ID, members[0].DepartmentID)
	departments, err := repo.GetEnterpriseDepartmentUsage(ctx, filters)
	require.NoError(t, err)
	require.Len(t, departments, 1)
	require.Equal(t, &a.ID, departments[0].DepartmentID)
	// Departed user's enterprise history remains queryable while personal/other-enterprise rows stay out.
	filters.EnterpriseDepartmentID = 0
	filters.UserID = memberA.UserID
	stats, err = repo.GetStatsWithFilters(ctx, filters)
	require.NoError(t, err)
	require.EqualValues(t, 1, stats.TotalRequests)
	require.InDelta(t, 1, stats.TotalActualCost, 1e-9)
}
