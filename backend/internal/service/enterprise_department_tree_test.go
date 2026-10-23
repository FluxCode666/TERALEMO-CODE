//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

func TestEnterpriseDepartmentPathParsing(t *testing.T) {
	ids, err := ParseEnterpriseDepartmentPath("/3/7/11/15/")
	require.NoError(t, err)
	require.Equal(t, []int64{3, 7, 11, 15}, ids)
	for _, path := range []string{"3/7/", "/3//7/", "/0/", "/3/3/", "/1/2/3/4/5/6/7/8/9/"} {
		_, err := ParseEnterpriseDepartmentPath(path)
		require.Error(t, err, path)
	}
	require.Equal(t, "/3/7/11/15/", EnterpriseDepartmentPath(ids))
}

func TestEnterpriseBillingFourLevelAncestorsFailClosedAndWindowReset(t *testing.T) {
	ctx := context.Background()
	now := timezone.Now()
	expired := now.AddDate(0, 0, -40)
	for _, cached := range []bool{false, true} {
		for ancestor := range 4 {
			member := &EnterpriseMemberContext{EnterpriseID: 10, MemberID: 30, DepartmentID: enterpriseTestID(23), DepartmentPathIDs: []int64{20, 21, 22, 23}, EnterpriseStatus: EnterpriseStatusActive, MemberStatus: EnterpriseMemberStatusActive}
			quotas := []EnterpriseQuota{}
			for i, scope := range enterpriseBillingScopes(member) {
				quotas = append(quotas, EnterpriseQuota{ID: int64(i + 1), EnterpriseID: 10, ScopeType: scope.ScopeType, ScopeID: scope.ScopeID})
			}
			limit := 1.0
			q := &quotas[ancestor+1]
			q.DailyLimitUSD = &limit
			q.DailyUsageUSD = 1
			q.DailyWindowStart = &now
			reader := &enterpriseQuotaReaderStub{quotas: quotas}
			svc := &BillingCacheService{enterpriseBalanceRepo: &enterpriseBalanceReaderStub{balance: 100}, enterpriseQuotaRepo: reader}
			if cached {
				svc.enterpriseCache = &enterpriseRuntimeCacheStub{balance: 100, quotas: quotas}
			}
			require.ErrorIs(t, svc.checkEnterpriseBillingEligibility(ctx, member, &Group{ID: 1}, 0), ErrEnterpriseBillingDepartmentQuotaExceeded)
			q.DailyWindowStart = &expired
			require.NoError(t, svc.checkEnterpriseBillingEligibility(ctx, member, &Group{ID: 1}, 0))
			if cached {
				svc.enterpriseCache = &enterpriseRuntimeCacheStub{balance: 100, quotas: quotas[:len(quotas)-1]}
			}
			reader.quotas = quotas[:len(quotas)-1]
			require.ErrorIs(t, svc.checkEnterpriseBillingEligibility(ctx, member, &Group{ID: 1}, 0), ErrBillingServiceUnavailable)
		}
	}
}

