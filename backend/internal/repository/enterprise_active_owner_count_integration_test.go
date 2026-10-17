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

func TestEnterpriseRepositoryActiveOwnerCountMatchesAvailableOwners(t *testing.T) {
	// All fixtures stay in the caller transaction and roll back in t.Cleanup.
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	client := tx.Client()
	enterprises := NewEnterpriseRepository(client)
	members := NewEnterpriseMemberRepository(client)
	prefix := fmt.Sprintf("owner-count-%d", time.Now().UnixNano())
	createEnterprise := func(name string) *service.Enterprise {
		e := &service.Enterprise{Name: prefix + "-" + name, Settings: service.DefaultEnterpriseSettings()}
		require.NoError(t, enterprises.Create(ctx, e))
		return e
	}
	company := createEnterprise("mixed")
	empty := createEnterprise("empty")
	other := createEnterprise("other")
	createOwner := func(enterpriseID int64) *service.EnterpriseMember {
		m := enterpriseMemberFixture(t, ctx, enterpriseID, nil)
		m.Role = service.EnterpriseRoleOwner
		require.NoError(t, members.Update(ctx, m))
		return m
	}
	first, second := createOwner(company.ID), createOwner(company.ID)
	createOwner(other.ID)
	// Active non-owners, unavailable memberships and unavailable users never count.
	enterpriseMemberFixture(t, ctx, company.ID, nil)
	disabledMember := createOwner(company.ID)
	disabledMember.Status = service.EnterpriseMemberStatusDisabled
	require.NoError(t, members.Update(ctx, disabledMember))
	deletedMember := createOwner(company.ID)
	require.NoError(t, members.Delete(ctx, company.ID, deletedMember.ID))
	disabledUser := createOwner(company.ID)
	_, err := client.ExecContext(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, disabledUser.UserID)
	require.NoError(t, err)
	deletedUser := createOwner(company.ID)
	_, err = client.ExecContext(ctx, `UPDATE users SET deleted_at=NOW() WHERE id=$1`, deletedUser.UserID)
	require.NoError(t, err)

	assertCounts := func(expected map[int64]int) {
		t.Helper()
		for enterpriseID, want := range expected {
			detail, err := enterprises.GetByID(ctx, enterpriseID)
			require.NoError(t, err)
			require.Equal(t, want, detail.ActiveOwnerCount, "detail enterprise %d", enterpriseID)
			guardCount, err := members.CountActiveOwners(ctx, enterpriseID)
			require.NoError(t, err)
			require.EqualValues(t, guardCount, detail.ActiveOwnerCount, "management guard must use the same definition")
		}
		listed, page, err := enterprises.List(ctx, pagination.PaginationParams{Page: 1, PageSize: 10}, service.EnterpriseListFilters{Search: prefix})
		require.NoError(t, err)
		require.Len(t, listed, len(expected))
		require.EqualValues(t, len(expected), page.Total)
		for _, e := range listed {
			want, exists := expected[e.ID]
			require.True(t, exists)
			require.Equal(t, want, e.ActiveOwnerCount, "listed enterprise %d", e.ID)
		}
	}
	assertCounts(map[int64]int{company.ID: 2, empty.ID: 0, other.ID: 1})
	// Platform moderation can disable every owner; subsequent list/detail reads
	// must expose zero so management can identify the enterprise needing repair.
	_, err = client.ExecContext(ctx, `UPDATE users SET status='disabled' WHERE id IN ($1,$2)`, first.UserID, second.UserID)
	require.NoError(t, err)
	assertCounts(map[int64]int{company.ID: 0, empty.ID: 0, other.ID: 1})

	listed, page, err := enterprises.List(ctx, pagination.DefaultPagination(), service.EnterpriseListFilters{Search: prefix + "-absent"})
	require.NoError(t, err)
	require.Empty(t, listed)
	require.Zero(t, page.Total)
}
