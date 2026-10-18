//go:build unit

package service

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnterpriseGatewayFallbackGroupAccess(t *testing.T) {
	const primaryID, fallbackID int64 = 10, 11
	for _, tc := range []struct {
		name         string
		member       *EnterpriseMemberContext
		subscription bool
		subscribed   bool
		wantErr      error
	}{
		{name: "pool excludes fallback returns original restriction", member: &EnterpriseMemberContext{EnterpriseStatus: EnterpriseStatusActive, MemberStatus: EnterpriseMemberStatusActive, GroupPoolConfigured: true, GroupPoolIDs: []int64{primaryID}}, wantErr: ErrClaudeCodeOnly},
		{name: "whitelist excludes fallback returns original restriction", member: &EnterpriseMemberContext{EnterpriseStatus: EnterpriseStatusActive, MemberStatus: EnterpriseMemberStatusActive, GroupWhitelistConfigured: true, GroupWhitelistIDs: []int64{primaryID}}, wantErr: ErrClaudeCodeOnly},
		{name: "pool grants exclusive fallback", member: &EnterpriseMemberContext{EnterpriseStatus: EnterpriseStatusActive, MemberStatus: EnterpriseMemberStatusActive, GroupPoolConfigured: true, GroupPoolIDs: []int64{primaryID, fallbackID}}},
		{name: "personal rules still constrain unconfigured pool", member: &EnterpriseMemberContext{EnterpriseStatus: EnterpriseStatusActive, MemberStatus: EnterpriseMemberStatusActive}, wantErr: ErrClaudeCodeOnly},
		{name: "disabled enterprise", member: &EnterpriseMemberContext{EnterpriseStatus: EnterpriseStatusDisabled, MemberStatus: EnterpriseMemberStatusActive}, wantErr: ErrEnterpriseGatewayDisabled},
		{name: "disabled member", member: &EnterpriseMemberContext{EnterpriseStatus: EnterpriseStatusActive, MemberStatus: EnterpriseMemberStatusDisabled}, wantErr: ErrEnterpriseGatewayMemberDisabled},
		{name: "subscription fallback also needs own subscription", subscription: true, member: &EnterpriseMemberContext{EnterpriseStatus: EnterpriseStatusActive, MemberStatus: EnterpriseMemberStatusActive}, wantErr: ErrClaudeCodeOnly},
		{name: "subscription fallback with valid subscription", subscription: true, subscribed: true, member: &EnterpriseMemberContext{EnterpriseStatus: EnterpriseStatusActive, MemberStatus: EnterpriseMemberStatusActive}},
		{name: "personal fallback unchanged"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			groups := map[int64]*Group{
				primaryID:  {ID: primaryID, Status: StatusActive, Platform: PlatformAnthropic, Hydrated: true, ClaudeCodeOnly: true, FallbackGroupID: new(fallbackID)},
				fallbackID: {ID: fallbackID, Status: StatusActive, Platform: PlatformAnthropic, Hydrated: true, IsExclusive: true},
			}
			if tc.subscription {
				groups[fallbackID].SubscriptionType = SubscriptionTypeSubscription
			}
			subs := &enterpriseAccessSubscriptionRepo{}
			if tc.subscribed {
				subs.groups = []int64{fallbackID}
			}
			svc := &GatewayService{groupRepo: &mockGroupRepoForGateway{groups: groups}, userSubRepo: subs}
			ctx := WithEnterpriseGatewayUser(context.Background(), &User{ID: 8, Enterprise: tc.member})
			group, _, err := svc.resolveGatewayGroup(ctx, new(primaryID))
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				require.Nil(t, group)
			} else {
				require.NoError(t, err)
				require.Equal(t, fallbackID, group.ID)
			}
		})
	}
}

func TestEnterpriseGatewayContextClearsRemovedMembership(t *testing.T) {
	ctx := WithEnterpriseGatewayUser(context.Background(), &User{ID: 8, Enterprise: &EnterpriseMemberContext{EnterpriseStatus: EnterpriseStatusDisabled}})
	ctx = WithEnterpriseGatewayUser(ctx, &User{ID: 8})
	require.NoError(t, (&GatewayService{}).checkEnterpriseResolvedGroup(ctx, &Group{ID: 42}, 42))
	personal := context.Background()
	require.Equal(t, personal, WithEnterpriseGatewayUser(personal, &User{ID: 9}), "personal requests must retain the same context")
}

func TestEnterpriseSessionEligibilityRechecksWithoutCountingRPM(t *testing.T) {
	user, quotas := enterpriseEligibilityFixture()
	user.Status = StatusActive
	user.RPMLimit = 100
	group := &Group{ID: 42, RPMLimit: 100}
	cache := &enterpriseRuntimeCacheStub{balance: 50, quotas: quotas}
	rpm := &userRPMCacheStub{}
	svc := &BillingCacheService{enterpriseCache: cache, userRPMCache: rpm}
	ctx := context.Background()
	require.NoError(t, svc.CheckBillingEligibility(ctx, user, nil, group, nil, ""))
	for range 3 {
		require.NoError(t, svc.CheckEnterpriseSessionEligibility(ctx, user, nil, group, nil, ""))
	}
	require.EqualValues(t, 1, atomic.LoadInt32(&rpm.userCalls))
	require.EqualValues(t, 1, atomic.LoadInt32(&rpm.userGroupCalls))
	require.Equal(t, 4, cache.balanceReads)
	require.Equal(t, 4, cache.quotaReads)
	cache.balance = 0
	require.ErrorIs(t, svc.CheckEnterpriseSessionEligibility(ctx, user, nil, group, nil, ""), ErrEnterpriseBillingBalanceInsufficient)
	cache.balance = 50
	quotas[0].TotalLimitUSD = new(float64(1))
	quotas[0].TotalUsageUSD = 1
	require.ErrorIs(t, svc.CheckEnterpriseSessionEligibility(ctx, user, nil, group, nil, ""), ErrEnterpriseBillingMemberQuotaExceeded)
	require.EqualValues(t, 1, atomic.LoadInt32(&rpm.userCalls), "rejected turns must not count RPM either")
}

func TestEnterpriseSessionEligibilityRequiresGroupAndDoesNotTouchPersonal(t *testing.T) {
	var absent *BillingCacheService
	require.NoError(t, absent.CheckEnterpriseSessionEligibility(context.Background(), &User{ID: 1, Status: StatusActive}, nil, nil, nil, ""))
	user, _ := enterpriseEligibilityFixture()
	user.Status = StatusActive
	require.ErrorIs(t, absent.CheckEnterpriseSessionEligibility(context.Background(), user, nil, &Group{ID: 42}, nil, ""), ErrBillingServiceUnavailable)
	require.ErrorIs(t, (&BillingCacheService{}).CheckEnterpriseSessionEligibility(context.Background(), user, nil, nil, nil, ""), ErrEnterpriseGatewayGroupNotAllowed)
}

func TestEnterpriseSessionEligibilityRejectsInactiveUserBeforePersonalOrEnterpriseChecks(t *testing.T) {
	for _, member := range []*EnterpriseMemberContext{nil, {EnterpriseID: 1}} {
		var unavailable *BillingCacheService
		user := &User{ID: 42, Status: StatusDisabled, Enterprise: member}
		require.ErrorIs(t, unavailable.CheckEnterpriseSessionEligibility(context.Background(), user, nil, nil, nil, ""), ErrGatewayUserInactive)
	}
}
