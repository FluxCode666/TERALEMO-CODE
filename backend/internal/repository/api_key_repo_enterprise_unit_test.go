package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyEnterpriseContextProjectionSQLite(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "api-key-enterprise-context@example.com")
	group, err := client.Group.Create().SetName("enterprise-exclusive").SetIsExclusive(true).Save(ctx)
	require.NoError(t, err)
	other, err := client.Group.Create().SetName("enterprise-other").Save(ctx)
	require.NoError(t, err)
	enterprise, err := client.Enterprise.Create().SetName("enterprise-auth").Save(ctx)
	require.NoError(t, err)
	department, err := client.EnterpriseDepartment.Create().SetEnterpriseID(enterprise.ID).SetName("R&D").Save(ctx)
	require.NoError(t, err)
	member, err := client.EnterpriseMember.Create().SetEnterpriseID(enterprise.ID).SetUserID(user.ID).SetDepartmentID(department.ID).SetStatus(service.EnterpriseMemberStatusDisabled).Save(ctx)
	require.NoError(t, err)
	_, err = client.EnterpriseAllowedGroup.Create().SetEnterpriseID(enterprise.ID).SetGroupID(group.ID).Save(ctx)
	require.NoError(t, err)
	_, err = client.EnterpriseGroupWhitelist.Create().SetEnterpriseID(enterprise.ID).SetScopeType(service.EnterpriseScopeEnterprise).SetScopeID(enterprise.ID).SetGroupID(group.ID).Save(ctx)
	require.NoError(t, err)
	_, err = client.EnterpriseGroupWhitelist.Create().SetEnterpriseID(enterprise.ID).SetScopeType(service.EnterpriseScopeDepartment).SetScopeID(department.ID).SetGroupID(other.ID).Save(ctx)
	require.NoError(t, err)
	key := &service.APIKey{UserID: user.ID, GroupID: &group.ID, Key: "sk-enterprise-context", Name: "enterprise", Status: service.StatusActive}
	require.NoError(t, repo.Create(ctx, key))

	for name, load := range map[string]func() (*service.APIKey, error){
		"id":   func() (*service.APIKey, error) { return repo.GetByID(ctx, key.ID) },
		"key":  func() (*service.APIKey, error) { return repo.GetByKey(ctx, key.Key) },
		"auth": func() (*service.APIKey, error) { return repo.GetByKeyForAuth(ctx, key.Key) },
	} {
		t.Run(name, func(t *testing.T) {
			loaded, err := load()
			require.NoError(t, err)
			membership := loaded.User.Enterprise
			require.NotNil(t, membership)
			require.Equal(t, enterprise.ID, membership.EnterpriseID)
			require.Equal(t, member.ID, membership.MemberID)
			require.Equal(t, department.ID, *membership.DepartmentID)
			require.False(t, membership.Active(), "disabled memberships must remain present")
			require.True(t, membership.GroupPoolConfigured)
			require.Equal(t, []int64{group.ID}, membership.GroupPoolIDs)
			require.True(t, membership.GroupWhitelistConfigured)
			require.Empty(t, membership.GroupWhitelistIDs)
			require.False(t, membership.AllowsGroup(group.ID), "disjoint configured whitelists deny everything")
		})
	}
	_, err = client.EnterpriseMember.UpdateOneID(member.ID).SetDeletedAt(time.Now()).Save(ctx)
	require.NoError(t, err)
	loaded, err := repo.GetByKeyForAuth(ctx, key.Key)
	require.NoError(t, err)
	require.Nil(t, loaded.User.Enterprise, "removed members return to personal access and billing")
}
