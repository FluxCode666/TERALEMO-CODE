package handler

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type enterpriseChartContractRepo struct {
	service.UsageLogRepository
	trend usagestats.TrendDataPoint
	model usagestats.ModelStat
}

func (r enterpriseChartContractRepo) GetUsageTrendWithUsageFilters(context.Context, time.Time, time.Time, string, usagestats.UsageLogFilters) ([]usagestats.TrendDataPoint, error) {
	return []usagestats.TrendDataPoint{r.trend}, nil
}
func (r enterpriseChartContractRepo) GetModelStatsWithUsageFiltersBySource(context.Context, time.Time, time.Time, usagestats.UsageLogFilters, string) ([]usagestats.ModelStat, error) {
	return []usagestats.ModelStat{r.model}, nil
}

func TestEnterpriseUsageChartsMatchPersonalJSON(t *testing.T) {
	repo := enterpriseChartContractRepo{
		trend: usagestats.TrendDataPoint{Date: "2026-10-18", Requests: 2, InputTokens: 3, OutputTokens: 5, CacheCreationTokens: 7, CacheReadTokens: 11, TotalTokens: 26, Cost: 4.2, ActualCost: 2.1},
		model: usagestats.ModelStat{Model: "public-model", Requests: 2, InputTokens: 3, OutputTokens: 5, CacheCreationTokens: 7, CacheReadTokens: 11, TotalTokens: 26, Cost: 4.2, ActualCost: 2.1, AccountCost: 999},
	}
	svc := service.NewEnterpriseUsageService(repo)
	scope := service.EnterpriseUsageScope{EnterpriseID: 1}
	trend, err := svc.Trend(context.Background(), scope, usagestats.UsageLogFilters{}, "day")
	require.NoError(t, err)
	models, err := svc.Models(context.Background(), scope, usagestats.UsageLogFilters{})
	require.NoError(t, err)
	for _, tc := range []struct {
		name                 string
		personal, enterprise any
	}{
		{"trend", []usagestats.TrendDataPoint{repo.trend}, trend},
		{"models", userModelStatsFromUsageStats([]usagestats.ModelStat{repo.model}), models},
	} {
		t.Run(tc.name, func(t *testing.T) {
			personal, err := json.Marshal(tc.personal)
			require.NoError(t, err)
			enterprise, err := json.Marshal(tc.enterprise)
			require.NoError(t, err)
			require.JSONEq(t, string(personal), string(enterprise))
			for _, field := range []string{"account_cost", "upstream", "999"} {
				require.NotContains(t, string(enterprise), field)
			}
		})
	}
}
