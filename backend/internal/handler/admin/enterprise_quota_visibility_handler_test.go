//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type enterpriseQuotaVisibilityHTTPStub struct {
	enterpriseManagementAPI
	visible bool
	reset   bool
}

func (s *enterpriseQuotaVisibilityHTTPStub) response(a service.EnterpriseActor) *service.EnterpriseQuota {
	visible := a.IsAdmin || s.visible
	now := time.Now()
	limit := 9.0
	return &service.EnterpriseQuota{ScopeType: service.EnterpriseScopeMember, ScopeID: 20, DailyLimitUSD: &limit, UsageVisible: &visible, DailyUsageUSD: 1, WeeklyUsageUSD: 2, MonthlyUsageUSD: 3, TotalUsageUSD: 4, DailyWindowStart: &now, WeeklyWindowStart: &now, MonthlyWindowStart: &now}
}
func (s *enterpriseQuotaVisibilityHTTPStub) ListQuotas(_ context.Context, a service.EnterpriseActor, _ int64, _ string) ([]service.EnterpriseQuota, error) {
	return []service.EnterpriseQuota{*s.response(a)}, nil
}
func (s *enterpriseQuotaVisibilityHTTPStub) UpdateQuota(_ context.Context, a service.EnterpriseActor, _ int64, _ string, _ int64, _ service.EnterpriseQuotaLimitsInput) (*service.EnterpriseQuota, error) {
	return s.response(a), nil
}
func (s *enterpriseQuotaVisibilityHTTPStub) ResetQuota(context.Context, service.EnterpriseActor, int64, string, int64, []string) error {
	s.reset = true
	return nil
}

func TestEnterpriseHTTPQuotaResponsesApplyUsageVisibility(t *testing.T) {
	for _, tc := range []struct {
		name           string
		admin, visible bool
	}{
		{"manager hidden", false, false}, {"manager visible", false, true}, {"administrator", true, false},
	} {
		for _, operation := range []string{"list", "update", "reset"} {
			t.Run(tc.name+"/"+operation, func(t *testing.T) {
				s := &enterpriseQuotaVisibilityHTTPStub{visible: tc.visible}
				base := "/api/v1/enterprise/quotas"
				if tc.admin {
					base = "/api/v1/admin/enterprises/8/quotas"
				}
				method, path, body := http.MethodGet, base, ""
				if operation == "update" {
					method = http.MethodPut
					path += "/member/20"
					body = `{"daily_limit_usd":9,"weekly_limit_usd":null,"monthly_limit_usd":null,"total_limit_usd":null}`
				}
				if operation == "reset" {
					method = http.MethodPost
					path += "/member/20/reset"
					body = `{"windows":["daily"]}`
				}
				w := enterpriseTestRequest(enterpriseTestHandler(s, "manager"), method, path, body)
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())
				var envelope struct {
					Data json.RawMessage `json:"data"`
				}
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
				var item map[string]any
				if operation == "list" {
					var items []map[string]any
					require.NoError(t, json.Unmarshal(envelope.Data, &items))
					require.Len(t, items, 1)
					item = items[0]
				} else {
					require.NoError(t, json.Unmarshal(envelope.Data, &item))
				}
				visible := tc.admin || tc.visible
				require.Equal(t, visible, item["usage_visible"])
				require.EqualValues(t, 9, item["daily_limit_usd"])
				for _, key := range []string{"daily_usage_usd", "weekly_usage_usd", "monthly_usage_usd", "total_usage_usd", "daily_resets_at", "weekly_resets_at", "monthly_resets_at"} {
					require.Contains(t, item, key)
					if visible {
						require.NotNil(t, item[key], key)
					} else {
						require.Nil(t, item[key], key)
					}
				}
				require.Equal(t, operation == "reset", s.reset)
			})
		}
	}
}
