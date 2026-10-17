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

func TestEnterpriseWhitelistCannotGrantGroupsUnderConfiguredEmptyParent(t *testing.T) {
	for _, parent := range []string{"pool", EnterpriseScopeEnterprise, EnterpriseScopeDepartment} {
		t.Run(parent, func(t *testing.T) {
			f := newEnterpriseManagementTestFixture(t)
			switch parent {
			case "pool":
				f.e.e.GroupPoolConfigured = true
			case EnterpriseScopeEnterprise:
				f.g.whitelists[EnterpriseQuotaScope{parent, 1}] = EnterpriseGroupWhitelist{GroupIDs: []int64{}}
			case EnterpriseScopeDepartment:
				f.g.whitelists[EnterpriseQuotaScope{parent, 3}] = EnterpriseGroupWhitelist{GroupIDs: []int64{}}
			}
			err := f.s.SetGroupWhitelist(context.Background(), EnterpriseActor{IsAdmin: true}, 1, EnterpriseScopeMember, 20, []int64{1})
			require.ErrorIs(t, err, ErrEnterpriseGroupOutOfScope)
			require.Zero(t, f.g.writes)
			// Explicit cancellation remains permitted even when an ancestor denies all.
			err = f.s.SetGroupWhitelist(context.Background(), EnterpriseActor{IsAdmin: true}, 1, EnterpriseScopeMember, 20, []int64{})
			require.NoError(t, err)
			_, configured := f.g.whitelists[EnterpriseQuotaScope{EnterpriseScopeMember, 20}]
			require.False(t, configured)
		})
	}
}
