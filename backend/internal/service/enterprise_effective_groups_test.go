//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnterpriseEffectiveGroupsPermissions(t *testing.T) {
	for _, tc := range []struct {
		name, role string
		admin      bool
		callerID   int64
		department *int64
		target     int64
		denied     bool
	}{
		{name: "admin", admin: true, target: 20},
		{name: "owner any member", role: EnterpriseRoleOwner, callerID: 10, target: 20},
		{name: "manager same department", role: EnterpriseRoleManager, callerID: 50, department: enterpriseTestID(3), target: 20},
		{name: "manager other department", role: EnterpriseRoleManager, callerID: 50, department: enterpriseTestID(4), target: 20, denied: true},
		{name: "manager missing department", role: EnterpriseRoleManager, callerID: 50, target: 20, denied: true},
		{name: "member self default", role: EnterpriseRoleMember, callerID: 20, target: 0},
		{name: "member self explicit", role: EnterpriseRoleMember, callerID: 20, target: 20},
		{name: "member other", role: EnterpriseRoleMember, callerID: 20, target: 10, denied: true},
		{name: "foreign member", role: EnterpriseRoleOwner, callerID: 10, target: 99, denied: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newEnterpriseManagementTestFixture(t)
			f.e.caller = &EnterpriseMemberContext{EnterpriseID: 1, MemberID: tc.callerID, Role: tc.role, DepartmentID: tc.department, MemberStatus: EnterpriseMemberStatusActive, EnterpriseStatus: EnterpriseStatusActive}
			f.m.items[99] = EnterpriseMember{ID: 99, EnterpriseID: 2, UserID: 99}
			out, err := f.s.GetEffectiveGroups(context.Background(), EnterpriseActor{UserID: tc.callerID, IsAdmin: tc.admin}, 1, tc.target)
			if tc.denied {
				require.ErrorIs(t, err, ErrEnterprisePermissionDenied)
				return
			}
			require.NoError(t, err)
			require.EqualValues(t, 20, out.MemberID)
		})
	}
}

func TestEnterpriseEffectiveGroupsIntersections(t *testing.T) {
	for _, tc := range []struct {
		name                           string
		pool                           bool
		poolIDs                        []int64
		enterprise, department, member []int64
		want                           []int64
		restricted                     bool
	}{
		{name: "unrestricted"},
		{name: "pool only", pool: true, poolIDs: []int64{1, 2}, want: []int64{1, 2}, restricted: true},
		{name: "configured empty pool", pool: true, want: []int64{}, restricted: true},
		{name: "enterprise only", enterprise: []int64{2, 1}, want: []int64{1, 2}, restricted: true},
		{name: "department only", department: []int64{2}, want: []int64{2}, restricted: true},
		{name: "member only", member: []int64{1}, want: []int64{1}, restricted: true},
		{name: "all levels", pool: true, poolIDs: []int64{1, 2}, enterprise: []int64{1, 2}, department: []int64{2}, member: []int64{1, 2}, want: []int64{2}, restricted: true},
		{name: "disjoint", enterprise: []int64{1}, member: []int64{2}, want: []int64{}, restricted: true},
		{name: "configured empty whitelist", enterprise: []int64{}, want: []int64{}, restricted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newEnterpriseManagementTestFixture(t)
			f.e.e.GroupPoolConfigured, f.g.pool = tc.pool, tc.poolIDs
			snapshot := &EnterpriseMemberContext{GroupPoolConfigured: tc.pool, GroupPoolIDs: tc.poolIDs}
			for _, level := range []struct {
				scope string
				id    int64
				ids   []int64
			}{{EnterpriseScopeEnterprise, 1, tc.enterprise}, {EnterpriseScopeDepartment, 3, tc.department}, {EnterpriseScopeMember, 20, tc.member}} {
				if level.ids != nil {
					f.g.whitelists[EnterpriseQuotaScope{level.scope, level.id}] = EnterpriseGroupWhitelist{GroupIDs: level.ids}
					snapshot.ApplyGroupWhitelist(level.ids)
				}
			}
			out, err := f.s.GetEffectiveGroups(context.Background(), EnterpriseActor{IsAdmin: true}, 1, 20)
			require.NoError(t, err)
			require.Equal(t, tc.restricted, out.Restricted)
			require.Equal(t, tc.want, out.EffectiveGroupIDs)
			require.NotNil(t, out.Levels.Department)
			require.NotNil(t, out.Groups)
			require.NotNil(t, out.Levels.Member.GroupIDs)
			require.Len(t, out.Groups, len(tc.want))
			for _, g := range out.Groups {
				require.True(t, snapshot.AllowsGroup(g.ID))
			}
			for _, id := range []int64{1, 2, 3} {
				found := false
				for _, actual := range out.EffectiveGroupIDs {
					found = found || actual == id
				}
				if out.Restricted {
					require.Equal(t, snapshot.AllowsGroup(id), found)
				}
			}
			data, err := json.Marshal(out)
			require.NoError(t, err)
			if !out.Restricted {
				require.Contains(t, string(data), `"effective_group_ids":null`)
				require.Contains(t, string(data), `"groups":[]`)
			}
		})
	}
	f := newEnterpriseManagementTestFixture(t)
	out, err := f.s.GetEffectiveGroups(context.Background(), EnterpriseActor{IsAdmin: true}, 1, 10)
	require.NoError(t, err)
	require.Nil(t, out.Levels.Department)
}
