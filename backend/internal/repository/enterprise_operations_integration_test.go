//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type enterpriseImportIntegrationAdmin struct {
	service.AdminService
	users service.UserRepository
}

func (a *enterpriseImportIntegrationAdmin) CreateUser(ctx context.Context, in *service.CreateUserInput) (*service.User, error) {
	user := &service.User{Email: in.Email, Username: in.Username, Role: service.RoleUser, Status: service.StatusActive, Balance: *in.Balance, Concurrency: in.Concurrency}
	if err := user.SetPassword(in.Password); err != nil {
		return nil, err
	}
	if err := a.users.Create(ctx, user); err != nil {
		return nil, err
	}
	if strings.HasPrefix(in.Email, "fail-") {
		return nil, infraerrors.BadRequest("CREATE_FAILED", "模拟建号失败")
	}
	return user, nil
}

func enterpriseOperationsIntegrationService(t *testing.T) *service.EnterpriseService {
	t.Helper()
	client := testEntClient(t)
	users := NewUserRepository(client, integrationDB)
	auth := service.NewAuthService(client, users, nil, NewRefreshTokenCache(integrationRedis), nil, nil, nil, nil, nil, nil, nil, nil, nil)
	return service.ProvideEnterpriseService(client, NewEnterpriseRepository(client), NewEnterpriseDepartmentRepository(client), NewEnterpriseMemberRepository(client), NewEnterpriseQuotaRepository(client), NewEnterpriseBalanceLogRepository(client), NewEnterpriseAuditLogRepository(client), NewEnterpriseGroupRepository(client), users, NewGroupRepository(client, integrationDB), &enterpriseImportIntegrationAdmin{users: users}, nil, service.NewNoopEnterpriseBillingCache(), auth, nil, NewUserGroupRateRepository(integrationDB))
}

