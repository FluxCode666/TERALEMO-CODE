//go:build integration

package repository

import (
	"context"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestEnterpriseGroupRatesRoundTripAndLegacyPreservation(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	client := tx.Client()
	enterprise := enterpriseFixture(t, ctx)
	first := mustCreateGroup(t, client, &service.Group{Name: t.Name() + "-first", RateMultiplier: 1})
	second := mustCreateGroup(t, client, &service.Group{Name: t.Name() + "-second", RateMultiplier: 1})
	third := mustCreateGroup(t, client, &service.Group{Name: t.Name() + "-third", RateMultiplier: 1})
	legacy := NewEnterpriseGroupRepository(client)
	repo := legacy.(service.EnterpriseGroupRateRepository)

	groups, err := repo.GetAllowedGroups(ctx, enterprise.ID)
	require.NoError(t, err)
	require.NotNil(t, groups)
	require.Empty(t, groups)
	require.NoError(t, repo.SetAllowedGroups(ctx, enterprise.ID, []service.EnterpriseAllowedGroup{
		{GroupID: first.ID, RateMultiplier: ratePointer(0.81234)},
		{GroupID: second.ID},
	}))
	groups, err = repo.GetAllowedGroups(ctx, enterprise.ID)
	require.NoError(t, err)
	require.Equal(t, []service.EnterpriseAllowedGroup{
		{GroupID: first.ID, RateMultiplier: ratePointer(0.8123)},
		{GroupID: second.ID},
	}, groups)

	require.NoError(t, legacy.SetAllowedGroupIDs(ctx, enterprise.ID, []int64{third.ID, first.ID, first.ID}))
	groups, err = repo.GetAllowedGroups(ctx, enterprise.ID)
	require.NoError(t, err)
	require.Equal(t, []service.EnterpriseAllowedGroup{
		{GroupID: first.ID, RateMultiplier: ratePointer(0.8123)},
		{GroupID: third.ID},
	}, groups, "legacy payload retains existing overrides and new groups inherit")

	require.ErrorIs(t, repo.SetAllowedGroups(ctx, enterprise.ID, []service.EnterpriseAllowedGroup{
		{GroupID: first.ID, RateMultiplier: ratePointer(2)},
		{GroupID: 9223372036854775807},
	}), service.ErrEnterpriseGroupOutOfScope)
	unchanged, err := repo.GetAllowedGroups(ctx, enterprise.ID)
	require.NoError(t, err)
	require.Equal(t, groups, unchanged, "an invalid replacement must leave the pool and overrides unchanged")

	require.NoError(t, repo.SetAllowedGroups(ctx, enterprise.ID, []service.EnterpriseAllowedGroup{
		{GroupID: first.ID},
		{GroupID: third.ID, RateMultiplier: ratePointer(0)},
	}))
	groups, err = repo.GetAllowedGroups(ctx, enterprise.ID)
	require.NoError(t, err)
	require.Equal(t, []service.EnterpriseAllowedGroup{
		{GroupID: first.ID},
		{GroupID: third.ID, RateMultiplier: ratePointer(0)},
	}, groups, "explicit null clears overrides while zero remains an override")
	require.NoError(t, legacy.SetAllowedGroupIDs(ctx, enterprise.ID, nil))
	groups, err = repo.GetAllowedGroups(ctx, enterprise.ID)
	require.NoError(t, err)
	require.Empty(t, groups)
	company, err := NewEnterpriseRepository(client).GetByID(ctx, enterprise.ID)
	require.NoError(t, err)
	require.False(t, company.GroupPoolConfigured)
}

func TestUserGroupRateEffectivePostgresMembershipAndPriority(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	client := tx.Client()
	enterprise := enterpriseFixture(t, ctx)
	member := enterpriseMemberFixture(t, ctx, enterprise.ID, nil)
	group := mustCreateGroup(t, client, &service.Group{Name: t.Name(), RateMultiplier: 1})
	groups := NewEnterpriseGroupRepository(client).(service.EnterpriseGroupRateRepository)
	repo := &userGroupRateRepository{sql: client}
	require.NoError(t, repo.SyncUserGroupRates(ctx, member.UserID, map[int64]*float64{group.ID: ratePointer(0.6)}))
	require.NoError(t, groups.SetAllowedGroups(ctx, enterprise.ID, []service.EnterpriseAllowedGroup{
		{GroupID: group.ID, RateMultiplier: ratePointer(0.8)},
	}))
	assertRates := func(want float64) {
		t.Helper()
		rate, err := repo.GetEffectiveByUserAndGroup(ctx, member.UserID, group.ID)
		require.NoError(t, err)
		require.Equal(t, ratePointer(want), rate)
		rates, err := repo.GetEffectiveByUserID(ctx, member.UserID)
		require.NoError(t, err)
		require.Equal(t, map[int64]float64{group.ID: want}, rates)
	}
	assertRates(0.8)
	personal, err := repo.GetByUserID(ctx, member.UserID)
	require.NoError(t, err)
	require.Equal(t, map[int64]float64{group.ID: 0.6}, personal)
	require.NoError(t, groups.SetAllowedGroups(ctx, enterprise.ID, []service.EnterpriseAllowedGroup{{GroupID: group.ID}}))
	assertRates(0.6)
	require.NoError(t, groups.SetAllowedGroups(ctx, enterprise.ID, []service.EnterpriseAllowedGroup{
		{GroupID: group.ID, RateMultiplier: ratePointer(0)},
	}))
	assertRates(0)
	require.NoError(t, NewEnterpriseMemberRepository(client).Delete(ctx, enterprise.ID, member.ID))
	assertRates(0.6)
}
