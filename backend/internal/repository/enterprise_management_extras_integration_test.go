//go:build integration

package repository

import (
	"context"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestEnterpriseUserListFiltersCurrentMembership(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	e := enterpriseFixture(t, ctx)
	other := enterpriseFixture(t, ctx)
	member := enterpriseMemberFixture(t, ctx, e.ID, nil)
	departed := enterpriseMemberFixture(t, ctx, e.ID, nil)
	require.NoError(t, NewEnterpriseMemberRepository(tx.Client()).Delete(ctx, e.ID, departed.ID))
	enterpriseMemberFixture(t, ctx, other.ID, nil)
	repo := newUserRepositoryWithSQL(tx.Client(), tx.Client())
	users, page, err := repo.ListWithFilters(ctx, pagination.PaginationParams{Page: 1, PageSize: 10}, service.UserListFilters{EnterpriseID: e.ID})
	require.NoError(t, err)
	require.EqualValues(t, 1, page.Total)
	require.Len(t, users, 1)
	require.Equal(t, member.UserID, users[0].ID)
}

func TestEnterpriseGroupDeletionCleansPoolAndWhitelists(t *testing.T) {
	for _, cascade := range []bool{false, true} {
		t.Run(map[bool]string{false: "delete", true: "cascade"}[cascade], func(t *testing.T) {
			tx := testEntTx(t)
			ctx := dbent.NewTxContext(context.Background(), tx)
			client := tx.Client()
			e := enterpriseFixture(t, ctx)
			member := enterpriseMemberFixture(t, ctx, e.ID, nil)
			deleted := enterpriseGroupFixture(t, client)
			kept := enterpriseGroupFixture(t, client)
			groups := NewEnterpriseGroupRepository(client)
			require.NoError(t, groups.SetAllowedGroupIDs(ctx, e.ID, []int64{deleted, kept}))
			require.NoError(t, groups.SetWhitelist(ctx, e.ID, service.EnterpriseScopeEnterprise, e.ID, []int64{deleted, kept}))
			require.NoError(t, groups.SetWhitelist(ctx, e.ID, service.EnterpriseScopeMember, member.ID, []int64{deleted, kept}))
			repo := newGroupRepositoryWithSQL(client, client)
			if cascade {
				_, err := repo.DeleteCascade(ctx, deleted)
				require.NoError(t, err)
			} else {
				require.NoError(t, repo.Delete(ctx, deleted))
			}
			pool, err := groups.GetAllowedGroupIDs(ctx, e.ID)
			require.NoError(t, err)
			require.Equal(t, []int64{kept}, pool)
			whitelists, err := groups.ListWhitelists(ctx, e.ID)
			require.NoError(t, err)
			require.Len(t, whitelists, 2)
			for _, whitelist := range whitelists {
				require.Equal(t, []int64{kept}, whitelist.GroupIDs)
			}
		})
	}
}
