//go:build integration

package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func enterpriseConcurrencyService(t *testing.T, audit service.EnterpriseAuditLogRepository, settings *service.SettingService) *service.EnterpriseService {
	t.Helper()
	client := testEntClient(t)
	users := NewUserRepository(client, integrationDB)
	if audit == nil {
		audit = NewEnterpriseAuditLogRepository(client)
	}
	return service.NewEnterpriseService(client, NewEnterpriseRepository(client), NewEnterpriseDepartmentRepository(client), NewEnterpriseMemberRepository(client), NewEnterpriseQuotaRepository(client), NewEnterpriseBalanceLogRepository(client), audit, NewEnterpriseGroupRepository(client), users, NewGroupRepository(client, integrationDB), &enterpriseImportIntegrationAdmin{users: users}, nil, service.NewNoopEnterpriseBillingCache(), settings)
}

func TestEnterpriseMemberConcurrencyUpdatesCurrentMembersAndInheritance(t *testing.T) {
	ctx := context.Background()
	f, _ := treeBillingFixture(t) // Registers Cleanup for committed data, including removed memberships.
	other, _ := treeBillingFixture(t)
	client := testEntClient(t)
	members := NewEnterpriseMemberRepository(client)
	users := NewUserRepository(client, integrationDB)
	disabled := enterpriseMemberFixture(t, ctx, f.enterprise.ID, nil)
	disabled.Status = service.EnterpriseMemberStatusDisabled
	require.NoError(t, members.Update(ctx, disabled))
	_, err := integrationDB.ExecContext(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, disabled.UserID)
	require.NoError(t, err)
	removed := enterpriseMemberFixture(t, ctx, f.enterprise.ID, nil)
	require.NoError(t, members.Delete(ctx, f.enterprise.ID, removed.ID))
	for _, uid := range []int64{f.member.UserID, disabled.UserID, removed.UserID, other.member.UserID} {
		_, err := users.BatchSetConcurrency(ctx, []int64{uid}, 4)
		require.NoError(t, err)
	}
	svc := enterpriseConcurrencyService(t, nil, nil)
	actor := service.EnterpriseActor{UserID: f.member.UserID, IsAdmin: true}
	value := 21
	result, err := svc.UpdateMemberConcurrency(ctx, actor, f.enterprise.ID, &value)
	require.NoError(t, err)
	require.Equal(t, 2, result.UpdatedCount)
	for uid, expected := range map[int64]int{f.member.UserID: value, disabled.UserID: value, removed.UserID: 4, other.member.UserID: 4} {
		u, err := users.GetByID(ctx, uid)
		require.NoError(t, err)
		require.Equal(t, expected, u.Concurrency)
	}
	enterprises := NewEnterpriseRepository(client)
	e, err := enterprises.GetByID(ctx, f.enterprise.ID)
	require.NoError(t, err)
	require.Equal(t, &value, e.MemberConcurrency)
	listed, _, err := enterprises.List(ctx, pagination.DefaultPagination(), service.EnterpriseListFilters{Search: e.Name})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, &value, listed[0].MemberConcurrency)
	listedMembers, _, err := members.List(ctx, e.ID, pagination.DefaultPagination(), service.EnterpriseMemberListFilters{})
	require.NoError(t, err)
	require.Len(t, listedMembers, 2)
	for _, m := range listedMembers {
		require.Equal(t, value, m.Concurrency)
	}
	logs, _, err := NewEnterpriseAuditLogRepository(client).List(ctx, e.ID, pagination.DefaultPagination(), service.EnterpriseAuditActionMembersConcurrencyUpdate)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	require.Nil(t, logs[0].Detail["old_value"])
	require.Equal(t, float64(value), logs[0].Detail["new_value"])
	require.Equal(t, float64(2), logs[0].Detail["updated_count"])

	// Rejoin an existing account: apply the enterprise value, keep it on removal.
	joined, err := svc.CreateMember(ctx, actor, e.ID, service.CreateEnterpriseMemberInput{UserID: removed.UserID})
	require.NoError(t, err)
	require.NoError(t, svc.RemoveMember(ctx, actor, e.ID, joined.ID))
	u, err := users.GetByID(ctx, removed.UserID)
	require.NoError(t, err)
	require.Equal(t, value, u.Concurrency)

	result, err = svc.UpdateMemberConcurrency(ctx, actor, e.ID, nil)
	require.NoError(t, err)
	require.Nil(t, result.MemberConcurrency)
	require.Zero(t, result.UpdatedCount)
	u, err = users.GetByID(ctx, disabled.UserID)
	require.NoError(t, err)
	require.Equal(t, value, u.Concurrency)
	e, err = enterprises.GetByID(ctx, e.ID)
	require.NoError(t, err)
	require.Nil(t, e.MemberConcurrency)
	newMember, err := svc.CreateMember(ctx, actor, e.ID, service.CreateEnterpriseMemberInput{Email: "cleared-" + uuid.NewString() + "@example.com", Password: "password-123"})
	require.NoError(t, err)
	u, err = users.GetByID(ctx, newMember.UserID)
	require.NoError(t, err)
	require.Equal(t, 5, u.Concurrency)
}

