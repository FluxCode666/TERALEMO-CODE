//go:build unit

package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func identityQuotaFixture() *service.EnterpriseQuota {
	now := timezone.Now()
	day, week, month := timezone.StartOfDay(now), timezone.StartOfWeek(now), timezone.StartOfMonth(now)
	daily, weekly, monthly := 10.0, 0.0, 100.0
	return &service.EnterpriseQuota{
		EnterpriseID: 2, ScopeType: service.EnterpriseScopeMember, ScopeID: 7,
		DailyLimitUSD: &daily, WeeklyLimitUSD: &weekly, MonthlyLimitUSD: &monthly,
		DailyUsageUSD: 2.5, WeeklyUsageUSD: 0, MonthlyUsageUSD: 96, TotalUsageUSD: 120,
		DailyWindowStart: &day, WeeklyWindowStart: &week, MonthlyWindowStart: &month,
	}
}

func TestEnterpriseIdentityMemberQuotaJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, scenario := range []string{"visible", "hidden", "expired", "null-quota", "personal"} {
		t.Run(scenario, func(t *testing.T) {
			balance, frozen := 42.5, 5.0
			departmentID, departmentName := int64(3), "Engineering / Platform"
			identity := &service.EnterpriseIdentity{
				ID: 2, Name: "Acme", Role: service.EnterpriseRoleMember,
				DepartmentID: &departmentID, DepartmentName: &departmentName,
				MemberStatus: "active", EnterpriseStatus: "active", BalanceVisible: scenario != "hidden",
				Balance: &balance, FrozenBalance: &frozen, MemberQuota: identityQuotaFixture(),
			}
			if scenario == "expired" {
				past := timezone.StartOfDay(timezone.Now()).AddDate(0, 0, -1)
				identity.MemberQuota.DailyWindowStart = &past
			}
			if scenario == "null-quota" {
				identity.MemberQuota = nil
			}
			if scenario == "personal" {
				identity = nil
			}
			for _, endpoint := range []string{"auth/me", "user/profile"} {
				t.Run(endpoint, func(t *testing.T) {
					user := &service.User{ID: 11, Email: "member@example.com", Username: "Member", Role: service.RoleUser, Status: service.StatusActive, Balance: 987.65}
					users := service.NewUserService(&userHandlerRepoStub{user: user}, nil, nil, nil)
					w := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(w)
					c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/"+endpoint, nil)
					c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: user.ID})
					if endpoint == "auth/me" {
						h := NewAuthHandler(nil, nil, users, nil, nil, nil, nil, nil)
						h.enterpriseIdentity = identityReaderStub{identity: identity}
						h.GetCurrentUser(c)
						// Browser QA consumes this actual handler response without
						// maintaining a separate hand-written auth/me contract.
						t.Logf("AUTH_ME_FIXTURE %s %s", scenario, w.Body.String())
					} else {
						h := NewUserHandler(users, nil, nil, nil, nil, nil)
						h.enterpriseIdentity = identityReaderStub{identity: identity}
						h.GetProfile(c)
					}
					require.Equal(t, http.StatusOK, w.Code, w.Body.String())
					var payload struct {
						Data struct {
							Enterprise map[string]json.RawMessage `json:"enterprise"`
						} `json:"data"`
					}
					require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
					enterprise := payload.Data.Enterprise
					if identity == nil {
						require.Nil(t, enterprise)
						return
					}
					require.JSONEq(t, `3`, string(enterprise["department_id"]))
					require.JSONEq(t, `"Engineering / Platform"`, string(enterprise["department_name"]))
					if scenario == "hidden" {
						require.JSONEq(t, `null`, string(enterprise["balance"]))
						require.JSONEq(t, `null`, string(enterprise["frozen_balance"]))
					} else {
						require.JSONEq(t, `42.5`, string(enterprise["balance"]))
					}
					require.Contains(t, enterprise, "member_quota")
					if identity.MemberQuota == nil {
						require.JSONEq(t, `null`, string(enterprise["member_quota"]))
						return
					}
					var quota map[string]json.RawMessage
					require.NoError(t, json.Unmarshal(enterprise["member_quota"], &quota))
					require.Len(t, quota, 14)
					for key, value := range map[string]string{
						"scope_type": `"member"`, "scope_id": `7`, "usage_visible": `true`,
						"daily_limit_usd": `10`, "weekly_limit_usd": `0`, "monthly_limit_usd": `100`, "total_limit_usd": `null`,
						"weekly_usage_usd": `0`, "monthly_usage_usd": `96`, "total_usage_usd": `120`,
					} {
						require.Contains(t, quota, key)
						require.JSONEq(t, value, string(quota[key]), key)
					}
					wantDaily := "2.5"
					if scenario == "expired" {
						wantDaily = "0"
					}
					require.JSONEq(t, wantDaily, string(quota["daily_usage_usd"]))
					now := timezone.Now()
					for key, want := range map[string]time.Time{
						"daily_resets_at":   timezone.StartOfDay(now).AddDate(0, 0, 1),
						"weekly_resets_at":  timezone.StartOfWeek(now).AddDate(0, 0, 7),
						"monthly_resets_at": timezone.StartOfMonth(now).AddDate(0, 1, 0),
					} {
						require.Contains(t, quota, key)
						var got time.Time
						require.NoError(t, json.Unmarshal(quota[key], &got))
						require.True(t, want.Equal(got), "%s: want %s, got %s", key, want, got)
					}
					require.NotContains(t, quota, "DailyLimitUSD")
					require.NotContains(t, quota, "DailyWindowStart")
					require.Equal(t, 2.5, identity.MemberQuota.DailyUsageUSD, "conversion must not mutate accounting")
				})
			}
		})
	}
}
