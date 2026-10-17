package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func wsEnterpriseMember(departmentID int64) *service.EnterpriseMemberContext {
	return &service.EnterpriseMemberContext{EnterpriseID: 10, MemberID: 30, EnterpriseStatus: service.EnterpriseStatusActive, MemberStatus: service.EnterpriseMemberStatusActive, DepartmentID: &departmentID}
}

func wsEnterpriseConnection() *service.APIKey {
	groupID := int64(77)
	return &service.APIKey{ID: 5, Key: "sk-enterprise-ws", GroupID: &groupID, User: &service.User{ID: 9, Status: service.StatusActive, Balance: 7, Enterprise: wsEnterpriseMember(20)}, Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeStandard, RateMultiplier: 3}}
}

func wsEnterpriseLatest(conn *service.APIKey) *service.APIKey {
	latest := *conn
	user := *conn.User
	user.Enterprise = wsEnterpriseMember(20)
	user.Balance = 100
	latest.User = &user
	group := *conn.Group
	group.RateMultiplier = 0.3
	latest.Group = &group
	return &latest
}

func TestRefreshOpenAIWSTurnEnterpriseContext(t *testing.T) {
	conn := wsEnterpriseConnection()
	latest := wsEnterpriseLatest(conn)
	latest.User.Enterprise.GroupWhitelistConfigured = true
	latest.User.Enterprise.GroupWhitelistIDs = []int64{77}
	got := refreshOpenAIWSTurnBillingAPIKey(context.Background(), wsTurnAPIKeyLookupFunc(func(context.Context, string) (*service.APIKey, error) { return latest, nil }), conn)
	require.NotNil(t, got)
	require.NotSame(t, conn, got)
	require.NotSame(t, conn.User, got.User)
	require.EqualValues(t, 20, *got.User.Enterprise.DepartmentID)
	require.EqualValues(t, 20, *conn.User.Enterprise.DepartmentID)
	require.Equal(t, latest.User.Balance, got.User.Balance, "the complete authenticated user refreshes")
	require.Equal(t, 0.3, got.Group.RateMultiplier)
	require.Equal(t, 3.0, conn.Group.RateMultiplier)
	latest.User.Enterprise.GroupWhitelistIDs[0] = 99
	*latest.User.Enterprise.DepartmentID = 99
	require.Equal(t, []int64{77}, got.User.Enterprise.GroupWhitelistIDs)
	require.EqualValues(t, 20, *got.User.Enterprise.DepartmentID, "a later lookup mutation cannot change an admitted turn")
}

func TestRefreshOpenAIWSTurnEnterpriseFailureAndIdentityChangesClose(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*service.APIKey, *service.APIKey)
		err    error
	}{
		{name: "lookup fails", err: errors.New("auth unavailable")},
		{name: "latest user missing", mutate: func(_, latest *service.APIKey) { latest.User = nil }},
		{name: "member removed", mutate: func(_, latest *service.APIKey) { latest.User.Enterprise = nil }},
		{name: "joined enterprise", mutate: func(conn, _ *service.APIKey) { conn.User.Enterprise = nil }},
		{name: "enterprise changed", mutate: func(_, latest *service.APIKey) { latest.User.Enterprise.EnterpriseID++ }},
		{name: "membership recreated", mutate: func(_, latest *service.APIKey) { latest.User.Enterprise.MemberID++ }},
		{name: "department changed", mutate: func(_, latest *service.APIKey) { latest.User.Enterprise.DepartmentID = new(int64(21)) }},
		{name: "department removed", mutate: func(_, latest *service.APIKey) { latest.User.Enterprise.DepartmentID = nil }},
		{name: "user changed", mutate: func(_, latest *service.APIKey) { latest.User.ID++ }},
		{name: "key changed", mutate: func(_, latest *service.APIKey) { latest.ID++ }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn := wsEnterpriseConnection()
			latest := wsEnterpriseLatest(conn)
			if tc.mutate != nil {
				tc.mutate(conn, latest)
			}
			lookup := wsTurnAPIKeyLookupFunc(func(context.Context, string) (*service.APIKey, error) { return latest, tc.err })
			got, err := refreshOpenAIWSTurnBillingAPIKeyChecked(context.Background(), lookup, conn)
			require.Nil(t, got)
			require.ErrorIs(t, err, errOpenAIWSEnterpriseBillingReconnect)
		})
	}
}

