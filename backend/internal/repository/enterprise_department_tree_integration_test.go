//go:build integration

package repository

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func treeDepartment(t *testing.T, ctx context.Context, client *dbent.Client, eid int64, parent *int64, name string) *service.EnterpriseDepartment {
	t.Helper()
	d := &service.EnterpriseDepartment{EnterpriseID: eid, ParentID: parent, Name: name}
	require.NoError(t, NewEnterpriseDepartmentRepository(client).Create(ctx, d))
	return d
}

func treeManagementService(client *dbent.Client) *service.EnterpriseService {
	return service.NewEnterpriseService(client, NewEnterpriseRepository(client), NewEnterpriseDepartmentRepository(client), NewEnterpriseMemberRepository(client), NewEnterpriseQuotaRepository(client), NewEnterpriseBalanceLogRepository(client), NewEnterpriseAuditLogRepository(client), NewEnterpriseGroupRepository(client), NewUserRepository(client, integrationDB), NewGroupRepository(client, integrationDB), nil, nil, service.NewNoopEnterpriseBillingCache())
}

func treeBillingFixture(t *testing.T) (enterpriseBillingFixture, []*service.EnterpriseDepartment) {
	t.Helper()
	f := newEnterpriseBillingFixture(t)
	// These tests commit management audit / whitelist data in addition to billing.
	// Register after the billing fixture so foreign keys are removed first.
	t.Cleanup(func() {
		for _, table := range []string{"enterprise_audit_logs", "enterprise_group_whitelists", "enterprise_group_whitelist_scopes", "enterprise_allowed_groups"} {
			_, err := integrationDB.ExecContext(context.Background(), "DELETE FROM "+table+" WHERE enterprise_id=$1", f.enterprise.ID)
			require.NoError(t, err)
		}
	})
	client := testEntClient(t)
	ctx := context.Background()
	nodes := []*service.EnterpriseDepartment{f.department}
	for i := 1; i < 4; i++ {
		nodes = append(nodes, treeDepartment(t, ctx, client, f.enterprise.ID, &nodes[i-1].ID, fmt.Sprintf("level-%d", i+1)))
	}
	f.member.DepartmentID = &nodes[3].ID
	require.NoError(t, NewEnterpriseMemberRepository(client).Update(ctx, f.member))
	return f, nodes
}

