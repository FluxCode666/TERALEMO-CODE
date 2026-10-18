//go:build unit

package service

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

type enterpriseManagementTestTxKey struct{}
type enterpriseManagementTestEnterpriseRepo struct {
	EnterpriseRepository
	e      Enterprise
	caller *EnterpriseMemberContext
	ids    []int64
}

func (r *enterpriseManagementTestEnterpriseRepo) GetByID(context.Context, int64) (*Enterprise, error) {
	v := r.e
	return &v, nil
}
func (r *enterpriseManagementTestEnterpriseRepo) GetMemberContextByUserID(context.Context, int64) (*EnterpriseMemberContext, error) {
	return r.caller, nil
}
func (r *enterpriseManagementTestEnterpriseRepo) ListMemberUserIDs(context.Context, int64) ([]int64, error) {
	return r.ids, nil
}
func (r *enterpriseManagementTestEnterpriseRepo) Update(_ context.Context, e *Enterprise) error {
	r.e = *e
	return nil
}

type enterpriseManagementTestMembers struct {
	EnterpriseMemberRepository
	items                                 map[int64]EnterpriseMember
	owners                                int64
	updated, deleted, created, countCalls int
	t                                     *testing.T
}

func (r *enterpriseManagementTestMembers) GetByID(_ context.Context, eid, id int64) (*EnterpriseMember, error) {
	v, ok := r.items[id]
	if !ok || v.EnterpriseID != eid {
		return nil, ErrEnterpriseMemberNotFound
	}
	return &v, nil
}
func (r *enterpriseManagementTestMembers) GetByUserID(_ context.Context, uid int64) (*EnterpriseMember, error) {
	for _, v := range r.items {
		if v.UserID == uid {
			c := v
			return &c, nil
		}
	}
	return nil, ErrEnterpriseMemberNotFound
}
func (r *enterpriseManagementTestMembers) CountActiveOwners(ctx context.Context, id int64) (int64, error) {
	r.t.Helper()
	if ctx.Value(enterpriseManagementTestTxKey{}) != true {
		r.t.Fatal("owner count escaped transaction")
	}
	r.countCalls++
	return r.owners, nil
}
func (r *enterpriseManagementTestMembers) Update(_ context.Context, m *EnterpriseMember) error {
	r.updated++
	r.items[m.ID] = *m
	return nil
}
func (r *enterpriseManagementTestMembers) Create(_ context.Context, m *EnterpriseMember) error {
	r.created++
	m.ID = 99
	r.items[m.ID] = *m
	return nil
}
func (r *enterpriseManagementTestMembers) Delete(_ context.Context, eid, id int64) error {
	r.deleted++
	delete(r.items, id)
	return nil
}

type enterpriseManagementTestDepartments struct {
	EnterpriseDepartmentRepository
	items map[int64]EnterpriseDepartment
}

func (r *enterpriseManagementTestDepartments) GetByID(_ context.Context, eid, id int64) (*EnterpriseDepartment, error) {
	v, ok := r.items[id]
	if !ok || v.EnterpriseID != eid {
		return nil, ErrEnterpriseDepartmentNotFound
	}
	return &v, nil
}

type enterpriseManagementTestQuotas struct {
	EnterpriseQuotaRepository
	items          map[EnterpriseQuotaScope]EnterpriseQuota
	updated, reset int
}

func (r *enterpriseManagementTestQuotas) GetByScope(_ context.Context, eid int64, scope string, id int64) (*EnterpriseQuota, error) {
	v, ok := r.items[EnterpriseQuotaScope{scope, id}]
	if !ok {
		return nil, nil
	}
	return &v, nil
}
func (r *enterpriseManagementTestQuotas) GetByScopes(ctx context.Context, eid int64, scopes []EnterpriseQuotaScope) ([]EnterpriseQuota, error) {
	out := []EnterpriseQuota{}
	for _, scope := range scopes {
		v, _ := r.GetByScope(ctx, eid, scope.ScopeType, scope.ScopeID)
		if v != nil {
			out = append(out, *v)
		}
	}
	return out, nil
}
func (r *enterpriseManagementTestQuotas) List(_ context.Context, eid int64, scope string) ([]EnterpriseQuota, error) {
	out := []EnterpriseQuota{}
	for _, q := range r.items {
		if q.EnterpriseID == eid && (scope == "" || q.ScopeType == scope) {
			out = append(out, q)
		}
	}
	return out, nil
}
func (r *enterpriseManagementTestQuotas) UpdateLimits(_ context.Context, q *EnterpriseQuota) error {
	r.updated++
	r.items[EnterpriseQuotaScope{q.ScopeType, q.ScopeID}] = *q
	return nil
}
func (r *enterpriseManagementTestQuotas) ResetUsage(_ context.Context, eid int64, scope string, id int64, windows []string, now time.Time) error {
	r.reset++
	return nil
}

