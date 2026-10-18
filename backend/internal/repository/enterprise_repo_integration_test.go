//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enterprisequota"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func enterpriseFixture(t *testing.T, ctx context.Context) *service.Enterprise {
	t.Helper()
	e := &service.Enterprise{Name: fmt.Sprintf("enterprise-%s-%d", t.Name(), time.Now().UnixNano()), Status: service.EnterpriseStatusActive, Settings: service.DefaultEnterpriseSettings()}
	// Keep the database's VARCHAR(100) boundary even for long subtest names.
	if len(e.Name) > 100 {
		e.Name = fmt.Sprintf("enterprise-%d", time.Now().UnixNano())
	}
	require.NoError(t, NewEnterpriseRepository(testEntClient(t)).Create(ctx, e))
	return e
}
func enterpriseMemberFixture(t *testing.T, ctx context.Context, enterpriseID int64, departmentID *int64) *service.EnterpriseMember {
	t.Helper()
	client := clientFromContext(ctx, testEntClient(t))
	user := mustCreateUser(t, client, &service.User{Email: fmt.Sprintf("enterprise-%d@example.com", time.Now().UnixNano()), Username: "enterprise-user"})
	member := &service.EnterpriseMember{EnterpriseID: enterpriseID, UserID: user.ID, DepartmentID: departmentID, Role: service.EnterpriseRoleMember, Status: service.EnterpriseMemberStatusActive}
	require.NoError(t, NewEnterpriseMemberRepository(testEntClient(t)).Create(ctx, member))
	return member
}
func enterpriseGroupFixture(t *testing.T, client *dbent.Client) int64 {
	t.Helper()
	id := mustCreateGroup(t, client, &service.Group{Name: fmt.Sprintf("enterprise-group-%d", time.Now().UnixNano())}).ID
	// 已提交的分组会污染按平台计数的其他套件（如 GroupRepoSuite）；事务内创建的分组随回滚消失，DELETE 无副作用。
	t.Cleanup(func() { _, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM groups WHERE id=$1", id) })
	return id
}

func TestEnterpriseRepositoryUniqueAndSoftDelete(t *testing.T) {
	ctx := context.Background()
	er := NewEnterpriseRepository(testEntClient(t))
	dr := NewEnterpriseDepartmentRepository(testEntClient(t))
	mr := NewEnterpriseMemberRepository(testEntClient(t))
	qr := NewEnterpriseQuotaRepository(testEntClient(t))
	e := enterpriseFixture(t, ctx)
	duplicate := &service.Enterprise{Name: e.Name, Status: service.EnterpriseStatusActive}
	require.ErrorIs(t, er.Create(ctx, duplicate), service.ErrEnterpriseNameExists)
	d := &service.EnterpriseDepartment{EnterpriseID: e.ID, Name: "Engineering"}
	require.NoError(t, dr.Create(ctx, d))
	require.ErrorIs(t, dr.Create(ctx, &service.EnterpriseDepartment{EnterpriseID: e.ID, Name: d.Name}), service.ErrEnterpriseDepartmentNameExists)
	second := enterpriseFixture(t, ctx)
	require.NoError(t, dr.Create(ctx, &service.EnterpriseDepartment{EnterpriseID: second.ID, Name: d.Name}))
	m := enterpriseMemberFixture(t, ctx, e.ID, &d.ID)
	require.ErrorIs(t, mr.Create(ctx, &service.EnterpriseMember{EnterpriseID: second.ID, UserID: m.UserID}), service.ErrEnterpriseMemberExists)
	q, err := qr.GetByScope(ctx, e.ID, service.EnterpriseScopeMember, m.ID)
	require.NoError(t, err)
	require.Nil(t, q.DailyLimitUSD)
	require.NoError(t, mr.Delete(ctx, e.ID, m.ID))
	absent, err := er.GetMemberContextByUserID(ctx, m.UserID)
	require.NoError(t, err)
	require.Nil(t, absent)
	oldQuota, err := testEntClient(t).EnterpriseQuota.Query().Where(enterprisequota.IDEQ(q.ID)).Only(mixins.SkipSoftDelete(ctx))
	require.NoError(t, err)
	require.NotNil(t, oldQuota.DeletedAt)
	rejoined := &service.EnterpriseMember{EnterpriseID: second.ID, UserID: m.UserID, Role: service.EnterpriseRoleMember, Status: service.EnterpriseMemberStatusActive}
	require.NoError(t, mr.Create(ctx, rejoined))
	require.NotEqual(t, m.ID, rejoined.ID)
	newQuota, err := qr.GetByScope(ctx, second.ID, service.EnterpriseScopeMember, rejoined.ID)
	require.NoError(t, err)
	require.NotEqual(t, q.ID, newQuota.ID)
	require.NoError(t, dr.Delete(ctx, e.ID, d.ID))
	replacement := &service.EnterpriseDepartment{EnterpriseID: e.ID, Name: d.Name}
	require.NoError(t, dr.Create(ctx, replacement))
	require.NotEqual(t, d.ID, replacement.ID)
	empty := enterpriseFixture(t, ctx)
	require.NoError(t, er.Delete(ctx, empty.ID))
	require.ErrorIs(t, func() error { _, err := er.GetByID(ctx, empty.ID); return err }(), service.ErrEnterpriseNotFound)
	require.NoError(t, er.Create(ctx, &service.Enterprise{Name: empty.Name}))
}

func TestEnterpriseRepositoryCRUDCountsAndFilters(t *testing.T) {
	ctx := context.Background()
	er := NewEnterpriseRepository(testEntClient(t))
	dr := NewEnterpriseDepartmentRepository(testEntClient(t))
	mr := NewEnterpriseMemberRepository(testEntClient(t))
	e := enterpriseFixture(t, ctx)
	d := &service.EnterpriseDepartment{EnterpriseID: e.ID, Name: "Research", SortOrder: 7}
	require.NoError(t, dr.Create(ctx, d))
	m := enterpriseMemberFixture(t, ctx, e.ID, &d.ID)
	direct := enterpriseMemberFixture(t, ctx, e.ID, nil)
	m.Role = service.EnterpriseRoleManager
	m.Title = "Lead"
	require.NoError(t, mr.Update(ctx, m))
	got, err := mr.GetByID(ctx, e.ID, m.ID)
	require.NoError(t, err)
	require.Equal(t, "Research", got.DepartmentName)
	require.Equal(t, "Lead", got.Title)
	require.NotEmpty(t, got.Email)
	departments, err := dr.List(ctx, e.ID)
	require.NoError(t, err)
	require.Len(t, departments, 1)
	require.EqualValues(t, 1, departments[0].MemberCount)
	details, err := er.GetByID(ctx, e.ID)
	require.NoError(t, err)
	require.EqualValues(t, 2, details.MemberCount)
	require.EqualValues(t, 1, details.DepartmentCount)
	listed, page, err := er.List(ctx, pagination.DefaultPagination(), service.EnterpriseListFilters{Search: e.Name, Status: service.EnterpriseStatusActive})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.EqualValues(t, 1, page.Total)
	require.EqualValues(t, 2, listed[0].MemberCount)
	members, mp, err := mr.List(ctx, e.ID, pagination.DefaultPagination(), service.EnterpriseMemberListFilters{DepartmentID: &d.ID, Role: service.EnterpriseRoleManager, Search: got.Email})
	require.NoError(t, err)
	require.Len(t, members, 1)
	require.EqualValues(t, 1, mp.Total)
	zero := int64(0)
	members, mp, err = mr.List(ctx, e.ID, pagination.DefaultPagination(), service.EnterpriseMemberListFilters{DepartmentID: &zero})
	require.NoError(t, err)
	require.EqualValues(t, 1, mp.Total)
	require.Equal(t, direct.ID, members[0].ID)
	ids, err := er.ListMemberUserIDs(ctx, e.ID)
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{m.UserID, direct.UserID}, ids)
	d.Name = "Development"
	d.SortOrder = 2
	require.NoError(t, dr.Update(ctx, d))
	updated, err := dr.GetByID(ctx, e.ID, d.ID)
	require.NoError(t, err)
	require.Equal(t, "Development", updated.Name)
	e.Name += "-updated"
	e.Notes = "notes"
	e.Balance = 12345
	e.TotalRecharged = 12345
	require.NoError(t, er.Update(ctx, e))
	details, err = er.GetByID(ctx, e.ID)
	require.NoError(t, err)
	require.Zero(t, details.Balance)
	require.Zero(t, details.TotalRecharged)
	require.Equal(t, "notes", details.Notes)
}

func TestEnterpriseRepositoryMemberContextGroups(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	er := NewEnterpriseRepository(testEntClient(t))
	gr := NewEnterpriseGroupRepository(testEntClient(t))
	mr := NewEnterpriseMemberRepository(testEntClient(t))
	e := enterpriseFixture(t, ctx)
	d := &service.EnterpriseDepartment{EnterpriseID: e.ID, Name: "Engineering"}
	require.NoError(t, NewEnterpriseDepartmentRepository(testEntClient(t)).Create(ctx, d))
	m := enterpriseMemberFixture(t, ctx, e.ID, &d.ID)
	direct := enterpriseMemberFixture(t, ctx, e.ID, nil)
	a := enterpriseGroupFixture(t, tx.Client())
	b := enterpriseGroupFixture(t, tx.Client())
	c := enterpriseGroupFixture(t, tx.Client())
	snapshot, err := er.GetMemberContextByUserID(ctx, m.UserID)
	require.NoError(t, err)
	require.True(t, snapshot.Active())
	require.False(t, snapshot.GroupPoolConfigured)
	require.False(t, snapshot.GroupWhitelistConfigured)
	require.NoError(t, gr.SetAllowedGroupIDs(ctx, e.ID, []int64{a, b, c, a}))
	require.NoError(t, gr.SetWhitelist(ctx, e.ID, service.EnterpriseScopeEnterprise, e.ID, []int64{a, b}))
	require.NoError(t, gr.SetWhitelist(ctx, e.ID, service.EnterpriseScopeDepartment, d.ID, []int64{b, c}))
	require.NoError(t, gr.SetWhitelist(ctx, e.ID, service.EnterpriseScopeMember, m.ID, []int64{a, b, c}))
	contexts, err := er.GetMemberContextsByUserIDs(ctx, []int64{m.UserID, direct.UserID, 0, m.UserID})
	require.NoError(t, err)
	require.Len(t, contexts, 2)
	snapshot = contexts[m.UserID]
	require.Equal(t, m.ID, snapshot.MemberID)
	require.Equal(t, &d.ID, snapshot.DepartmentID)
	require.ElementsMatch(t, []int64{a, b, c}, snapshot.GroupPoolIDs)
	require.True(t, snapshot.GroupWhitelistConfigured)
	require.ElementsMatch(t, []int64{a, b}, snapshot.GroupWhitelistIDs)
	require.True(t, snapshot.AllowsGroup(b))
	require.True(t, snapshot.AllowsGroup(a))
	require.Nil(t, contexts[direct.UserID].DepartmentID)
	require.ElementsMatch(t, []int64{a, b}, contexts[direct.UserID].GroupWhitelistIDs)
	// Enterprise tightening applies while legacy child rows remain ignored.
	require.NoError(t, gr.SetWhitelist(ctx, e.ID, service.EnterpriseScopeEnterprise, e.ID, []int64{a}))
	snapshot, err = er.GetMemberContextByUserID(ctx, m.UserID)
	require.NoError(t, err)
	require.True(t, snapshot.GroupWhitelistConfigured)
	require.ElementsMatch(t, []int64{a}, snapshot.GroupWhitelistIDs)
	require.True(t, snapshot.AllowsGroup(a))
	require.NoError(t, gr.SetWhitelist(ctx, e.ID, service.EnterpriseScopeDepartment, d.ID, nil))
	snapshot, err = er.GetMemberContextByUserID(ctx, m.UserID)
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{a}, snapshot.GroupWhitelistIDs)
	// Empty pool falls back to personal rules; the remaining whitelist still applies.
	require.NoError(t, gr.SetAllowedGroupIDs(ctx, e.ID, nil))
	snapshot, err = er.GetMemberContextByUserID(ctx, m.UserID)
	require.NoError(t, err)
	require.False(t, snapshot.GroupPoolConfigured)
	e.Status = service.EnterpriseStatusDisabled
	require.NoError(t, er.Update(ctx, e))
	snapshot, err = er.GetMemberContextByUserID(ctx, m.UserID)
	require.NoError(t, err)
	require.NotNil(t, snapshot)
	require.False(t, snapshot.Active())
	m.Status = service.EnterpriseMemberStatusDisabled
	require.NoError(t, mr.Update(ctx, m))
	snapshot, err = er.GetMemberContextByUserID(ctx, m.UserID)
	require.NoError(t, err)
	require.Equal(t, service.EnterpriseMemberStatusDisabled, snapshot.MemberStatus)
	require.NoError(t, mr.Delete(ctx, e.ID, m.ID))
	snapshot, err = er.GetMemberContextByUserID(ctx, m.UserID)
	require.NoError(t, err)
	require.Nil(t, snapshot)
	whitelists, err := gr.ListWhitelists(ctx, e.ID)
	require.NoError(t, err)
	for _, w := range whitelists {
		require.False(t, w.ScopeType == service.EnterpriseScopeMember && w.ScopeID == m.ID)
	}
	contexts, err = er.GetMemberContextsByUserIDs(ctx, nil)
	require.NoError(t, err)
	require.Empty(t, contexts)
}