func TestEnterpriseManagerSubtreePermissionsUsageAndRanking(t *testing.T) {
	c := &EnterpriseMemberContext{EnterpriseID: 1, MemberID: 2, DepartmentID: enterpriseTestID(7), DepartmentPathIDs: []int64{3, 7}, DepartmentSubtreeIDs: []int64{7, 11, 15}, Role: EnterpriseRoleManager, EnterpriseStatus: EnterpriseStatusActive, MemberStatus: EnterpriseMemberStatusActive}
	settings := EnterpriseSettings{ManagerCanManageMembers: true, ManagerCanEditMemberQuota: true, ManagerCanEditMemberGroups: true, DepartmentQuotaVisibleToManager: true, MemberQuotaVisibleToManager: true}
	for _, id := range []int64{7, 11, 15, 3, 99} {
		m := &EnterpriseMember{EnterpriseID: 1, DepartmentID: &id, DepartmentPathIDs: []int64{3, id}, Role: EnterpriseRoleMember}
		if id == 11 || id == 15 {
			m.DepartmentPathIDs = []int64{3, 7, 11, id}
		}
		for _, op := range []EnterprisePermission{EnterprisePermissionManageMember, EnterprisePermissionEditQuota, EnterprisePermissionViewQuota} {
			err := CheckEnterprisePermission(c, settings, op, EnterprisePermissionTarget{ScopeType: EnterpriseScopeMember, ScopeID: 100, Member: m})
			require.Equal(t, id == 7 || id == 11 || id == 15, err == nil, "id=%d op=%s", id, op)
		}
		_, err := ConstrainEnterpriseUsageFilters(EnterpriseUsageScope{EnterpriseID: 1, UserID: 1, Member: c}, usagestats.UsageLogFilters{EnterpriseDepartmentID: id}, time.Now())
		require.Equal(t, id == 7 || id == 11 || id == 15, err == nil)
	}
	require.NoError(t, CheckEnterprisePermission(c, settings, EnterprisePermissionDepartmentUsage, EnterprisePermissionTarget{}))
	repo := &enterpriseUsageRepositoryStub{}
	svc := NewEnterpriseUsageService(repo)
	_, err := svc.Departments(context.Background(), EnterpriseUsageScope{EnterpriseID: 1, Member: c}, usagestats.UsageLogFilters{EnterpriseDepartmentID: 11})
	require.NoError(t, err)
	require.EqualValues(t, 11, repo.captured.EnterpriseDepartmentID)
	settings.ManagerCanManageMembers = false
	require.ErrorIs(t, CheckEnterprisePermission(c, settings, EnterprisePermissionManageMember, EnterprisePermissionTarget{Member: &EnterpriseMember{DepartmentID: enterpriseTestID(11), DepartmentPathIDs: []int64{3, 7, 11}, Role: EnterpriseRoleMember}}), ErrEnterprisePermissionDenied)
}

func TestEnterpriseTreeAuthSnapshotVersionAndClone(t *testing.T) {
	require.Equal(t, 29, apiKeyAuthSnapshotVersion)
	c := &EnterpriseMemberContext{DepartmentID: enterpriseTestID(15), DepartmentPathIDs: []int64{3, 7, 11, 15}, DepartmentSubtreeIDs: []int64{15, 19}}
	clone := cloneEnterpriseMemberContext(c)
	clone.DepartmentPathIDs[0] = 99
	clone.DepartmentSubtreeIDs[0] = 99
	*clone.DepartmentID = 99
	require.Equal(t, []int64{3, 7, 11, 15}, c.DepartmentPathIDs)
	require.Equal(t, []int64{15, 19}, c.DepartmentSubtreeIDs)
	require.EqualValues(t, 15, *c.DepartmentID)
}

func TestEnterpriseBatchHoldCarriesFrozenAncestorChain(t *testing.T) {
	job := &BatchImageJob{APIKeyID: enterpriseTestID(1), EnterpriseID: enterpriseTestID(2), EnterpriseMemberID: enterpriseTestID(3), EnterpriseDepartmentID: enterpriseTestID(15), EnterpriseDepartmentPathIDs: []int64{3, 7, 11, 15}}
	cmd, err := buildBatchImageHoldCommand(job, "hold", 0, "")
	require.NoError(t, err)
	require.Equal(t, job.EnterpriseDepartmentPathIDs, cmd.EnterpriseDepartmentPathIDs)
	cmd.EnterpriseDepartmentPathIDs[0] = 99
	require.EqualValues(t, 3, job.EnterpriseDepartmentPathIDs[0])
}

func TestEnterpriseFrozenChainChangesBillingFingerprint(t *testing.T) {
	a := &UsageBillingCommand{EnterpriseID: 1, EnterpriseMemberID: 2, EnterpriseDepartmentID: 11, EnterpriseDepartmentPathIDs: []int64{3, 7, 11}}
	b := *a
	b.EnterpriseDepartmentPathIDs = []int64{3, 9, 11}
	a.Normalize()
	b.Normalize()
	require.NotEqual(t, a.RequestFingerprint, b.RequestFingerprint)
	old := &UsageBillingCommand{EnterpriseID: 1, EnterpriseMemberID: 2, EnterpriseDepartmentID: 11}
	root := *old
	root.EnterpriseDepartmentPathIDs = []int64{11}
	old.Normalize()
	root.Normalize()
	require.Equal(t, old.RequestFingerprint, root.RequestFingerprint)
	require.Error(t, ValidateFrozenEnterpriseDepartmentIDs([]int64{3, 7, 11}, 7))
	require.Error(t, ValidateFrozenEnterpriseDepartmentIDs([]int64{3, 3, 11}, 11))
}