func TestEnterpriseDepartmentTreeMoveDeleteDepthAndSiblingUniqueness(t *testing.T) {
	ctx := context.Background()
	f, nodes := treeBillingFixture(t)
	client := testEntClient(t)
	dr := NewEnterpriseDepartmentRepository(client)
	sibling := treeDepartment(t, ctx, client, f.enterprise.ID, nil, "other-root")
	sameName := treeDepartment(t, ctx, client, f.enterprise.ID, &sibling.ID, nodes[1].Name)
	require.ErrorIs(t, dr.Create(ctx, &service.EnterpriseDepartment{EnterpriseID: f.enterprise.ID, ParentID: &sibling.ID, Name: sameName.Name}), service.ErrEnterpriseDepartmentNameExists)
	require.ErrorIs(t, dr.Delete(ctx, f.enterprise.ID, nodes[0].ID), service.ErrEnterpriseDepartmentNotEmpty)
	root, err := dr.GetByID(ctx, f.enterprise.ID, nodes[0].ID)
	require.NoError(t, err)
	require.Zero(t, root.MemberCount)
	require.EqualValues(t, 1, root.TotalMemberCount)
	nodes[0].ParentID = &nodes[3].ID
	require.ErrorIs(t, dr.Update(ctx, nodes[0]), service.ErrEnterpriseDepartmentCycle)
	nodes[0].ParentID = nil
	nodes[1].ParentID = &sibling.ID
	nodes[1].Name = "moved"
	quotaBefore, err := NewEnterpriseQuotaRepository(client).GetByScope(ctx, f.enterprise.ID, service.EnterpriseScopeDepartment, nodes[1].ID)
	require.NoError(t, err)
	require.NoError(t, dr.Update(ctx, nodes[1]))
	leaf, err := dr.GetByID(ctx, f.enterprise.ID, nodes[3].ID)
	require.NoError(t, err)
	require.Equal(t, []int64{sibling.ID, nodes[1].ID, nodes[2].ID, nodes[3].ID}, leaf.PathIDs)
	require.Equal(t, []string{sibling.Name, "moved", nodes[2].Name, nodes[3].Name}, leaf.PathNames)
	require.Equal(t, 4, leaf.Depth)
	quotaAfter, err := NewEnterpriseQuotaRepository(client).GetByScope(ctx, f.enterprise.ID, service.EnterpriseScopeDepartment, nodes[1].ID)
	require.NoError(t, err)
	require.Equal(t, quotaBefore.ID, quotaAfter.ID)
	parent := sibling
	for depth := 2; depth <= 8; depth++ {
		parent = treeDepartment(t, ctx, client, f.enterprise.ID, &parent.ID, fmt.Sprintf("deep-%d", depth))
	}
	require.ErrorIs(t, dr.Create(ctx, &service.EnterpriseDepartment{EnterpriseID: f.enterprise.ID, ParentID: &parent.ID, Name: "too-deep"}), service.ErrEnterpriseDepartmentDepthExceeded)
	nodes[1].ParentID = &parent.ID
	require.ErrorIs(t, dr.Update(ctx, nodes[1]), service.ErrEnterpriseDepartmentDepthExceeded)
	zero := int64(0)
	svc := treeManagementService(client)
	moved, err := svc.UpdateDepartment(ctx, service.EnterpriseActor{UserID: f.member.UserID, IsAdmin: true}, f.enterprise.ID, nodes[1].ID, service.UpdateEnterpriseDepartmentInput{ParentID: &zero})
	require.NoError(t, err)
	require.Nil(t, moved.ParentID)
	require.Equal(t, 1, moved.Depth)
	leaf, err = dr.GetByID(ctx, f.enterprise.ID, nodes[3].ID)
	require.NoError(t, err)
	require.Equal(t, 3, leaf.Depth)
}

func TestEnterpriseDepartmentTreeConcurrentMovesAndMemberManagementNoDeadlock(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	f, _ := treeBillingFixture(t)
	client := testEntClient(t)
	a := treeDepartment(t, ctx, client, f.enterprise.ID, nil, "concurrent-A")
	b := treeDepartment(t, ctx, client, f.enterprise.ID, nil, "concurrent-B")
	svc := treeManagementService(client)
	actor := service.EnterpriseActor{UserID: f.member.UserID, IsAdmin: true}
	start := make(chan struct{})
	results := make(chan error, 3)
	var ready sync.WaitGroup
	ready.Add(3)
	for _, pair := range [][2]int64{{a.ID, b.ID}, {b.ID, a.ID}} {
		go func(pair [2]int64) {
			ready.Done()
			<-start
			_, err := svc.UpdateDepartment(ctx, actor, f.enterprise.ID, pair[0], service.UpdateEnterpriseDepartmentInput{ParentID: &pair[1]})
			results <- err
		}(pair)
	}
	go func() {
		ready.Done()
		<-start
		title := "concurrent"
		_, err := svc.UpdateMember(ctx, actor, f.enterprise.ID, f.member.ID, service.UpdateEnterpriseMemberInput{Title: &title})
		results <- err
	}()
	ready.Wait()
	close(start)
	success, cycles := 0, 0
	for range 3 {
		select {
		case err := <-results:
			if err == nil {
				success++
			} else {
				require.ErrorIs(t, err, service.ErrEnterpriseDepartmentCycle)
				cycles++
			}
		case <-ctx.Done():
			t.Fatal("department/member mutation deadlocked")
		}
	}
	require.Equal(t, 2, success)
	require.Equal(t, 1, cycles)
}

