//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type publicUsageStatsRepoStub struct {
	dailyUsageRepoStub
	stats []usagestats.ModelStat
	keyID int64
}

func (s *publicUsageStatsRepoStub) GetAPIKeyDashboardStats(context.Context, int64) (*usagestats.UserDashboardStats, error) {
	return &usagestats.UserDashboardStats{}, nil
}

func (s *publicUsageStatsRepoStub) GetModelStatsWithFilters(_ context.Context, _, _ time.Time, _, keyID, _, _ int64, _ *int16, _ *bool, _ *int8) ([]usagestats.ModelStat, error) {
	s.keyID = keyID
	return s.stats, nil
}

type publicUsageUserRepoStub struct{ service.UserRepository }

func (*publicUsageUserRepoStub) GetByID(context.Context, int64) (*service.User, error) {
	return &service.User{ID: 42, Balance: 10}, nil
}

func (*publicUsageUserRepoStub) GetUserAvatar(context.Context, int64) (*service.UserAvatar, error) {
	return nil, nil
}

func TestGatewayUsageModelStatsWhitelistOmitsUpstreamCosts(t *testing.T) {
	for _, path := range []string{"/v1/usage", "/antigravity/v1/usage"} {
		for _, payer := range []string{"personal", "enterprise"} {
			for _, mode := range []string{"wallet", "quota", "subscription"} {
				t.Run(path+"/"+payer+"/"+mode, func(t *testing.T) {
					repo := &publicUsageStatsRepoStub{stats: []usagestats.ModelStat{{
						Model: "gpt-test", Requests: 2, InputTokens: 10, OutputTokens: 20,
						CacheCreationTokens: 3, CacheReadTokens: 4, TotalTokens: 37,
						Cost: 0.25, ActualCost: 0.2, AccountCost: 911.0,
					}}}
					reader := &usageEnterpriseIdentityStub{}
					if payer == "enterprise" {
						reader.identity = &service.EnterpriseIdentity{ID: 7, EnterpriseStatus: service.EnterpriseStatusActive, MemberStatus: service.EnterpriseMemberStatusActive}
					}
					h := &GatewayHandler{
						usageService:             service.NewUsageService(repo, nil, nil, nil),
						userService:              service.NewUserService(&publicUsageUserRepoStub{}, nil, nil, nil),
						enterpriseIdentityReader: reader,
					}
					key := &service.APIKey{ID: 8, UserID: 42, Status: service.StatusAPIKeyActive}
					if mode == "quota" {
						key.Quota = 5
					} else if mode == "subscription" {
						key.Group = &service.Group{SubscriptionType: service.SubscriptionTypeSubscription}
					}
					router := gin.New()
					router.GET(path, func(c *gin.Context) {
						c.Set(string(middleware.ContextKeyAPIKey), key)
						c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
						h.Usage(c)
					})
					response := httptest.NewRecorder()
					router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
					require.Equal(t, http.StatusOK, response.Code, response.Body.String())
					require.Equal(t, int64(8), repo.keyID)
					var payload map[string]any
					require.NoError(t, json.Unmarshal(response.Body.Bytes(), &payload))
					stats := payload["model_stats"].([]any)
					require.Len(t, stats, 1)
					require.Equal(t, map[string]any{
						"model": "gpt-test", "requests": 2.0, "input_tokens": 10.0, "output_tokens": 20.0,
						"cache_creation_tokens": 3.0, "cache_read_tokens": 4.0, "total_tokens": 37.0,
						"cost": 0.25, "actual_cost": 0.2,
					}, stats[0])
					for _, field := range []string{"account_cost", "total_account_cost", "today_account_cost"} {
						require.NotContains(t, response.Body.String(), `"`+field+`"`)
					}
				})
			}
		}
	}
}