type enterpriseConcurrencyFailAudit struct {
	service.EnterpriseAuditLogRepository
}

func (enterpriseConcurrencyFailAudit) Create(context.Context, *service.EnterpriseAuditLog) error {
	return errors.New("audit unavailable")
}

func TestEnterpriseMemberConcurrencyAuditFailureRollsBackUsersAndSetting(t *testing.T) {
	ctx := context.Background()
	f, _ := treeBillingFixture(t)
	client := testEntClient(t)
	users := NewUserRepository(client, integrationDB)
	_, err := users.BatchSetConcurrency(ctx, []int64{f.member.UserID}, 6)
	require.NoError(t, err)
	svc := enterpriseConcurrencyService(t, enterpriseConcurrencyFailAudit{}, nil)
	value := 28
	_, err = svc.UpdateMemberConcurrency(ctx, service.EnterpriseActor{IsAdmin: true}, f.enterprise.ID, &value)
	require.ErrorContains(t, err, "audit unavailable")
	u, err := users.GetByID(ctx, f.member.UserID)
	require.NoError(t, err)
	require.Equal(t, 6, u.Concurrency)
	e, err := NewEnterpriseRepository(client).GetByID(ctx, f.enterprise.ID)
	require.NoError(t, err)
	require.Nil(t, e.MemberConcurrency)
}

type enterpriseConcurrencySettings struct {
	service.SettingRepository
	value string
}

func (s enterpriseConcurrencySettings) GetValue(context.Context, string) (string, error) {
	return s.value, nil
}

func TestEnterpriseMemberConcurrencyCreationEntrypointsPostgres(t *testing.T) {
	for _, entry := range []string{"enterprise_create", "import", "admin_create", "new_owner"} {
		for _, override := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/override=%t", entry, override), func(t *testing.T) {
				tx := testEntTx(t) // Every inserted row rolls back through t.Cleanup.
				ctx := dbent.NewTxContext(context.Background(), tx)
				e := enterpriseFixture(t, ctx)
				owner := enterpriseMemberFixture(t, ctx, e.ID, nil)
				members := NewEnterpriseMemberRepository(testEntClient(t))
				owner.Role = service.EnterpriseRoleOwner
				require.NoError(t, members.Update(ctx, owner))
				settings := service.NewSettingService(enterpriseConcurrencySettings{value: "8"}, &config.Config{})
				svc := enterpriseConcurrencyService(t, nil, settings)
				actor := service.EnterpriseActor{UserID: owner.UserID, IsAdmin: entry == "admin_create" || entry == "new_owner"}
				want := 8
				if override {
					v := 14
					_, err := svc.UpdateMemberConcurrency(ctx, service.EnterpriseActor{IsAdmin: true}, e.ID, &v)
					require.NoError(t, err)
					want = v
				}
				email := uuid.NewString() + "@example.com"
				password := "password-123"
				switch entry {
				case "import":
					result, err := svc.CommitImport(ctx, actor, e.ID, []service.EnterpriseImportRow{{Row: 1, Email: email, Role: "member", Password: &password}})
					require.NoError(t, err)
					require.Equal(t, 1, result.Summary.Created)
				case "new_owner":
					// The new enterprise has no override at creation.
					want = 8
					_, err := svc.CreateEnterprise(ctx, actor, service.CreateEnterpriseInput{Name: uuid.NewString(), Owner: &service.EnterpriseOwnerInput{Email: email, Password: password}})
					require.NoError(t, err)
				default:
					_, err := svc.CreateMember(ctx, actor, e.ID, service.CreateEnterpriseMemberInput{Email: email, Password: password})
					require.NoError(t, err)
				}
				u, err := NewUserRepository(testEntClient(t), integrationDB).GetByEmail(ctx, email)
				require.NoError(t, err)
				require.Equal(t, want, u.Concurrency)
				require.Zero(t, u.Balance)
			})
		}
	}
}

