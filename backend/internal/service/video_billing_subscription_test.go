//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestVideoBillingEnterpriseSubscriptionMatchesOrdinaryGateway(t *testing.T) {
	svc, repo, input := videoTaskServiceFixture(t)
	member, _ := enterpriseEligibilityFixture()
	input.APIKey.User.Enterprise = member.Enterprise
	input.APIKey.User.Balance = 0
	input.APIKey.Group.SubscriptionType = SubscriptionTypeSubscription
	input.APIKey.Group.DailyLimitUSD = f64p(1)
	input.Subscription = &UserSubscription{ID: 901}
	personal := &enterprisePersonalCacheStub{subscription: &SubscriptionCacheData{
		Status: SubscriptionStatusActive, ExpiresAt: time.Now().Add(time.Hour), DailyUsage: .2,
	}}
	enterprise := &enterpriseRuntimeCacheStub{balanceErr: errors.New("must not read wallet"), quotaErr: errors.New("must not read wallet quotas")}
	eligibility := &BillingCacheService{cache: personal, enterpriseCache: enterprise}
	ctx := context.Background()
	check := func() error {
		return eligibility.CheckBillingEligibility(ctx, input.APIKey.User, input.APIKey, input.APIKey.Group, input.Subscription, PlatformVideo)
	}
	require.NoError(t, check())
	require.Zero(t, enterprise.balanceReads)
	require.Zero(t, enterprise.quotaReads)
	require.Zero(t, personal.reads)

	v := acceptVideoFixture(t, svc, input)
	require.Equal(t, input.Subscription.ID, *v.SubscriptionID)
	require.Equal(t, member.Enterprise.EnterpriseID, v.EnterpriseID)
	handled, err := svc.ObserveVideoTaskBilling(ctx, v.Protocol, v.TaskID, &OpenAIForwardResult{
		AsyncTaskStatus: "completed", VideoCount: 1, VideoDurationSeconds: 4,
	}, false)
	require.NoError(t, err)
	require.True(t, handled)
	require.Len(t, repo.settlements, 1)
	usage := repo.settlements[0].Usage
	require.Equal(t, BillingTypeSubscription, usage.BillingType)
	require.Equal(t, input.Subscription.ID, *usage.SubscriptionID)
	require.Equal(t, member.Enterprise.EnterpriseID, *usage.EnterpriseID)
	require.InDelta(t, .4, usage.ActualCost, 1e-12)

	personal.subscription.DailyUsage = 1
	require.ErrorIs(t, check(), ErrDailyLimitExceeded)
	personal.subscription.DailyUsage = .2
	input.APIKey.User.Enterprise.MemberStatus = EnterpriseMemberStatusDisabled
	require.ErrorIs(t, check(), ErrEnterpriseGatewayMemberDisabled)
}
