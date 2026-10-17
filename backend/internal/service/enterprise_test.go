//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnterpriseMemberContextActive(t *testing.T) {
	var absent *EnterpriseMemberContext
	require.False(t, absent.Active())
	for _, enterpriseStatus := range []string{EnterpriseStatusActive, EnterpriseStatusDisabled, ""} {
		for _, memberStatus := range []string{EnterpriseMemberStatusActive, EnterpriseMemberStatusDisabled, ""} {
			c := &EnterpriseMemberContext{EnterpriseStatus: enterpriseStatus, MemberStatus: memberStatus}
			require.Equal(t, enterpriseStatus == EnterpriseStatusActive && memberStatus == EnterpriseMemberStatusActive, c.Active())
		}
	}
}

func TestEnterpriseMemberContextAllowsGroup(t *testing.T) {
	tests := []struct {
		name string
		ctx  *EnterpriseMemberContext
		want bool
	}{
		{"non-member", nil, true},
		{"unconfigured", &EnterpriseMemberContext{}, true},
		{"pool grant", &EnterpriseMemberContext{GroupPoolConfigured: true, GroupPoolIDs: []int64{1}}, true},
		{"outside pool", &EnterpriseMemberContext{GroupPoolConfigured: true, GroupPoolIDs: []int64{2}}, false},
		{"whitelist only", &EnterpriseMemberContext{GroupWhitelistConfigured: true, GroupWhitelistIDs: []int64{1}}, true},
		{"outside whitelist", &EnterpriseMemberContext{GroupWhitelistConfigured: true, GroupWhitelistIDs: []int64{2}}, false},
		{"empty intersection", &EnterpriseMemberContext{GroupWhitelistConfigured: true}, false},
		{"pool and whitelist", &EnterpriseMemberContext{GroupPoolConfigured: true, GroupPoolIDs: []int64{1, 2}, GroupWhitelistConfigured: true, GroupWhitelistIDs: []int64{1, 3}}, true},
		{"pool cannot override whitelist", &EnterpriseMemberContext{GroupPoolConfigured: true, GroupPoolIDs: []int64{1}, GroupWhitelistConfigured: true, GroupWhitelistIDs: []int64{2}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { require.Equal(t, tt.want, tt.ctx.AllowsGroup(1)) })
	}
}

func TestNoopEnterpriseBillingCache(t *testing.T) {
	cache := NewNoopEnterpriseBillingCache()
	require.NoError(t, cache.InvalidateEnterpriseBalance(context.Background(), 1))
	require.NoError(t, cache.InvalidateEnterpriseQuota(context.Background(), EnterpriseScopeMember, 2))
}