func TestRefreshOpenAIWSTurnEnterpriseRefreshesDespiteGroupChange(t *testing.T) {
	conn := wsEnterpriseConnection()
	latest := wsEnterpriseLatest(conn)
	other := int64(88)
	latest.GroupID = &other
	latest.Group.ID = other
	latest.User.Enterprise.MemberStatus = service.EnterpriseMemberStatusDisabled
	got := refreshOpenAIWSTurnBillingAPIKey(context.Background(), wsTurnAPIKeyLookupFunc(func(context.Context, string) (*service.APIKey, error) { return latest, nil }), conn)
	require.NotNil(t, got)
	require.Same(t, conn.Group, got.Group, "connection routing group remains fixed")
	require.Equal(t, service.EnterpriseMemberStatusDisabled, got.User.Enterprise.MemberStatus, "group change must not suppress enterprise status refresh")
}

func TestOpenAIWSTurnEnterpriseBeginFreezesContextAndRejectsIdentityChange(t *testing.T) {
	repo := newWSGroupPricingAPIKeyRepoStub(3)
	repo.apiKey.User.Enterprise = wsEnterpriseMember(20)
	apiKeyService := newWSGroupPricingAPIKeyService(repo)
	conn, err := apiKeyService.GetByKey(context.Background(), repo.apiKey.Key)
	require.NoError(t, err)
	var keys openAIWSTurnBillingAPIKeys
	ctx := context.WithValue(context.Background(), ctxkey.Group, conn.Group)
	require.NoError(t, repo.adminUpdate(apiKeyService, func(key *service.APIKey, _ *service.Group) { key.User.Enterprise.GroupWhitelistIDs = []int64{77} }))
	turnCtx, err := keys.begin(ctx, apiKeyService, 2, conn)
	require.NoError(t, err)
	turn2 := keys.forTurn(2, conn)
	require.EqualValues(t, 20, *turn2.User.Enterprise.DepartmentID)
	require.EqualValues(t, 20, *conn.User.Enterprise.DepartmentID)
	require.Same(t, turn2.Group, turnCtx.Value(ctxkey.Group))
	require.NoError(t, repo.adminUpdate(apiKeyService, func(key *service.APIKey, _ *service.Group) { key.User.Enterprise.GroupWhitelistIDs = []int64{78} }))
	_, err = keys.begin(ctx, apiKeyService, 3, conn)
	require.NoError(t, err)
	require.Equal(t, []int64{77}, keys.forTurn(2, conn).User.Enterprise.GroupWhitelistIDs, "previous turn keeps its own member snapshot")
	require.Equal(t, []int64{78}, keys.forTurn(3, conn).User.Enterprise.GroupWhitelistIDs)
	require.NoError(t, repo.adminUpdate(apiKeyService, func(key *service.APIKey, _ *service.Group) { key.User.Enterprise = nil }))
	_, err = keys.begin(ctx, apiKeyService, 4, conn)
	require.ErrorIs(t, err, errOpenAIWSEnterpriseBillingReconnect)
	require.NotContains(t, keys.keys, 4, "rejected identity never replaces an accepted turn")
}

func TestOpenAIWSTurnEnterpriseBeginWithoutLookupFailsClosed(t *testing.T) {
	var keys openAIWSTurnBillingAPIKeys
	conn := wsEnterpriseConnection()
	_, err := keys.begin(context.Background(), nil, 2, conn)
	require.ErrorIs(t, err, errOpenAIWSEnterpriseBillingReconnect)
	conn.User.Enterprise = nil
	_, err = keys.begin(context.Background(), nil, 2, conn)
	require.ErrorIs(t, err, errOpenAIWSEnterpriseBillingReconnect)
	require.NotContains(t, keys.keys, 2)
}

func TestOpenAIWSTurnEnterpriseBeginDetectsUngroupedJoin(t *testing.T) {
	repo := newWSGroupPricingAPIKeyRepoStub(3)
	apiKeyService := newWSGroupPricingAPIKeyService(repo)
	conn, err := apiKeyService.GetByKey(context.Background(), repo.apiKey.Key)
	require.NoError(t, err)
	conn.Group = nil
	require.NoError(t, repo.adminUpdate(apiKeyService, func(key *service.APIKey, _ *service.Group) { key.User.Enterprise = wsEnterpriseMember(20) }))
	var keys openAIWSTurnBillingAPIKeys
	_, err = keys.begin(context.Background(), apiKeyService, 2, conn)
	require.ErrorIs(t, err, errOpenAIWSEnterpriseBillingReconnect)
}

