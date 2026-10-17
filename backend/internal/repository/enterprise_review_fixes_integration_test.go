//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enterprisequota"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestEnterpriseScopeDeletionArchivesQuotaRows(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	client := testEntClient(t)
	e := enterpriseFixture(t, ctx)
	d := &service.EnterpriseDepartment{EnterpriseID: e.ID, Name: "Archived department"}
	departments := NewEnterpriseDepartmentRepository(client)
	members := NewEnterpriseMemberRepository(client)
	quotas := NewEnterpriseQuotaRepository(client)
	require.NoError(t, departments.Create(ctx, d))
	m := enterpriseMemberFixture(t, ctx, e.ID, &d.ID)
	memberQuota, err := quotas.GetByScope(ctx, e.ID, service.EnterpriseScopeMember, m.ID)
	require.NoError(t, err)
	departmentQuota, err := quotas.GetByScope(ctx, e.ID, service.EnterpriseScopeDepartment, d.ID)
	require.NoError(t, err)
	require.NoError(t, members.Delete(ctx, e.ID, m.ID))
	require.NoError(t, departments.Delete(ctx, e.ID, d.ID))
	for _, q := range []*service.EnterpriseQuota{memberQuota, departmentQuota} {
		archived, err := tx.Client().EnterpriseQuota.Query().Where(enterprisequota.IDEQ(q.ID)).Only(mixins.SkipSoftDelete(ctx))
		require.NoError(t, err, "the frozen billing identity must still find the archived quota")
		require.NotNil(t, archived.DeletedAt)
		require.Equal(t, q.ScopeID, archived.ScopeID)
	}
}

func TestEnterpriseGroupConfigurationPersistsEmptyAndExplicitClear(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	client := testEntClient(t)
	e := enterpriseFixture(t, ctx)
	m := enterpriseMemberFixture(t, ctx, e.ID, nil)
	groups := NewEnterpriseGroupRepository(client)
	enterprises := NewEnterpriseRepository(client)
	groupID := enterpriseGroupFixture(t, tx.Client())
	w, err := groups.GetWhitelist(ctx, e.ID, service.EnterpriseScopeMember, m.ID)
	require.NoError(t, err)
	require.Nil(t, w)
	require.NoError(t, groups.SetAllowedGroupIDs(ctx, e.ID, []int64{groupID}))
	require.NoError(t, groups.SetWhitelist(ctx, e.ID, service.EnterpriseScopeMember, m.ID, []int64{groupID}))
	var scopeID, repeatedScopeID int64
	require.NoError(t, enterpriseQueryOne(ctx, tx.Client(), `SELECT id FROM enterprise_group_whitelist_scopes WHERE scope_type = $1 AND scope_id = $2`, []any{service.EnterpriseScopeMember, m.ID}, func(s enterpriseScanner) error { return s.Scan(&scopeID) }))
	require.Positive(t, scopeID)
	// Rewriting a configured layer uses its unique scope key and retains the
	// database-generated ID instead of creating a second marker row.
	require.NoError(t, groups.SetWhitelist(ctx, e.ID, service.EnterpriseScopeMember, m.ID, []int64{groupID}))
	require.NoError(t, enterpriseQueryOne(ctx, tx.Client(), `SELECT id FROM enterprise_group_whitelist_scopes WHERE scope_type = $1 AND scope_id = $2`, []any{service.EnterpriseScopeMember, m.ID}, func(s enterpriseScanner) error { return s.Scan(&repeatedScopeID) }))
	require.Equal(t, scopeID, repeatedScopeID)
	require.NoError(t, newGroupRepositoryWithSQL(tx.Client(), tx.Client()).Delete(ctx, groupID))
	contexts, err := enterprises.GetMemberContextsByUserIDs(ctx, []int64{m.UserID})
	require.NoError(t, err)
	c := contexts[m.UserID]
	require.True(t, c.GroupPoolConfigured)
	require.True(t, c.GroupWhitelistConfigured)
	require.Empty(t, c.GroupPoolIDs)
	require.Empty(t, c.GroupWhitelistIDs)
	require.False(t, c.AllowsGroup(groupID+1))
	e, err = enterprises.GetByID(ctx, e.ID)
	require.NoError(t, err)
	require.True(t, e.GroupPoolConfigured)
	w, err = groups.GetWhitelist(ctx, e.ID, service.EnterpriseScopeMember, m.ID)
	require.NoError(t, err)
	require.NotNil(t, w)
	require.Empty(t, w.GroupIDs)
	listed, err := groups.ListWhitelists(ctx, e.ID)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Empty(t, listed[0].GroupIDs)
	// Empty PUT cancels the layer, unlike automatic deletion cleanup.
	require.NoError(t, groups.SetAllowedGroupIDs(ctx, e.ID, []int64{}))
	require.NoError(t, groups.SetWhitelist(ctx, e.ID, service.EnterpriseScopeMember, m.ID, []int64{}))
	c, err = enterprises.GetMemberContextByUserID(ctx, m.UserID)
	require.NoError(t, err)
	require.False(t, c.GroupPoolConfigured)
	require.False(t, c.GroupWhitelistConfigured)
	require.True(t, c.AllowsGroup(groupID+1))
	w, err = groups.GetWhitelist(ctx, e.ID, service.EnterpriseScopeMember, m.ID)
	require.NoError(t, err)
	require.Nil(t, w)
}

