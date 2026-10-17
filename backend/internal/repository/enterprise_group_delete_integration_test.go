//go:build integration

package repository

import (
	"context"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestEnterpriseGroupDeleteWithTransactionClientWithoutTxContext(t *testing.T) {
	tx := testEntTx(t)
	client := tx.Client()
	txCtx := dbent.NewTxContext(context.Background(), tx)
	enterprise := enterpriseFixture(t, txCtx)
	other := enterpriseFixture(t, txCtx)
	unrelated := enterpriseFixture(t, txCtx)
	active := enterpriseMemberFixture(t, txCtx, enterprise.ID, nil)
	disabled := enterpriseMemberFixture(t, txCtx, enterprise.ID, nil)
	departed := enterpriseMemberFixture(t, txCtx, enterprise.ID, nil)
	members := NewEnterpriseMemberRepository(client)
	disabled.Status = service.EnterpriseMemberStatusDisabled
	require.NoError(t, members.Update(txCtx, disabled))
	require.NoError(t, members.Delete(txCtx, enterprise.ID, departed.ID))
	otherMember := enterpriseMemberFixture(t, txCtx, other.ID, nil)
	enterpriseMemberFixture(t, txCtx, unrelated.ID, nil)
	groupID := enterpriseGroupFixture(t, client)
	groups := NewEnterpriseGroupRepository(client)
	require.NoError(t, groups.SetAllowedGroupIDs(txCtx, enterprise.ID, []int64{groupID}))
	require.NoError(t, groups.SetWhitelist(txCtx, enterprise.ID, service.EnterpriseScopeMember, active.ID, []int64{groupID}))
	require.NoError(t, groups.SetWhitelist(txCtx, other.ID, service.EnterpriseScopeMember, otherMember.ID, []int64{groupID}))
	repo := newGroupRepositoryWithSQL(client, client)
	// This is the established GroupRepoSuite calling pattern: a transaction
	// client, but a plain context. Delete must reuse rather than start a nested tx.
	ctx := context.Background()
	ids, err := repo.GetEnterpriseGroupAffectedUserIDs(ctx, groupID)
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{active.UserID, disabled.UserID, otherMember.UserID}, ids)
	require.NoError(t, repo.Delete(ctx, groupID))
	ids, err = repo.GetEnterpriseGroupAffectedUserIDs(ctx, groupID)
	require.NoError(t, err)
	require.Empty(t, ids)
	pool, err := groups.GetAllowedGroupIDs(txCtx, enterprise.ID)
	require.NoError(t, err)
	require.Empty(t, pool)
	for _, id := range []int64{enterprise.ID, other.ID} {
		whitelists, err := groups.ListWhitelists(txCtx, id)
		require.NoError(t, err)
		require.Empty(t, whitelists)
	}
	// The caller still owns a live, uncommitted transaction after Delete returns.
	rows, err := tx.QueryContext(ctx, "SELECT deleted_at IS NOT NULL FROM groups WHERE id=$1", groupID)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	require.True(t, rows.Next())
	var deleted bool
	require.NoError(t, rows.Scan(&deleted))
	require.True(t, deleted)
}
