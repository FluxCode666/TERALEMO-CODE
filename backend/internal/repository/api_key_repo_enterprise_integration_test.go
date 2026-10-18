//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyEnterpriseAuthProjection(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	e := enterpriseFixture(t, ctx)
	m := enterpriseMemberFixture(t, ctx, e.ID, nil)
	groupID := enterpriseGroupFixture(t, tx.Client())
	require.NoError(t, NewEnterpriseGroupRepository(tx.Client()).SetAllowedGroupIDs(ctx, e.ID, []int64{groupID}))
	_, err := tx.Client().EnterpriseMember.UpdateOneID(m.ID).SetStatus(service.EnterpriseMemberStatusDisabled).Save(ctx)
	require.NoError(t, err)
	repo := NewAPIKeyRepository(tx.Client(), integrationDB)
	key := &service.APIKey{UserID: m.UserID, GroupID: &groupID, Key: fmt.Sprintf("sk-enterprise-auth-%d", m.ID), Name: "enterprise-auth", Status: service.StatusActive}
	require.NoError(t, repo.Create(ctx, key))
	loaded, err := repo.GetByKeyForAuth(ctx, key.Key)
	require.NoError(t, err)
	require.NotNil(t, loaded.User.Enterprise)
	require.Equal(t, e.ID, loaded.User.Enterprise.EnterpriseID)
	require.Equal(t, m.ID, loaded.User.Enterprise.MemberID)
	require.False(t, loaded.User.Enterprise.Active())
	require.Equal(t, []int64{groupID}, loaded.User.Enterprise.GroupPoolIDs)
	require.True(t, loaded.User.Enterprise.AllowsGroup(groupID))
	other := enterpriseMemberFixture(t, ctx, e.ID, nil)
	contexts, err := NewEnterpriseRepository(tx.Client()).GetMemberContextsByUserIDs(ctx, []int64{m.UserID, other.UserID, 0})
	require.NoError(t, err)
	require.Len(t, contexts, 2)
	require.Equal(t, loaded.User.Enterprise, contexts[m.UserID])
}
