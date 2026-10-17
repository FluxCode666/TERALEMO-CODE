//go:build unit

package service

import (
	"context"
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
		{name: "pool excludes fallback", member: &EnterpriseMemberContext{EnterpriseStatus: EnterpriseStatusActive, MemberStatus: EnterpriseMemberStatusActive, GroupPoolConfigured: true, GroupPoolIDs: []int64{primaryID}}, wantErr: ErrEnterpriseGatewayGroupNotAllowed},
		{name: "whitelist excludes fallback", member: &EnterpriseMemberContext{EnterpriseStatus: EnterpriseStatusActive, MemberStatus: EnterpriseMemberStatusActive, GroupWhitelistConfigured: true, GroupWhitelistIDs: []int64{primaryID}}, wantErr: ErrEnterpriseGatewayGroupNotAllowed},
		{name: "pool grants exclusive fallback", member: &EnterpriseMemberContext{EnterpriseStatus: EnterpriseStatusActive, MemberStatus: EnterpriseMemberStatusActive, GroupPoolConfigured: true, GroupPoolIDs: []int64{primaryID, fallbackID}}},
		{name: "personal rules still constrain unconfigured pool", member: &EnterpriseMemberContext{EnterpriseStatus: EnterpriseStatusActive, MemberStatus: EnterpriseMemberStatusActive}, wantErr: ErrEnterpriseGatewayGroupNotAllowed},
		{name: "disabled enterprise", member: &EnterpriseMemberContext{EnterpriseStatus: EnterpriseStatusDisabled, MemberStatus: EnterpriseMemberStatusActive}, wantErr: ErrEnterpriseGatewayDisabled},
		{name: "disabled member", member: &EnterpriseMemberContext{EnterpriseStatus: EnterpriseStatusActive, MemberStatus: EnterpriseMemberStatusDisabled}, wantErr: ErrEnterpriseGatewayMemberDisabled},
		{name: "subscription fallback also needs own subscription", subscription: true, member: &EnterpriseMemberContext{EnterpriseStatus: EnterpriseStatusActive, MemberStatus: EnterpriseMemberStatusActive}, wantErr: ErrEnterpriseGatewayGroupNotAllowed},
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