func TestEnterpriseImportTransactionsOriginAndPasswordSessions(t *testing.T) {
	ctx := context.Background()
	f, nodes := treeBillingFixture(t)
	client := testEntClient(t)
	users := NewUserRepository(client, integrationDB)
	members := NewEnterpriseMemberRepository(client)
	f.member.Role = service.EnterpriseRoleOwner
	require.NoError(t, members.Update(ctx, f.member))
	owner, err := users.GetByID(ctx, f.member.UserID)
	require.NoError(t, err)
	svc := enterpriseOperationsIntegrationService(t)
	suffix := uuid.NewString()
	explicit := "enterprise-explicit-secret-123"
	// An existing platform account is occupied on the enterprise surface, while
	// the admin surface may attach it without changing its origin or wallet.
	existing := mustCreateUser(t, client, &service.User{Email: "occupied-" + suffix + "@example.com", Balance: 17})
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM users WHERE id=$1 AND NOT EXISTS (SELECT 1 FROM enterprise_members WHERE user_id=$1)", existing.ID)
	})
	rows := []service.EnterpriseImportRow{
		{Row: 1, Email: "generated-" + suffix + "@example.com", Username: "Generated", DepartmentPath: "New/Team", Role: "员工"},
		{Row: 2, Email: owner.Email, DepartmentPath: strings.Join([]string{nodes[0].Name, nodes[1].Name, nodes[2].Name, nodes[3].Name}, "/"), Role: "企业负责人", Title: "Updated owner"},
		{Row: 3, Email: existing.Email, Role: "member"},
		{Row: 4, Email: "dup-" + suffix + "@example.com", Role: "member"},
		{Row: 5, Email: "dup-" + suffix + "@example.com", Role: "member"},
		{Row: 6, Email: "deep-" + suffix + "@example.com", DepartmentPath: "1/2/3/4/5/6/7/8/9", Role: "member"},
		{Row: 7, Email: "manager-" + suffix + "@example.com", Role: "manager"},
		{Row: 8, Email: "fail-" + suffix + "@example.com", Role: "member"},
		{Row: 9, Email: "explicit-" + suffix + "@example.com", DepartmentPath: "Other/Team", Role: "member", Password: &explicit},
	}
	actor := service.EnterpriseActor{UserID: f.member.UserID}
	preview, err := svc.PreviewImport(ctx, actor, f.enterprise.ID, rows)
	require.NoError(t, err)
	require.Equal(t, service.EnterpriseImportPreviewSummary{Total: 9, Create: 3, Update: 1, Error: 5}, preview.Summary)
	require.Equal(t, []string{"New", "New/Team", "Other", "Other/Team"}, preview.DepartmentsToCreate)
	raw, err := json.Marshal(preview)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "password")
	result, err := svc.CommitImport(ctx, actor, f.enterprise.ID, rows)
	require.NoError(t, err)
	require.Equal(t, service.EnterpriseImportCommitSummary{Total: 9, Created: 2, Updated: 1, Failed: 6}, result.Summary)
	require.NotEmpty(t, result.Rows[0].GeneratedPassword)
	require.Empty(t, result.Rows[7].GeneratedPassword)
	_, failedUserErr := users.GetByEmail(ctx, rows[7].Email)
	require.ErrorIs(t, failedUserErr, service.ErrUserNotFound)
	require.Empty(t, result.Rows[8].GeneratedPassword)
	target, err := members.GetByID(ctx, f.enterprise.ID, result.Rows[0].MemberID)
	require.NoError(t, err)
	require.True(t, target.AccountCreatedByEnterprise)
	user, err := users.GetByID(ctx, target.UserID)
	require.NoError(t, err)
	require.Zero(t, user.Balance)
	require.True(t, user.CheckPassword(result.Rows[0].GeneratedPassword))
	// Real refresh sessions are deleted, and password-hash changes revoke the
	// access-token fingerprint checked by the JWT middleware.
	cache := NewRefreshTokenCache(integrationRedis)
	hashes := []string{uuid.NewString(), uuid.NewString()}
	t.Cleanup(func() { _ = cache.DeleteUserRefreshTokens(context.Background(), target.UserID) })
	for _, hash := range hashes {
		require.NoError(t, cache.StoreRefreshToken(ctx, hash, &service.RefreshTokenData{UserID: target.UserID}, time.Hour))
		require.NoError(t, cache.AddToUserTokenSet(ctx, target.UserID, hash, time.Hour))
	}
	oldHash := user.PasswordHash
	reset, err := svc.ResetMemberPassword(ctx, actor, f.enterprise.ID, target.ID, nil)
	require.NoError(t, err)
	require.NotEmpty(t, reset.GeneratedPassword)
	user, err = users.GetByID(ctx, target.UserID)
	require.NoError(t, err)
	require.NotEqual(t, oldHash, user.PasswordHash)
	require.True(t, user.CheckPassword(reset.GeneratedPassword))
	for _, hash := range hashes {
		_, err := cache.GetRefreshToken(ctx, hash)
		require.ErrorIs(t, err, service.ErrRefreshTokenNotFound)
	}
	// Register this cleanup before attaching, so the fixture removes membership
	// first (Cleanup callbacks run in reverse registration order).
	attached, err := svc.CommitImport(ctx, service.EnterpriseActor{UserID: owner.ID, IsAdmin: true}, f.enterprise.ID, []service.EnterpriseImportRow{{Row: 1, Email: existing.Email, Role: "member"}})
	require.NoError(t, err)
	require.Equal(t, 1, attached.Summary.Created)
	existingMember, err := members.GetByID(ctx, f.enterprise.ID, attached.Rows[0].MemberID)
	require.NoError(t, err)
	require.False(t, existingMember.AccountCreatedByEnterprise)
	_, err = svc.ResetMemberPassword(ctx, service.EnterpriseActor{IsAdmin: true}, f.enterprise.ID, existingMember.ID, nil)
	require.ErrorIs(t, err, service.ErrEnterprisePasswordResetForbidden)
	existing, err = users.GetByID(ctx, existing.ID)
	require.NoError(t, err)
	require.Equal(t, 17.0, existing.Balance)
	logs, _, err := NewEnterpriseAuditLogRepository(client).List(ctx, f.enterprise.ID, pagination.PaginationParams{Page: 1, PageSize: 100}, "")
	require.NoError(t, err)
	raw, err = json.Marshal(logs)
	require.NoError(t, err)
	for _, secret := range []string{explicit, result.Rows[0].GeneratedPassword, reset.GeneratedPassword} {
		require.NotContains(t, string(raw), secret)
	}
	// Every committed fixture row, including imported users, is removed by the
	// billing fixture. Extra standalone-user cleanup only handles early failures.
}

