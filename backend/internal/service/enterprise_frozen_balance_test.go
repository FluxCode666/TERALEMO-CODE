//go:build unit

package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestEnterpriseFrozenBalanceVisibilityInOverviewAndIdentity(t *testing.T) {
	for _, role := range []string{EnterpriseRoleOwner, EnterpriseRoleManager, EnterpriseRoleMember} {
		for _, visibleSetting := range []bool{false, true} {
			f := newEnterpriseManagementTestFixture(t)
			f.e.e.Balance, f.e.e.FrozenBalance = 20, 7
			f.e.e.Settings.BalanceVisibleToManager, f.e.e.Settings.BalanceVisibleToMember = visibleSetting, visibleSetting
			f.e.caller = &EnterpriseMemberContext{EnterpriseID: 1, MemberID: 20, DepartmentID: enterpriseTestID(3), Role: role, EnterpriseStatus: EnterpriseStatusActive, MemberStatus: EnterpriseMemberStatusActive}
			m := f.m.items[f.e.caller.MemberID]
			m.Role = role
			f.m.items[m.ID] = m
			overview, err := f.s.Overview(context.Background(), EnterpriseActor{UserID: m.UserID}, 1)
			require.NoError(t, err)
			identity, err := f.s.Identity(context.Background(), m.UserID)
			require.NoError(t, err)
			visible := role == EnterpriseRoleOwner || visibleSetting
			require.Equal(t, visible, overview.BalanceVisible)
			require.Equal(t, visible, identity.BalanceVisible)
			if visible {
				require.EqualValues(t, 7, *overview.FrozenBalance)
				require.EqualValues(t, 7, *identity.FrozenBalance)
				require.EqualValues(t, 20, *identity.Balance)
			} else {
				require.Nil(t, overview.Balance)
				require.Nil(t, overview.FrozenBalance)
				require.Nil(t, identity.Balance)
				require.Nil(t, identity.FrozenBalance)
			}
		}
	}
}

func TestEnterpriseFrozenBalanceBlocksDeleteAndDissolve(t *testing.T) {
	for _, dissolve := range []bool{false, true} {
		f := newEnterpriseManagementTestFixture(t)
		f.e.e.FrozenBalance = .01
		f.e.ids = nil
		repo := &enterpriseDissolveTestRepo{enterpriseManagementTestEnterpriseRepo: f.e, t: t}
		f.s.enterprises = repo
		apply := f.s.DeleteEnterprise
		if dissolve {
			apply = f.s.DissolveEnterprise
		}
		require.ErrorIs(t, apply(context.Background(), EnterpriseActor{IsAdmin: true}, 1), ErrEnterpriseFrozenBalancePending)
		require.False(t, repo.deleted || repo.dissolved)
		require.Empty(t, f.a.logs)
	}
}
