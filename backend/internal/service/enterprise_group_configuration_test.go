//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnterpriseConfiguredEmptyPoolDoesNotFallBackToPublicGroups(t *testing.T) {
	for _, role := range []string{EnterpriseRoleOwner, EnterpriseRoleManager, EnterpriseRoleMember} {
		t.Run(role, func(t *testing.T) {
			f := newEnterpriseManagementTestFixture(t)
			f.e.e.GroupPoolConfigured = true
			f.e.caller = &EnterpriseMemberContext{EnterpriseID: 1, MemberID: 20, Role: role, MemberStatus: EnterpriseMemberStatusActive, EnterpriseStatus: EnterpriseStatusActive, GroupPoolConfigured: true}
			groups, err := f.s.GetAllowedGroups(context.Background(), EnterpriseActor{UserID: 20}, 1)
			require.NoError(t, err)
			require.Empty(t, groups)
		})
	}
}

func TestEnterpriseWhitelistPoolBoundaryAndDisabledScopes(t *testing.T) {
	f := newEnterpriseManagementTestFixture(t)
	actor := EnterpriseActor{IsAdmin: true}
	f.e.e.GroupPoolConfigured = true
	require.ErrorIs(t, f.s.SetGroupWhitelist(context.Background(), actor, 1, EnterpriseScopeEnterprise, 1, []int64{1}), ErrEnterpriseGroupOutOfScope)
	for _, scope := range []string{EnterpriseScopeMember, EnterpriseScopeDepartment} {
		for _, ids := range [][]int64{{1}, {}} {
			require.ErrorIs(t, f.s.SetGroupWhitelist(context.Background(), actor, 1, scope, 20, ids), ErrEnterpriseGroupScopeDisabled)
		}
	}
	require.Zero(t, f.g.writes)
	require.NoError(t, f.s.SetGroupWhitelist(context.Background(), actor, 1, EnterpriseScopeEnterprise, 1, []int64{}))
	require.NotContains(t, f.g.whitelists, EnterpriseQuotaScope{EnterpriseScopeEnterprise, 1})
}