func TestEnterpriseAccountOriginMigrationBackfillBoundary(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	e := enterpriseFixture(t, ctx)
	var members []*service.EnterpriseMember
	for _, gap := range []time.Duration{0, 10 * time.Second, -10 * time.Second, 10*time.Second + time.Millisecond, -11 * time.Second} {
		member := enterpriseMemberFixture(t, ctx, e.ID, nil)
		members = append(members, member)
		_, err := tx.ExecContext(ctx, `UPDATE users SET created_at=(SELECT created_at FROM enterprise_members WHERE id=$1)+$2::interval WHERE id=$3`, member.ID, fmt.Sprintf("%f seconds", gap.Seconds()), member.UserID)
		require.NoError(t, err)
	}
	migration, err := migrations.FS.ReadFile("252_enterprise_member_account_origin.sql")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	repo := NewEnterpriseMemberRepository(tx.Client())
	for i, member := range members {
		stored, err := repo.GetByID(ctx, e.ID, member.ID)
		require.NoError(t, err)
		require.Equal(t, i < 3, stored.AccountCreatedByEnterprise)
	}
}

func TestEnterpriseUsageSeriesRawSnapshotsTopOtherAndSubtree(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	client := tx.Client()
	e := enterpriseFixture(t, ctx)
	other := enterpriseFixture(t, ctx)
	root := treeDepartment(t, ctx, client, e.ID, nil, "总部")
	departments := []*service.EnterpriseDepartment{}
	members := []*service.EnterpriseMember{}
	for _, name := range []string{"A", "B", "C"} {
		d := treeDepartment(t, ctx, client, e.ID, &root.ID, name)
		departments = append(departments, d)
		members = append(members, enterpriseMemberFixture(t, ctx, e.ID, &d.ID))
	}
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: members[0].UserID, Key: "sk-series-" + uuid.NewString(), Name: "series"})
	account := mustCreateAccount(t, client, &service.Account{Name: "enterprise-series"})
	repo := newUsageLogRepositoryWithSQL(client, tx)
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(48 * time.Hour)
	for _, v := range []struct {
		member     int
		model      string
		cost       float64
		hour       int
		enterprise int64
	}{
		{0, "heavy", 10, 1, e.ID}, {0, "heavy", 2, 25, e.ID}, {1, "small", 3, 2, e.ID}, {2, "tiny", 1, 2, e.ID},
		{2, "outside", 1000, 2, other.ID}, {2, "personal", 10000, 2, 0},
	} {
		_, err := repo.Create(ctx, &service.UsageLog{UserID: members[v.member].UserID, APIKeyID: key.ID, AccountID: account.ID, RequestID: uuid.NewString(), Model: "private-upstream", RequestedModel: v.model, InputTokens: 3, OutputTokens: 4, CacheCreationTokens: 5, CacheReadTokens: 6, ActualCost: v.cost, EnterpriseID: &v.enterprise, EnterpriseDepartmentID: &departments[v.member].ID, CreatedAt: start.Add(time.Duration(v.hour) * time.Hour)})
		require.NoError(t, err)
	}
	// Poison all-site rollups; every series must still sum the four raw rows.
	_, err := tx.ExecContext(ctx, `INSERT INTO usage_dashboard_daily(bucket_date,total_requests,actual_cost) VALUES($1,99999,99999) ON CONFLICT(bucket_date) DO UPDATE SET total_requests=99999,actual_cost=99999`, start)
	require.NoError(t, err)
	svc := service.NewEnterpriseUsageService(repo)
	scope := service.EnterpriseUsageScope{EnterpriseID: e.ID}
	filters := usagestats.UsageLogFilters{StartTime: &start, EndTime: &end}
	for _, granularity := range []string{"day", "hour"} {
		for _, dimension := range []string{"models", "members", "departments"} {
			points, err := svc.SeriesTrend(ctx, scope, filters, dimension, granularity, 1)
			require.NoError(t, err)
			require.Len(t, points, 3)
			var requests, tokens int64
			var cost float64
			var others int
			for _, point := range points {
				requests += point.Requests
				tokens += point.TotalTokens
				cost += point.ActualCost
				if granularity == "hour" {
					require.Len(t, point.Date, 16)
				} else {
					require.Len(t, point.Date, 10)
				}
				switch dimension {
				case "models":
					if *point.Model == "__other__" {
						others++
						require.Equal(t, 4.0, point.ActualCost)
					} else {
						require.Equal(t, "heavy", *point.Model)
					}
				case "members":
					if *point.UserID == 0 {
						others++
						require.Equal(t, "__other__", *point.Username)
						require.Equal(t, 4.0, point.ActualCost)
					} else {
						require.Equal(t, members[0].UserID, *point.UserID)
					}
				case "departments":
					if *point.DepartmentID == 0 {
						others++
						require.Equal(t, "__other__", *point.DepartmentName)
						require.Equal(t, 4.0, point.ActualCost)
					} else {
						require.Equal(t, "总部/A", *point.DepartmentName)
					}
				}
			}
			require.Equal(t, int64(4), requests)
			require.Equal(t, int64(72), tokens)
			require.Equal(t, 16.0, cost)
			require.Equal(t, 1, others)
		}
	}
	scope.Member = &service.EnterpriseMemberContext{EnterpriseID: e.ID, MemberStatus: service.EnterpriseMemberStatusActive, Role: service.EnterpriseRoleManager, DepartmentID: &departments[0].ID, DepartmentSubtreeIDs: []int64{departments[0].ID}}
	points, err := svc.SeriesTrend(ctx, scope, filters, "departments", "day", 8)
	require.NoError(t, err)
	require.Len(t, points, 2)
	for _, point := range points {
		require.Equal(t, departments[0].ID, *point.DepartmentID)
	}
	filters.EnterpriseDepartmentID = departments[1].ID
	_, err = svc.SeriesTrend(ctx, scope, filters, "models", "day", 8)
	require.ErrorIs(t, err, service.ErrEnterprisePermissionDenied)
	scope.Member = nil
	filters.EnterpriseDepartmentID = root.ID
	filters.Model = "small"
	filters.APIKeyID = key.ID
	filters.UserID = members[1].UserID
	points, err = svc.SeriesTrend(ctx, scope, filters, "models", "day", 8)
	require.NoError(t, err)
	require.Len(t, points, 1)
	require.Equal(t, 3.0, points[0].ActualCost)
}