func TestUsageBillingFourLevelFrozenChainApplyIdempotentAfterMove(t *testing.T) {
	ctx := context.Background()
	f, nodes := treeBillingFixture(t)
	client := testEntClient(t)
	membership, err := NewEnterpriseRepository(client).GetMemberContextByUserID(ctx, f.member.UserID)
	require.NoError(t, err)
	cmd := f.command(1.25)
	cmd.EnterpriseDepartmentID = nodes[3].ID
	cmd.EnterpriseDepartmentPathIDs = membership.DepartmentPathIDs
	require.Len(t, cmd.EnterpriseDepartmentPathIDs, 4)
	// Reparent and archive the old root before the frozen request finishes.
	zero := int64(0)
	_, err = treeManagementService(client).UpdateDepartment(ctx, service.EnterpriseActor{UserID: f.member.UserID, IsAdmin: true}, f.enterprise.ID, nodes[1].ID, service.UpdateEnterpriseDepartmentInput{ParentID: &zero})
	require.NoError(t, err)
	require.NoError(t, NewEnterpriseDepartmentRepository(client).Delete(ctx, f.enterprise.ID, nodes[0].ID))
	billing := NewUsageBillingRepository(client, integrationDB)
	result, err := billing.Apply(ctx, cmd)
	require.NoError(t, err)
	require.True(t, result.Applied)
	result, err = billing.Apply(ctx, cmd)
	require.NoError(t, err)
	require.False(t, result.Applied)
	rows, err := integrationDB.QueryContext(ctx, `SELECT total_usage_usd FROM enterprise_quotas WHERE enterprise_id=$1 ORDER BY id`, f.enterprise.ID)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	count := 0
	for rows.Next() {
		var used float64
		require.NoError(t, rows.Scan(&used))
		require.Equal(t, 1.25, used)
		count++
	}
	require.NoError(t, rows.Err())
	require.Equal(t, 6, count)
}

func TestEnterpriseTreeWhitelistContextAndParentWrites(t *testing.T) {
	ctx := context.Background()
	f, nodes := treeBillingFixture(t)
	client := testEntClient(t)
	er := NewEnterpriseRepository(client)
	gr := NewEnterpriseGroupRepository(client)
	a, b, c := enterpriseGroupFixture(t, client), enterpriseGroupFixture(t, client), enterpriseGroupFixture(t, client)
	require.NoError(t, gr.SetAllowedGroupIDs(ctx, f.enterprise.ID, []int64{a, b, c}))
	for _, v := range []struct {
		scope  string
		id     int64
		groups []int64
	}{{service.EnterpriseScopeEnterprise, f.enterprise.ID, []int64{a, b, c}}, {service.EnterpriseScopeDepartment, nodes[0].ID, []int64{a, b}}, {service.EnterpriseScopeDepartment, nodes[1].ID, []int64{b, c}}, {service.EnterpriseScopeDepartment, nodes[2].ID, []int64{b}}, {service.EnterpriseScopeMember, f.member.ID, []int64{a, b}}} {
		require.NoError(t, gr.SetWhitelist(ctx, f.enterprise.ID, v.scope, v.id, v.groups))
	}
	member, err := er.GetMemberContextByUserID(ctx, f.member.UserID)
	require.NoError(t, err)
	require.Equal(t, []int64{b}, member.GroupWhitelistIDs)
	require.True(t, member.AllowsGroup(b))
	require.False(t, member.AllowsGroup(a))
	require.False(t, member.AllowsGroup(c))
	svc := treeManagementService(client)
	actor := service.EnterpriseActor{UserID: f.member.UserID, IsAdmin: true}
	require.ErrorIs(t, svc.SetGroupWhitelist(ctx, actor, f.enterprise.ID, service.EnterpriseScopeDepartment, nodes[3].ID, []int64{c}), service.ErrEnterpriseGroupOutOfScope)
	require.NoError(t, svc.SetGroupWhitelist(ctx, actor, f.enterprise.ID, service.EnterpriseScopeDepartment, nodes[3].ID, []int64{b}))
	// PUT [] cancels a layer. To exercise a configured-empty restriction,
	// remove its group rows while preserving the configuration marker.
	_, err = integrationDB.ExecContext(ctx, `DELETE FROM enterprise_group_whitelists WHERE enterprise_id=$1 AND scope_type='department' AND scope_id=$2`, f.enterprise.ID, nodes[1].ID)
	require.NoError(t, err)
	member, err = er.GetMemberContextByUserID(ctx, f.member.UserID)
	require.NoError(t, err)
	require.True(t, member.GroupWhitelistConfigured)
	require.Empty(t, member.GroupWhitelistIDs)
}