func TestOpenAIWSTurnUngroupedPersonalKeepsSnapshotAndLookupFailureCloses(t *testing.T) {
	for _, lookupErr := range []error{nil, errors.New("auth unavailable")} {
		conn := wsEnterpriseConnection()
		conn.User.Enterprise = nil
		conn.Group = nil
		latest := *conn
		latest.User = &service.User{ID: conn.User.ID, Status: service.StatusActive, Balance: 100}
		got, err := refreshOpenAIWSTurnBillingAPIKeyChecked(context.Background(), wsTurnAPIKeyLookupFunc(func(context.Context, string) (*service.APIKey, error) { return &latest, lookupErr }), conn)
		if lookupErr != nil {
			require.ErrorIs(t, err, errOpenAIWSEnterpriseBillingReconnect)
			require.Nil(t, got)
		} else {
			require.NoError(t, err)
			require.Same(t, conn, got)
			require.Equal(t, 7.0, got.User.Balance)
		}
	}
}

type wsEnterpriseRuntimeCache struct {
	service.EnterpriseBillingRuntimeCache
	balance float64
	quotas  []service.EnterpriseQuota
}

func (c *wsEnterpriseRuntimeCache) GetEnterpriseBalance(context.Context, int64) (float64, error) {
	return c.balance, nil
}
func (c *wsEnterpriseRuntimeCache) GetEnterpriseQuotas(context.Context, int64, []service.EnterpriseQuotaScope) ([]service.EnterpriseQuota, error) {
	return c.quotas, nil
}

func TestOpenAIWSTurnEnterpriseEligibilityRechecksBalanceQuotasAndAccess(t *testing.T) {
	for _, tc := range []struct {
		name    string
		balance float64
		mutate  func(*service.APIKey, []service.EnterpriseQuota)
		want    error
	}{
		{name: "allowed", balance: 5},
		{name: "enterprise balance exhausted", want: service.ErrEnterpriseBillingBalanceInsufficient},
		{name: "member disabled", balance: 5, mutate: func(k *service.APIKey, _ []service.EnterpriseQuota) {
			k.User.Enterprise.MemberStatus = service.EnterpriseMemberStatusDisabled
		}, want: service.ErrEnterpriseGatewayMemberDisabled},
		{name: "group removed", balance: 5, mutate: func(k *service.APIKey, _ []service.EnterpriseQuota) {
			k.User.Enterprise.GroupWhitelistConfigured = true
		}, want: service.ErrEnterpriseGatewayGroupNotAllowed},
		{name: "member quota exhausted", balance: 5, mutate: func(_ *service.APIKey, quotas []service.EnterpriseQuota) {
			limit := 1.0
			quotas[0].TotalLimitUSD = &limit
			quotas[0].TotalUsageUSD = 1
		}, want: service.ErrEnterpriseBillingMemberQuotaExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key := wsEnterpriseConnection()
			quotas := []service.EnterpriseQuota{{ID: 1, EnterpriseID: 10, ScopeType: "member", ScopeID: 30}, {ID: 2, EnterpriseID: 10, ScopeType: "department", ScopeID: 20}, {ID: 3, EnterpriseID: 10, ScopeType: "enterprise", ScopeID: 10}}
			if tc.mutate != nil {
				tc.mutate(key, quotas)
			}
			billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, &config.Config{}, nil)
			t.Cleanup(billing.Stop)
			billing.ConfigureEnterpriseBilling(&wsEnterpriseRuntimeCache{balance: tc.balance, quotas: quotas}, nil, nil)
			h := &OpenAIGatewayHandler{billingCacheService: billing}
			err := h.checkOpenAIWSTurnEnterpriseBilling(context.Background(), key, nil)
			if tc.want == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tc.want)
				var closeErr *service.OpenAIWSClientCloseError
				require.ErrorAs(t, err, &closeErr)
				require.Equal(t, coderws.StatusPolicyViolation, closeErr.StatusCode())
			}
		})
	}
	h := &OpenAIGatewayHandler{}
	key := wsEnterpriseConnection()
	key.User.Enterprise = nil
	require.NoError(t, h.checkOpenAIWSTurnEnterpriseBilling(context.Background(), key, nil), "personal turn must not touch billing dependencies")
}

