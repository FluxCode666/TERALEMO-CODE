//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// 开关语义与 CountActiveOwners 的"有效负责人"一致：成员关系未软删除、
// role=owner、成员状态 active，且用户本身 active。非企业用户与非负责人均不显示。
func TestUserListHideEnterpriseEmployees(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	client := tx.Client()
	members := NewEnterpriseMemberRepository(client)
	e := enterpriseFixture(t, ctx)
	other := enterpriseFixture(t, ctx)

	newMember := func(enterpriseID int64, role, status string) *service.EnterpriseMember {
		t.Helper()
		user := mustCreateUser(t, client, &service.User{
			Email:    fmt.Sprintf("hide-emp-%d@example.com", time.Now().UnixNano()),
			Username: "hide-emp",
		})
		m := &service.EnterpriseMember{EnterpriseID: enterpriseID, UserID: user.ID, Role: role, Status: status}
		require.NoError(t, members.Create(ctx, m))
		return m
	}

	owner := newMember(e.ID, service.EnterpriseRoleOwner, service.EnterpriseMemberStatusActive)
	secondOwner := newMember(e.ID, service.EnterpriseRoleOwner, service.EnterpriseMemberStatusActive)
	employee := newMember(e.ID, service.EnterpriseRoleMember, service.EnterpriseMemberStatusActive)
	manager := newMember(e.ID, service.EnterpriseRoleManager, service.EnterpriseMemberStatusActive)
	disabledMemberOwner := newMember(e.ID, service.EnterpriseRoleOwner, service.EnterpriseMemberStatusDisabled)
	deletedOwner := newMember(e.ID, service.EnterpriseRoleOwner, service.EnterpriseMemberStatusActive)
	require.NoError(t, members.Delete(ctx, e.ID, deletedOwner.ID))
	disabledUserOwner := newMember(e.ID, service.EnterpriseRoleOwner, service.EnterpriseMemberStatusActive)
	require.NoError(t, client.User.UpdateOneID(disabledUserOwner.UserID).SetStatus(service.StatusDisabled).Exec(ctx))
	otherOwner := newMember(other.ID, service.EnterpriseRoleOwner, service.EnterpriseMemberStatusActive)
	plain := mustCreateUser(t, client, &service.User{
		Email:    fmt.Sprintf("hide-emp-plain-%d@example.com", time.Now().UnixNano()),
		Username: "hide-emp-plain",
	})

	repo := newUserRepositoryWithSQL(client, client)
	ids := func(users []service.User) []int64 {
		out := make([]int64, 0, len(users))
		for i := range users {
			out = append(out, users[i].ID)
		}
		return out
	}

	t.Run("scoped to enterprise keeps only available owners", func(t *testing.T) {
		users, page, err := repo.ListWithFilters(ctx, pagination.PaginationParams{Page: 1, PageSize: 10},
			service.UserListFilters{EnterpriseID: e.ID, HideEnterpriseEmployees: true})
		require.NoError(t, err)
		require.EqualValues(t, 2, page.Total)
		require.ElementsMatch(t, []int64{owner.UserID, secondOwner.UserID}, ids(users))
	})

	t.Run("toggle off restores the full membership list", func(t *testing.T) {
		users, page, err := repo.ListWithFilters(ctx, pagination.PaginationParams{Page: 1, PageSize: 10},
			service.UserListFilters{EnterpriseID: e.ID})
		require.NoError(t, err)
		require.EqualValues(t, 6, page.Total)
		require.ElementsMatch(t, []int64{
			owner.UserID, secondOwner.UserID, employee.UserID, manager.UserID,
			disabledMemberOwner.UserID, disabledUserOwner.UserID,
		}, ids(users))
	})

	t.Run("global filter excludes non owners and non enterprise users", func(t *testing.T) {
		users, _, err := repo.ListWithFilters(ctx, pagination.PaginationParams{Page: 1, PageSize: 100},
			service.UserListFilters{HideEnterpriseEmployees: true})
		require.NoError(t, err)
		got := ids(users)
		require.Subset(t, got, []int64{owner.UserID, secondOwner.UserID, otherOwner.UserID})
		for _, excluded := range []int64{
			employee.UserID, manager.UserID, disabledMemberOwner.UserID,
			deletedOwner.UserID, disabledUserOwner.UserID, plain.ID,
		} {
			require.NotContains(t, got, excluded)
		}
	})

	t.Run("filters before pagination and count", func(t *testing.T) {
		users, page, err := repo.ListWithFilters(ctx, pagination.PaginationParams{Page: 1, PageSize: 1},
			service.UserListFilters{EnterpriseID: e.ID, HideEnterpriseEmployees: true})
		require.NoError(t, err)
		require.EqualValues(t, 2, page.Total, "total 必须是过滤后的数量")
		require.Len(t, users, 1)
		require.Subset(t, []int64{owner.UserID, secondOwner.UserID}, ids(users))
	})

	t.Run("combines with enterprise id of another enterprise", func(t *testing.T) {
		users, page, err := repo.ListWithFilters(ctx, pagination.PaginationParams{Page: 1, PageSize: 10},
			service.UserListFilters{EnterpriseID: other.ID, HideEnterpriseEmployees: true})
		require.NoError(t, err)
		require.EqualValues(t, 1, page.Total)
		require.Equal(t, []int64{otherOwner.UserID}, ids(users))
	})
}