type enterpriseTreeMoveDepartmentsStub struct {
	*enterpriseManagementTestDepartments
}

func (s *enterpriseTreeMoveDepartmentsStub) Update(_ context.Context, d *EnterpriseDepartment) error {
	d.PathIDs = []int64{4, d.ID}
	d.PathNames = []string{"root", "moved"}
	d.Path = EnterpriseDepartmentPath(d.PathIDs)
	d.Depth = 2
	s.items[d.ID] = *d
	return nil
}
func TestEnterpriseDepartmentMoveInvalidatesAuthWithDelayedSecondDelete(t *testing.T) {
	f := newEnterpriseManagementTestFixture(t)
	departments := &enterpriseTreeMoveDepartmentsStub{&enterpriseManagementTestDepartments{items: map[int64]EnterpriseDepartment{3: {ID: 3, EnterpriseID: 1, PathIDs: []int64{3}, Path: "/3/", Depth: 1}, 4: {ID: 4, EnterpriseID: 1, PathIDs: []int64{4}, Path: "/4/", Depth: 1}}}}
	f.s.departments = departments
	f.q.items[EnterpriseQuotaScope{EnterpriseScopeDepartment, 4}] = EnterpriseQuota{EnterpriseID: 1, ScopeType: EnterpriseScopeDepartment, ScopeID: 4}
	cache := &enterpriseDelayedInvalidationStub{calls: make(chan enterpriseDelayedInvalidationCall, 6)}
	f.s.authCache = cache
	_, err := f.s.UpdateDepartment(context.Background(), EnterpriseActor{UserID: 10, IsAdmin: true}, 1, 3, UpdateEnterpriseDepartmentInput{ParentID: enterpriseTestID(4)})
	require.NoError(t, err)
	for range 2 {
		for _, id := range f.e.ids {
			select {
			case call := <-cache.calls:
				require.Equal(t, id, call.id)
			case <-time.After(4 * time.Second):
				t.Fatal("move omitted delayed authentication invalidation")
			}
		}
	}
}

func TestEnterpriseDepartmentWhitelistWritesRemainDisabledWithAncestors(t *testing.T) {
	f := newEnterpriseManagementTestFixture(t)
	d := &enterpriseManagementTestDepartments{items: map[int64]EnterpriseDepartment{3: {ID: 3, EnterpriseID: 1, PathIDs: []int64{3}}, 4: {ID: 4, EnterpriseID: 1, PathIDs: []int64{3, 4}}, 5: {ID: 5, EnterpriseID: 1, PathIDs: []int64{3, 4, 5}}}}
	f.s.departments = d
	f.e.e.GroupPoolConfigured = true
	f.g.pool = []int64{1, 2}
	f.g.whitelists[EnterpriseQuotaScope{EnterpriseScopeEnterprise, 1}] = EnterpriseGroupWhitelist{GroupIDs: []int64{1, 2}}
	f.g.whitelists[EnterpriseQuotaScope{EnterpriseScopeDepartment, 3}] = EnterpriseGroupWhitelist{GroupIDs: []int64{1}}
	f.g.whitelists[EnterpriseQuotaScope{EnterpriseScopeDepartment, 4}] = EnterpriseGroupWhitelist{GroupIDs: []int64{1, 2}}
	actor := EnterpriseActor{UserID: 10, IsAdmin: true}
	require.ErrorIs(t, f.s.SetGroupWhitelist(context.Background(), actor, 1, EnterpriseScopeDepartment, 5, []int64{2}), ErrEnterpriseGroupScopeDisabled)
	require.ErrorIs(t, f.s.SetGroupWhitelist(context.Background(), actor, 1, EnterpriseScopeDepartment, 5, []int64{1}), ErrEnterpriseGroupScopeDisabled)
	f.g.whitelists[EnterpriseQuotaScope{EnterpriseScopeDepartment, 3}] = EnterpriseGroupWhitelist{GroupIDs: []int64{}}
	require.ErrorIs(t, f.s.SetGroupWhitelist(context.Background(), actor, 1, EnterpriseScopeDepartment, 5, []int64{1}), ErrEnterpriseGroupScopeDisabled)
}