func TestMigration253EnterpriseConcurrencyBackfill(t *testing.T) {
	for _, tc := range []struct {
		name, setting string
		want          int
	}{
		{"configured", "12", 12}, {"missing", "", 5}, {"invalid", "abc", 5},
		{"zero", "0", 5}, {"negative", "-2", 5}, {"fraction", "1.5", 5},
		{"overflow", "2147483648", 5}, {"huge", "999999999999999999999999999999999999", 5},
		{"leading_zeros", "0000000000000000000009", 9}, {"maximum", "2147483647", 2147483647},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := testEntTx(t)
			ctx := dbent.NewTxContext(context.Background(), tx)
			e := enterpriseFixture(t, ctx)
			_, err := tx.ExecContext(ctx, `DELETE FROM settings WHERE key='default_concurrency'`)
			require.NoError(t, err)
			if tc.setting != "" {
				_, err = tx.ExecContext(ctx, `INSERT INTO settings (key,value) VALUES ('default_concurrency',$1)`, tc.setting)
				require.NoError(t, err)
			}
			members := make([]*service.EnterpriseMember, 5)
			for i := range members {
				members[i] = enterpriseMemberFixture(t, ctx, e.ID, nil)
				_, err := tx.ExecContext(ctx, `UPDATE users SET concurrency=$2 WHERE id=$1`, members[i].UserID, 1)
				require.NoError(t, err)
				_, err = tx.ExecContext(ctx, `UPDATE enterprise_members SET account_created_by_enterprise=$2 WHERE id=$1`, members[i].ID, i != 1)
				require.NoError(t, err)
			}
			// enterprise-created but already customized, removed, or user-deleted.
			_, err = tx.ExecContext(ctx, `UPDATE users SET concurrency=3 WHERE id=$1`, members[2].UserID)
			require.NoError(t, err)
			_, err = tx.ExecContext(ctx, `UPDATE enterprise_members SET deleted_at=NOW() WHERE id=$1`, members[3].ID)
			require.NoError(t, err)
			_, err = tx.ExecContext(ctx, `UPDATE users SET deleted_at=NOW() WHERE id=$1`, members[4].UserID)
			require.NoError(t, err)
			content, err := migrations.FS.ReadFile("253_enterprise_member_concurrency.sql")
			require.NoError(t, err)
			for range 2 { // DDL and backfill are safe to replay.
				_, err = tx.ExecContext(ctx, string(content))
				require.NoError(t, err)
			}
			for i, want := range []int{tc.want, 1, 3, 1, 1} {
				var got int
				rows, err := tx.QueryContext(ctx, `SELECT concurrency FROM users WHERE id=$1`, members[i].UserID)
				require.NoError(t, err)
				require.True(t, rows.Next())
				require.NoError(t, rows.Scan(&got))
				require.NoError(t, rows.Close())
				require.Equal(t, want, got)
			}
		})
	}
}