type enterpriseManagementTestAudit struct {
	EnterpriseAuditLogRepository
	logs []EnterpriseAuditLog
	err  error
}

func (r *enterpriseManagementTestAudit) Create(_ context.Context, l *EnterpriseAuditLog) error {
	if r.err != nil {
		return r.err
	}
	r.logs = append(r.logs, *l)
	return nil
}

type enterpriseManagementTestAuth struct{ ids []int64 }

func (r *enterpriseManagementTestAuth) InvalidateAuthCacheByUserID(_ context.Context, id int64) {
	r.ids = append(r.ids, id)
}

type enterpriseManagementTestBilling struct {
	scopes   []EnterpriseQuotaScope
	balances []int64
}

func (r *enterpriseManagementTestBilling) InvalidateEnterpriseBalance(_ context.Context, id int64) error {
	r.balances = append(r.balances, id)
	return nil
}
func (r *enterpriseManagementTestBilling) InvalidateEnterpriseQuota(_ context.Context, scope string, id int64) error {
	r.scopes = append(r.scopes, EnterpriseQuotaScope{scope, id})
	return nil
}

type enterpriseManagementTestUsers struct{ users map[int64]User }

func (r *enterpriseManagementTestUsers) GetByID(_ context.Context, id int64) (*User, error) {
	v, ok := r.users[id]
	if !ok {
		return nil, ErrUserNotFound
	}
	return &v, nil
}

func (r *enterpriseManagementTestUsers) BatchSetConcurrency(_ context.Context, ids []int64, value int) (int, error) {
	count := 0
	for _, id := range ids {
		if user, ok := r.users[id]; ok {
			user.Concurrency = value
			r.users[id] = user
			count++
		}
	}
	return count, nil
}

type enterpriseManagementTestUserCreator struct {
	user   User
	inputs []*CreateUserInput
	tx     bool
}

func (r *enterpriseManagementTestUserCreator) CreateUser(ctx context.Context, in *CreateUserInput) (*User, error) {
	r.inputs = append(r.inputs, in)
	r.tx = ctx.Value(enterpriseManagementTestTxKey{}) == true
	v := r.user
	return &v, nil
}

type enterpriseManagementTestGroups struct{ groups map[int64]Group }

func (r *enterpriseManagementTestGroups) GetByID(_ context.Context, id int64) (*Group, error) {
	v, ok := r.groups[id]
	if !ok {
		return nil, ErrGroupNotFound
	}
	return &v, nil
}
func (r *enterpriseManagementTestGroups) ListActive(context.Context) ([]Group, error) {
	out := []Group{}
	for _, g := range r.groups {
		if g.Status == StatusActive {
			out = append(out, g)
		}
	}
	return out, nil
}

type enterpriseManagementTestEnterpriseGroups struct {
	EnterpriseGroupRepository
	pool       []int64
	whitelists map[EnterpriseQuotaScope]EnterpriseGroupWhitelist
	writes     int
}

