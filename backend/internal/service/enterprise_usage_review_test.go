//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

type enterpriseSensitiveUsageRepository struct{ enterpriseUsageRepositoryStub }

func (*enterpriseSensitiveUsageRepository) GetStatsWithFilters(context.Context, usagestats.UsageLogFilters) (*usagestats.UsageStats, error) {
	accountCost := 999.0
	return &usagestats.UsageStats{TotalRequests: 1, TotalTokens: 2, TotalActualCost: 3, TotalAccountCost: &accountCost, UpstreamEndpoints: []usagestats.EndpointStat{{Endpoint: "private-upstream"}}, EndpointPaths: []usagestats.EndpointStat{{Endpoint: "private-path"}}}, nil
}
func (*enterpriseSensitiveUsageRepository) GetUsageTrendWithUsageFilters(context.Context, time.Time, time.Time, string, usagestats.UsageLogFilters) ([]usagestats.TrendDataPoint, error) {
	return []usagestats.TrendDataPoint{{Date: "2026-10-17", Requests: 1, TotalTokens: 2, ActualCost: 3, Cost: 4}}, nil
}
func (*enterpriseSensitiveUsageRepository) GetModelStatsWithUsageFiltersBySource(context.Context, time.Time, time.Time, usagestats.UsageLogFilters, string) ([]usagestats.ModelStat, error) {
	return []usagestats.ModelStat{{Model: "public-model", Requests: 1, TotalTokens: 2, ActualCost: 3, AccountCost: 999, Cost: 4}}, nil
}

func TestEnterpriseUsageAggregatesWhitelistSensitiveSourceFields(t *testing.T) {
	ctx := context.Background()
	svc := NewEnterpriseUsageService(&enterpriseSensitiveUsageRepository{})
	scope := EnterpriseUsageScope{EnterpriseID: 1}
	stats, err := svc.Stats(ctx, scope, usagestats.UsageLogFilters{})
	require.NoError(t, err)
	models, err := svc.Models(ctx, scope, usagestats.UsageLogFilters{})
	require.NoError(t, err)
	trend, err := svc.Trend(ctx, scope, usagestats.UsageLogFilters{}, "day")
	require.NoError(t, err)
	today, month, err := svc.OverviewStats(ctx, scope)
	require.NoError(t, err)
	require.Equal(t, 3.0, today)
	require.Equal(t, 3.0, month)
	for _, value := range []any{stats, models, trend, EnterpriseMemberUsage{}, EnterpriseDepartmentUsage{}} {
		encoded, err := json.Marshal(value)
		require.NoError(t, err)
		for _, private := range []string{"total_account_cost", "account_cost", "upstream_endpoints", "endpoint_paths", "private-upstream", "private-path", "999"} {
			require.NotContains(t, string(encoded), private)
		}
		require.Contains(t, string(encoded), "actual_cost")
	}
}

// Embedded legacy methods would panic if a compatibility fallback reached them.
type enterpriseUnsupportedUsageRepository struct{ UsageLogRepository }

func TestEnterpriseUsageMissingCapabilitiesCannotFallBackToLegacyQueries(t *testing.T) {
	ctx := context.Background()
	repo := &enterpriseUnsupportedUsageRepository{}
	svc := NewEnterpriseUsageService(repo)
	scope := EnterpriseUsageScope{EnterpriseID: 1}
	_, _, err := svc.List(ctx, scope, pagination.DefaultPagination(), usagestats.UsageLogFilters{})
	require.ErrorContains(t, err, "repository unavailable")
	_, err = svc.Trend(ctx, scope, usagestats.UsageLogFilters{}, "day")
	require.ErrorContains(t, err, "repository unavailable")
	_, err = svc.Models(ctx, scope, usagestats.UsageLogFilters{})
	require.ErrorContains(t, err, "repository unavailable")
	_, err = svc.APIKeyOptions(ctx, scope, usagestats.UsageLogFilters{}, "", 20)
	require.ErrorContains(t, err, "repository unavailable")
	_, err = svc.ModelOptions(ctx, scope, usagestats.UsageLogFilters{}, "", 20)
	require.ErrorContains(t, err, "repository unavailable")
	_, err = svc.Members(ctx, scope, usagestats.UsageLogFilters{})
	require.ErrorContains(t, err, "repository unavailable")
	_, err = svc.Departments(ctx, scope, usagestats.UsageLogFilters{})
	require.ErrorContains(t, err, "repository unavailable")
	common := &UsageService{usageRepo: repo}
	for _, f := range []usagestats.UsageLogFilters{{EnterpriseID: 1}, {EnterpriseDepartmentID: 2}} {
		_, err = common.GetUsageTrendWithFilters(ctx, time.Now().Add(-time.Hour), time.Now(), "day", f)
		require.ErrorContains(t, err, "require repository support")
		_, err = common.GetModelStatsWithFiltersBySource(ctx, time.Now().Add(-time.Hour), time.Now(), f, "requested")
		require.ErrorContains(t, err, "require repository support")
		_, err = common.GetGroupStatsWithFilters(ctx, time.Now().Add(-time.Hour), time.Now(), f)
		require.ErrorContains(t, err, "require repository support")
	}
}
