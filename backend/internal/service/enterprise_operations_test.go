//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

func (r *enterpriseManagementTestUsers) GetByEmail(_ context.Context, email string) (*User, error) {
	for _, user := range r.users {
		if strings.EqualFold(user.Email, email) {
			copy := user
			return &copy, nil
		}
	}
	return nil, ErrUserNotFound
}
func (r *enterpriseManagementTestUsers) Update(_ context.Context, u *User, fields UserUpdateFields) error {
	if fields != (UserUpdateFields{PasswordHash: true}) {
		return errors.New("unexpected password write fields")
	}
	old := r.users[u.ID]
	old.PasswordHash = u.PasswordHash
	r.users[u.ID] = old
	return nil
}
func (r *enterpriseManagementTestDepartments) List(context.Context, int64) ([]EnterpriseDepartment, error) {
	out := []EnterpriseDepartment{}
	for _, d := range r.items {
		out = append(out, d)
	}
	return out, nil
}
func (r *enterpriseManagementTestDepartments) Create(_ context.Context, d *EnterpriseDepartment) error {
	d.ID = int64(len(r.items) + 50)
	d.PathIDs = []int64{d.ID}
	d.PathNames = []string{d.Name}
	d.Depth = 1
	if d.ParentID != nil {
		parent := r.items[*d.ParentID]
		d.PathIDs = append(append([]int64{}, parent.PathIDs...), d.ID)
		d.PathNames = append(append([]string{}, parent.PathNames...), d.Name)
		d.Depth = parent.Depth + 1
	}
	r.items[d.ID] = *d
	return nil
}

type enterpriseImportTestCreator struct {
	users     *enterpriseManagementTestUsers
	next      int64
	failEmail string
}

func (r *enterpriseImportTestCreator) CreateUser(ctx context.Context, in *CreateUserInput) (*User, error) {
	if in.Email == r.failEmail {
		return nil, infraerrors.BadRequest("CREATE_FAILED", "模拟建号失败")
	}
	r.next++
	u := &User{ID: r.next, Email: in.Email, Username: in.Username, Role: RoleUser, Status: StatusActive, Balance: *in.Balance, Concurrency: in.Concurrency}
	if err := u.SetPassword(in.Password); err != nil {
		return nil, err
	}
	r.users.users[u.ID] = *u
	return u, nil
}

type enterpriseImportTestMembers struct {
	*enterpriseManagementTestMembers
}

