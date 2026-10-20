//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEnterpriseIdentityAlwaysCarriesOwnMemberQuota(t *testing.T) {
	for _, tc := range []struct {
		name    string
		role    string
		setting bool
	}{
		{"owner", EnterpriseRoleOwner, false},
		{"manager", EnterpriseRoleManager, true},
		{"member with legacy switch off", EnterpriseRoleMember, false},
		{"member with legacy switch on", EnterpriseRoleMember, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newEnterpriseManagementTestFixture(t)
			f.e.e.Settings.MemberQuotaVisibleToMember = tc.setting
			f.e.caller = &EnterpriseMemberContext{EnterpriseID: 1, MemberID: 20, DepartmentID: enterpriseTestID(3), Role: tc.role, EnterpriseStatus: EnterpriseStatusActive, MemberStatus: EnterpriseMemberStatusActive}
			m := f.m.items[20]
			m.Role = tc.role
			f.m.items[20] = m
			identity, err := f.s.Identity(context.Background(), m.UserID)
			require.NoError(t, err)
			require.NotNil(t, identity.MemberQuota)
			require.Equal(t, EnterpriseScopeMember, identity.MemberQuota.ScopeType)
			require.EqualValues(t, 20, identity.MemberQuota.ScopeID)
			require.EqualValues(t, 7, identity.MemberQuota.DailyUsageUSD)
		})
	}
}

func TestEnterpriseIdentityWithoutQuotaRowReturnsNilMemberQuota(t *testing.T) {
	f := newEnterpriseManagementTestFixture(t)
	f.e.caller = &EnterpriseMemberContext{EnterpriseID: 1, MemberID: 10, Role: EnterpriseRoleOwner, EnterpriseStatus: EnterpriseStatusActive, MemberStatus: EnterpriseMemberStatusActive}
	delete(f.q.items, EnterpriseQuotaScope{EnterpriseScopeMember, 10})
	identity, err := f.s.Identity(context.Background(), 10)
	require.NoError(t, err)
	require.Nil(t, identity.MemberQuota)
}

func TestEnterpriseIdentityMemberQuotaIsIndependentFromBalanceVisibility(t *testing.T) {
	// Balance hiding must never strip the caller's own quota from the identity.
	f := newEnterpriseManagementTestFixture(t)
	f.e.e.Balance = 100
	f.e.e.Settings.BalanceVisibleToMember = false
	f.e.caller = &EnterpriseMemberContext{EnterpriseID: 1, MemberID: 20, DepartmentID: enterpriseTestID(3), Role: EnterpriseRoleMember, EnterpriseStatus: EnterpriseStatusActive, MemberStatus: EnterpriseMemberStatusActive}
	identity, err := f.s.Identity(context.Background(), 20)
	require.NoError(t, err)
	require.False(t, identity.BalanceVisible)
	require.Nil(t, identity.Balance)
	require.NotNil(t, identity.MemberQuota)
	require.Nil(t, identity.MemberQuota.DailyLimitUSD, "no limit configured means unlimited")
	require.EqualValues(t, 7, identity.MemberQuota.DailyUsageUSD)
}

func TestEnterpriseIdentityMemberQuotaReturnsRawWindows(t *testing.T) {
	// The service returns the stored row; the DTO layer zeroes expired-window usage.
	f := newEnterpriseManagementTestFixture(t)
	f.e.caller = &EnterpriseMemberContext{EnterpriseID: 1, MemberID: 20, DepartmentID: enterpriseTestID(3), Role: EnterpriseRoleMember, EnterpriseStatus: EnterpriseStatusActive, MemberStatus: EnterpriseMemberStatusActive}
	past := time.Now().Add(-48 * time.Hour)
	q := f.q.items[EnterpriseQuotaScope{EnterpriseScopeMember, 20}]
	q.DailyWindowStart = &past
	f.q.items[EnterpriseQuotaScope{EnterpriseScopeMember, 20}] = q
	identity, err := f.s.Identity(context.Background(), 20)
	require.NoError(t, err)
	require.NotNil(t, identity.MemberQuota)
	require.EqualValues(t, 7, identity.MemberQuota.DailyUsageUSD)
}