func (r *enterpriseManagementTestEnterpriseGroups) GetAllowedGroupIDs(context.Context, int64) ([]int64, error) {
	return r.pool, nil
}
func (r *enterpriseManagementTestEnterpriseGroups) SetAllowedGroupIDs(_ context.Context, _ int64, ids []int64) error {
	r.writes++
	r.pool = ids
	return nil
}
func (r *enterpriseManagementTestEnterpriseGroups) GetWhitelist(_ context.Context, _ int64, scope string, id int64) (*EnterpriseGroupWhitelist, error) {
	w, ok := r.whitelists[EnterpriseQuotaScope{scope, id}]
	if !ok {
		return nil, nil
	}
	return &w, nil
}
func (r *enterpriseManagementTestEnterpriseGroups) SetWhitelist(_ context.Context, eid int64, scope string, id int64, ids []int64) error {
	r.writes++
	if len(ids) == 0 {
		delete(r.whitelists, EnterpriseQuotaScope{scope, id})
		return nil
	}
	r.whitelists[EnterpriseQuotaScope{scope, id}] = EnterpriseGroupWhitelist{EnterpriseID: eid, ScopeType: scope, ScopeID: id, GroupIDs: ids}
	return nil
}

type enterpriseManagementTestFixture struct {
	s       *EnterpriseService
	e       *enterpriseManagementTestEnterpriseRepo
	m       *enterpriseManagementTestMembers
	q       *enterpriseManagementTestQuotas
	g       *enterpriseManagementTestEnterpriseGroups
	a       *enterpriseManagementTestAudit
	auth    *enterpriseManagementTestAuth
	billing *enterpriseManagementTestBilling
	creator *enterpriseManagementTestUserCreator
}

