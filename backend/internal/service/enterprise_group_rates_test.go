//go:build unit

package service

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

type enterpriseManagementGroupRates struct {
	*enterpriseManagementTestEnterpriseGroups
	rates map[int64]*float64
	tx    bool
}

func (r *enterpriseManagementGroupRates) GetAllowedGroups(context.Context, int64) ([]EnterpriseAllowedGroup, error) {
	groups := make([]EnterpriseAllowedGroup, 0, len(r.pool))
	for _, id := range r.pool {
		groups = append(groups, EnterpriseAllowedGroup{GroupID: id, RateMultiplier: r.rates[id]})
	}
	return groups, nil
}

func (r *enterpriseManagementGroupRates) SetAllowedGroups(ctx context.Context, id int64, groups []EnterpriseAllowedGroup) error {
	r.tx = ctx.Value(enterpriseManagementTestTxKey{}) == true
	r.rates = make(map[int64]*float64, len(groups))
	ids := make([]int64, 0, len(groups))
	for _, group := range groups {
		ids = append(ids, group.GroupID)
		r.rates[group.GroupID] = group.RateMultiplier
	}
	return r.SetAllowedGroupIDs(ctx, id, ids)
}

type enterpriseManagementEffectiveRates struct {
	UserGroupRateRepository
	rates map[int64]float64
	user  int64
	err   error
}

func (r *enterpriseManagementEffectiveRates) GetEffectiveByUserAndGroup(context.Context, int64, int64) (*float64, error) {
	panic("group listing must use the batch query")
}

func (r *enterpriseManagementEffectiveRates) GetEffectiveByUserID(_ context.Context, id int64) (map[int64]float64, error) {
	r.user = id
	return r.rates, r.err
}

func enterpriseManagementWithRates(f *enterpriseManagementTestFixture) *enterpriseManagementGroupRates {
	r := &enterpriseManagementGroupRates{enterpriseManagementTestEnterpriseGroups: f.g, rates: map[int64]*float64{}}
	f.s.enterpriseGroups = r
	return r
}

func TestEnterpriseGroupRatesSaveAndAudit(t *testing.T) {
	f := newEnterpriseManagementTestFixture(t)
	r := enterpriseManagementWithRates(f)
	r.pool = []int64{1, 2}
	r.rates[1], r.rates[2] = enterpriseTestFloat(0.8), enterpriseTestFloat(0.6)
	groups := []EnterpriseAllowedGroup{{GroupID: 2}, {GroupID: 1, RateMultiplier: enterpriseTestFloat(0.4)}}
	require.NoError(t, f.s.SetAllowedGroupRates(context.Background(), EnterpriseActor{UserID: 99, IsAdmin: true}, 1, groups))
	require.True(t, r.tx)
	require.Equal(t, []int64{1, 2}, r.pool)
	require.Equal(t, 0.4, *r.rates[1])
	require.Nil(t, r.rates[2])
	require.Equal(t, int64(2), groups[0].GroupID, "caller input must not be sorted in place")
	require.Equal(t, []int64{10, 20, 30}, f.auth.ids)
	require.Len(t, f.a.logs, 1)
	log := f.a.logs[0]
	require.Equal(t, EnterpriseAuditActionGroupPoolUpdate, log.Action)
	require.Equal(t, RoleAdmin, log.ActorRole)
	before := log.Detail["before"].(map[string]any)["groups"].([]EnterpriseAllowedGroup)
	after := log.Detail["after"].(map[string]any)["groups"].([]EnterpriseAllowedGroup)
	require.Equal(t, []EnterpriseAllowedGroup{{GroupID: 1, RateMultiplier: enterpriseTestFloat(0.8)}, {GroupID: 2, RateMultiplier: enterpriseTestFloat(0.6)}}, before)
	require.Equal(t, []EnterpriseAllowedGroup{{GroupID: 1, RateMultiplier: enterpriseTestFloat(0.4)}, {GroupID: 2}}, after)
}

func TestEnterpriseGroupRatesLegacyUpdatePreservesOverrides(t *testing.T) {
	f := newEnterpriseManagementTestFixture(t)
	r := enterpriseManagementWithRates(f)
	r.pool = []int64{1, 2}
	r.rates[1], r.rates[2] = enterpriseTestFloat(0.8), enterpriseTestFloat(0.6)
	require.NoError(t, f.s.SetAllowedGroups(context.Background(), EnterpriseActor{IsAdmin: true}, 1, []int64{1, 1}))
	require.Equal(t, []int64{1}, r.pool)
	require.Equal(t, 0.8, *r.rates[1])
	after := f.a.logs[0].Detail["after"].(map[string]any)["groups"].([]EnterpriseAllowedGroup)
	require.Equal(t, []EnterpriseAllowedGroup{{GroupID: 1, RateMultiplier: enterpriseTestFloat(0.8)}}, after)
}