func TestEnterpriseRepositoryOuterTransactionRollback(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	e := enterpriseFixture(t, ctx)
	m := enterpriseMemberFixture(t, ctx, e.ID, nil)
	er := NewEnterpriseRepository(testEntClient(t))
	got, err := er.GetByID(ctx, e.ID)
	require.NoError(t, err)
	require.Equal(t, e.ID, got.ID)
	_, err = er.GetByID(context.Background(), e.ID)
	require.ErrorIs(t, err, service.ErrEnterpriseNotFound)
	require.NoError(t, tx.Rollback())
	_, err = er.GetByID(context.Background(), e.ID)
	require.ErrorIs(t, err, service.ErrEnterpriseNotFound)
	memberContext, err := er.GetMemberContextByUserID(context.Background(), m.UserID)
	require.NoError(t, err)
	require.Nil(t, memberContext)
	count, err := testEntClient(t).EnterpriseQuota.Query().Where(enterprisequota.EnterpriseIDEQ(e.ID)).Count(context.Background())
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestEnterpriseRepositoryScopeIsolationAndAtomicGroupReplacement(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	er := NewEnterpriseRepository(client)
	dr := NewEnterpriseDepartmentRepository(client)
	mr := NewEnterpriseMemberRepository(client)
	gr := NewEnterpriseGroupRepository(client)
	e := enterpriseFixture(t, ctx)
	other := enterpriseFixture(t, ctx)
	d := &service.EnterpriseDepartment{EnterpriseID: other.ID, Name: "Other enterprise"}
	require.NoError(t, dr.Create(ctx, d))
	member := enterpriseMemberFixture(t, ctx, e.ID, nil)
	_, err := dr.GetByID(ctx, e.ID, d.ID)
	require.ErrorIs(t, err, service.ErrEnterpriseDepartmentNotFound)
	_, err = mr.GetByID(ctx, other.ID, member.ID)
	require.ErrorIs(t, err, service.ErrEnterpriseMemberNotFound)
	require.ErrorIs(t, gr.SetWhitelist(ctx, e.ID, service.EnterpriseScopeDepartment, d.ID, nil), service.ErrEnterpriseInvalidScope)
	require.ErrorIs(t, gr.SetWhitelist(ctx, e.ID, "arbitrary_table", d.ID, nil), service.ErrEnterpriseInvalidScope)
	a := enterpriseGroupFixture(t, client)
	require.NoError(t, gr.SetAllowedGroupIDs(ctx, e.ID, []int64{a}))
	require.NoError(t, gr.SetWhitelist(ctx, e.ID, service.EnterpriseScopeMember, member.ID, []int64{a}))
	invalid := int64(-1)
	require.Error(t, gr.SetAllowedGroupIDs(ctx, e.ID, []int64{a, invalid}))
	pool, err := gr.GetAllowedGroupIDs(ctx, e.ID)
	require.NoError(t, err)
	require.Equal(t, []int64{a}, pool)
	require.Error(t, gr.SetWhitelist(ctx, e.ID, service.EnterpriseScopeMember, member.ID, []int64{invalid}))
	whitelist, err := gr.GetWhitelist(ctx, e.ID, service.EnterpriseScopeMember, member.ID)
	require.NoError(t, err)
	require.Equal(t, []int64{a}, whitelist.GroupIDs)
	// SQL defaults must decode identically to Ent/domain defaults.
	_, err = client.ExecContext(ctx, `UPDATE enterprises SET settings = '{}'::jsonb WHERE id=$1`, e.ID)
	require.NoError(t, err)
	loaded, err := er.GetByID(ctx, e.ID)
	require.NoError(t, err)
	require.Equal(t, service.DefaultEnterpriseSettings(), loaded.Settings)
}

func TestEnterpriseMemberOwnerCountLocksCallerTransaction(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	e := enterpriseFixture(t, ctx)
	mr := NewEnterpriseMemberRepository(client)
	first := enterpriseMemberFixture(t, ctx, e.ID, nil)
	second := enterpriseMemberFixture(t, ctx, e.ID, nil)
	first.Role = service.EnterpriseRoleOwner
	second.Role = service.EnterpriseRoleOwner
	require.NoError(t, mr.Update(ctx, first))
	require.NoError(t, mr.Update(ctx, second))
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	count, err := mr.CountActiveOwners(txCtx, e.ID)
	require.NoError(t, err)
	require.EqualValues(t, 2, count)
	// Another management transaction must wait before it can make an owner decision.
	waiting := testEntTx(t)
	deadline, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	_, err = mr.CountActiveOwners(dbent.NewTxContext(deadline, waiting), e.ID)
	require.Error(t, err, "the competing owner count must wait on the enterprise lock")
	require.ErrorIs(t, deadline.Err(), context.DeadlineExceeded)
	// lib/pq may close the connection on context cancellation, so rollback is
	// cleanup only and can return driver.ErrBadConn for the waiting transaction.
	_ = waiting.Rollback()
	first.Status = service.EnterpriseMemberStatusDisabled
	require.NoError(t, mr.Update(txCtx, first))
	count, err = mr.CountActiveOwners(txCtx, e.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	require.NoError(t, tx.Commit())
	count, err = mr.CountActiveOwners(ctx, e.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
}