func TestEnterpriseManagerSubtreeListsQuotasUsageRankingsAndOverrides(t *testing.T) {
	ctx := context.Background()
	f, nodes := treeBillingFixture(t)
	client := testEntClient(t)
	mr := NewEnterpriseMemberRepository(client)
	manager := enterpriseMemberFixture(t, ctx, f.enterprise.ID, &nodes[1].ID)
	manager.Role = service.EnterpriseRoleManager
	require.NoError(t, mr.Update(ctx, manager))
	sibling := treeDepartment(t, ctx, client, f.enterprise.ID, &nodes[0].ID, "sibling")
	outside := enterpriseMemberFixture(t, ctx, f.enterprise.ID, &sibling.ID)
	svc := treeManagementService(client)
	actor := service.EnterpriseActor{UserID: manager.UserID}
	er := NewEnterpriseRepository(client)
	e, err := er.GetByID(ctx, f.enterprise.ID)
	require.NoError(t, err)
	e.Settings.ManagerCanManageMembers = true
	e.Settings.ManagerCanEditMemberQuota = true
	e.Settings.ManagerCanEditMemberGroups = true
	e.Settings.DepartmentQuotaVisibleToManager = true
	e.Settings.MemberQuotaVisibleToManager = true
	require.NoError(t, er.Update(ctx, e))
	memberContext, err := er.GetMemberContextByUserID(ctx, manager.UserID)
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{nodes[1].ID, nodes[2].ID, nodes[3].ID}, memberContext.DepartmentSubtreeIDs)
	deps, err := svc.ListDepartments(ctx, actor, e.ID)
	require.NoError(t, err)
	require.Len(t, deps, 3)
	lastLogin := time.Now().UTC().Truncate(time.Microsecond)
	_, err = client.User.UpdateOneID(f.member.UserID).SetLastLoginAt(lastLogin).Save(ctx)
	require.NoError(t, err)
	members, page, err := svc.ListMembers(ctx, actor, e.ID, pagination.DefaultPagination(), service.EnterpriseMemberListFilters{})
	require.NoError(t, err)
	require.Len(t, members, 2)
	require.EqualValues(t, 2, page.Total)
	pathNames := []string{nodes[0].Name, nodes[1].Name, nodes[2].Name, nodes[3].Name}
	for _, m := range members {
		if m.ID == f.member.ID {
			require.Equal(t, pathNames, m.DepartmentPathNames)
			require.Equal(t, nodes[3].Name, m.DepartmentName)
			require.NotNil(t, m.LastLoginAt)
			require.True(t, lastLogin.Equal(*m.LastLoginAt))
		}
	}
	identity, err := svc.Identity(ctx, f.member.UserID)
	require.NoError(t, err)
	require.Equal(t, strings.Join(pathNames, "/"), *identity.DepartmentName)
	overview, err := svc.Overview(ctx, service.EnterpriseActor{UserID: f.member.UserID}, e.ID)
	require.NoError(t, err)
	require.Equal(t, strings.Join(pathNames, "/"), overview.Me.DepartmentName)
	require.Equal(t, pathNames, overview.Me.DepartmentPathNames)
	for _, forbidden := range []int64{sibling.ID, nodes[0].ID} {
		_, _, err := svc.ListMembers(ctx, actor, e.ID, pagination.DefaultPagination(), service.EnterpriseMemberListFilters{DepartmentID: &forbidden})
		require.ErrorIs(t, err, service.ErrEnterprisePermissionDenied)
	}
	title := "managed"
	_, err = svc.UpdateMember(ctx, actor, e.ID, f.member.ID, service.UpdateEnterpriseMemberInput{Title: &title})
	require.NoError(t, err)
	_, err = svc.UpdateMember(ctx, actor, e.ID, outside.ID, service.UpdateEnterpriseMemberInput{Title: &title})
	require.ErrorIs(t, err, service.ErrEnterprisePermissionDenied)
	quota := 2.0
	_, err = svc.UpdateQuota(ctx, actor, e.ID, service.EnterpriseScopeMember, f.member.ID, service.EnterpriseQuotaLimitsInput{DailyLimitUSD: &quota})
	require.NoError(t, err)
	_, err = svc.UpdateQuota(ctx, actor, e.ID, service.EnterpriseScopeMember, outside.ID, service.EnterpriseQuotaLimitsInput{DailyLimitUSD: &quota})
	require.ErrorIs(t, err, service.ErrEnterprisePermissionDenied)
	require.NoError(t, svc.SetGroupWhitelist(ctx, actor, e.ID, service.EnterpriseScopeMember, f.member.ID, []int64{}))
	require.ErrorIs(t, svc.SetGroupWhitelist(ctx, actor, e.ID, service.EnterpriseScopeMember, outside.ID, []int64{}), service.ErrEnterprisePermissionDenied)
	quotas, err := svc.ListQuotas(ctx, actor, e.ID, "")
	require.NoError(t, err)
	require.Len(t, quotas, 5)
	// Use the raw usage reader so the same constraints exercise logs, statistics,
	// member ranking and the newly available manager department ranking.
	account := mustCreateAccount(t, client, &service.Account{Name: "tree-usage"})
	t.Cleanup(func() {
		_, err := integrationDB.Exec(`DELETE FROM usage_logs WHERE account_id=$1`, account.ID)
		require.NoError(t, err)
		_, err = integrationDB.Exec(`DELETE FROM accounts WHERE id=$1`, account.ID)
		require.NoError(t, err)
	})
	repo := NewUsageLogRepository(client, integrationDB)
	for _, v := range []struct {
		department, user int64
		cost             float64
	}{{nodes[3].ID, f.member.UserID, 1}, {nodes[2].ID, f.member.UserID, 2}, {sibling.ID, outside.UserID, 100}, {nodes[0].ID, outside.UserID, 1000}} {
		log := &service.UsageLog{UserID: v.user, APIKeyID: f.key.ID, AccountID: account.ID, Model: "tree-model", ActualCost: v.cost, CreatedAt: time.Now()}
		service.ApplyUsageLogPayerIdentity(log, e.ID, v.department)
		_, err := repo.Create(ctx, log)
		require.NoError(t, err)
	}
	usage := service.NewEnterpriseUsageService(repo)
	scope := service.EnterpriseUsageScope{EnterpriseID: e.ID, UserID: manager.UserID, Member: memberContext}
	logs, _, err := usage.List(ctx, scope, pagination.DefaultPagination(), usagestats.UsageLogFilters{})
	require.NoError(t, err)
	require.Len(t, logs, 2)
	stats, err := usage.Stats(ctx, scope, usagestats.UsageLogFilters{})
	require.NoError(t, err)
	require.Equal(t, 3.0, stats.TotalActualCost)
	ranks, err := usage.Members(ctx, scope, usagestats.UsageLogFilters{})
	require.NoError(t, err)
	require.Len(t, ranks, 1)
	require.Equal(t, 3.0, ranks[0].ActualCost)
	departments, err := usage.Departments(ctx, scope, usagestats.UsageLogFilters{})
	require.NoError(t, err)
	require.Len(t, departments, 2)
	for _, id := range []int64{sibling.ID, nodes[0].ID} {
		_, err := usage.Departments(ctx, scope, usagestats.UsageLogFilters{EnterpriseDepartmentID: id})
		require.ErrorIs(t, err, service.ErrEnterprisePermissionDenied)
	}
	stats, err = usage.Stats(ctx, scope, usagestats.UsageLogFilters{EnterpriseDepartmentID: nodes[2].ID})
	require.NoError(t, err)
	require.Equal(t, 3.0, stats.TotalActualCost)
}

