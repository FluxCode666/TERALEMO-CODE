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

// 开关只隐藏企业员工：企业用户中仅保留与 CountActiveOwners 一致的"有效负责人"
// （成员关系未软删除、role=owner、成员状态 active，且用户本身 active）；
// 非企业用户（含已退出企业、被禁用的用户）不受影响，照常显示。
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
	disabledPlain := mustCreateUser(t, client, &service.User{
		Email:    fmt.Sprintf("hide-emp-plain-disabled-%d@example.com", time.Now().UnixNano()),
		Username: "hide-emp-plain-disabled",
		Status:   service.StatusDisabled,
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

	t.Run("global filter hides employees but keeps non enterprise users", func(t *testing.T) {
		users, page, err := repo.ListWithFilters(ctx, pagination.PaginationParams{Page: 1, PageSize: 100},
			service.UserListFilters{Search: "hide-emp", HideEnterpriseEmployees: true})
		require.NoError(t, err)
		// 已退出企业（成员关系软删除）的用户视为非企业用户，照常显示。
		require.ElementsMatch(t, []int64{
			owner.UserID, secondOwner.UserID, otherOwner.UserID,
			plain.ID, disabledPlain.ID, deletedOwner.UserID,
		}, ids(users))
		require.EqualValues(t, 6, page.Total)
	})

	t.Run("combines with status filter for non enterprise users", func(t *testing.T) {
		users, _, err := repo.ListWithFilters(ctx, pagination.PaginationParams{Page: 1, PageSize: 100},
			service.UserListFilters{Search: "hide-emp", Status: service.StatusDisabled, HideEnterpriseEmployees: true})
		require.NoError(t, err)
		require.Equal(t, []int64{disabledPlain.ID}, ids(users))
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