func newEnterpriseManagementTestFixture(t *testing.T) *enterpriseManagementTestFixture {
	e := &enterpriseManagementTestEnterpriseRepo{e: Enterprise{ID: 1, Name: "Acme", Status: EnterpriseStatusActive, Settings: DefaultEnterpriseSettings()}, ids: []int64{10, 20, 30}}
	m := &enterpriseManagementTestMembers{t: t, owners: 1, items: map[int64]EnterpriseMember{10: {ID: 10, EnterpriseID: 1, UserID: 10, Role: EnterpriseRoleOwner, Status: EnterpriseMemberStatusActive, UserStatus: StatusActive}, 20: {ID: 20, EnterpriseID: 1, UserID: 20, Role: EnterpriseRoleMember, Status: EnterpriseMemberStatusActive, DepartmentID: enterpriseTestID(3)}}}
	q := &enterpriseManagementTestQuotas{items: map[EnterpriseQuotaScope]EnterpriseQuota{{EnterpriseScopeEnterprise, 1}: {EnterpriseID: 1, ScopeType: EnterpriseScopeEnterprise, ScopeID: 1}, {EnterpriseScopeDepartment, 3}: {EnterpriseID: 1, ScopeType: EnterpriseScopeDepartment, ScopeID: 3}, {EnterpriseScopeMember, 20}: {EnterpriseID: 1, ScopeType: EnterpriseScopeMember, ScopeID: 20, DailyUsageUSD: 7}}}
	a := &enterpriseManagementTestAudit{}
	auth := &enterpriseManagementTestAuth{}
	billing := &enterpriseManagementTestBilling{}
	creator := &enterpriseManagementTestUserCreator{user: User{ID: 30, Role: RoleUser}}
	groups := &enterpriseManagementTestGroups{groups: map[int64]Group{1: {ID: 1, Status: StatusActive}, 2: {ID: 2, Status: StatusActive, IsExclusive: true}, 3: {ID: 3, Status: StatusDisabled}}}
	eg := &enterpriseManagementTestEnterpriseGroups{whitelists: map[EnterpriseQuotaScope]EnterpriseGroupWhitelist{}}
	s := &EnterpriseService{enterprises: e, departments: &enterpriseManagementTestDepartments{items: map[int64]EnterpriseDepartment{3: {ID: 3, EnterpriseID: 1}}}, members: m, quotas: q, auditLogs: a, users: &enterpriseManagementTestUsers{users: map[int64]User{20: {ID: 20, Role: RoleUser}, 30: {ID: 30, Role: RoleUser}, 40: {ID: 40, Role: RoleAdmin}}}, groups: groups, admin: creator, enterpriseGroups: eg, authCache: auth, billingCache: billing}
	s.transact = func(ctx context.Context, fn func(context.Context) error) error {
		return fn(context.WithValue(ctx, enterpriseManagementTestTxKey{}, true))
	}
	return &enterpriseManagementTestFixture{s: s, e: e, m: m, q: q, g: eg, a: a, auth: auth, billing: billing, creator: creator}
}
func enterpriseTestFloat(v float64) *float64 { return &v }
func enterpriseTestString(v string) *string  { return &v }
func TestEnterpriseServiceLastOwnerIsCheckedInsideTransaction(t *testing.T) {
	for _, action := range []string{"disable", "demote", "remove"} {
		t.Run(action, func(t *testing.T) {
			f := newEnterpriseManagementTestFixture(t)
			a := EnterpriseActor{UserID: 1, IsAdmin: true}
			var err error
			switch action {
			case "disable":
				_, err = f.s.UpdateMember(context.Background(), a, 1, 10, UpdateEnterpriseMemberInput{Status: enterpriseTestString(EnterpriseMemberStatusDisabled)})
			case "demote":
				_, err = f.s.UpdateMember(context.Background(), a, 1, 10, UpdateEnterpriseMemberInput{Role: enterpriseTestString(EnterpriseRoleMember)})
			case "remove":
				err = f.s.RemoveMember(context.Background(), a, 1, 10)
			}
			if !errors.Is(err, ErrEnterpriseLastOwner) {
				t.Fatalf("got %v", err)
			}
			if f.m.updated != 0 || f.m.deleted != 0 || len(f.auth.ids) != 0 || len(f.a.logs) != 0 {
				t.Fatal("rejected mutation produced side effects")
			}
			if f.m.countCalls < 2 {
				t.Fatal("owner mutation was not guarded by count in transaction")
			}
		})
	}
}
func TestEnterpriseServiceManagerMemberEditsAreNarrow(t *testing.T) {
	for _, tc := range []struct {
		name       string
		in         UpdateEnterpriseMemberInput
		department int64
		targetRole string
		want       error
	}{
		{"title", UpdateEnterpriseMemberInput{Title: enterpriseTestString("developer")}, 3, EnterpriseRoleMember, nil},
		{"role", UpdateEnterpriseMemberInput{Role: enterpriseTestString(EnterpriseRoleOwner)}, 3, EnterpriseRoleMember, ErrEnterprisePermissionDenied},
		{"department", UpdateEnterpriseMemberInput{DepartmentID: enterpriseTestID(3)}, 3, EnterpriseRoleMember, ErrEnterprisePermissionDenied},
		{"other_department", UpdateEnterpriseMemberInput{Title: enterpriseTestString("x")}, 4, EnterpriseRoleMember, ErrEnterprisePermissionDenied},
		{"owner", UpdateEnterpriseMemberInput{Title: enterpriseTestString("x")}, 3, EnterpriseRoleOwner, ErrEnterprisePermissionDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newEnterpriseManagementTestFixture(t)
			f.e.e.Settings.ManagerCanManageMembers = true
			f.e.caller = &EnterpriseMemberContext{EnterpriseID: 1, MemberID: 50, Role: EnterpriseRoleManager, DepartmentID: enterpriseTestID(3), MemberStatus: EnterpriseMemberStatusActive, EnterpriseStatus: EnterpriseStatusActive}
			m := f.m.items[20]
			m.DepartmentID = enterpriseTestID(tc.department)
			m.Role = tc.targetRole
			f.m.items[20] = m
			_, err := f.s.UpdateMember(context.Background(), EnterpriseActor{UserID: 50}, 1, 20, tc.in)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
			if tc.want == nil {
				if len(f.a.logs) != 1 || f.a.logs[0].Detail["before"] == nil || f.a.logs[0].Detail["after"] == nil {
					t.Fatal("audit before/after missing")
				}
				if len(f.auth.ids) != 1 || f.auth.ids[0] != 20 {
					t.Fatal("affected user cache was not invalidated")
				}
			}
		})
	}
}
func TestEnterpriseServiceMemberCreationGuards(t *testing.T) {
	t.Run("admin user", func(t *testing.T) {
		f := newEnterpriseManagementTestFixture(t)
		_, err := f.s.CreateMember(context.Background(), EnterpriseActor{IsAdmin: true}, 1, CreateEnterpriseMemberInput{UserID: 40})
		if !errors.Is(err, ErrEnterpriseMemberAdminForbidden) || f.m.created != 0 {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("one enterprise", func(t *testing.T) {
		f := newEnterpriseManagementTestFixture(t)
		_, err := f.s.CreateMember(context.Background(), EnterpriseActor{IsAdmin: true}, 1, CreateEnterpriseMemberInput{UserID: 20})
		if !errors.Is(err, ErrEnterpriseMemberExists) || f.m.created != 0 {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("manager department", func(t *testing.T) {
		f := newEnterpriseManagementTestFixture(t)
		_, err := f.s.CreateMember(context.Background(), EnterpriseActor{IsAdmin: true}, 1, CreateEnterpriseMemberInput{UserID: 30, Role: EnterpriseRoleManager})
		if !errors.Is(err, ErrEnterpriseManagerRequiresDepartment) {
			t.Fatal(err)
		}
	})
	t.Run("create uses shared core in transaction", func(t *testing.T) {
		f := newEnterpriseManagementTestFixture(t)
		m, err := f.s.CreateMember(context.Background(), EnterpriseActor{UserID: 1, IsAdmin: true}, 1, CreateEnterpriseMemberInput{Email: "new@example.com", Password: "password", DepartmentID: enterpriseTestID(3)})
		if err != nil {
			t.Fatal(err)
		}
		if !f.creator.tx || len(f.creator.inputs) != 1 || m.UserID != 30 || f.creator.inputs[0].Role != RoleUser {
			t.Fatal("account creation did not reuse shared transactional creator")
		}
		if f.a.logs[0].Action != EnterpriseAuditActionMemberCreate {
			t.Fatal("wrong create audit action")
		}
	})
	t.Run("workbench cannot import user", func(t *testing.T) {
		f := newEnterpriseManagementTestFixture(t)
		f.e.caller = &EnterpriseMemberContext{EnterpriseID: 1, MemberID: 10, Role: EnterpriseRoleOwner, MemberStatus: EnterpriseMemberStatusActive, EnterpriseStatus: EnterpriseStatusActive}
		_, err := f.s.CreateMember(context.Background(), EnterpriseActor{UserID: 10}, 1, CreateEnterpriseMemberInput{UserID: 30})
		if !errors.Is(err, ErrEnterprisePermissionDenied) {
			t.Fatal(err)
		}
	})
}
func TestEnterpriseServiceQuotaChecksAllParentsAndKeepsUsage(t *testing.T) {
	for _, tc := range []struct {
		name                          string
		enterprise, department, child *float64
		want                          error
	}{
		{"department stricter", enterpriseTestFloat(100), enterpriseTestFloat(20), enterpriseTestFloat(30), ErrEnterpriseQuotaExceedsParent},
		{"enterprise stricter", enterpriseTestFloat(10), enterpriseTestFloat(20), enterpriseTestFloat(15), ErrEnterpriseQuotaExceedsParent},
		{"equal", enterpriseTestFloat(10), enterpriseTestFloat(20), enterpriseTestFloat(10), nil},
		{"null child inherits", enterpriseTestFloat(10), enterpriseTestFloat(20), nil, nil},
		{"zero", enterpriseTestFloat(0), nil, enterpriseTestFloat(0), nil},
		{"negative", nil, nil, enterpriseTestFloat(-1), ErrEnterpriseInvalidInput},
		{"nan", nil, nil, enterpriseTestFloat(math.NaN()), ErrEnterpriseInvalidInput},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newEnterpriseManagementTestFixture(t)
			e := f.q.items[EnterpriseQuotaScope{EnterpriseScopeEnterprise, 1}]
			e.DailyLimitUSD = tc.enterprise
			f.q.items[EnterpriseQuotaScope{EnterpriseScopeEnterprise, 1}] = e
			d := f.q.items[EnterpriseQuotaScope{EnterpriseScopeDepartment, 3}]
			d.DailyLimitUSD = tc.department
			f.q.items[EnterpriseQuotaScope{EnterpriseScopeDepartment, 3}] = d
			q, err := f.s.UpdateQuota(context.Background(), EnterpriseActor{IsAdmin: true}, 1, EnterpriseScopeMember, 20, EnterpriseQuotaLimitsInput{DailyLimitUSD: tc.child})
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
			if tc.want == nil {
				if q.DailyUsageUSD != 7 {
					t.Fatal("limit change overwrote usage")
				}
				if len(f.auth.ids) != 1 || f.auth.ids[0] != 20 || len(f.billing.scopes) != 1 {
					t.Fatal("quota caches were not invalidated")
				}
			} else if f.q.updated != 0 || len(f.billing.scopes) > 0 {
				t.Fatal("rejected quota produced side effects")
			}
		})
	}
}
func TestEnterpriseServiceWhitelistUsesOnlyEnterprisePool(t *testing.T) {
	for _, tc := range []struct {
		name string
		ids  []int64
		want error
	}{
		{"inside", []int64{1}, nil}, {"ignores department", []int64{2}, nil}, {"disabled", []int64{3}, ErrEnterpriseGroupOutOfScope}, {"missing", []int64{99}, ErrEnterpriseGroupOutOfScope}, {"empty removes restriction", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newEnterpriseManagementTestFixture(t)
			f.g.pool = []int64{1, 2, 3}
			f.e.e.GroupPoolConfigured = true
			f.g.whitelists[EnterpriseQuotaScope{EnterpriseScopeEnterprise, 1}] = EnterpriseGroupWhitelist{GroupIDs: []int64{1, 2}}
			f.g.whitelists[EnterpriseQuotaScope{EnterpriseScopeDepartment, 3}] = EnterpriseGroupWhitelist{GroupIDs: []int64{1}}
			err := f.s.SetGroupWhitelist(context.Background(), EnterpriseActor{IsAdmin: true}, 1, EnterpriseScopeEnterprise, 1, tc.ids)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
			if tc.want == nil && len(f.auth.ids) != 3 {
				t.Fatal("enterprise member caches not invalidated")
			}
			if tc.want != nil && f.g.writes != 0 {
				t.Fatal("invalid whitelist was stored")
			}
		})
	}
}
func TestEnterpriseServiceCacheInvalidationOccursAfterAuditSuccess(t *testing.T) {
	f := newEnterpriseManagementTestFixture(t)
	f.a.err = errors.New("audit unavailable")
	_, err := f.s.UpdateSettings(context.Background(), EnterpriseActor{IsAdmin: true}, 1, DefaultEnterpriseSettings())
	if err == nil {
		t.Fatal("expected audit error")
	}
	if len(f.auth.ids) != 0 || len(f.billing.scopes) != 0 {
		t.Fatal("failed transaction invalidated caches")
	}
}
func TestEnterpriseServiceGroupsApplyPoolOrPersonalRules(t *testing.T) {
	f := newEnterpriseManagementTestFixture(t)
	f.e.caller = &EnterpriseMemberContext{EnterpriseID: 1, MemberID: 20, Role: EnterpriseRoleMember, MemberStatus: EnterpriseMemberStatusActive, EnterpriseStatus: EnterpriseStatusActive}
	groups, err := f.s.GetAllowedGroups(context.Background(), EnterpriseActor{UserID: 20}, 1)
	if err != nil || len(groups) != 1 || groups[0].ID != 1 {
		t.Fatalf("personal fallback groups=%v err=%v", groups, err)
	}
	f.g.pool = []int64{2}
	f.e.e.GroupPoolConfigured = true
	f.e.caller.GroupPoolConfigured = true
	f.e.caller.GroupPoolIDs = []int64{2}
	groups, err = f.s.GetAllowedGroups(context.Background(), EnterpriseActor{UserID: 20}, 1)
	if err != nil || len(groups) != 1 || groups[0].ID != 2 {
		t.Fatalf("enterprise pool groups=%v err=%v", groups, err)
	}
	f.e.caller.GroupWhitelistConfigured = true
	f.e.caller.GroupWhitelistIDs = []int64{}
	groups, err = f.s.GetAllowedGroups(context.Background(), EnterpriseActor{UserID: 20}, 1)
	if err != nil || len(groups) != 0 {
		t.Fatalf("empty effective intersection groups=%v err=%v", groups, err)
	}
}

func TestEnterpriseServiceDisabledEnterpriseAllowsSettingsRead(t *testing.T) {
	f := newEnterpriseManagementTestFixture(t)
	f.e.e.Status = EnterpriseStatusDisabled
	f.e.caller = &EnterpriseMemberContext{EnterpriseID: 1, MemberID: 10, Role: EnterpriseRoleOwner, EnterpriseStatus: EnterpriseStatusDisabled, MemberStatus: EnterpriseMemberStatusActive}
	if _, err := f.s.GetSettings(context.Background(), EnterpriseActor{UserID: 10}, 1); err != nil {
		t.Fatalf("read settings on disabled enterprise: %v", err)
	}
	if _, err := f.s.UpdateSettings(context.Background(), EnterpriseActor{UserID: 10}, 1, DefaultEnterpriseSettings()); !errors.Is(err, ErrEnterpriseDisabled) {
		t.Fatalf("write settings got %v", err)
	}
	f.e.caller.MemberStatus = EnterpriseMemberStatusDisabled
	if _, err := f.s.GetSettings(context.Background(), EnterpriseActor{UserID: 10}, 1); !errors.Is(err, ErrEnterpriseMemberDisabled) {
		t.Fatalf("disabled member read got %v", err)
	}
}
func TestEnterpriseServiceValidatesDatabaseLengthsBeforePersistence(t *testing.T) {
	f := newEnterpriseManagementTestFixture(t)
	long := ""
	for range 101 {
		long += "a"
	}
	if _, err := f.s.CreateDepartment(context.Background(), EnterpriseActor{IsAdmin: true}, 1, EnterpriseDepartmentInput{Name: long}); !errors.Is(err, ErrEnterpriseInvalidInput) {
		t.Fatalf("department length got %v", err)
	}
	if _, err := f.s.UpdateMember(context.Background(), EnterpriseActor{IsAdmin: true}, 1, 20, UpdateEnterpriseMemberInput{Title: &long}); !errors.Is(err, ErrEnterpriseInvalidInput) {
		t.Fatalf("member title length got %v", err)
	}
	if _, err := f.s.CreateEnterprise(context.Background(), EnterpriseActor{IsAdmin: true}, CreateEnterpriseInput{Name: long}); !errors.Is(err, ErrEnterpriseInvalidInput) {
		t.Fatalf("enterprise length got %v", err)
	}
}
func TestEnterpriseServiceInvalidScopeAndResetGuards(t *testing.T) {
	f := newEnterpriseManagementTestFixture(t)
	if _, err := f.s.UpdateQuota(context.Background(), EnterpriseActor{IsAdmin: true}, 1, EnterpriseScopeMember, 999, EnterpriseQuotaLimitsInput{}); !errors.Is(err, ErrEnterpriseInvalidScope) {
		t.Fatalf("forged scope got %v", err)
	}
	if err := f.s.ResetQuota(context.Background(), EnterpriseActor{IsAdmin: true}, 1, EnterpriseScopeMember, 20, []string{"yearly"}); !errors.Is(err, ErrEnterpriseInvalidInput) {
		t.Fatalf("invalid reset window got %v", err)
	}
	f.e.e.Settings.ManagerCanEditMemberQuota = true
	f.e.caller = &EnterpriseMemberContext{EnterpriseID: 1, MemberID: 50, Role: EnterpriseRoleManager, DepartmentID: enterpriseTestID(3), MemberStatus: EnterpriseMemberStatusActive, EnterpriseStatus: EnterpriseStatusActive}
	if err := f.s.ResetQuota(context.Background(), EnterpriseActor{UserID: 50}, 1, EnterpriseScopeMember, 20, []string{EnterpriseQuotaWindowDaily}); !errors.Is(err, ErrEnterprisePermissionDenied) {
		t.Fatalf("manager reset got %v", err)
	}
	if f.q.reset != 0 {
		t.Fatal("forbidden reset reached persistence")
	}
}