func TestEnterpriseTreeQuotaHierarchyAllAncestorsAndParentReduction(t *testing.T) {
	ctx := context.Background()
	f, nodes := treeBillingFixture(t)
	svc := treeManagementService(testEntClient(t))
	actor := service.EnterpriseActor{UserID: f.member.UserID, IsAdmin: true}
	set := func(scope string, id int64, limit float64) error {
		_, err := svc.UpdateQuota(ctx, actor, f.enterprise.ID, scope, id, service.EnterpriseQuotaLimitsInput{DailyLimitUSD: &limit})
		return err
	}
	require.NoError(t, set(service.EnterpriseScopeEnterprise, f.enterprise.ID, 100))
	require.NoError(t, set(service.EnterpriseScopeDepartment, nodes[0].ID, 20))
	require.NoError(t, set(service.EnterpriseScopeDepartment, nodes[1].ID, 10))
	// An unlimited intermediate department does not erase the ancestor cap.
	require.ErrorIs(t, set(service.EnterpriseScopeDepartment, nodes[3].ID, 11), service.ErrEnterpriseQuotaExceedsParent)
	require.NoError(t, set(service.EnterpriseScopeDepartment, nodes[3].ID, 8))
	require.ErrorIs(t, set(service.EnterpriseScopeMember, f.member.ID, 9), service.ErrEnterpriseQuotaExceedsParent)
	require.NoError(t, set(service.EnterpriseScopeMember, f.member.ID, 7))
	require.ErrorIs(t, set(service.EnterpriseScopeDepartment, nodes[1].ID, 6), service.ErrEnterpriseQuotaExceedsParent)
	require.ErrorIs(t, set(service.EnterpriseScopeEnterprise, f.enterprise.ID, 5), service.ErrEnterpriseQuotaExceedsParent)
}

