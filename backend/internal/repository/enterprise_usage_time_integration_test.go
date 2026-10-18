//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestEnterpriseUsagePreciseTimeHalfOpenAndHourlyBuckets(t *testing.T) {
	for _, zone := range []string{"Asia/Shanghai", "Asia/Kathmandu"} {
		t.Run(zone, func(t *testing.T) {
			previousZone := timezone.Name()
			t.Cleanup(func() { require.NoError(t, timezone.Init(previousZone)) })
			require.NoError(t, timezone.Init(zone))
			// All fixtures and poisoned aggregates are rolled back by t.Cleanup.
			tx := testEntTx(t)
			ctx := dbent.NewTxContext(context.Background(), tx)
			client := tx.Client()
			repo := newUsageLogRepositoryWithSQL(client, tx)
			svc := service.NewEnterpriseUsageService(repo)
			e := enterpriseFixture(t, ctx)
			other := enterpriseFixture(t, ctx)
			root := treeDepartment(t, ctx, client, e.ID, nil, "总部")
			var departments []*service.EnterpriseDepartment
			var members []*service.EnterpriseMember
			for i := range 3 {
				d := treeDepartment(t, ctx, client, e.ID, &root.ID, fmt.Sprintf("D%d", i))
				departments = append(departments, d)
				members = append(members, enterpriseMemberFixture(t, ctx, e.ID, &d.ID))
			}
			key := mustCreateApiKey(t, client, &service.APIKey{UserID: members[0].UserID, Key: "sk-precise-time", Name: "precise"})
			account := mustCreateAccount(t, client, &service.Account{Name: "precise-time"})
			start := time.Date(2026, 10, 17, 10, 25, 0, 0, timezone.Location())
			end := start.Add(24 * time.Hour)
			var includedIDs []int64
			for i, row := range []struct {
				at         time.Time
				member     int
				model      string
				cost       float64
				enterprise int64
				included   bool
			}{
				{start.Add(-time.Microsecond), 0, "outside-start", 999, e.ID, false},
				{start, 0, "heavy", 4, e.ID, true},
				{start.Add(35 * time.Minute), 1, "small", 2, e.ID, true},
				{time.Date(2026, 10, 18, 0, 0, 0, 0, timezone.Location()), 2, "tiny", 1, e.ID, true},
				{end.Add(-time.Microsecond), 0, "heavy", 3, e.ID, true},
				{end, 0, "outside-end", 999, e.ID, false},
				{start, 0, "foreign", 9999, other.ID, false},
				{start, 0, "personal", 9999, 0, false},
			} {
				log := &service.UsageLog{UserID: members[row.member].UserID, APIKeyID: key.ID, AccountID: account.ID, RequestID: fmt.Sprintf("precise-%d", i), Model: "upstream", RequestedModel: row.model, InputTokens: 1, OutputTokens: 2, CacheCreationTokens: 3, CacheReadTokens: 4, ActualCost: row.cost, EnterpriseID: &row.enterprise, EnterpriseDepartmentID: &departments[row.member].ID, CreatedAt: row.at}
				_, err := repo.Create(ctx, log)
				require.NoError(t, err)
				if row.included {
					includedIDs = append(includedIDs, log.ID)
				}
			}
			_, err := tx.ExecContext(ctx, `INSERT INTO usage_dashboard_hourly(bucket_start,total_requests,actual_cost) VALUES($1,99999,99999) ON CONFLICT(bucket_start) DO UPDATE SET total_requests=99999,actual_cost=99999`, start.Truncate(time.Hour))
			require.NoError(t, err)
			// The DB session stays UTC; query offsets deliberately differ from the application timezone.
			queryStart, queryEnd := start.UTC(), end.In(time.FixedZone("request", -4*60*60))
			filters := usagestats.UsageLogFilters{StartTime: &queryStart, EndTime: &queryEnd}
			scope := service.EnterpriseUsageScope{EnterpriseID: e.ID}
			list, page, err := svc.List(ctx, scope, pagination.DefaultPagination(), filters)
			require.NoError(t, err)
			require.EqualValues(t, 4, page.Total)
			var ids []int64
			for _, log := range list {
				ids = append(ids, log.ID)
			}
			require.ElementsMatch(t, includedIDs, ids)
			stats, err := svc.Stats(ctx, scope, filters)
			require.NoError(t, err)
			require.Equal(t, &service.EnterpriseUsageStats{TotalRequests: 4, TotalTokens: 40, TotalActualCost: 10}, stats)
			models, err := svc.Models(ctx, scope, filters)
			require.NoError(t, err)
			require.Len(t, models, 3)
			var modelRequests int64
			for _, model := range models {
				require.Contains(t, []string{"heavy", "small", "tiny"}, model.Model)
				modelRequests += model.Requests
			}
			require.EqualValues(t, 4, modelRequests)
			options, err := svc.ModelOptions(ctx, scope, filters, "", 20)
			require.NoError(t, err)
			require.ElementsMatch(t, []service.EnterpriseUsageModelOption{{Model: "heavy", Requests: 2}, {Model: "small", Requests: 1}, {Model: "tiny", Requests: 1}}, options)
			memberUsage, err := svc.Members(ctx, scope, filters)
			require.NoError(t, err)
			require.Len(t, memberUsage, 3)
			var memberRequests int64
			for _, member := range memberUsage {
				memberRequests += member.Requests
			}
			require.EqualValues(t, 4, memberRequests)
			departmentUsage, err := svc.Departments(ctx, scope, filters)
			require.NoError(t, err)
			require.Len(t, departmentUsage, 3)
			var departmentRequests int64
			for _, department := range departmentUsage {
				departmentRequests += department.Requests
			}
			require.EqualValues(t, 4, departmentRequests)
			wantDates := []string{"2026-10-17 10:00", "2026-10-17 11:00", "2026-10-18 00:00", "2026-10-18 10:00"}
			trend, err := svc.Trend(ctx, scope, filters, "hour")
			require.NoError(t, err)
			require.Len(t, trend, 4)
			for i, point := range trend {
				require.Equal(t, wantDates[i], point.Date)
				require.EqualValues(t, 1, point.Requests)
			}
			for _, dimension := range []string{"models", "members", "departments"} {
				points, err := svc.SeriesTrend(ctx, scope, filters, dimension, "hour", 1)
				require.NoError(t, err)
				require.Len(t, points, 4)
				var requests, tokens int64
				var cost, otherCost float64
				for i, point := range points {
					require.Equal(t, wantDates[i], point.Date)
					requests += point.Requests
					tokens += point.TotalTokens
					cost += point.ActualCost
					if (point.Model != nil && *point.Model == "__other__") || (point.UserID != nil && *point.UserID == 0) || (point.DepartmentID != nil && *point.DepartmentID == 0) {
						otherCost += point.ActualCost
					}
				}
				require.EqualValues(t, 4, requests)
				require.EqualValues(t, 40, tokens)
				require.Equal(t, 10.0, cost)
				require.Equal(t, 3.0, otherCost)
			}
			for _, role := range []string{service.EnterpriseRoleOwner, service.EnterpriseRoleManager, service.EnterpriseRoleMember} {
				scope.UserID = members[0].UserID
				scope.Member = &service.EnterpriseMemberContext{EnterpriseID: e.ID, Role: role, MemberStatus: "active", DepartmentID: &departments[0].ID}
				stats, err := svc.Stats(ctx, scope, filters)
				require.NoError(t, err)
				if role == service.EnterpriseRoleOwner {
					require.EqualValues(t, 4, stats.TotalRequests)
				} else {
					require.EqualValues(t, 2, stats.TotalRequests)
					require.Equal(t, 7.0, stats.TotalActualCost)
				}
			}
		})
	}
}