func TestOpenAIResponsesWebSocket_EnterpriseDepartmentChangeRequiresReconnect(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModePassthrough, service.OpenAIWSIngressModeCtxPool} {
		t.Run(mode, func(t *testing.T) {
			repo := newWSGroupPricingAPIKeyRepoStub(3)
			repo.apiKey.User.Enterprise = wsEnterpriseMember(20)
			apiKeyService := newWSGroupPricingAPIKeyService(repo)
			got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{firstPayload: wsGroupPricingFirst, secondPayload: wsGroupPricingSecond, ingressMode: mode, apiKeyService: apiKeyService, apiKeyCredential: wsGroupPricingKey, afterFirstUpstreamRequest: func(*service.ChannelService) error {
				return repo.adminUpdate(apiKeyService, func(k *service.APIKey, _ *service.Group) { k.User.Enterprise = wsEnterpriseMember(21) })
			}, secondTurnCloseExpected: true, closeStatus: coderws.StatusPolicyViolation, closeReason: "please reconnect"})
			require.Len(t, got.logs, 1)
			require.NotNil(t, got.logs[0].EnterpriseDepartmentID)
			require.EqualValues(t, 20, *got.logs[0].EnterpriseDepartmentID)
			require.EqualValues(t, 10, *got.logs[0].EnterpriseID)
		})
	}
}

func TestOpenAIResponsesWebSocket_EnterpriseMembershipRefreshClosesBeforeNextTurn(t *testing.T) {
	for _, tc := range []struct {
		name          string
		initialMember bool
		mutate        func(*wsGroupPricingAPIKeyRepoStub)
		reason        string
	}{
		{name: "disabled", initialMember: true, mutate: func(repo *wsGroupPricingAPIKeyRepoStub) {
			repo.apiKey.User.Enterprise.MemberStatus = service.EnterpriseMemberStatusDisabled
		}, reason: "enterprise billing check failed"},
		{name: "group denied", initialMember: true, mutate: func(repo *wsGroupPricingAPIKeyRepoStub) { repo.apiKey.User.Enterprise.GroupWhitelistConfigured = true }, reason: "enterprise billing check failed"},
		{name: "joined", mutate: func(repo *wsGroupPricingAPIKeyRepoStub) { repo.apiKey.User.Enterprise = wsEnterpriseMember(20) }, reason: "please reconnect"},
		{name: "removed", initialMember: true, mutate: func(repo *wsGroupPricingAPIKeyRepoStub) { repo.apiKey.User.Enterprise = nil }, reason: "please reconnect"},
		{name: "changed enterprise", initialMember: true, mutate: func(repo *wsGroupPricingAPIKeyRepoStub) { repo.apiKey.User.Enterprise.EnterpriseID++ }, reason: "please reconnect"},
		{name: "lookup fails", initialMember: true, mutate: func(repo *wsGroupPricingAPIKeyRepoStub) { repo.err = errors.New("auth unavailable") }, reason: "please reconnect"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := newWSGroupPricingAPIKeyRepoStub(3)
			if tc.initialMember {
				repo.apiKey.User.Enterprise = wsEnterpriseMember(20)
			}
			apiKeyService := newWSGroupPricingAPIKeyService(repo)
			runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{firstPayload: wsGroupPricingFirst, secondPayload: wsGroupPricingSecond, apiKeyService: apiKeyService, apiKeyCredential: wsGroupPricingKey, afterFirstUpstreamRequest: func(*service.ChannelService) error {
				return repo.adminUpdate(apiKeyService, func(_ *service.APIKey, _ *service.Group) { tc.mutate(repo) })
			}, secondTurnCloseExpected: true, closeStatus: coderws.StatusPolicyViolation, closeReason: tc.reason})
		})
	}
}

func TestOpenAIResponsesWebSocket_UserDisabledAfterFirstTurn(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModePassthrough, service.OpenAIWSIngressModeCtxPool} {
		for _, enterprise := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/enterprise=%t", mode, enterprise), func(t *testing.T) {
				repo := newWSGroupPricingAPIKeyRepoStub(3)
				if enterprise {
					repo.apiKey.User.Enterprise = wsEnterpriseMember(20)
				}
				apiKeys := newWSGroupPricingAPIKeyService(repo)
				got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
					firstPayload: wsGroupPricingFirst, secondPayload: wsGroupPricingSecond, ingressMode: mode,
					apiKeyService: apiKeys, apiKeyCredential: wsGroupPricingKey,
					afterFirstUpstreamRequest: func(*service.ChannelService) error {
						return repo.adminUpdate(apiKeys, func(key *service.APIKey, _ *service.Group) { key.User.Status = service.StatusDisabled })
					},
					secondTurnCloseExpected: true, closeStatus: coderws.StatusPolicyViolation, closeReason: "user account is not active",
				})
				require.Len(t, got.logs, 1, "disabled user's second turn must neither reach upstream nor settle")
				require.Len(t, got.clientEvents, 2)
				require.Equal(t, "USER_INACTIVE", gjson.GetBytes(got.clientEvents[1], "error.code").String())
			})
		}
	}
}