func TestBatchImageEnterpriseFrozenTreeSnapshotSurvivesMove(t *testing.T) {
	ctx := context.Background()
	f, nodes := treeBillingFixture(t)
	client := testEntClient(t)
	f.department = nodes[3]
	job := enterpriseBatchJobFixture(t, f, 4)
	require.Equal(t, []int64{nodes[0].ID, nodes[1].ID, nodes[2].ID, nodes[3].ID}, job.EnterpriseDepartmentPathIDs)
	loaded, err := NewBatchImageRepository(integrationDB).GetBatchImageJobByBatchID(ctx, job.BatchID)
	require.NoError(t, err)
	require.Equal(t, job.EnterpriseDepartmentPathIDs, loaded.EnterpriseDepartmentPathIDs)
	billing := NewUsageBillingRepository(client, integrationDB)
	hold, err := billing.ReserveBatchImageBalance(ctx, enterpriseBatchHoldCommand(loaded, "reserve", 0))
	require.NoError(t, err)
	require.True(t, hold.Applied)
	zero := int64(0)
	_, err = treeManagementService(client).UpdateDepartment(ctx, service.EnterpriseActor{UserID: f.member.UserID, IsAdmin: true}, f.enterprise.ID, nodes[2].ID, service.UpdateEnterpriseDepartmentInput{ParentID: &zero})
	require.NoError(t, err)
	capture, err := billing.CaptureBatchImageBalance(ctx, enterpriseBatchHoldCommand(loaded, "capture", 2.5))
	require.NoError(t, err)
	require.True(t, capture.Applied)
	capture, err = billing.CaptureBatchImageBalance(ctx, enterpriseBatchHoldCommand(loaded, "capture", 2.5))
	require.NoError(t, err)
	require.False(t, capture.Applied)
	rows, err := integrationDB.QueryContext(ctx, `SELECT total_usage_usd FROM enterprise_quotas WHERE enterprise_id=$1`, f.enterprise.ID)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	count := 0
	for rows.Next() {
		var used float64
		require.NoError(t, rows.Scan(&used))
		require.Equal(t, 2.5, used)
		count++
	}
	require.NoError(t, rows.Err())
	require.Equal(t, 6, count)
}