func TestEnterpriseBatchQuotasPartialAncestorAndManagerSubtree(t *testing.T) {
	ctx := context.Background()
	f, nodes := treeBillingFixture(t)
	client := testEntClient(t)
	svc := enterpriseOperationsIntegrationService(t)
	quotas := NewEnterpriseQuotaRepository(client)
	capValue := 5.0
	_, err := svc.UpdateQuota(ctx, service.EnterpriseActor{IsAdmin: true}, f.enterprise.ID, service.EnterpriseScopeDepartment, nodes[0].ID, service.EnterpriseQuotaLimitsInput{DailyLimitUSD: &capValue})
	require.NoError(t, err)
	monthly := 12.0
	_, err = svc.UpdateQuota(ctx, service.EnterpriseActor{IsAdmin: true}, f.enterprise.ID, service.EnterpriseScopeMember, f.member.ID, service.EnterpriseQuotaLimitsInput{MonthlyLimitUSD: &monthly})
	require.NoError(t, err)
	manager := enterpriseMemberFixture(t, ctx, f.enterprise.ID, &nodes[1].ID)
	manager.Role = service.EnterpriseRoleManager
	require.NoError(t, NewEnterpriseMemberRepository(client).Update(ctx, manager))
	otherDepartment := treeDepartment(t, ctx, client, f.enterprise.ID, nil, "outside-quota")
	outside := enterpriseMemberFixture(t, ctx, f.enterprise.ID, &otherDepartment.ID)
	actor := service.EnterpriseActor{UserID: manager.UserID}
	value := 4.0
	input := service.EnterpriseBatchMemberQuotaInput{MemberIDs: []int64{f.member.ID, outside.ID, 99999999}, Fields: []string{"daily"}, EnterpriseQuotaLimitsInput: service.EnterpriseQuotaLimitsInput{DailyLimitUSD: &value}}
	result, err := svc.BatchUpdateMemberQuotas(ctx, actor, f.enterprise.ID, input)
	require.NoError(t, err)
	require.Empty(t, result.Updated)
	require.Len(t, result.Failed, 3)
	settings := service.DefaultEnterpriseSettings()
	settings.ManagerCanEditMemberQuota = true
	_, err = svc.UpdateSettings(ctx, service.EnterpriseActor{IsAdmin: true}, f.enterprise.ID, settings)
	require.NoError(t, err)
	tooHigh := 6.0
	input.MemberIDs = []int64{f.member.ID}
	input.DailyLimitUSD = &tooHigh
	result, err = svc.BatchUpdateMemberQuotas(ctx, actor, f.enterprise.ID, input)
	require.NoError(t, err)
	require.Len(t, result.Failed, 1)
	require.Equal(t, "ENTERPRISE_QUOTA_EXCEEDS_PARENT", result.Failed[0].Code)
	input.MemberIDs = []int64{f.member.ID, outside.ID, 99999999}
	input.DailyLimitUSD = &value
	result, err = svc.BatchUpdateMemberQuotas(ctx, actor, f.enterprise.ID, input)
	require.NoError(t, err)
	require.Equal(t, []int64{f.member.ID}, result.Updated)
	require.Len(t, result.Failed, 2)
	stored, err := quotas.GetByScope(ctx, f.enterprise.ID, service.EnterpriseScopeMember, f.member.ID)
	require.NoError(t, err)
	require.Equal(t, &value, stored.DailyLimitUSD)
	require.Equal(t, &monthly, stored.MonthlyLimitUSD)
	input.MemberIDs = []int64{f.member.ID}
	input.DailyLimitUSD = nil
	result, err = svc.BatchUpdateMemberQuotas(ctx, actor, f.enterprise.ID, input)
	require.NoError(t, err)
	require.Equal(t, []int64{f.member.ID}, result.Updated)
	stored, err = quotas.GetByScope(ctx, f.enterprise.ID, service.EnterpriseScopeMember, f.member.ID)
	require.NoError(t, err)
	require.Nil(t, stored.DailyLimitUSD)
	require.Equal(t, &monthly, stored.MonthlyLimitUSD)
}