func TestOpenAIWSTurnPersonalStatusRefreshIgnoresGroupCompatibility(t *testing.T) {
	for _, noGroup := range []bool{false, true} {
		conn := wsEnterpriseConnection()
		conn.User.Enterprise = nil
		latest := *conn
		latest.User = &service.User{ID: conn.User.ID, Status: service.StatusDisabled}
		latest.GroupID = new(int64(999))
		if noGroup {
			conn.Group = nil
		}
		got, err := refreshOpenAIWSTurnBillingAPIKeyChecked(context.Background(), wsTurnAPIKeyLookupFunc(func(context.Context, string) (*service.APIKey, error) { return &latest, nil }), conn)
		require.NoError(t, err)
		require.Equal(t, service.StatusDisabled, got.User.Status)
		require.Equal(t, service.StatusActive, conn.User.Status, "admitted first turn retains its active snapshot")
		require.ErrorIs(t, (&OpenAIGatewayHandler{}).checkOpenAIWSTurnEnterpriseBilling(context.Background(), got, nil), service.ErrGatewayUserInactive)
	}
}

func TestOpenAIResponsesWebSocket_QuotaErrorBeforeUpgradePreservesHTTPDetails(t *testing.T) {
	for _, scope := range []string{service.EnterpriseScopeEnterprise, service.EnterpriseScopeDepartment, service.EnterpriseScopeMember} {
		t.Run(scope, func(t *testing.T) {
			key := wsEnterpriseConnection()
			quotas := wsEnterpriseQuotas()
			for i := range quotas {
				if quotas[i].ScopeType == scope {
					quotas[i].MonthlyLimitUSD = new(float64(0))
				}
			}
			cfg := &config.Config{}
			billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
			t.Cleanup(billing.Stop)
			billing.ConfigureEnterpriseBilling(&wsEnterpriseRuntimeCache{balance: 50, quotas: quotas}, nil, nil)
			h := &OpenAIGatewayHandler{gatewayService: &service.OpenAIGatewayService{}, billingCacheService: billing, apiKeyService: &service.APIKeyService{}, concurrencyHelper: NewConcurrencyHelper(service.NewConcurrencyService(&concurrencyCacheMock{}), SSEPingFormatNone, time.Second)}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/openai/v1/responses", nil)
			c.Request.Header.Set("Upgrade", "websocket")
			c.Request.Header.Set("Connection", "Upgrade")
			c.Set(string(middleware.ContextKeyAPIKey), key)
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: key.User.ID})
			h.ResponsesWebSocket(c)
			require.Equal(t, http.StatusTooManyRequests, w.Code)
			require.NotEmpty(t, w.Header().Get("Retry-After"))
			require.Equal(t, wsEnterpriseQuotaErrorCode(scope), gjson.GetBytes(w.Body.Bytes(), "error.code").String())
			require.Equal(t, scope, gjson.GetBytes(w.Body.Bytes(), "error.metadata.scope").String())
			require.Equal(t, "monthly", gjson.GetBytes(w.Body.Bytes(), "error.metadata.window").String())
			require.NotEmpty(t, gjson.GetBytes(w.Body.Bytes(), "error.metadata.window_resets_at").String())
		})
	}
}

