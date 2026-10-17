//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type enterpriseQuotaVisibilityListStub struct {
	*enterpriseManagementTestQuotas
}

func (s *enterpriseQuotaVisibilityListStub) List(_ context.Context, _ int64, scope string) ([]EnterpriseQuota, error) {
	items := make([]EnterpriseQuota, 0, len(s.items))
	for _, q := range s.items {
		if scope == "" || scope == q.ScopeType {
			items = append(items, q)
		}
	}
	return items, nil
}

func TestEnterpriseQuotaReadVisibilitySeparatesLimitsFromUsage(t *testing.T) {
	for _, tc := range []struct {
		name, role        string
		admin, edit, view bool
		want              []int64
	}{
		{"administrator", EnterpriseRoleMember, true, false, false, []int64{1, 3, 20, 30, 40, 60}},
		{"owner", EnterpriseRoleOwner, false, false, false, []int64{1, 3, 20, 30, 40, 60}},
		{"manager edits hidden same-department member limits", EnterpriseRoleManager, false, true, false, []int64{20}},
		{"manager without view or edit", EnterpriseRoleManager, false, false, false, nil},
		{"manager views same department", EnterpriseRoleManager, false, false, true, []int64{3, 20, 40, 60}},
		{"member views self", EnterpriseRoleMember, false, false, true, []int64{20}},
		{"member hidden self", EnterpriseRoleMember, false, false, false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newEnterpriseManagementTestFixture(t)
			f.e.e.Settings.ManagerCanEditMemberQuota = tc.edit
			f.e.e.Settings.MemberQuotaVisibleToManager = tc.view
			f.e.e.Settings.DepartmentQuotaVisibleToManager = tc.view
			f.e.e.Settings.MemberQuotaVisibleToMember = tc.view
			memberID := int64(50)
			if tc.role == EnterpriseRoleMember {
				memberID = 20
			}
			f.e.caller = &EnterpriseMemberContext{EnterpriseID: 1, MemberID: memberID, Role: tc.role, DepartmentID: enterpriseTestID(3), MemberStatus: EnterpriseMemberStatusActive, EnterpriseStatus: EnterpriseStatusActive}
			for _, item := range []struct {
				id, department int64
				role           string
			}{{30, 4, EnterpriseRoleMember}, {40, 3, EnterpriseRoleManager}, {60, 3, EnterpriseRoleOwner}} {
				f.m.items[item.id] = EnterpriseMember{ID: item.id, EnterpriseID: 1, UserID: item.id, DepartmentID: enterpriseTestID(item.department), Role: item.role, Status: EnterpriseMemberStatusActive}
				f.q.items[EnterpriseQuotaScope{EnterpriseScopeMember, item.id}] = EnterpriseQuota{EnterpriseID: 1, ScopeType: EnterpriseScopeMember, ScopeID: item.id, DailyLimitUSD: enterpriseTestFloat(10), DailyUsageUSD: 8}
			}
			f.s.quotas = &enterpriseQuotaVisibilityListStub{f.q}
			items, err := f.s.ListQuotas(context.Background(), EnterpriseActor{UserID: 20, IsAdmin: tc.admin}, 1, "")
			require.NoError(t, err)
			ids := make([]int64, 0, len(items))
			for _, q := range items {
				ids = append(ids, q.ScopeID)
				require.NotNil(t, q.UsageVisible)
				require.Equal(t, tc.admin || tc.role == EnterpriseRoleOwner || tc.view, *q.UsageVisible)
			}
			require.ElementsMatch(t, tc.want, ids)
		})
	}
}

func TestEnterpriseQuotaWriteVisibilityPreservesAccounting(t *testing.T) {
	for _, tc := range []struct {
		name, role  string
		admin, view bool
	}{
		{"administrator", EnterpriseRoleMember, true, false},
		{"owner", EnterpriseRoleOwner, false, false},
		{"manager hidden usage", EnterpriseRoleManager, false, false},
		{"manager visible usage", EnterpriseRoleManager, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newEnterpriseManagementTestFixture(t)
			f.e.e.Settings.ManagerCanEditMemberQuota = true
			f.e.e.Settings.MemberQuotaVisibleToManager = tc.view
			f.e.caller = &EnterpriseMemberContext{EnterpriseID: 1, MemberID: 50, Role: tc.role, DepartmentID: enterpriseTestID(3), MemberStatus: EnterpriseMemberStatusActive, EnterpriseStatus: EnterpriseStatusActive}
			q, err := f.s.UpdateQuota(context.Background(), EnterpriseActor{UserID: 50, IsAdmin: tc.admin}, 1, EnterpriseScopeMember, 20, EnterpriseQuotaLimitsInput{DailyLimitUSD: enterpriseTestFloat(9)})
			require.NoError(t, err)
			require.NotNil(t, q.UsageVisible)
			require.Equal(t, tc.admin || tc.role == EnterpriseRoleOwner || tc.view, *q.UsageVisible)
			require.EqualValues(t, 9, *q.DailyLimitUSD)
			stored := f.q.items[EnterpriseQuotaScope{EnterpriseScopeMember, 20}]
			require.EqualValues(t, 7, stored.DailyUsageUSD)
			require.Nil(t, stored.UsageVisible, "response metadata must not be persisted")
		})
	}
}