func (r *enterpriseImportTestMembers) Create(_ context.Context, m *EnterpriseMember) error {
	m.ID = int64(100 + len(r.items))
	m.UserStatus = StatusActive
	r.items[m.ID] = *m
	return nil
}
func (r *enterpriseImportTestMembers) CountActiveOwners(ctx context.Context, id int64) (int64, error) {
	if _, err := r.enterpriseManagementTestMembers.CountActiveOwners(ctx, id); err != nil {
		return 0, err
	}
	var n int64
	for _, m := range r.items {
		if enterpriseMemberIsAvailableOwner(&m) {
			n++
		}
	}
	return n, nil
}
func newEnterpriseImportTestFixture(t *testing.T) (*enterpriseManagementTestFixture, *enterpriseManagementTestUsers, *enterpriseImportTestCreator) {
	t.Helper()
	f := newEnterpriseManagementTestFixture(t)
	users := &enterpriseManagementTestUsers{users: map[int64]User{
		10: {ID: 10, Email: "owner@example.com", Role: RoleUser, Status: StatusActive},
		20: {ID: 20, Email: "existing@example.com", Role: RoleUser, Status: StatusActive},
		30: {ID: 30, Email: "occupied@example.com", Role: RoleUser, Status: StatusActive},
		40: {ID: 40, Email: "admin@example.com", Role: RoleAdmin, Status: StatusActive},
	}}
	f.s.users = users
	creator := &enterpriseImportTestCreator{users: users, next: 1000, failEmail: "fail@example.com"}
	f.s.admin = creator
	f.s.members = &enterpriseImportTestMembers{f.m}
	f.s.departments = &enterpriseManagementTestDepartments{items: map[int64]EnterpriseDepartment{3: {ID: 3, EnterpriseID: 1, Name: "A", PathIDs: []int64{3}, PathNames: []string{"A"}, Depth: 1}}}
	f.e.caller = &EnterpriseMemberContext{EnterpriseID: 1, MemberID: 10, Role: EnterpriseRoleOwner, MemberStatus: EnterpriseMemberStatusActive, EnterpriseStatus: EnterpriseStatusActive}
	return f, users, creator
}
func TestEnterpriseImportPreviewCommitAndSecrets(t *testing.T) {
	f, users, _ := newEnterpriseImportTestFixture(t)
	password := "provided-secret-123"
	rows := []EnterpriseImportRow{
		{Row: 1, Email: " NEW@example.com ", Username: "New", DepartmentPath: " A / B / C ", Role: "员工"},
		{Row: 2, Email: "existing@example.com", DepartmentPath: "A", Role: "member", Title: "Engineer"},
		{Row: 3, Email: "occupied@example.com", Role: "member"},
		{Row: 4, Email: "dup@example.com", Role: "member"},
		{Row: 5, Email: "DUP@example.com", Role: "member"},
		{Row: 6, Email: "deep@example.com", DepartmentPath: "1/2/3/4/5/6/7/8/9", Role: "member"},
		{Row: 7, Email: "manager@example.com", Role: "部门负责人"},
		{Row: 8, Email: "explicit@example.com", Role: "member", Password: &password},
	}
	actor := EnterpriseActor{UserID: 10}
	preview, err := f.s.PreviewImport(context.Background(), actor, 1, rows)
	require.NoError(t, err)
	require.Equal(t, EnterpriseImportPreviewSummary{Total: 8, Create: 2, Update: 1, Error: 5}, preview.Summary)
	require.Equal(t, []string{"A/B", "A/B/C"}, preview.DepartmentsToCreate)
	require.Equal(t, "邮箱已被占用", preview.Rows[2].Messages[0])
	encoded, err := json.Marshal(preview)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "password")
	require.NotContains(t, string(encoded), password)
	require.Empty(t, f.a.logs)
	result, err := f.s.CommitImport(context.Background(), actor, 1, rows)
	require.NoError(t, err)
	require.Equal(t, EnterpriseImportCommitSummary{Total: 8, Created: 2, Updated: 1, Failed: 5}, result.Summary)
	require.Len(t, result.Rows[0].GeneratedPassword, 32)
	generatedUser := users.users[result.Rows[0].UserID]
	require.True(t, generatedUser.CheckPassword(result.Rows[0].GeneratedPassword))
	require.Empty(t, result.Rows[7].GeneratedPassword)
	explicitUser := users.users[result.Rows[7].UserID]
	require.True(t, explicitUser.CheckPassword(password))
	require.True(t, f.m.items[result.Rows[0].MemberID].AccountCreatedByEnterprise)
	require.Zero(t, users.users[result.Rows[0].UserID].Balance)
	require.Equal(t, "Engineer", f.m.items[20].Title)
	encoded, err = json.Marshal(f.a.logs)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), password)
	require.NotContains(t, string(encoded), result.Rows[0].GeneratedPassword)
	last := f.a.logs[len(f.a.logs)-1]
	require.Equal(t, "member.import", last.Action)
	require.Equal(t, result.Summary, last.Detail["after"])
	// Reimport of identical member data skips; a failed account does not abort peers.
	again, err := f.s.CommitImport(context.Background(), actor, 1, []EnterpriseImportRow{
		{Row: 1, Email: "existing@example.com", Role: "member", DepartmentPath: "A", Title: "Engineer"},
		{Row: 2, Email: "fail@example.com", Role: "member"},
		{Row: 3, Email: "success@example.com", Role: "member"},
	})
	require.NoError(t, err)
	require.Equal(t, EnterpriseImportCommitSummary{Total: 3, Created: 1, Skipped: 1, Failed: 1}, again.Summary)
	require.Empty(t, again.Rows[1].GeneratedPassword)
}