func TestEnterpriseUsageRolling24HoursAcrossMidnight(t *testing.T) {
	previousZone := timezone.Name()
	t.Cleanup(func() { require.NoError(t, timezone.Init(previousZone)) })
	require.NoError(t, timezone.Init("Asia/Shanghai"))
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)
	svc := service.NewEnterpriseUsageService(repo)
	e := enterpriseFixture(t, ctx)
	member := enterpriseMemberFixture(t, ctx, e.ID, nil)
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: member.UserID, Key: "sk-rolling-24", Name: "rolling"})
	account := mustCreateAccount(t, client, &service.Account{Name: "rolling-24"})
	floor := time.Date(2026, 10, 17, 10, 0, 0, 0, timezone.Location())
	for i := range 25 {
		for _, minute := range []int{0, 30} {
			_, err := repo.Create(ctx, &service.UsageLog{UserID: member.UserID, APIKeyID: key.ID, AccountID: account.ID, RequestID: fmt.Sprintf("rolling-%d-%d", i, minute), Model: "rolling", ActualCost: 1, EnterpriseID: &e.ID, CreatedAt: floor.Add(time.Duration(i)*time.Hour + time.Duration(minute)*time.Minute)})
			require.NoError(t, err)
		}
	}
	for _, offset := range []time.Duration{0, 25 * time.Minute} {
		start := floor.Add(offset).UTC()
		end := start.Add(24 * time.Hour)
		filters := usagestats.UsageLogFilters{StartTime: &start, EndTime: &end}
		scope := service.EnterpriseUsageScope{EnterpriseID: e.ID}
		trend, err := svc.Trend(ctx, scope, filters, "hour")
		require.NoError(t, err)
		wantBuckets := 24
		if offset != 0 {
			wantBuckets = 25
		}
		require.Len(t, trend, wantBuckets)
		var requests int64
		for i, point := range trend {
			require.Equal(t, floor.Add(time.Duration(i)*time.Hour).Format("2006-01-02 15:00"), point.Date)
			requests += point.Requests
		}
		require.EqualValues(t, 48, requests)
		for _, dimension := range []string{"models", "members", "departments"} {
			points, err := svc.SeriesTrend(ctx, scope, filters, dimension, "hour", 8)
			require.NoError(t, err)
			require.Len(t, points, wantBuckets)
			for i, point := range points {
				require.Equal(t, trend[i].Date, point.Date)
				require.Equal(t, trend[i].Requests, point.Requests)
			}
		}
	}
}