func TestEnterpriseGroupRatesValidationAndPermissions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		groups []EnterpriseAllowedGroup
		admin  bool
		want   error
	}{
		{"negative", []EnterpriseAllowedGroup{{GroupID: 1, RateMultiplier: enterpriseTestFloat(-1)}}, true, ErrEnterpriseInvalidInput},
		{"zero", []EnterpriseAllowedGroup{{GroupID: 1, RateMultiplier: enterpriseTestFloat(0)}}, true, ErrEnterpriseInvalidInput},
		{"nan", []EnterpriseAllowedGroup{{GroupID: 1, RateMultiplier: enterpriseTestFloat(math.NaN())}}, true, ErrEnterpriseInvalidInput},
		{"infinite", []EnterpriseAllowedGroup{{GroupID: 1, RateMultiplier: enterpriseTestFloat(math.Inf(1))}}, true, ErrEnterpriseInvalidInput},
		{"duplicate", []EnterpriseAllowedGroup{{GroupID: 1}, {GroupID: 1}}, true, ErrEnterpriseInvalidInput},
		{"missing", []EnterpriseAllowedGroup{{GroupID: 99}}, true, ErrEnterpriseGroupOutOfScope},
		{"disabled", []EnterpriseAllowedGroup{{GroupID: 3}}, true, ErrEnterpriseGroupOutOfScope},
		{"invalid id", []EnterpriseAllowedGroup{{GroupID: 0}}, true, ErrEnterpriseGroupOutOfScope},
		{"owner cannot edit", []EnterpriseAllowedGroup{{GroupID: 1}}, false, ErrEnterprisePermissionDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newEnterpriseManagementTestFixture(t)
			enterpriseManagementWithRates(f)
			err := f.s.SetAllowedGroupRates(context.Background(), EnterpriseActor{UserID: 10, IsAdmin: tc.admin}, 1, tc.groups)
			require.ErrorIs(t, err, tc.want)
			require.Zero(t, f.g.writes)
			require.Empty(t, f.a.logs)
			require.Empty(t, f.auth.ids)
		})
	}
}

func TestEnterpriseGroupRatesAuditFailureDoesNotInvalidate(t *testing.T) {
	f := newEnterpriseManagementTestFixture(t)
	enterpriseManagementWithRates(f)
	f.a.err = errors.New("audit unavailable")
	require.EqualError(t, f.s.SetAllowedGroupRates(context.Background(), EnterpriseActor{IsAdmin: true}, 1, []EnterpriseAllowedGroup{{GroupID: 1}}), "audit unavailable")
	require.Empty(t, f.auth.ids)
}

func TestEnterpriseGroupRatesViewsUseFinalRates(t *testing.T) {
	for _, role := range []string{EnterpriseRoleOwner, EnterpriseRoleManager, EnterpriseRoleMember} {
		t.Run(role, func(t *testing.T) {
			f := newEnterpriseManagementTestFixture(t)
			r := enterpriseManagementWithRates(f)
			r.pool, r.rates[1] = []int64{1, 2}, enterpriseTestFloat(0.8)
			f.e.e.GroupPoolConfigured = true
			f.e.caller = &EnterpriseMemberContext{EnterpriseID: 1, MemberID: 20, Role: role, MemberStatus: EnterpriseMemberStatusActive, EnterpriseStatus: EnterpriseStatusActive, GroupPoolConfigured: true, GroupPoolIDs: []int64{1, 2}}
			groups := f.s.groups.(*enterpriseManagementTestGroups)
			first, second := groups.groups[1], groups.groups[2]
			first.RateMultiplier, second.RateMultiplier = 2, 3
			groups.groups[1], groups.groups[2] = first, second
			rates := &enterpriseManagementEffectiveRates{rates: map[int64]float64{1: 0.8, 2: 0.5}}
			f.s.groupRates = rates
			views, err := f.s.GetAllowedGroups(context.Background(), EnterpriseActor{UserID: 20}, 1)
			require.NoError(t, err)
			require.Equal(t, int64(20), rates.user)
			require.Len(t, views, 2)
			for _, view := range views {
				if view.ID == 1 {
					require.Equal(t, 0.8, view.RateMultiplier)
					require.Equal(t, 2.0, view.DefaultRateMultiplier)
					require.Equal(t, enterpriseTestFloat(0.8), view.EnterpriseRateMultiplier)
				} else {
					require.Equal(t, 0.5, view.RateMultiplier)
					require.Equal(t, 3.0, view.DefaultRateMultiplier)
					require.Nil(t, view.EnterpriseRateMultiplier)
				}
				require.Equal(t, view.RateMultiplier, view.EffectiveRateMultiplier)
			}
		})
	}
}

func TestEnterpriseGroupRatesAdminViewDoesNotUseAdminPersonalRate(t *testing.T) {
	f := newEnterpriseManagementTestFixture(t)
	r := enterpriseManagementWithRates(f)
	r.pool, r.rates[1] = []int64{1}, enterpriseTestFloat(0.8)
	f.e.e.GroupPoolConfigured = true
	f.s.groupRates = &enterpriseManagementEffectiveRates{err: errors.New("must not query admin rates")}
	views, err := f.s.GetAllowedGroups(context.Background(), EnterpriseActor{UserID: 99, IsAdmin: true}, 1)
	require.NoError(t, err)
	require.Len(t, views, 1)
	require.Equal(t, 0.8, views[0].EffectiveRateMultiplier)
}
