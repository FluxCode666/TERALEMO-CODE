//go:build unit

package dto

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestEnterpriseQuotaDTOHidesUsageAndResetTimes(t *testing.T) {
	for _, visible := range []bool{false, true} {
		t.Run(map[bool]string{false: "hidden", true: "visible"}[visible], func(t *testing.T) {
			now := time.Now()
			limit := 10.0
			q := &service.EnterpriseQuota{ScopeType: service.EnterpriseScopeMember, ScopeID: 7, UsageVisible: &visible, DailyLimitUSD: &limit, DailyUsageUSD: 1, WeeklyUsageUSD: 2, MonthlyUsageUSD: 3, TotalUsageUSD: 4, DailyWindowStart: &now, WeeklyWindowStart: &now, MonthlyWindowStart: &now}
			body, err := json.Marshal(EnterpriseQuotaFromService(q))
			require.NoError(t, err)
			var result map[string]any
			require.NoError(t, json.Unmarshal(body, &result))
			require.Equal(t, visible, result["usage_visible"])
			require.EqualValues(t, 10, result["daily_limit_usd"])
			for _, key := range []string{"daily_usage_usd", "weekly_usage_usd", "monthly_usage_usd", "total_usage_usd", "daily_resets_at", "weekly_resets_at", "monthly_resets_at"} {
				require.Contains(t, result, key)
				if visible {
					require.NotNil(t, result[key], key)
				} else {
					require.Nil(t, result[key], key)
				}
			}
			require.EqualValues(t, 1, q.DailyUsageUSD)
		})
	}
	// Existing internal/overview callers return fully visible quota objects.
	result := EnterpriseQuotaFromService(&service.EnterpriseQuota{})
	require.True(t, result.UsageVisible)
	require.NotNil(t, result.TotalUsageUSD)
}
