//go:build unit

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// Both enterprise and personal wallet balances are deliberately insufficient
// for wallet billing. A valid subscription must remain the sole payer while
// the enterprise identity is retained for attribution and frozen multipliers.
func videoSecuritySubscriptionFixture(t *testing.T, db *sql.DB, limit float64) *service.VideoBillingReservation {
	t.Helper()
	v := videoFixture(t, db, 0, .6)
	require.NoError(t, db.QueryRow(`INSERT INTO enterprises(name,balance) VALUES($1,0) RETURNING id`, v.ID).Scan(&v.EnterpriseID))
	require.NoError(t, db.QueryRow(`INSERT INTO enterprise_departments(enterprise_id,name) VALUES($1,'subscription') RETURNING id`, v.EnterpriseID).Scan(&v.EnterpriseDepartmentID))
	require.NoError(t, db.QueryRow(`INSERT INTO enterprise_members(enterprise_id,user_id,department_id) VALUES($1,$2,$3) RETURNING id`, v.EnterpriseID, v.UserID, v.EnterpriseDepartmentID).Scan(&v.EnterpriseMemberID))
	v.EnterpriseDepartmentPathIDs = []int64{v.EnterpriseDepartmentID}
	for _, scope := range []struct {
		name string
		id   int64
	}{{"enterprise", v.EnterpriseID}, {"department", v.EnterpriseDepartmentID}, {"member", v.EnterpriseMemberID}} {
		videoExec(t, db, `INSERT INTO enterprise_quotas(enterprise_id,scope_type,scope_id,daily_limit_usd,weekly_limit_usd,monthly_limit_usd,total_limit_usd)
 VALUES($1,$2,$3,0,0,0,0)`, v.EnterpriseID, scope.name, scope.id)
	}
	videoExec(t, db, `INSERT INTO user_platform_quotas(user_id,platform,daily_limit_usd,weekly_limit_usd,monthly_limit_usd) VALUES($1,'video',0,0,0)`, v.UserID)
	videoExec(t, db, `UPDATE groups SET subscription_type='subscription',daily_limit_usd=$2,weekly_limit_usd=$2,monthly_limit_usd=$2 WHERE id=$1`, v.GroupID, limit)
	var subID int64
	require.NoError(t, db.QueryRow(`INSERT INTO user_subscriptions(user_id,group_id,starts_at,expires_at) VALUES($1,$2,NOW()-INTERVAL '1 minute',NOW()+INTERVAL '30 days') RETURNING id`, v.UserID, v.GroupID).Scan(&subID))
	v.SubscriptionID = &subID
	return v
}

func videoSecurityNonSubscriptionEffects(t *testing.T, db *sql.DB, v *service.VideoBillingReservation) {
	t.Helper()
	var balance, frozen float64
	for _, payer := range []struct {
		table string
		id    int64
	}{{"users", v.UserID}, {"enterprises", v.EnterpriseID}} {
		require.NoError(t, db.QueryRow(`SELECT balance,frozen_balance FROM `+payer.table+` WHERE id=$1`, payer.id).Scan(&balance, &frozen))
		require.Zero(t, balance, "%s wallet must not pay for subscription tasks", payer.table)
		require.Zero(t, frozen, "%s wallet must not freeze subscription funds", payer.table)
	}
	var enterpriseUsage, platformUsage float64
	require.NoError(t, db.QueryRow(`SELECT SUM(daily_usage_usd+weekly_usage_usd+monthly_usage_usd+total_usage_usd) FROM enterprise_quotas WHERE enterprise_id=$1`, v.EnterpriseID).Scan(&enterpriseUsage))
	require.Zero(t, enterpriseUsage, "company, department, and member quotas are wallet-only")
	require.NoError(t, db.QueryRow(`SELECT daily_usage_usd+weekly_usage_usd+monthly_usage_usd FROM user_platform_quotas WHERE user_id=$1 AND platform='video'`, v.UserID).Scan(&platformUsage))
	require.Zero(t, platformUsage, "platform wallet quota must not count a subscription charge")
}