func TestEnterpriseImportAdminExistingUsersAndManagerRestrictions(t *testing.T) {
	f, _, _ := newEnterpriseImportTestFixture(t)
	rows := []EnterpriseImportRow{{Row: 1, Email: "occupied@example.com", Role: "member"}, {Row: 2, Email: "admin@example.com", Role: "member"}}
	preview, err := f.s.PreviewImport(context.Background(), EnterpriseActor{IsAdmin: true}, 1, rows)
	require.NoError(t, err)
	require.Equal(t, 1, preview.Summary.Create)
	require.Equal(t, 1, preview.Summary.Error)
	result, err := f.s.CommitImport(context.Background(), EnterpriseActor{IsAdmin: true}, 1, rows)
	require.NoError(t, err)
	require.Equal(t, int64(30), result.Rows[0].UserID)
	require.False(t, f.m.items[result.Rows[0].MemberID].AccountCreatedByEnterprise)
	f.e.caller.Role = EnterpriseRoleManager
	f.e.caller.DepartmentID = enterpriseTestID(3)
	_, err = f.s.PreviewImport(context.Background(), EnterpriseActor{UserID: 10}, 1, rows)
	require.ErrorIs(t, err, ErrEnterprisePermissionDenied)
	f.e.e.Settings.ManagerCanManageMembers = true
	preview, err = f.s.PreviewImport(context.Background(), EnterpriseActor{UserID: 10}, 1, []EnterpriseImportRow{
		{Row: 1, Email: "manager-child@example.com", Role: "member", DepartmentPath: "A/Child"},
		{Row: 2, Email: "role@example.com", Role: "manager", DepartmentPath: "A"},
		{Row: 3, Email: "unassigned@example.com", Role: "member"},
		{Row: 4, Email: "existing@example.com", Role: "member", DepartmentPath: "A", Title: "Managed"},
		{Row: 5, Email: "new-member@example.com", Role: "member", DepartmentPath: "A"},
	})
	require.NoError(t, err)
	require.Equal(t, EnterpriseImportPreviewSummary{Total: 5, Create: 1, Update: 1, Error: 3}, preview.Summary)
	require.Empty(t, preview.DepartmentsToCreate)
}

func TestEnterpriseImportLimitAndCommitRevalidation(t *testing.T) {
	f, users, _ := newEnterpriseImportTestFixture(t)
	_, err := f.s.PreviewImport(context.Background(), EnterpriseActor{UserID: 10}, 1, make([]EnterpriseImportRow, 1001))
	require.Equal(t, 400, infraerrors.Code(err))
	_, err = f.s.CommitImport(context.Background(), EnterpriseActor{IsAdmin: true}, 1, make([]EnterpriseImportRow, 1001))
	require.Equal(t, 400, infraerrors.Code(err))
	preview, err := f.s.PreviewImport(context.Background(), EnterpriseActor{UserID: 10}, 1, make([]EnterpriseImportRow, 1000))
	require.NoError(t, err)
	require.Equal(t, 1000, preview.Summary.Error)
	row := EnterpriseImportRow{Row: 1, Email: "raced@example.com", Role: "member"}
	preview, err = f.s.PreviewImport(context.Background(), EnterpriseActor{UserID: 10}, 1, []EnterpriseImportRow{row})
	require.NoError(t, err)
	require.Equal(t, 1, preview.Summary.Create)
	users.users[50] = User{ID: 50, Email: row.Email, Role: RoleUser}
	result, err := f.s.CommitImport(context.Background(), EnterpriseActor{UserID: 10}, 1, []EnterpriseImportRow{row})
	require.NoError(t, err)
	require.Equal(t, 1, result.Summary.Failed)
	require.Equal(t, "邮箱已被占用", result.Rows[0].Message)
	owner := EnterpriseImportRow{Row: 1, Email: "owner@example.com", Role: "member"}
	preview, err = f.s.PreviewImport(context.Background(), EnterpriseActor{IsAdmin: true}, 1, []EnterpriseImportRow{owner})
	require.NoError(t, err)
	require.Equal(t, 1, preview.Summary.Error)
}

type enterpriseSessionsTestStub struct{ users []int64 }

