//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestEnterpriseUsageOptionsVisibilitySearchAndHistory(t *testing.T) {
	// testEntTx registers t.Cleanup to roll back every fixture and mutation.
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)
	svc := service.NewEnterpriseUsageService(repo)
	e, other := enterpriseFixture(t, ctx), enterpriseFixture(t, ctx)
	dr := NewEnterpriseDepartmentRepository(client)
	a := &service.EnterpriseDepartment{EnterpriseID: e.ID, Name: "A"}
	b := &service.EnterpriseDepartment{EnterpriseID: e.ID, Name: "B"}
	require.NoError(t, dr.Create(ctx, a))
	require.NoError(t, dr.Create(ctx, b))
	ma := enterpriseMemberFixture(t, ctx, e.ID, &a.ID)
	mb := enterpriseMemberFixture(t, ctx, e.ID, &b.ID)
	peer := enterpriseMemberFixture(t, ctx, e.ID, &a.ID)
	foreign := enterpriseMemberFixture(t, ctx, other.ID, nil)
	keys := make(map[int64]*service.APIKey)
	for _, m := range []*service.EnterpriseMember{ma, mb, peer, foreign} {
		keys[m.UserID] = mustCreateApiKey(t, client, &service.APIKey{UserID: m.UserID, Key: fmt.Sprintf("sk-options-secret-%d", m.UserID), Name: fmt.Sprintf("key-%d", m.UserID)})
	}
	_, err := tx.ExecContext(ctx, `UPDATE users SET username='Alice_One', email='alice.options@example.com' WHERE id=$1`, ma.UserID)
	require.NoError(t, err)
	deleted := mustCreateApiKey(t, client, &service.APIKey{UserID: ma.UserID, Key: "sk-options-deleted-secret", Name: "deleted-key"})
	_, err = tx.ExecContext(ctx, `UPDATE api_keys SET deleted_at=NOW() WHERE id=$1`, deleted.ID)
	require.NoError(t, err)
	account := mustCreateAccount(t, client, &service.Account{Name: "private-options-upstream"})
	now := time.Now().UTC()
	for i, v := range []struct {
		enterprise, department, user int64
		model, requested             string
		at                           time.Time
	}{
		{e.ID, a.ID, ma.UserID, "private-billing", "public-a", now},
		{e.ID, a.ID, ma.UserID, "private-billing", "public-a", now},
		{e.ID, a.ID, peer.UserID, "public-legacy", "", now},
		{e.ID, b.ID, mb.UserID, "private-billing", "public-b", now},
		{other.ID, 0, foreign.UserID, "foreign-only", "foreign-only", now},
		{other.ID, 0, ma.UserID, "cross-enterprise", "cross-enterprise", now},
		{0, 0, ma.UserID, "personal-only", "personal-only", now},
		{e.ID, a.ID, ma.UserID, "old-only", "old-only", now.AddDate(0, 0, -100)},
	} {
		log := &service.UsageLog{UserID: v.user, APIKeyID: keys[v.user].ID, AccountID: account.ID, Model: v.model, RequestedModel: v.requested, RequestID: fmt.Sprintf("options-%d", i), InputTokens: 2, OutputTokens: 3, CacheCreationTokens: 5, CacheReadTokens: 7, TotalCost: 4, ActualCost: 2, CreatedAt: v.at}
		if v.enterprise != 0 {
			log.EnterpriseID = &v.enterprise
		}
		if v.department != 0 {
			log.EnterpriseDepartmentID = &v.department
		}
		_, err := repo.Create(ctx, log)
		require.NoError(t, err)
	}
	// Poison global aggregates; all enterprise charts and options must use raw logs.
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	_, err = tx.ExecContext(ctx, `INSERT INTO usage_dashboard_daily(bucket_date,total_requests,actual_cost) VALUES($1,99999,99999) ON CONFLICT(bucket_date) DO UPDATE SET total_requests=99999,actual_cost=99999`, day)
	require.NoError(t, err)
	for _, tc := range []struct {
		role     string
		keyUsers []int64
		models   []service.EnterpriseUsageModelOption
	}{
		{"", []int64{ma.UserID, mb.UserID, peer.UserID}, []service.EnterpriseUsageModelOption{{Model: "public-a", Requests: 2}, {Model: "public-b", Requests: 1}, {Model: "public-legacy", Requests: 1}}},
		{service.EnterpriseRoleOwner, []int64{ma.UserID, mb.UserID, peer.UserID}, []service.EnterpriseUsageModelOption{{Model: "public-a", Requests: 2}, {Model: "public-b", Requests: 1}, {Model: "public-legacy", Requests: 1}}},
		{service.EnterpriseRoleManager, []int64{ma.UserID, peer.UserID}, []service.EnterpriseUsageModelOption{{Model: "public-a", Requests: 2}, {Model: "public-legacy", Requests: 1}}},
		{service.EnterpriseRoleMember, []int64{ma.UserID}, []service.EnterpriseUsageModelOption{{Model: "public-a", Requests: 2}}},
	} {
		t.Run("role="+tc.role, func(t *testing.T) {
			scope := service.EnterpriseUsageScope{EnterpriseID: e.ID, UserID: ma.UserID}
			if tc.role != "" {
				scope.Member = &service.EnterpriseMemberContext{EnterpriseID: e.ID, Role: tc.role, DepartmentID: &a.ID, MemberStatus: service.EnterpriseMemberStatusActive}
			}
			options, err := svc.APIKeyOptions(ctx, scope, usagestats.UsageLogFilters{}, "", 20)
			require.NoError(t, err)
			var users []int64
			for _, option := range options {
				users = append(users, option.User.ID)
			}
			require.ElementsMatch(t, tc.keyUsers, users)
			data, err := json.Marshal(options)
			require.NoError(t, err)
			require.NotContains(t, string(data), "sk-options")
			require.NotContains(t, string(data), `"key":`)
			models, err := svc.ModelOptions(ctx, scope, usagestats.UsageLogFilters{}, "", 20)
			require.NoError(t, err)
			require.Equal(t, tc.models, models)
		})
	}
	owner := service.EnterpriseUsageScope{EnterpriseID: e.ID}
	for _, term := range []string{keys[ma.UserID].Name, "alice_one", "ALICE.OPTIONS@", "_One"} {
		options, err := svc.APIKeyOptions(ctx, owner, usagestats.UsageLogFilters{}, term, 20)
		require.NoError(t, err)
		require.Len(t, options, 1)
		require.Equal(t, ma.UserID, options[0].User.ID)
	}
	for _, term := range []string{"%", "foreign", "deleted-key", "no-match"} {
		options, err := svc.APIKeyOptions(ctx, owner, usagestats.UsageLogFilters{}, term, 20)
		require.NoError(t, err)
		require.Empty(t, options)
	}
	models, err := svc.ModelOptions(ctx, owner, usagestats.UsageLogFilters{}, "PUBLIC-A", 1)
	require.NoError(t, err)
	require.Equal(t, []service.EnterpriseUsageModelOption{{Model: "public-a", Requests: 2}}, models)
	for _, term := range []string{"%", "private-billing", "foreign-only", "no-match"} {
		models, err := svc.ModelOptions(ctx, owner, usagestats.UsageLogFilters{}, term, 20)
		require.NoError(t, err)
		require.Empty(t, models)
	}
	start := now.AddDate(0, 0, -110)
	models, err = svc.ModelOptions(ctx, owner, usagestats.UsageLogFilters{StartTime: &start}, "old-only", 20)
	require.NoError(t, err)
	require.Equal(t, []service.EnterpriseUsageModelOption{{Model: "old-only", Requests: 1}}, models)
	// Move one user and remove another: only Key options track current membership.
	ma.DepartmentID = &b.ID
	mr := NewEnterpriseMemberRepository(client)
	require.NoError(t, mr.Update(ctx, ma))
	require.NoError(t, mr.Delete(ctx, e.ID, peer.ID))
	manager := service.EnterpriseUsageScope{EnterpriseID: e.ID, UserID: ma.UserID, Member: &service.EnterpriseMemberContext{EnterpriseID: e.ID, Role: service.EnterpriseRoleManager, DepartmentID: &a.ID, MemberStatus: service.EnterpriseMemberStatusActive}}
	options, err := svc.APIKeyOptions(ctx, manager, usagestats.UsageLogFilters{}, "", 20)
	require.NoError(t, err)
	require.Empty(t, options)
	models, err = svc.ModelOptions(ctx, manager, usagestats.UsageLogFilters{}, "", 20)
	require.NoError(t, err)
	require.Equal(t, []service.EnterpriseUsageModelOption{{Model: "public-a", Requests: 2}, {Model: "public-legacy", Requests: 1}}, models)
	// Historical Key IDs still filter charts even after membership changes.
	for _, granularity := range []string{"day", "hour"} {
		trend, err := svc.Trend(ctx, manager, usagestats.UsageLogFilters{APIKeyID: keys[ma.UserID].ID}, granularity)
		require.NoError(t, err)
		require.Len(t, trend, 1)
		require.EqualValues(t, 2, trend[0].Requests)
		require.EqualValues(t, 4, trend[0].InputTokens)
		require.EqualValues(t, 6, trend[0].OutputTokens)
		require.EqualValues(t, 10, trend[0].CacheCreationTokens)
		require.EqualValues(t, 14, trend[0].CacheReadTokens)
		require.EqualValues(t, 34, trend[0].TotalTokens)
		require.Equal(t, 8.0, trend[0].Cost)
		require.Equal(t, 4.0, trend[0].ActualCost)
	}
	// A member context cannot be reused against another enterprise.
	manager.EnterpriseID = other.ID
	_, err = svc.APIKeyOptions(ctx, manager, usagestats.UsageLogFilters{}, "", 20)
	require.ErrorIs(t, err, service.ErrEnterprisePermissionDenied)
	_, err = svc.ModelOptions(ctx, manager, usagestats.UsageLogFilters{}, "", 20)
	require.ErrorIs(t, err, service.ErrEnterprisePermissionDenied)
}