func TestOpenAIResponsesWebSocket_QuotaErrorAfterFirstTurnPreservesEventDetails(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModePassthrough, service.OpenAIWSIngressModeCtxPool} {
		for _, scope := range []string{service.EnterpriseScopeEnterprise, service.EnterpriseScopeDepartment, service.EnterpriseScopeMember} {
			t.Run(mode+"/"+scope, func(t *testing.T) {
				repo := newWSGroupPricingAPIKeyRepoStub(3)
				repo.apiKey.User.Enterprise = wsEnterpriseMember(20)
				apiKeys := newWSGroupPricingAPIKeyService(repo)
				cache := &wsEnterpriseRuntimeCache{balance: 50, quotas: wsEnterpriseQuotas()}
				got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
					firstPayload: wsGroupPricingFirst, secondPayload: wsGroupPricingSecond, ingressMode: mode,
					apiKeyService: apiKeys, apiKeyCredential: wsGroupPricingKey,
					configureBilling: func(cfg *config.Config, billing *service.BillingCacheService) {
						cfg.RunMode = config.RunModeStandard
						billing.ConfigureEnterpriseBilling(cache, nil, nil)
					},
					afterFirstUpstreamRequest: func(*service.ChannelService) error {
						for i := range cache.quotas {
							if cache.quotas[i].ScopeType == scope {
								cache.quotas[i].MonthlyLimitUSD = new(float64(0))
							}
						}
						return nil
					},
					secondTurnCloseExpected: true, closeStatus: coderws.StatusPolicyViolation, closeReason: "enterprise billing check failed",
				})
				require.Len(t, got.logs, 1)
				require.Len(t, got.clientEvents, 2, "structured billing error must precede the close frame")
				errEvent := got.clientEvents[1]
				require.Equal(t, "error", gjson.GetBytes(errEvent, "type").String())
				require.Equal(t, wsEnterpriseQuotaErrorCode(scope), gjson.GetBytes(errEvent, "error.code").String())
				require.Equal(t, scope, gjson.GetBytes(errEvent, "error.metadata.scope").String())
				require.Equal(t, "monthly", gjson.GetBytes(errEvent, "error.metadata.window").String())
				require.NotEmpty(t, gjson.GetBytes(errEvent, "error.metadata.window_resets_at").String())
				require.Positive(t, gjson.GetBytes(errEvent, "error.retry_after").Int())
			})
		}
	}
}

func wsEnterpriseQuotas() []service.EnterpriseQuota {
	return []service.EnterpriseQuota{{ID: 1, EnterpriseID: 10, ScopeType: service.EnterpriseScopeMember, ScopeID: 30}, {ID: 2, EnterpriseID: 10, ScopeType: service.EnterpriseScopeDepartment, ScopeID: 20}, {ID: 3, EnterpriseID: 10, ScopeType: service.EnterpriseScopeEnterprise, ScopeID: 10}}
}

func wsEnterpriseQuotaErrorCode(scope string) string {
	switch scope {
	case service.EnterpriseScopeMember:
		return "MEMBER_QUOTA_EXCEEDED"
	case service.EnterpriseScopeDepartment:
		return "DEPARTMENT_QUOTA_EXCEEDED"
	default:
		return "ENTERPRISE_QUOTA_EXCEEDED"
	}
}

func TestOpenAIResponsesWebSocket_PersonalUserStatusLookupFailureClosesBeforeSecondTurn(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModePassthrough, service.OpenAIWSIngressModeCtxPool} {
		t.Run(mode, func(t *testing.T) {
			repo := newWSGroupPricingAPIKeyRepoStub(3)
			apiKeys := newWSGroupPricingAPIKeyService(repo)
			got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
				firstPayload: wsGroupPricingFirst, secondPayload: wsGroupPricingSecond, ingressMode: mode,
				apiKeyService: apiKeys, apiKeyCredential: wsGroupPricingKey,
				afterFirstUpstreamRequest: func(*service.ChannelService) error {
					return repo.adminUpdate(apiKeys, func(_ *service.APIKey, _ *service.Group) { repo.err = errors.New("cannot confirm user state") })
				},
				secondTurnCloseExpected: true, closeStatus: coderws.StatusPolicyViolation, closeReason: "please reconnect",
			})
			require.Len(t, got.logs, 1)
			require.Len(t, got.clientEvents, 2)
			require.Equal(t, "billing_session_reconnect_required", gjson.GetBytes(got.clientEvents[1], "error.code").String())
		})
	}
}

func TestOpenAIWSTurnPersonalMissingCurrentUserCloses(t *testing.T) {
	for _, ungrouped := range []bool{false, true} {
		conn := wsEnterpriseConnection()
		conn.User.Enterprise = nil
		if ungrouped {
			conn.Group = nil
		}
		latest := *conn
		latest.User = nil
		got, err := refreshOpenAIWSTurnBillingAPIKeyChecked(context.Background(), wsTurnAPIKeyLookupFunc(func(context.Context, string) (*service.APIKey, error) { return &latest, nil }), conn)
		require.Nil(t, got)
		require.ErrorIs(t, err, errOpenAIWSEnterpriseBillingReconnect)
	}
}