func (s *enterpriseSessionsTestStub) RevokeAllUserSessions(_ context.Context, id int64) error {
	s.users = append(s.users, id)
	return nil
}
func TestEnterpriseResetPasswordPermissionsSessionsAndSecrets(t *testing.T) {
	for _, tc := range []struct {
		name, role, targetRole        string
		admin, created, enabled, same bool
		want                          error
	}{
		{"owner member", "owner", "member", false, true, false, true, nil},
		{"owner manager", "owner", "manager", false, true, false, true, nil},
		{"owner other owner", "owner", "owner", false, true, false, true, ErrEnterprisePermissionDenied},
		{"admin owner", "member", "owner", true, true, false, true, nil},
		{"admin platform user", "owner", "member", true, false, false, true, ErrEnterprisePasswordResetForbidden},
		{"owner platform user", "owner", "member", false, false, false, true, ErrEnterprisePasswordResetForbidden},
		{"manager enabled", "manager", "member", false, true, true, true, nil},
		{"manager disabled", "manager", "member", false, true, false, true, ErrEnterprisePermissionDenied},
		{"manager outside", "manager", "member", false, true, true, false, ErrEnterprisePermissionDenied},
		{"manager manager", "manager", "manager", false, true, true, true, ErrEnterprisePermissionDenied},
		{"member", "member", "member", false, true, true, true, ErrEnterprisePermissionDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, users, _ := newEnterpriseImportTestFixture(t)
			f.e.caller.Role = tc.role
			f.e.caller.DepartmentID = enterpriseTestID(3)
			f.e.e.Settings.ManagerCanManageMembers = tc.enabled
			m := f.m.items[20]
			m.AccountCreatedByEnterprise = tc.created
			m.Role = tc.targetRole
			if !tc.same {
				m.DepartmentID = enterpriseTestID(99)
			}
			f.m.items[20] = m
			sessions := &enterpriseSessionsTestStub{}
			f.s.sessions = sessions
			before := resolvedTokenVersion(&User{ID: 20, Email: users.users[20].Email})
			result, err := f.s.ResetMemberPassword(context.Background(), EnterpriseActor{UserID: 10, IsAdmin: tc.admin}, 1, 20, nil)
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Empty(t, sessions.users)
				return
			}
			user := users.users[20]
			require.NotEqual(t, before, resolvedTokenVersion(&user))
			require.True(t, user.CheckPassword(result.GeneratedPassword))
			require.Equal(t, []int64{20}, sessions.users)
			require.Contains(t, f.auth.ids, int64(20))
			encoded, err := json.Marshal(f.a.logs)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), result.GeneratedPassword)
			require.Equal(t, "member.reset_password", f.a.logs[0].Action)
		})
	}
	f, _, _ := newEnterpriseImportTestFixture(t)
	m := f.m.items[20]
	m.AccountCreatedByEnterprise = true
	f.m.items[20] = m
	password := "short"
	_, err := f.s.ResetMemberPassword(context.Background(), EnterpriseActor{IsAdmin: true}, 1, 20, &password)
	require.ErrorIs(t, err, ErrEnterpriseInvalidInput)
	password = "explicit-reset-123"
	result, err := f.s.ResetMemberPassword(context.Background(), EnterpriseActor{IsAdmin: true}, 1, 20, &password)
	require.NoError(t, err)
	require.Empty(t, result.GeneratedPassword)
}