func TestVideoBillingSecurityPostgresEnterpriseSubscriptionAdmissionAndSettlement(t *testing.T) {
	db := videoTestDB(t)
	ctx := context.Background()
	v := videoSecuritySubscriptionFixture(t, db, 1)
	repo := NewVideoBillingReservationRepository(db)
	require.NoError(t, repo.Reserve(ctx, v), "subscription admits despite zero personal/enterprise balances and zero enterprise/platform quotas")
	videoSecurityNonSubscriptionEffects(t, db, v)
	over := videoClone(v, v.ID+"-over")
	require.ErrorIs(t, repo.Reserve(ctx, over), service.ErrVideoReservationQuotaExceeded, "pending $0.60 plus another $0.60 exceeds the $1 subscription limit")
	var failedRows int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM video_billing_reservations WHERE id=$1`, over.ID).Scan(&failedRows))
	require.Zero(t, failedRows, "rejected reservation must roll back")
	require.NoError(t, repo.Accept(ctx, v.ID, "subscription-"+v.ID, v.AccountID))
	usage := videoUsage(v, .4)
	result, err := repo.Settle(ctx, &service.VideoReservationSettlement{ID: v.ID, Usage: usage})
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.InDelta(t, .4, result.ActualAmount, 1e-12)
	videoSecurityNonSubscriptionEffects(t, db, v)
	var daily, weekly, monthly float64
	require.NoError(t, db.QueryRow(`SELECT daily_usage_usd,weekly_usage_usd,monthly_usage_usd FROM user_subscriptions WHERE id=$1`, *v.SubscriptionID).Scan(&daily, &weekly, &monthly))
	for _, actual := range []float64{daily, weekly, monthly} {
		require.InDelta(t, .4, actual, 1e-12, "each subscription window must count actual cost once")
	}
	var logSubscription, logEnterprise, logDepartment int64
	require.NoError(t, db.QueryRow(`SELECT subscription_id,enterprise_id,enterprise_department_id FROM usage_logs WHERE request_id=$1 AND api_key_id=$2`, usage.RequestID, v.APIKeyID).Scan(&logSubscription, &logEnterprise, &logDepartment))
	require.Equal(t, *v.SubscriptionID, logSubscription)
	require.Equal(t, v.EnterpriseID, logEnterprise)
	require.Equal(t, v.EnterpriseDepartmentID, logDepartment)
	var keyTotal, key5h, key1d, key7d float64
	require.NoError(t, db.QueryRow(`SELECT quota_used,usage_5h,usage_1d,usage_7d FROM api_keys WHERE id=$1`, v.APIKeyID).Scan(&keyTotal, &key5h, &key1d, &key7d))
	for _, actual := range []float64{keyTotal, key5h, key1d, key7d} {
		require.InDelta(t, .4, actual, 1e-12, "key counters also count subscription consumption")
	}
	// The unspent hold is released: $0.40 actual + $0.60 new hold fits exactly.
	require.NoError(t, repo.Reserve(ctx, videoClone(v, v.ID+"-remaining")))
	result, err = repo.Settle(ctx, &service.VideoReservationSettlement{ID: v.ID, Usage: videoUsage(v, .4)})
	require.NoError(t, err)
	require.False(t, result.Applied, "re-observation cannot increment subscription twice")
	t.Log("zero wallets/enterprise/platform limits admit subscription; $0.60 outstanding hold blocks another $0.60; $0.40 actual increments all subscription/key windows once, retains both identities, and leaves wallets/company/platform quotas untouched")
}

func TestVideoBillingSecurityPostgresEnterpriseSubscriptionDisabledAdmissionRetainsObligation(t *testing.T) {
	db := videoTestDB(t)
	ctx := context.Background()
	for _, scope := range []struct {
		name  string
		table string
		err   error
	}{
		{"enterprise", "enterprises", service.ErrEnterpriseGatewayDisabled},
		{"member", "enterprise_members", service.ErrEnterpriseGatewayMemberDisabled},
	} {
		t.Run(scope.name, func(t *testing.T) {
			v := videoSecuritySubscriptionFixture(t, db, 10)
			repo := NewVideoBillingReservationRepository(db)
			require.NoError(t, repo.Reserve(ctx, v))
			require.NoError(t, repo.Accept(ctx, v.ID, "disabled-after-accept-"+v.ID, v.AccountID))
			id := v.EnterpriseID
			if scope.name == "member" {
				id = v.EnterpriseMemberID
			}
			videoExec(t, db, `UPDATE `+scope.table+` SET status='disabled' WHERE id=$1`, id)
			require.ErrorIs(t, repo.Reserve(ctx, videoClone(v, v.ID+"-disabled")), scope.err)
			result, err := repo.Settle(ctx, &service.VideoReservationSettlement{ID: v.ID, Usage: videoUsage(v, .4)})
			require.NoError(t, err, "disabling cannot erase an accepted payment obligation")
			require.True(t, result.Applied)
			var daily, weekly, monthly float64
			require.NoError(t, db.QueryRow(`SELECT daily_usage_usd,weekly_usage_usd,monthly_usage_usd FROM user_subscriptions WHERE id=$1`, *v.SubscriptionID).Scan(&daily, &weekly, &monthly))
			for _, actual := range []float64{daily, weekly, monthly} {
				require.InDelta(t, .4, actual, 1e-12)
			}
			videoSecurityNonSubscriptionEffects(t, db, v)
		})
	}
}

func TestVideoBillingSecurityPostgresEnterpriseSubscriptionKeyLimits(t *testing.T) {
	db := videoTestDB(t)
	ctx := context.Background()
	for _, keyLimit := range []struct {
		name   string
		column string
		err    error
	}{
		{"total", "quota", service.ErrVideoReservationQuotaExceeded},
		{"5h", "rate_limit_5h", service.ErrAPIKeyRateLimit5hExceeded},
		{"1d", "rate_limit_1d", service.ErrAPIKeyRateLimit1dExceeded},
		{"7d", "rate_limit_7d", service.ErrAPIKeyRateLimit7dExceeded},
	} {
		t.Run(keyLimit.name, func(t *testing.T) {
			v := videoSecuritySubscriptionFixture(t, db, 10)
			repo := NewVideoBillingReservationRepository(db)
			videoExec(t, db, `UPDATE api_keys SET `+keyLimit.column+`=0.5 WHERE id=$1`, v.APIKeyID)
			require.ErrorIs(t, repo.Reserve(ctx, videoClone(v, v.ID+"-too-large")), keyLimit.err)
			videoExec(t, db, `UPDATE api_keys SET `+keyLimit.column+`=1 WHERE id=$1`, v.APIKeyID)
			require.NoError(t, repo.Reserve(ctx, v))
			require.ErrorIs(t, repo.Reserve(ctx, videoClone(v, v.ID+"-pending")), keyLimit.err, "subscription must not bypass pending key holds")
			_, err := repo.Settle(ctx, &service.VideoReservationSettlement{ID: v.ID, Usage: videoUsage(v, .4)})
			require.NoError(t, err)
			require.NoError(t, repo.Reserve(ctx, videoClone(v, v.ID+"-exact")), "actual $0.40 plus held $0.60 fits the key limit")
			videoSecurityNonSubscriptionEffects(t, db, v)
		})
	}
}

func TestVideoBillingSecurityPostgresOriginFirstWriterWins(t *testing.T) {
	db := videoTestDB(t)
	ctx := context.Background()
	v := videoFixture(t, db, 1, .6)
	v.Snapshot = json.RawMessage(`{"model":"retained-model","nested":{"payer":17},"enabled":true,"counts":[1,2]}`)
	repo := NewVideoBillingReservationRepository(db)
	require.NoError(t, repo.Reserve(ctx, v))
	binder, ok := repo.(service.VideoBillingUpstreamOriginRepository)
	require.True(t, ok, "production repository must persist origin freezing")
	origins := []string{"https://upstream-a.example", "http://relay.internal:8080"}
	type binding struct {
		origin string
		err    error
	}
	start := make(chan struct{})
	results := make(chan binding, 20)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			origin, err := binder.BindUpstreamOrigin(ctx, v.ID, origins[i%len(origins)])
			results <- binding{origin, err}
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	winner := ""
	for result := range results {
		require.NoError(t, result.err)
		require.Contains(t, origins, result.origin)
		if winner == "" {
			winner = result.origin
		}
		require.Equal(t, winner, result.origin, "all simultaneous callers must observe the single frozen origin")
	}
	for _, candidate := range origins {
		bound, err := binder.BindUpstreamOrigin(ctx, v.ID, candidate)
		require.NoError(t, err)
		require.Equal(t, winner, bound, "an existing frozen origin cannot be overwritten")
	}
	_, err := binder.BindUpstreamOrigin(ctx, v.ID, "https://key@other.example/private")
	require.ErrorIs(t, err, service.ErrVideoReservationInvalid)
	stored, err := repo.Get(ctx, v.ID)
	require.NoError(t, err)
	var snapshot, expected map[string]any
	require.NoError(t, json.Unmarshal(stored.Snapshot, &snapshot))
	require.Equal(t, winner, snapshot["upstream_origin"])
	delete(snapshot, "upstream_origin")
	require.NoError(t, json.Unmarshal(v.Snapshot, &expected))
	require.Equal(t, expected, snapshot, "JSON origin binding must retain all other snapshot fields")
	t.Logf("20 simultaneous legacy origin binds select one immutable source (%s), preserving all pre-existing snapshot data", winner)
}

func TestVideoBillingSecurityPostgresCountActiveReservations(t *testing.T) {
	db := videoTestDB(t)
	ctx := context.Background()
	v := videoFixture(t, db, 100, .6)
	repo := NewVideoBillingReservationRepository(db)
	accountRepo := newAccountRepositoryWithSQL(nil, db, nil)
	count, err := accountRepo.CountActiveVideoBillingReservations(ctx, v.AccountID)
	require.NoError(t, err)
	require.Zero(t, count)
	for _, state := range []string{service.VideoReservationPreparing, service.VideoReservationAccepted, service.VideoReservationManual, service.VideoReservationSettled, service.VideoReservationReleased} {
		row := videoClone(v, fmt.Sprintf("%s-%s", v.ID, state))
		require.NoError(t, repo.Reserve(ctx, row))
		switch state {
		case service.VideoReservationAccepted:
			require.NoError(t, repo.Accept(ctx, row.ID, "count-"+row.ID, row.AccountID))
		case service.VideoReservationManual:
			require.NoError(t, repo.MarkManual(ctx, row.ID, "ambiguous upstream outcome"))
		case service.VideoReservationSettled:
			_, err := repo.Settle(ctx, &service.VideoReservationSettlement{ID: row.ID, Usage: videoUsage(row, .4)})
			require.NoError(t, err)
		case service.VideoReservationReleased:
			_, err := repo.Release(ctx, row.ID, "provider rejected")
			require.NoError(t, err)
		}
	}
	other := videoFixture(t, db, 100, .6)
	require.NoError(t, repo.Reserve(ctx, other))
	count, err = accountRepo.CountActiveVideoBillingReservations(ctx, v.AccountID)
	require.NoError(t, err)
	require.EqualValues(t, 3, count, "only preparing, accepted, and manual count; other accounts and terminal rows do not")
	for _, state := range []string{service.VideoReservationPreparing, service.VideoReservationAccepted, service.VideoReservationManual} {
		_, err := repo.Release(ctx, fmt.Sprintf("%s-%s", v.ID, state), "confirmed no remaining obligation")
		require.NoError(t, err)
	}
	count, err = accountRepo.CountActiveVideoBillingReservations(ctx, v.AccountID)
	require.NoError(t, err)
	require.Zero(t, count, "terminalizing the last active reservations removes the warning condition")
	count, err = accountRepo.CountActiveVideoBillingReservations(ctx, other.AccountID)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
}