type enterpriseImportFailDepartments struct {
	service.EnterpriseDepartmentRepository
	creates int
}

func (r *enterpriseImportFailDepartments) Create(ctx context.Context, d *service.EnterpriseDepartment) error {
	r.creates++
	if r.creates == 2 {
		return infraerrors.BadRequest("DEPARTMENT_CREATE_FAILED", "模拟部门创建失败")
	}
	return r.EnterpriseDepartmentRepository.Create(ctx, d)
}
func TestEnterpriseImportDepartmentPhaseRollsBackAtomically(t *testing.T) {
	ctx := context.Background()
	f, _ := treeBillingFixture(t)
	client := testEntClient(t)
	users := NewUserRepository(client, integrationDB)
	departments := &enterpriseImportFailDepartments{EnterpriseDepartmentRepository: NewEnterpriseDepartmentRepository(client)}
	svc := service.NewEnterpriseService(client, NewEnterpriseRepository(client), departments, NewEnterpriseMemberRepository(client), NewEnterpriseQuotaRepository(client), NewEnterpriseBalanceLogRepository(client), NewEnterpriseAuditLogRepository(client), NewEnterpriseGroupRepository(client), users, NewGroupRepository(client, integrationDB), &enterpriseImportIntegrationAdmin{users: users}, nil, service.NewNoopEnterpriseBillingCache(), nil)
	email := "atomic-" + uuid.NewString() + "@example.com"
	_, err := svc.CommitImport(ctx, service.EnterpriseActor{IsAdmin: true}, f.enterprise.ID, []service.EnterpriseImportRow{{Row: 1, Email: email, DepartmentPath: "Atomic/New", Role: "member"}})
	require.Equal(t, "DEPARTMENT_CREATE_FAILED", infraerrors.Reason(err))
	items, err := departments.List(ctx, f.enterprise.ID)
	require.NoError(t, err)
	for _, d := range items {
		require.NotEqual(t, "Atomic", d.Name)
	}
	_, err = users.GetByEmail(ctx, email)
	require.ErrorIs(t, err, service.ErrUserNotFound)
}