func TestEnterpriseBatchMemberQuotasMaskPartialAndAncestorPermissions(t *testing.T) {
	f := newEnterpriseManagementTestFixture(t)
	monthly, total := 20.0, 50.0
	q := f.q.items[EnterpriseQuotaScope{EnterpriseScopeMember, 20}]
	q.MonthlyLimitUSD = &monthly
	q.TotalLimitUSD = &total
	f.q.items[EnterpriseQuotaScope{EnterpriseScopeMember, 20}] = q
	upper, lower := 5.0, 4.0
	root := f.q.items[EnterpriseQuotaScope{EnterpriseScopeEnterprise, 1}]
	root.DailyLimitUSD = &upper
	f.q.items[EnterpriseQuotaScope{EnterpriseScopeEnterprise, 1}] = root
	result, err := f.s.BatchUpdateMemberQuotas(context.Background(), EnterpriseActor{IsAdmin: true}, 1, EnterpriseBatchMemberQuotaInput{MemberIDs: []int64{20, 99, 20}, Fields: []string{"daily"}, EnterpriseQuotaLimitsInput: EnterpriseQuotaLimitsInput{DailyLimitUSD: &lower}})
	require.NoError(t, err)
	require.Equal(t, []int64{20}, result.Updated)
	require.Len(t, result.Failed, 1)
	q = f.q.items[EnterpriseQuotaScope{EnterpriseScopeMember, 20}]
	require.Equal(t, &monthly, q.MonthlyLimitUSD)
	require.Equal(t, &total, q.TotalLimitUSD)
	require.Equal(t, "quota.update", f.a.logs[0].Action)
	tooHigh := 6.0
	result, err = f.s.BatchUpdateMemberQuotas(context.Background(), EnterpriseActor{IsAdmin: true}, 1, EnterpriseBatchMemberQuotaInput{MemberIDs: []int64{20}, Fields: []string{"daily"}, EnterpriseQuotaLimitsInput: EnterpriseQuotaLimitsInput{DailyLimitUSD: &tooHigh}})
	require.NoError(t, err)
	require.Empty(t, result.Updated)
	require.Equal(t, "ENTERPRISE_QUOTA_EXCEEDS_PARENT", result.Failed[0].Code)
	f.e.caller = &EnterpriseMemberContext{EnterpriseID: 1, Role: EnterpriseRoleManager, DepartmentID: enterpriseTestID(3), MemberStatus: EnterpriseMemberStatusActive, EnterpriseStatus: EnterpriseStatusActive}
	input := EnterpriseBatchMemberQuotaInput{MemberIDs: []int64{20}, Fields: []string{"daily"}}
	result, err = f.s.BatchUpdateMemberQuotas(context.Background(), EnterpriseActor{UserID: 10}, 1, input)
	require.NoError(t, err)
	require.Len(t, result.Failed, 1)
	f.e.e.Settings.ManagerCanEditMemberQuota = true
	result, err = f.s.BatchUpdateMemberQuotas(context.Background(), EnterpriseActor{UserID: 10}, 1, input)
	require.NoError(t, err)
	require.Equal(t, []int64{20}, result.Updated)
	require.Nil(t, f.q.items[EnterpriseQuotaScope{EnterpriseScopeMember, 20}].DailyLimitUSD)
	input.MemberIDs = make([]int64, 501)
	_, err = f.s.BatchUpdateMemberQuotas(context.Background(), EnterpriseActor{IsAdmin: true}, 1, input)
	require.ErrorIs(t, err, ErrEnterpriseInvalidInput)
}

type enterpriseSeriesRepositoryStub struct {
	*enterpriseUsageRepositoryStub
	dimension, granularity string
	top                    int
}

