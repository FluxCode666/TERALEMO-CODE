package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

func wsEnterpriseMember(departmentID int64) *service.EnterpriseMemberContext {
	return &service.EnterpriseMemberContext{EnterpriseID: 10, MemberID: 30, EnterpriseStatus: service.EnterpriseStatusActive, MemberStatus: service.EnterpriseMemberStatusActive, DepartmentID: &departmentID}
}

func wsEnterpriseConnection() *service.APIKey {
	groupID := int64(77)
	return &service.APIKey{ID: 5, Key: "sk-enterprise-ws", GroupID: &groupID, User: &service.User{ID: 9, Balance: 7, Enterprise: wsEnterpriseMember(20)}, Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeStandard, RateMultiplier: 3}}
}

func wsEnterpriseLatest(conn *service.APIKey) *service.APIKey {
	latest := *conn
	user := *conn.User
	user.Enterprise = wsEnterpriseMember(21)
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
	require.EqualValues(t, 21, *got.User.Enterprise.DepartmentID)
	require.EqualValues(t, 20, *conn.User.Enterprise.DepartmentID)
	require.Equal(t, conn.User.Balance, got.User.Balance, "only enterprise user context refreshes")
	require.Equal(t, 0.3, got.Group.RateMultiplier)
	require.Equal(t, 3.0, conn.Group.RateMultiplier)
	latest.User.Enterprise.GroupWhitelistIDs[0] = 99
	*latest.User.Enterprise.DepartmentID = 99
	require.Equal(t, []int64{77}, got.User.Enterprise.GroupWhitelistIDs)
	require.EqualValues(t, 21, *got.User.Enterprise.DepartmentID, "a later lookup mutation cannot change an admitted turn")
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
	require.NoError(t, repo.adminUpdate(apiKeyService, func(key *service.APIKey, _ *service.Group) { key.User.Enterprise = wsEnterpriseMember(21) }))
	turnCtx, err := keys.begin(ctx, apiKeyService, 2, conn)
	require.NoError(t, err)
	turn2 := keys.forTurn(2, conn)
	require.EqualValues(t, 21, *turn2.User.Enterprise.DepartmentID)
	require.EqualValues(t, 20, *conn.User.Enterprise.DepartmentID)
	require.Same(t, turn2.Group, turnCtx.Value(ctxkey.Group))
	require.NoError(t, repo.adminUpdate(apiKeyService, func(key *service.APIKey, _ *service.Group) { key.User.Enterprise = wsEnterpriseMember(22) }))
	_, err = keys.begin(ctx, apiKeyService, 3, conn)
	require.NoError(t, err)
	require.EqualValues(t, 21, *keys.forTurn(2, conn).User.Enterprise.DepartmentID, "previous turn keeps its own member snapshot")
	require.EqualValues(t, 22, *keys.forTurn(3, conn).User.Enterprise.DepartmentID)
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
	ctx, err := keys.begin(context.Background(), nil, 2, conn)
	require.NoError(t, err)
	require.NotNil(t, ctx)
	require.Same(t, conn, keys.forTurn(2, conn))
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

func TestOpenAIWSTurnUngroupedPersonalKeepsSnapshotAndLookupFailureBehavior(t *testing.T) {
	for _, lookupErr := range []error{nil, errors.New("auth unavailable")} {
		conn := wsEnterpriseConnection()
		conn.User.Enterprise = nil
		conn.Group = nil
		latest := *conn
		latest.User = &service.User{ID: conn.User.ID, Balance: 100}
		got, err := refreshOpenAIWSTurnBillingAPIKeyChecked(context.Background(), wsTurnAPIKeyLookupFunc(func(context.Context, string) (*service.APIKey, error) { return &latest, lookupErr }), conn)
		require.NoError(t, err)
		require.Same(t, conn, got)
		require.Equal(t, 7.0, got.User.Balance)
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

func TestOpenAIResponsesWebSocket_EnterpriseDepartmentRefreshReachesUsageLog(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModePassthrough, service.OpenAIWSIngressModeCtxPool} {
		t.Run(mode, func(t *testing.T) {
			repo := newWSGroupPricingAPIKeyRepoStub(3)
			repo.apiKey.User.Enterprise = wsEnterpriseMember(20)
			apiKeyService := newWSGroupPricingAPIKeyService(repo)
			got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{firstPayload: wsGroupPricingFirst, secondPayload: wsGroupPricingSecond, ingressMode: mode, apiKeyService: apiKeyService, apiKeyCredential: wsGroupPricingKey, afterFirstUpstreamRequest: func(*service.ChannelService) error {
				return repo.adminUpdate(apiKeyService, func(k *service.APIKey, _ *service.Group) { k.User.Enterprise = wsEnterpriseMember(21) })
			}})
			require.Len(t, got.logs, 2)
			require.NotNil(t, got.logs[0].EnterpriseDepartmentID)
			require.NotNil(t, got.logs[1].EnterpriseDepartmentID)
			require.EqualValues(t, 20, *got.logs[0].EnterpriseDepartmentID)
			require.EqualValues(t, 21, *got.logs[1].EnterpriseDepartmentID)
			require.EqualValues(t, 10, *got.logs[1].EnterpriseID)
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