func TestEnterpriseGroupConfigurationMigrationBackfillsExistingRows(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	e := enterpriseFixture(t, ctx)
	id := enterpriseGroupFixture(t, tx.Client())
	_, err := tx.ExecContext(ctx, `INSERT INTO enterprise_allowed_groups (enterprise_id, group_id) VALUES ($1,$2)`, e.ID, id)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `INSERT INTO enterprise_group_whitelists (enterprise_id, scope_type, scope_id, group_id) VALUES ($1,'enterprise',$1,$2)`, e.ID, id)
	require.NoError(t, err)
	content, err := migrations.FS.ReadFile("249_enterprise_group_configuration.sql")
	require.NoError(t, err)
	// Replay against a transaction-local legacy configuration to exercise backfill
	// and idempotence without mutating other tests' committed enterprise records.
	for range 2 {
		_, err = tx.ExecContext(ctx, string(content))
		require.NoError(t, err)
	}
	loaded, err := NewEnterpriseRepository(testEntClient(t)).GetByID(ctx, e.ID)
	require.NoError(t, err)
	require.True(t, loaded.GroupPoolConfigured)
	whitelist, err := NewEnterpriseGroupRepository(testEntClient(t)).GetWhitelist(ctx, e.ID, service.EnterpriseScopeEnterprise, e.ID)
	require.NoError(t, err)
	require.NotNil(t, whitelist)
	require.Equal(t, []int64{id}, whitelist.GroupIDs)
}

