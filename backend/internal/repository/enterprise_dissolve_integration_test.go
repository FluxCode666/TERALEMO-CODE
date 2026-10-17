//go:build integration

package repository

import (
	"context"
	"errors"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func enterpriseDissolveService(client *dbent.Client, audit service.EnterpriseAuditLogRepository) *service.EnterpriseService {
	return service.NewEnterpriseService(client, NewEnterpriseRepository(client), NewEnterpriseDepartmentRepository(client), NewEnterpriseMemberRepository(client), NewEnterpriseQuotaRepository(client), NewEnterpriseBalanceLogRepository(client), audit, NewEnterpriseGroupRepository(client), nil, nil, nil, nil, service.NewNoopEnterpriseBillingCache())
}

func TestEnterpriseDissolveArchivesMembersQuotasAndBalance(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	client := tx.Client()
	e := enterpriseFixture(t, ctx)
	department := &service.EnterpriseDepartment{EnterpriseID: e.ID, Name: "Research"}
	require.NoError(t, NewEnterpriseDepartmentRepository(client).Create(ctx, department))
	owner := enterpriseMemberFixture(t, ctx, e.ID, &department.ID)
	owner.Role = service.EnterpriseRoleOwner
	require.NoError(t, NewEnterpriseMemberRepository(client).Update(ctx, owner))
	departed := enterpriseMemberFixture(t, ctx, e.ID, nil)
	require.NoError(t, NewEnterpriseMemberRepository(client).Delete(ctx, e.ID, departed.ID))
	groupID := enterpriseGroupFixture(t, client)
	groups := NewEnterpriseGroupRepository(client)
	require.NoError(t, groups.SetAllowedGroupIDs(ctx, e.ID, []int64{groupID}))
	require.NoError(t, groups.SetWhitelist(ctx, e.ID, service.EnterpriseScopeMember, owner.ID, []int64{groupID}))
	_, err := client.ExecContext(ctx, `UPDATE users SET balance=13.5 WHERE id=$1`, owner.UserID)
	require.NoError(t, err)
	_, err = client.ExecContext(ctx, `UPDATE enterprises SET balance=42.25 WHERE id=$1`, e.ID)
	require.NoError(t, err)
	_, err = client.ExecContext(ctx, `UPDATE enterprise_quotas SET daily_usage_usd=7.5,total_usage_usd=9.5 WHERE enterprise_id=$1`, e.ID)
	require.NoError(t, err)
	svc := enterpriseDissolveService(client, NewEnterpriseAuditLogRepository(client))
	actor := service.EnterpriseActor{UserID: owner.UserID, IsAdmin: true}
	require.ErrorIs(t, svc.DeleteEnterprise(ctx, actor, e.ID), service.ErrEnterpriseNotEmpty)
	require.NoError(t, svc.DissolveEnterprise(ctx, actor, e.ID))

	for table, total := range map[string]int64{"enterprises": 1, "enterprise_members": 2, "enterprise_departments": 1, "enterprise_quotas": 4} {
		column := "enterprise_id"
		if table == "enterprises" {
			column = "id"
		}
		var count, archived int64
		err := enterpriseQueryOne(ctx, client, `SELECT COUNT(*),COUNT(deleted_at) FROM `+table+` WHERE `+column+`=$1`, []any{e.ID}, func(s enterpriseScanner) error { return s.Scan(&count, &archived) })
		require.NoError(t, err)
		require.Equal(t, total, count, table)
		require.Equal(t, total, archived, table)
	}
	var balance, daily, total float64
	var configured bool
	require.NoError(t, enterpriseQueryOne(ctx, client, `SELECT balance,group_pool_configured FROM enterprises WHERE id=$1`, []any{e.ID}, func(s enterpriseScanner) error { return s.Scan(&balance, &configured) }))
	require.Equal(t, 42.25, balance)
	require.False(t, configured)
	require.NoError(t, enterpriseQueryOne(ctx, client, `SELECT daily_usage_usd,total_usage_usd FROM enterprise_quotas WHERE enterprise_id=$1 AND scope_type='member' AND scope_id=$2`, []any{e.ID, owner.ID}, func(s enterpriseScanner) error { return s.Scan(&daily, &total) }))
	require.Equal(t, 7.5, daily)
	require.Equal(t, 9.5, total)
	for _, table := range []string{"enterprise_allowed_groups", "enterprise_group_whitelists", "enterprise_group_whitelist_scopes"} {
		var count int64
		require.NoError(t, enterpriseQueryOne(ctx, client, `SELECT COUNT(*) FROM `+table+` WHERE enterprise_id=$1`, []any{e.ID}, func(s enterpriseScanner) error { return s.Scan(&count) }))
		require.Zero(t, count, table)
	}
	memberContext, err := NewEnterpriseRepository(client).GetMemberContextByUserID(ctx, owner.UserID)
	require.NoError(t, err)
	require.Nil(t, memberContext)
	logs, _, err := NewEnterpriseAuditLogRepository(client).List(ctx, e.ID, pagination.DefaultPagination(), service.EnterpriseAuditActionEnterpriseDelete)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	require.EqualValues(t, 1, logs[0].Detail["member_count"])
	require.EqualValues(t, 42.25, logs[0].Detail["remaining_balance"])
	// Frozen personal balances and the catalog group itself survive dissolution.
	var personalBalance float64
	require.NoError(t, enterpriseQueryOne(ctx, client, `SELECT balance FROM users WHERE id=$1`, []any{owner.UserID}, func(s enterpriseScanner) error { return s.Scan(&personalBalance) }))
	require.Equal(t, 13.5, personalBalance)
	var found int64
	require.NoError(t, enterpriseQueryOne(ctx, client, `SELECT id FROM groups WHERE id=$1`, []any{groupID}, func(s enterpriseScanner) error { return s.Scan(&found) }))
	require.Equal(t, groupID, found)
}

type enterpriseDissolveFailAudit struct {
	service.EnterpriseAuditLogRepository
}

func (enterpriseDissolveFailAudit) Create(context.Context, *service.EnterpriseAuditLog) error {
	return errors.New("audit unavailable")
}

func TestEnterpriseDissolveRollsBackWhenAuditFails(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	e := enterpriseFixture(t, ctx)
	var userID int64
	// This test needs the service to commit/roll back its own transaction. Clean
	// every committed fixture so the full repository suite sees no extra users.
	t.Cleanup(func() {
		for _, table := range []string{"enterprise_group_whitelists", "enterprise_group_whitelist_scopes", "enterprise_allowed_groups", "enterprise_audit_logs", "enterprise_balance_logs", "enterprise_quotas", "enterprise_members", "enterprise_departments"} {
			_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM `+table+` WHERE enterprise_id=$1`, e.ID)
		}
		_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM enterprises WHERE id=$1`, e.ID)
		if userID > 0 {
			_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
		}
	})
	m := enterpriseMemberFixture(t, ctx, e.ID, nil)
	userID = m.UserID
	m.Role = service.EnterpriseRoleOwner
	require.NoError(t, NewEnterpriseMemberRepository(client).Update(ctx, m))
	err := enterpriseDissolveService(client, enterpriseDissolveFailAudit{}).DissolveEnterprise(ctx, service.EnterpriseActor{UserID: m.UserID, IsAdmin: true}, e.ID)
	require.ErrorContains(t, err, "audit unavailable")
	_, err = NewEnterpriseRepository(client).GetByID(ctx, e.ID)
	require.NoError(t, err)
	_, err = NewEnterpriseMemberRepository(client).GetByID(ctx, e.ID, m.ID)
	require.NoError(t, err)
	q, err := NewEnterpriseQuotaRepository(client).GetByScope(ctx, e.ID, service.EnterpriseScopeMember, m.ID)
	require.NoError(t, err)
	require.NotNil(t, q)
	require.Nil(t, q.DeletedAt)
}
