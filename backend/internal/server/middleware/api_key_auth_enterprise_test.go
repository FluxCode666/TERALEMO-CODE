//go:build unit

package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyAuthEnterpriseMembership(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name         string
		enterprise   *service.EnterpriseMemberContext
		subscription bool
		balance      float64
		mode, path   string
		status       int
		code         string
	}{
		{name: "enterprise disabled", enterprise: &service.EnterpriseMemberContext{EnterpriseStatus: service.EnterpriseStatusDisabled, MemberStatus: service.EnterpriseMemberStatusActive}, status: 403, code: "ENTERPRISE_DISABLED"},
		{name: "member disabled", enterprise: &service.EnterpriseMemberContext{EnterpriseStatus: service.EnterpriseStatusActive, MemberStatus: service.EnterpriseMemberStatusDisabled}, status: 403, code: "ENTERPRISE_MEMBER_DISABLED"},
		{name: "disabled on simple mode", enterprise: &service.EnterpriseMemberContext{EnterpriseStatus: service.EnterpriseStatusDisabled}, mode: config.RunModeSimple, status: 403, code: "ENTERPRISE_DISABLED"},
		{name: "disabled on billing introspection", enterprise: &service.EnterpriseMemberContext{EnterpriseStatus: service.EnterpriseStatusDisabled}, path: "/v1/sub2api/billing", status: 403, code: "ENTERPRISE_DISABLED"},
		{name: "pool denies public group", enterprise: &service.EnterpriseMemberContext{EnterpriseStatus: service.EnterpriseStatusActive, MemberStatus: service.EnterpriseMemberStatusActive, GroupPoolConfigured: true, GroupPoolIDs: []int64{7}}, status: 403, code: "ENTERPRISE_GROUP_NOT_ALLOWED"},
		{name: "whitelist denies subscription before bypass", subscription: true, enterprise: &service.EnterpriseMemberContext{EnterpriseStatus: service.EnterpriseStatusActive, MemberStatus: service.EnterpriseMemberStatusActive, GroupWhitelistConfigured: true}, status: 403, code: "ENTERPRISE_GROUP_NOT_ALLOWED"},
		{name: "enterprise ignores depleted personal balance", enterprise: &service.EnterpriseMemberContext{EnterpriseStatus: service.EnterpriseStatusActive, MemberStatus: service.EnterpriseMemberStatusActive}, balance: -5, status: 200},
		{name: "personal still rejects depleted balance", status: 403, code: "INSUFFICIENT_BALANCE"},
		{name: "personal positive balance unchanged", balance: 5, status: 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			group := &service.Group{ID: 42, Status: service.StatusActive, Hydrated: true, SubscriptionType: service.SubscriptionTypeStandard}
			if tc.subscription {
				group.SubscriptionType = service.SubscriptionTypeSubscription
			}
			key := &service.APIKey{ID: 1, UserID: 8, GroupID: &group.ID, Key: "enterprise-key", Status: service.StatusActive, Group: group, User: &service.User{ID: 8, Status: service.StatusActive, Balance: tc.balance, Enterprise: tc.enterprise}}
			repo := &stubApiKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) { return key, nil }}
			cfg := &config.Config{RunMode: config.RunModeStandard}
			if tc.mode != "" {
				cfg.RunMode = tc.mode
			}
			path := tc.path
			if path == "" {
				path = "/v1/messages"
			}
			svc := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg)
			router := gin.New()
			router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(svc, nil, cfg)))
			router.GET(path, func(c *gin.Context) { c.Status(http.StatusOK) })
			request := httptest.NewRequest(http.MethodGet, path, nil)
			request.Header.Set("x-api-key", key.Key)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, tc.status, response.Code, response.Body.String())
			if tc.code != "" {
				require.Contains(t, response.Body.String(), tc.code)
			}
		})
	}
}

func TestGoogleAPIKeyAuthEnterpriseMembership(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name   string
		member *service.EnterpriseMemberContext
		status int
		code   string
	}{
		{name: "enterprise disabled", member: &service.EnterpriseMemberContext{EnterpriseStatus: service.EnterpriseStatusDisabled}, status: 403, code: "ENTERPRISE_DISABLED"},
		{name: "member disabled", member: &service.EnterpriseMemberContext{EnterpriseStatus: service.EnterpriseStatusActive, MemberStatus: service.EnterpriseMemberStatusDisabled}, status: 403, code: "ENTERPRISE_MEMBER_DISABLED"},
		{name: "subscription not in whitelist", member: &service.EnterpriseMemberContext{EnterpriseStatus: service.EnterpriseStatusActive, MemberStatus: service.EnterpriseMemberStatusActive, GroupWhitelistConfigured: true}, status: 403, code: "ENTERPRISE_GROUP_NOT_ALLOWED"},
		{name: "enterprise skips empty personal balance", member: &service.EnterpriseMemberContext{EnterpriseStatus: service.EnterpriseStatusActive, MemberStatus: service.EnterpriseMemberStatusActive}, status: 200},
		{name: "personal balance remains enforced", status: 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			group := &service.Group{ID: 42, Status: service.StatusActive, Hydrated: true, SubscriptionType: service.SubscriptionTypeSubscription}
			key := &service.APIKey{ID: 1, UserID: 8, GroupID: &group.ID, Key: "enterprise-google-key", Status: service.StatusActive, Group: group, User: &service.User{ID: 8, Status: service.StatusActive, Enterprise: tc.member}}
			repo := &stubApiKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) { return key, nil }}
			cfg := &config.Config{RunMode: config.RunModeStandard}
			svc := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg)
			router := gin.New()
			router.Use(APIKeyAuthWithSubscriptionGoogle(svc, nil, cfg))
			router.GET("/v1beta/models", func(c *gin.Context) { c.Status(http.StatusOK) })
			request := httptest.NewRequest(http.MethodGet, "/v1beta/models", nil)
			request.Header.Set("x-goog-api-key", key.Key)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, tc.status, response.Code, response.Body.String())
			if tc.code != "" {
				require.Contains(t, response.Body.String(), tc.code)
				require.Contains(t, response.Body.String(), "google.rpc.ErrorInfo")
			}
		})
	}
}

func TestValidateAPIKeyGroupAllowedUngroupedEnterprise(t *testing.T) {
	groupID := int64(42)
	for _, tc := range []struct {
		name   string
		member *service.EnterpriseMemberContext
		want   bool
	}{
		{name: "personal key unchanged", want: true},
		{name: "unrestricted enterprise key unchanged", member: &service.EnterpriseMemberContext{}, want: true},
		{name: "configured pool rejects global account pool", member: &service.EnterpriseMemberContext{GroupPoolConfigured: true, GroupPoolIDs: []int64{42}}},
		{name: "configured whitelist rejects global account pool", member: &service.EnterpriseMemberContext{GroupWhitelistConfigured: true, GroupWhitelistIDs: []int64{42}}},
		{name: "configured empty whitelist intersection rejects", member: &service.EnterpriseMemberContext{GroupWhitelistConfigured: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, key := range []*service.APIKey{
				{User: &service.User{Enterprise: tc.member}},
				{User: &service.User{Enterprise: tc.member}, GroupID: &groupID},
				{User: &service.User{Enterprise: tc.member}, Group: &service.Group{ID: groupID}},
			} {
				require.Equal(t, tc.want, validateAPIKeyGroupAllowed(key))
			}
		})
	}
}