func TestEnterpriseUsageOptionsLimitBounds(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)
	svc := service.NewEnterpriseUsageService(repo)
	e := enterpriseFixture(t, ctx)
	m := enterpriseMemberFixture(t, ctx, e.ID, nil)
	account := mustCreateAccount(t, client, &service.Account{Name: "options-limit"})
	_, err := tx.ExecContext(ctx, `INSERT INTO api_keys(user_id,key,name) SELECT $1::bigint,'sk-option-limit-' || ($1::bigint)::text || '-' || n::text,'option-' || n::text FROM generate_series(1,105) n`, m.UserID)
	require.NoError(t, err)
	var keyID int64
	require.NoError(t, scanSingleRow(ctx, tx, `SELECT MIN(id) FROM api_keys WHERE user_id=$1`, []any{m.UserID}, &keyID))
	_, err = tx.ExecContext(ctx, `INSERT INTO usage_logs(user_id,api_key_id,account_id,enterprise_id,model,requested_model,created_at)
 SELECT $1,$2,$3,$4,'model-' || n::text,'model-' || n::text,NOW() FROM generate_series(1,105) n`, m.UserID, keyID, account.ID, e.ID)
	require.NoError(t, err)
	for _, tc := range []struct{ limit, want int }{{0, 20}, {1, 1}, {100, 100}, {101, 100}, {999, 100}} {
		options, err := svc.APIKeyOptions(ctx, service.EnterpriseUsageScope{EnterpriseID: e.ID}, usagestats.UsageLogFilters{}, "", tc.limit)
		require.NoError(t, err)
		require.Len(t, options, tc.want)
		models, err := svc.ModelOptions(ctx, service.EnterpriseUsageScope{EnterpriseID: e.ID}, usagestats.UsageLogFilters{}, "", tc.limit)
		require.NoError(t, err)
		require.Len(t, models, tc.want)
	}
}
