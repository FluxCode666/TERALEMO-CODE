//go:build unit

package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestEnterpriseLiveAndGrokRealtimeRejectBeforeCreatingSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []string{"live", "grok realtime"} {
		for _, tc := range []struct {
			name    string
			balance float64
			mutate  func(*service.APIKey, []service.EnterpriseQuota)
			status  int
			code    string
		}{
			{name: "enterprise balance exhausted", status: http.StatusForbidden, code: "ENTERPRISE_BALANCE_INSUFFICIENT"},
			{name: "member quota exhausted", balance: 5, status: http.StatusTooManyRequests, code: "MEMBER_QUOTA_EXCEEDED", mutate: func(_ *service.APIKey, quotas []service.EnterpriseQuota) {
				quotas[0].TotalLimitUSD = new(float64(1))
				quotas[0].TotalUsageUSD = 1
			}},
			{name: "enterprise disabled", balance: 5, status: http.StatusForbidden, code: "ENTERPRISE_DISABLED", mutate: func(key *service.APIKey, _ []service.EnterpriseQuota) {
				key.User.Enterprise.EnterpriseStatus = service.EnterpriseStatusDisabled
			}},
			{name: "group denied", balance: 5, status: http.StatusForbidden, code: "ENTERPRISE_GROUP_NOT_ALLOWED", mutate: func(key *service.APIKey, _ []service.EnterpriseQuota) {
				key.User.Enterprise.GroupWhitelistConfigured = true
			}},
		} {
			t.Run(endpoint+"/"+tc.name, func(t *testing.T) {
				key := wsEnterpriseConnection()
				key.Group.AllowLive = true
				quotas := []service.EnterpriseQuota{{ID: 1, EnterpriseID: 10, ScopeType: "member", ScopeID: 30}, {ID: 2, EnterpriseID: 10, ScopeType: "department", ScopeID: 20}, {ID: 3, EnterpriseID: 10, ScopeType: "enterprise", ScopeID: 10}}
				if tc.mutate != nil {
					tc.mutate(key, quotas)
				}
				billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, &config.Config{}, nil)
				t.Cleanup(billing.Stop)
				billing.ConfigureEnterpriseBilling(&wsEnterpriseRuntimeCache{balance: tc.balance, quotas: quotas}, nil, nil)
				h := &OpenAIGatewayHandler{
					gatewayService:      &service.OpenAIGatewayService{},
					billingCacheService: billing,
					apiKeyService:       &service.APIKeyService{},
					concurrencyHelper:   NewConcurrencyHelper(service.NewConcurrencyService(&fakeConcurrencyCache{}), SSEPingFormatComment, 0),
				}
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/live", bytes.NewBufferString(`{"sdp":"v=0\r\n","session":{"model":"gpt-live-test"}}`))
				c.Request.Header.Set("Content-Type", "application/json")
				c.Set(string(middleware.ContextKeyAPIKey), key)
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: key.User.ID, Concurrency: 10})
				if endpoint == "live" {
					h.Live(c)
				} else {
					key.Group.Platform = service.PlatformGrok
					c.Request.Header.Set("Upgrade", "websocket")
					c.Request.Header.Set("Connection", "Upgrade")
					h.GrokRealtime(c)
				}
				require.Equal(t, tc.status, recorder.Code, recorder.Body.String())
				require.Contains(t, recorder.Body.String(), tc.code)
			})
		}
	}
}

func TestEnterpriseLiveCallIdentityFreezesPayer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	key := wsEnterpriseConnection()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/live", nil)
	identity := liveCallIdentity(c, key, key.User.ID, nil)
	key.User.Enterprise.EnterpriseID++
	key.User.Enterprise.MemberID++
	*key.User.Enterprise.DepartmentID++
	require.EqualValues(t, 10, identity.EnterpriseID)
	require.EqualValues(t, 20, identity.EnterpriseDepartmentID)
	require.EqualValues(t, 30, identity.EnterpriseMemberID)
}