func TestEnterpriseActiveOwnersExcludeUnavailableUsers(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	client := testEntClient(t)
	e := enterpriseFixture(t, ctx)
	members := NewEnterpriseMemberRepository(client)
	first := enterpriseMemberFixture(t, ctx, e.ID, nil)
	second := enterpriseMemberFixture(t, ctx, e.ID, nil)
	first.Role, second.Role = service.EnterpriseRoleOwner, service.EnterpriseRoleOwner
	require.NoError(t, members.Update(ctx, first))
	require.NoError(t, members.Update(ctx, second))
	count, err := members.CountActiveOwners(ctx, e.ID)
	require.NoError(t, err)
	require.EqualValues(t, 2, count)
	_, err = tx.ExecContext(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, first.UserID)
	require.NoError(t, err)
	count, err = members.CountActiveOwners(ctx, e.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	_, err = tx.ExecContext(ctx, `UPDATE users SET deleted_at=NOW() WHERE id=$1`, second.UserID)
	require.NoError(t, err)
	count, err = members.CountActiveOwners(ctx, e.ID)
	require.NoError(t, err)
	require.Zero(t, count)
}

// These tests deliberately commit because two independent transactions must see
// the fixture. Clean every committed row before the next repository suite runs.
func enterpriseReviewCleanupCommitted(t *testing.T, enterpriseID int64, userIDs ...int64) {
	t.Helper()
	t.Cleanup(func() {
		for _, table := range []string{"enterprise_allowed_groups", "enterprise_group_whitelists", "enterprise_group_whitelist_scopes", "enterprise_quotas", "enterprise_balance_logs", "enterprise_audit_logs", "enterprise_members", "enterprise_departments"} {
			_, err := integrationDB.ExecContext(context.Background(), "DELETE FROM "+table+" WHERE enterprise_id=$1", enterpriseID)
			require.NoError(t, err)
		}
		_, err := integrationDB.ExecContext(context.Background(), "DELETE FROM enterprises WHERE id=$1", enterpriseID)
		require.NoError(t, err)
		for _, id := range userIDs {
			_, err := integrationDB.ExecContext(context.Background(), "DELETE FROM users WHERE id=$1", id)
			require.NoError(t, err)
		}
	})
}

func enterpriseReviewBackendPID(t *testing.T, ctx context.Context, client *dbent.Client) int64 {
	t.Helper()
	var pid int64
	require.NoError(t, enterpriseQueryOne(ctx, client, `SELECT pg_backend_pid()`, nil, func(s enterpriseScanner) error { return s.Scan(&pid) }))
	return pid
}

func TestEnterpriseGroupDeleteAndReferenceReplacementDoNotDeadlock(t *testing.T) {
	for _, kind := range []string{"pool", "whitelist"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			client := testEntClient(t)
			e := enterpriseFixture(t, ctx)
			enterpriseReviewCleanupCommitted(t, e.ID)
			oldID := enterpriseGroupFixture(t, client)
			newID := enterpriseGroupFixture(t, client)
			t.Cleanup(func() {
				_, err := integrationDB.ExecContext(context.Background(), `DELETE FROM scheduler_outbox WHERE group_id IN ($1,$2)`, oldID, newID)
				require.NoError(t, err)
			})
			groups := NewEnterpriseGroupRepository(client)
			set := func(ctx context.Context, ids []int64) error {
				if kind == "pool" {
					return groups.SetAllowedGroupIDs(ctx, e.ID, ids)
				}
				return groups.SetWhitelist(ctx, e.ID, service.EnterpriseScopeEnterprise, e.ID, ids)
			}
			require.NoError(t, set(ctx, []int64{oldID}))
			deleting := testEntTx(t)
			deletionCtx := dbent.NewTxContext(ctx, deleting)
			deletePID := enterpriseReviewBackendPID(t, ctx, deleting.Client())
			var lockedID int64
			require.NoError(t, enterpriseQueryOne(ctx, deleting.Client(), `SELECT id FROM groups WHERE id=$1 FOR UPDATE`, []any{oldID}, func(s enterpriseScanner) error { return s.Scan(&lockedID) }))
			rewriting := testEntTx(t)
			rewritePID := enterpriseReviewBackendPID(t, ctx, rewriting.Client())
			rewritten := make(chan error, 1)
			go func() {
				err := set(dbent.NewTxContext(ctx, rewriting), []int64{newID})
				if err == nil {
					err = rewriting.Commit()
				}
				rewritten <- err
			}()
			// Wait for the exact lock conflict, without relying on goroutine timing.
			require.Eventually(t, func() bool {
				var blocked bool
				err := integrationDB.QueryRowContext(ctx, `SELECT $1::integer = ANY(pg_blocking_pids($2::integer))`, deletePID, rewritePID).Scan(&blocked)
				return err == nil && blocked
			}, 3*time.Second, 10*time.Millisecond)
			// With the old order, rewriting already deleted the join and this cleanup
			// waits on it while rewriting waits on groups: PostgreSQL reports a deadlock.
			require.NoError(t, newGroupRepositoryWithSQL(deleting.Client(), deleting.Client()).Delete(deletionCtx, oldID))
			require.NoError(t, deleting.Commit())
			require.NoError(t, <-rewritten)
			if kind == "pool" {
				ids, err := groups.GetAllowedGroupIDs(ctx, e.ID)
				require.NoError(t, err)
				require.Equal(t, []int64{newID}, ids)
			} else {
				w, err := groups.GetWhitelist(ctx, e.ID, service.EnterpriseScopeEnterprise, e.ID)
				require.NoError(t, err)
				require.Equal(t, []int64{newID}, w.GroupIDs)
			}
		})
	}
}

func TestEnterpriseMemberCreateRechecksLockedUserRole(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := testEntClient(t)
	e := enterpriseFixture(t, ctx)
	user := mustCreateUser(t, client, &service.User{Email: "enterprise-review-admin-" + time.Now().Format("150405.000000000") + "@example.com"})
	enterpriseReviewCleanupCommitted(t, e.ID, user.ID)
	roleChange := testEntTx(t)
	rolePID := enterpriseReviewBackendPID(t, ctx, roleChange.Client())
	_, err := roleChange.ExecContext(ctx, `UPDATE users SET role='admin' WHERE id=$1`, user.ID)
	require.NoError(t, err)
	adding := testEntTx(t)
	addingPID := enterpriseReviewBackendPID(t, ctx, adding.Client())
	added := make(chan error, 1)
	go func() {
		added <- NewEnterpriseMemberRepository(client).Create(dbent.NewTxContext(ctx, adding), &service.EnterpriseMember{EnterpriseID: e.ID, UserID: user.ID})
	}()
	require.Eventually(t, func() bool {
		var blocked bool
		err := integrationDB.QueryRowContext(ctx, `SELECT $1::integer = ANY(pg_blocking_pids($2::integer))`, rolePID, addingPID).Scan(&blocked)
		return err == nil && blocked
	}, 3*time.Second, 10*time.Millisecond)
	require.NoError(t, roleChange.Commit())
	require.ErrorIs(t, <-added, service.ErrEnterpriseMemberAdminForbidden)
	require.NoError(t, adding.Rollback())
	c, err := NewEnterpriseRepository(client).GetMemberContextByUserID(ctx, user.ID)
	require.NoError(t, err)
	require.Nil(t, c)
}