func (r *enterpriseSeriesRepositoryStub) GetEnterpriseUsageSeries(_ context.Context, f usagestats.UsageLogFilters, dimension, granularity string, top int) ([]EnterpriseUsageSeriesPoint, error) {
	r.captured = f
	r.dimension = dimension
	r.granularity = granularity
	r.top = top
	return []EnterpriseUsageSeriesPoint{}, nil
}
func TestEnterpriseUsageSeriesGranularityTopAndScope(t *testing.T) {
	repo := &enterpriseSeriesRepositoryStub{enterpriseUsageRepositoryStub: &enterpriseUsageRepositoryStub{}}
	svc := NewEnterpriseUsageService(repo)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(31 * 24 * time.Hour)
	filters := usagestats.UsageLogFilters{StartTime: &start, EndTime: &end, Model: "m", APIKeyID: 7}
	manager := &EnterpriseMemberContext{EnterpriseID: 1, Role: EnterpriseRoleManager, DepartmentID: enterpriseTestID(3), DepartmentSubtreeIDs: []int64{3, 4}, MemberStatus: EnterpriseMemberStatusActive}
	scope := EnterpriseUsageScope{EnterpriseID: 1, UserID: 10, Member: manager}
	for _, dimension := range []string{"models", "members", "departments"} {
		_, err := svc.SeriesTrend(context.Background(), scope, filters, dimension, "hour", 99)
		require.NoError(t, err)
		require.Equal(t, 20, repo.top)
		require.Equal(t, "hour", repo.granularity)
		require.Equal(t, int64(1), repo.captured.EnterpriseID)
		require.Equal(t, int64(3), repo.captured.EnterpriseDepartmentScopeID)
		require.Equal(t, "m", repo.captured.Model)
		require.Equal(t, int64(7), repo.captured.APIKeyID)
	}
	end = end.Add(time.Nanosecond)
	_, err := svc.SeriesTrend(context.Background(), scope, filters, "models", "hour", 0)
	require.Equal(t, "INVALID_DATE_RANGE", infraerrors.Reason(err))
	_, err = svc.SeriesTrend(context.Background(), scope, filters, "models", "day", 0)
	require.NoError(t, err)
	require.Equal(t, 8, repo.top)
	_, err = svc.SeriesTrend(context.Background(), scope, filters, "models", "week", 0)
	require.Equal(t, 400, infraerrors.Code(err))
	filters.EnterpriseDepartmentID = 99
	_, err = svc.SeriesTrend(context.Background(), scope, filters, "models", "day", 8)
	require.ErrorIs(t, err, ErrEnterprisePermissionDenied)
	scope.Member = &EnterpriseMemberContext{EnterpriseID: 1, Role: EnterpriseRoleMember, MemberStatus: EnterpriseMemberStatusActive}
	filters.EnterpriseDepartmentID = 0
	_, err = svc.SeriesTrend(context.Background(), scope, filters, "departments", "day", 8)
	require.ErrorIs(t, err, ErrEnterprisePermissionDenied)
	_, err = svc.SeriesTrend(context.Background(), scope, filters, "members", "day", 8)
	require.NoError(t, err)
	require.Equal(t, int64(10), repo.captured.UserID)
}

func TestEnterprisePasswordMatchesPlatformCharacterAndBcryptBounds(t *testing.T) {
	for _, tc := range []struct {
		password string
		valid    bool
	}{
		{"12345", false}, {"123456", true}, {"密码", false}, {"企业安全密码", true}, {strings.Repeat("a", 72), true}, {strings.Repeat("a", 73), false},
	} {
		require.Equal(t, tc.valid, enterprisePasswordValid(tc.password))
	}
}

func (r *enterpriseManagementTestEnterpriseGroups) ListWhitelists(context.Context, int64) ([]EnterpriseGroupWhitelist, error) {
	out := []EnterpriseGroupWhitelist{}
	for key, w := range r.whitelists {
		w.ScopeType = key.ScopeType
		w.ScopeID = key.ScopeID
		out = append(out, w)
	}
	return out, nil
}
func TestEnterpriseWhitelistsListOnlyEnterpriseAndGroupPermissionDisabled(t *testing.T) {
	f := newEnterpriseManagementTestFixture(t)
	f.g.whitelists = map[EnterpriseQuotaScope]EnterpriseGroupWhitelist{
		{EnterpriseScopeEnterprise, 1}: {GroupIDs: []int64{1, 2}},
		{EnterpriseScopeDepartment, 3}: {GroupIDs: []int64{1}},
		{EnterpriseScopeMember, 20}:    {GroupIDs: []int64{2}},
	}
	for _, role := range []string{EnterpriseRoleOwner, EnterpriseRoleManager} {
		f.e.caller = &EnterpriseMemberContext{EnterpriseID: 1, Role: role, DepartmentID: enterpriseTestID(3), MemberStatus: EnterpriseMemberStatusActive, EnterpriseStatus: EnterpriseStatusActive}
		f.e.e.Settings.ManagerCanEditMemberGroups = true
		items, err := f.s.ListGroupWhitelists(context.Background(), EnterpriseActor{UserID: 10}, 1)
		require.NoError(t, err)
		require.Len(t, items, 1)
		require.Equal(t, EnterpriseScopeEnterprise, items[0].ScopeType)
		permissions := EnterprisePermissionsFor(f.e.caller, f.e.e.Settings)
		require.False(t, permissions.EditMemberGroups)
	}
	require.ErrorIs(t, CheckEnterprisePermission(f.e.caller, f.e.e.Settings, EnterprisePermissionEditWhitelist, EnterprisePermissionTarget{ScopeType: EnterpriseScopeEnterprise, ScopeID: 1}), ErrEnterprisePermissionDenied)
}
