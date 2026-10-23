//go:build unit

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// VIDEO_BILLING_TEST_DSN opts into a dedicated PostgreSQL database. These
// tests apply the actual migration chain; they never use the application's DSN.
var videoMigrateOnce sync.Once
var videoMigrateErr error

func videoTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("VIDEO_BILLING_TEST_DSN")
	if dsn == "" {
		t.Skip("set VIDEO_BILLING_TEST_DSN to a dedicated PostgreSQL test database")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	videoMigrateOnce.Do(func() { videoMigrateErr = ApplyMigrations(context.Background(), db) })
	if videoMigrateErr != nil {
		t.Fatal(videoMigrateErr)
	}
	return db
}

func videoExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func videoFixture(t *testing.T, db *sql.DB, balance, hold float64) *service.VideoBillingReservation {
	t.Helper()
	unique := fmt.Sprint(time.Now().UnixNano())
	v := &service.VideoBillingReservation{ID: "video-test-" + unique, Platform: "video", Protocol: "seedance", HoldAmount: hold,
		Contract: json.RawMessage(`{"mode":"video","unit_price":0.1,"multiplier":1}`), Snapshot: json.RawMessage(`{"model":"test"}`)}
	for _, entry := range []struct {
		query string
		args  []any
		out   *int64
	}{
		{`INSERT INTO users(email,password_hash,balance) VALUES($1,'test',$2) RETURNING id`, []any{unique + "@video.test", balance}, &v.UserID},
		{`INSERT INTO groups(name,platform) VALUES($1,'video') RETURNING id`, []any{"video-test-" + unique}, &v.GroupID},
		{`INSERT INTO accounts(name,platform,type) VALUES($1,'video','apikey') RETURNING id`, []any{"video-test-" + unique}, &v.AccountID},
	} {
		if err := db.QueryRow(entry.query, entry.args...).Scan(entry.out); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.QueryRow(`INSERT INTO api_keys(user_id,key,name,group_id) VALUES($1,$2,'video-test',$3) RETURNING id`, v.UserID, "sk-video-"+unique, v.GroupID).Scan(&v.APIKeyID); err != nil {
		t.Fatal(err)
	}
	return v
}

func videoClone(v *service.VideoBillingReservation, id string) *service.VideoBillingReservation {
	copy := *v
	copy.ID = id
	return &copy
}

func videoUsage(v *service.VideoBillingReservation, cost float64) *service.UsageLog {
	mode, res, duration := "video", "720p", 6
	return &service.UsageLog{UserID: v.UserID, APIKeyID: v.APIKeyID, AccountID: v.AccountID, GroupID: &v.GroupID,
		Model: "video-test", RequestID: "grok-video:" + v.ID, TotalCost: cost, ActualCost: cost, RateMultiplier: 1,
		VideoCount: 1, VideoResolution: &res, VideoDurationSeconds: &duration, BillingMode: &mode}
}

func videoWallet(t *testing.T, db *sql.DB, v *service.VideoBillingReservation, wantBalance, wantFrozen float64) {
	t.Helper()
	var balance, frozen float64
	table, id := "users", v.UserID
	if v.EnterpriseID > 0 {
		table, id = "enterprises", v.EnterpriseID
	}
	if err := db.QueryRow(`SELECT balance,frozen_balance FROM `+table+` WHERE id=$1`, id).Scan(&balance, &frozen); err != nil {
		t.Fatal(err)
	}
	if balance != wantBalance || frozen != wantFrozen {
		t.Fatalf("wallet balance=%f frozen=%f; want %f/%f", balance, frozen, wantBalance, wantFrozen)
	}
}

func TestVideoBillingReservationAmountChecks(t *testing.T) {
	if !videoExceeds(sql.NullFloat64{Valid: true, Float64: 1}, 0, .6, .6, true) {
		t.Fatal("pending tasks must consume quota")
	}
	if videoExceeds(sql.NullFloat64{Valid: true, Float64: 1}, 0, .4, .6, true) {
		t.Fatal("exactly covered task must fit")
	}
	if !videoExceeds(sql.NullFloat64{Valid: true}, 0, 0, 0, true) {
		t.Fatal("explicit zero disables quota")
	}
	if videoExceeds(sql.NullFloat64{}, 100, 100, 100, true) {
		t.Fatal("NULL quota is unlimited")
	}
	if validVideoAmount(-1) {
		t.Fatal("negative amount accepted")
	}
}

func TestVideoBillingReservationPostgresThreeSequentialSubmissions(t *testing.T) {
	db := videoTestDB(t)
	ctx := context.Background()
	v := videoFixture(t, db, 1, .6)
	repo := NewVideoBillingReservationRepository(db)
	if err := repo.Reserve(ctx, v); err != nil {
		t.Fatal(err)
	}
	if err := repo.Accept(ctx, v.ID, "seedance:sequential-"+v.ID, v.AccountID); err != nil {
		t.Fatal(err)
	}
	for i := 2; i <= 3; i++ {
		err := repo.Reserve(ctx, videoClone(v, fmt.Sprintf("%s-%d", v.ID, i)))
		if !errors.Is(err, service.ErrInsufficientBalance) {
			t.Fatalf("submission %d must be rejected before upstream: %v", i, err)
		}
		videoWallet(t, db, v, .4, .6)
	}
	t.Log("balance=$1.00; three $0.60 submissions: accepted, insufficient balance, insufficient balance; retained hold=$0.60")
}

func TestVideoBillingReservationPostgresConcurrentAndRestart(t *testing.T) {
	db := videoTestDB(t)
	ctx := context.Background()
	v := videoFixture(t, db, 1, .6)
	var success atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			repo := NewVideoBillingReservationRepository(db)
			err := repo.Reserve(ctx, videoClone(v, fmt.Sprintf("%s-%d", v.ID, i)))
			if err == nil {
				success.Add(1)
			} else if !errors.Is(err, service.ErrInsufficientBalance) {
				t.Errorf("unexpected reserve: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if success.Load() != 1 {
		t.Fatalf("accepted %d tasks, expected 1", success.Load())
	}
	videoWallet(t, db, v, .4, .6)
	// A fresh repository/connection pool sees holds after a process restart.
	restarted, err := sql.Open("postgres", os.Getenv("VIDEO_BILLING_TEST_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	err = NewVideoBillingReservationRepository(restarted).Reserve(ctx, videoClone(v, v.ID+"-restart"))
	if !errors.Is(err, service.ErrInsufficientBalance) {
		t.Fatalf("restart lost hold: %v", err)
	}
	t.Log("balance=1 accepted=1 of 12 concurrent $0.60 tasks; restart retains $0.60 hold")
}

func TestVideoBillingReservationPostgresCaptureAtomicLogCapAndDeletion(t *testing.T) {
	db := videoTestDB(t)
	ctx := context.Background()
	v := videoFixture(t, db, 1, .6)
	repo := NewVideoBillingReservationRepository(db)
	if err := repo.Reserve(ctx, v); err != nil {
		t.Fatal(err)
	}
	if err := repo.Reserve(ctx, v); err != nil {
		t.Fatal("idempotent reserve", err)
	}
	if err := repo.Accept(ctx, v.ID, "task-"+v.ID, v.AccountID); err != nil {
		t.Fatal(err)
	}
	if err := repo.BindFile(ctx, v.ID, "file-"+v.ID); err != nil {
		t.Fatal(err)
	}
	byFile, err := repo.GetByFile(ctx, v.Protocol, "file-"+v.ID)
	if err != nil || byFile.ID != v.ID {
		t.Fatal("file lookup", err)
	}
	videoExec(t, db, `UPDATE api_keys SET deleted_at=NOW(),group_id=NULL WHERE id=$1`, v.APIKeyID)
	videoExec(t, db, `UPDATE groups SET rate_multiplier=30,deleted_at=NOW() WHERE id=$1`, v.GroupID)
	// Force the usage insert to fail after all money effects. The entire
	// transaction, including its dedup key and frozen-balance capture, rolls back.
	bad := videoUsage(v, .4)
	bad.Model = strings.Repeat("x", 101)
	if _, err = repo.Settle(ctx, &service.VideoReservationSettlement{ID: v.ID, Usage: bad}); err == nil {
		t.Fatal("expected invalid log failure")
	}
	videoWallet(t, db, v, .4, .6)
	var claims int
	if err = db.QueryRow(`SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id=$1 AND api_key_id=$2`, bad.RequestID, v.APIKeyID).Scan(&claims); err != nil || claims != 0 {
		t.Fatalf("failed settlement retained claim: %d %v", claims, err)
	}
	log := videoUsage(v, 6)
	result, err := repo.Settle(ctx, &service.VideoReservationSettlement{ID: v.ID, Usage: log})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Applied || !result.Capped || result.ActualAmount != .6 {
		t.Fatalf("bad capped result %+v", result)
	}
	videoWallet(t, db, v, .4, 0)
	var logged float64
	if err = db.QueryRow(`SELECT actual_cost FROM usage_logs WHERE request_id=$1 AND api_key_id=$2`, log.RequestID, v.APIKeyID).Scan(&logged); err != nil || logged != .6 {
		t.Fatalf("log=%f err=%v", logged, err)
	}
	if result, err = repo.Settle(ctx, &service.VideoReservationSettlement{ID: v.ID, Usage: videoUsage(v, 6)}); err != nil || result.Applied {
		t.Fatalf("duplicate settle %+v %v", result, err)
	}
	if result, err = repo.Release(ctx, v.ID, "late cancel"); err != nil || result.Applied {
		t.Fatalf("capture/release race %+v %v", result, err)
	}
	videoWallet(t, db, v, .4, 0)
	t.Log("log failure rolls back all money; deleted key/group still settle; $6 capped to frozen $0.60; duplicate terminal calls are no-op")
}

func TestVideoBillingReservationPostgresConcurrentSettlement(t *testing.T) {
	db := videoTestDB(t)
	ctx := context.Background()
	v := videoFixture(t, db, 1, .6)
	repo := NewVideoBillingReservationRepository(db)
	if err := repo.Reserve(ctx, v); err != nil {
		t.Fatal(err)
	}
	if err := repo.Accept(ctx, v.ID, "task-"+v.ID, v.AccountID); err != nil {
		t.Fatal(err)
	}
	var applied atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := repo.Settle(ctx, &service.VideoReservationSettlement{ID: v.ID, Usage: videoUsage(v, .4)})
			if err != nil {
				t.Errorf("concurrent settle: %v", err)
				return
			}
			if result.Applied {
				applied.Add(1)
			}
		}()
	}
	wg.Wait()
	if applied.Load() != 1 {
		t.Fatalf("settled %d times", applied.Load())
	}
	videoWallet(t, db, v, .6, 0)
	var logs, claims int
	if err := db.QueryRow(`SELECT COUNT(*) FROM usage_logs WHERE request_id=$1 AND api_key_id=$2`, videoUsage(v, .4).RequestID, v.APIKeyID).Scan(&logs); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id=$1 AND api_key_id=$2`, videoUsage(v, .4).RequestID, v.APIKeyID).Scan(&claims); err != nil {
		t.Fatal(err)
	}
	if logs != 1 || claims != 1 {
		t.Fatalf("logs=%d claims=%d", logs, claims)
	}
	t.Log("20 concurrent observers capture exactly once; wallet, usage and dedup agree at $0.40")
}

func TestVideoBillingReservationPostgresReleaseLeaseAndManual(t *testing.T) {
	db := videoTestDB(t)
	ctx := context.Background()
	v := videoFixture(t, db, 1, .6)
	repo := NewVideoBillingReservationRepository(db)
	if err := repo.Reserve(ctx, v); err != nil {
		t.Fatal(err)
	}
	// Other tests leave obligations; constrain claims to this task's near-future
	// time by moving every other fixture out of the due window.
	videoExec(t, db, `UPDATE video_billing_reservations SET next_attempt_at=NOW()+INTERVAL '1 day' WHERE id<>$1`, v.ID)
	fresh, err := repo.ClaimDue(ctx, "early-worker", 1, time.Minute)
	if err != nil || len(fresh) != 0 {
		t.Fatalf("in-progress submission was claimed too early %+v %v", fresh, err)
	}
	videoExec(t, db, `UPDATE video_billing_reservations SET created_at=NOW()-INTERVAL '4 minutes' WHERE id=$1`, v.ID)
	first, err := repo.ClaimDue(ctx, "worker-one", 1, time.Minute)
	if err != nil || len(first) != 1 || first[0].ID != v.ID {
		t.Fatalf("claim %+v %v", first, err)
	}
	second, err := repo.ClaimDue(ctx, "worker-two", 1, time.Minute)
	if err != nil || len(second) != 0 {
		t.Fatalf("lease did not exclude second worker %+v %v", second, err)
	}
	if err := repo.Reschedule(ctx, v.ID, "worker-two", time.Now(), "wrong holder"); !errors.Is(err, service.ErrVideoReservationConflict) {
		t.Fatal("wrong holder rescheduled", err)
	}
	if err := repo.Reschedule(ctx, v.ID, "worker-one", time.Now().Add(-time.Second), "retry"); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkManual(ctx, v.ID, "unknown create outcome"); err != nil {
		t.Fatal(err)
	}
	videoWallet(t, db, v, .4, .6)
	second, err = repo.ClaimDue(ctx, "worker-two", 1, time.Minute)
	if err != nil || len(second) != 0 {
		t.Fatal("manual task should not auto expire", err)
	}
	result, err := repo.Release(ctx, v.ID, "confirmed failed")
	if err != nil || !result.Applied {
		t.Fatal(err)
	}
	videoWallet(t, db, v, 1, 0)
	result, err = repo.Release(ctx, v.ID, "duplicate")
	if err != nil || result.Applied {
		t.Fatal(err)
	}
	videoWallet(t, db, v, 1, 0)
}

func TestVideoBillingReservationPostgresQuotaHoldsAndFloor(t *testing.T) {
	db := videoTestDB(t)
	ctx := context.Background()
	for _, dimension := range []string{"key_total", "key_5h", "key_1d", "key_7d", "platform_daily", "platform_weekly", "platform_monthly"} {
		t.Run(dimension, func(t *testing.T) {
			v := videoFixture(t, db, 10, .6)
			repo := NewVideoBillingReservationRepository(db)
			switch dimension {
			case "key_total":
				videoExec(t, db, `UPDATE api_keys SET quota=1 WHERE id=$1`, v.APIKeyID)
			case "key_5h":
				videoExec(t, db, `UPDATE api_keys SET rate_limit_5h=1 WHERE id=$1`, v.APIKeyID)
			case "key_1d":
				videoExec(t, db, `UPDATE api_keys SET rate_limit_1d=1 WHERE id=$1`, v.APIKeyID)
			case "key_7d":
				videoExec(t, db, `UPDATE api_keys SET rate_limit_7d=1 WHERE id=$1`, v.APIKeyID)
			default:
				window := strings.TrimPrefix(dimension, "platform_")
				videoExec(t, db, `INSERT INTO user_platform_quotas(user_id,platform,`+window+`_limit_usd) VALUES($1,'video',1)`, v.UserID)
			}
			if err := repo.Reserve(ctx, v); err != nil {
				t.Fatal(err)
			}
			if err := repo.Reserve(ctx, videoClone(v, v.ID+"-over")); err == nil {
				t.Fatal("pending hold did not consume quota")
			}
			videoWallet(t, db, v, 9.4, .6)
		})
	}
	v := videoFixture(t, db, 10, .3)
	repo := NewVideoBillingReservationRepository(db)
	videoExec(t, db, `INSERT INTO user_platform_quotas(user_id,platform,daily_limit_usd) VALUES($1,'video',1)`, v.UserID)
	today := timezone.StartOfDay(timezone.Now())
	v.QuotaUsageFloor = &service.UserPlatformQuotaCacheEntry{DailyUsageUSD: .6, DailyWindowStart: &today}
	if err := repo.Reserve(ctx, v); err != nil {
		t.Fatal(err)
	}
	var used float64
	if err := db.QueryRow(`SELECT daily_usage_usd FROM user_platform_quotas WHERE user_id=$1`, v.UserID).Scan(&used); err != nil || used != .6 {
		t.Fatalf("cache floor not absorbed %f %v", used, err)
	}
	if err := repo.Reserve(ctx, videoClone(v, v.ID+"-floor-over")); err == nil {
		t.Fatal("cache floor + held task exceeded quota")
	}
}

func TestVideoBillingReservationPostgresEnterpriseHierarchy(t *testing.T) {
	db := videoTestDB(t)
	ctx := context.Background()
	for _, scope := range []string{"enterprise", "department", "member"} {
		t.Run(scope, func(t *testing.T) {
			v := videoFixture(t, db, 100, .6)
			repo := NewVideoBillingReservationRepository(db)
			if err := db.QueryRow(`INSERT INTO enterprises(name,balance) VALUES($1,1) RETURNING id`, v.ID).Scan(&v.EnterpriseID); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(`INSERT INTO enterprise_departments(enterprise_id,name) VALUES($1,'d') RETURNING id`, v.EnterpriseID).Scan(&v.EnterpriseDepartmentID); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(`INSERT INTO enterprise_members(enterprise_id,user_id,department_id) VALUES($1,$2,$3) RETURNING id`, v.EnterpriseID, v.UserID, v.EnterpriseDepartmentID).Scan(&v.EnterpriseMemberID); err != nil {
				t.Fatal(err)
			}
			v.EnterpriseDepartmentPathIDs = []int64{v.EnterpriseDepartmentID}
			for _, q := range []struct {
				scope string
				id    int64
			}{{"enterprise", v.EnterpriseID}, {"department", v.EnterpriseDepartmentID}, {"member", v.EnterpriseMemberID}} {
				videoExec(t, db, `INSERT INTO enterprise_quotas(enterprise_id,scope_type,scope_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, v.EnterpriseID, q.scope, q.id)
			}
			videoExec(t, db, `UPDATE enterprise_quotas SET daily_limit_usd=0.8 WHERE enterprise_id=$1 AND scope_type=$2`, v.EnterpriseID, scope)
			if err := repo.Reserve(ctx, v); err != nil {
				t.Fatal(err)
			}
			// Wallet has plenty left to prove this rejection is the quota, not funds.
			videoExec(t, db, `UPDATE enterprises SET balance=10 WHERE id=$1`, v.EnterpriseID)
			if err := repo.Reserve(ctx, videoClone(v, v.ID+"-quota")); err == nil {
				t.Fatal("enterprise quota did not include held task")
			}
			videoExec(t, db, `UPDATE enterprise_members SET deleted_at=NOW() WHERE id=$1`, v.EnterpriseMemberID)
			result, err := repo.Settle(ctx, &service.VideoReservationSettlement{ID: v.ID, Usage: videoUsage(v, .4)})
			if err != nil || !result.Applied {
				t.Fatal("archived membership must settle", err)
			}
			videoWallet(t, db, v, 10.2, 0)
			var count int
			if err := db.QueryRow(`SELECT COUNT(*) FROM enterprise_quotas WHERE enterprise_id=$1 AND total_usage_usd=0.4 AND daily_usage_usd=0.4`, v.EnterpriseID).Scan(&count); err != nil || count != 3 {
				t.Fatalf("quota scopes incremented=%d err=%v", count, err)
			}
			var personal float64
			_ = db.QueryRow(`SELECT balance FROM users WHERE id=$1`, v.UserID).Scan(&personal)
			if personal != 100 {
				t.Fatal("enterprise task charged personal balance")
			}
		})
	}
}

func TestVideoBillingReservationPostgresSubscriptionAndSnapshotMerge(t *testing.T) {
	db := videoTestDB(t)
	ctx := context.Background()
	v := videoFixture(t, db, 0, .6)
	repo := NewVideoBillingReservationRepository(db)
	videoExec(t, db, `UPDATE groups SET subscription_type='subscription',daily_limit_usd=1 WHERE id=$1`, v.GroupID)
	var subID int64
	if err := db.QueryRow(`INSERT INTO user_subscriptions(user_id,group_id,starts_at,expires_at) VALUES($1,$2,NOW(),NOW()+INTERVAL '30 days') RETURNING id`, v.UserID, v.GroupID).Scan(&subID); err != nil {
		t.Fatal(err)
	}
	v.SubscriptionID = &subID
	if err := repo.Reserve(ctx, v); err != nil {
		t.Fatal(err)
	}
	if err := repo.Reserve(ctx, videoClone(v, v.ID+"-sub-over")); err == nil {
		t.Fatal("subscription hold not enforced")
	}
	if _, err := repo.Settle(ctx, &service.VideoReservationSettlement{ID: v.ID, Usage: videoUsage(v, .4)}); err != nil {
		t.Fatal(err)
	}
	videoWallet(t, db, v, 0, 0)
	var usage float64
	if err := db.QueryRow(`SELECT daily_usage_usd FROM user_subscriptions WHERE id=$1`, subID).Scan(&usage); err != nil || usage != .4 {
		t.Fatalf("subscription %f %v", usage, err)
	}
	// Flusher snapshots must never overwrite an atomic capture with older data.
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	quotaRepo := NewUserPlatformQuotaRepository(client)
	qv := videoFixture(t, db, 10, .6)
	today := timezone.StartOfDay(timezone.Now())
	yesterday := today.AddDate(0, 0, -1)
	videoExec(t, db, `INSERT INTO user_platform_quotas(user_id,platform,daily_limit_usd,daily_usage_usd,weekly_usage_usd,monthly_usage_usd,daily_window_start,weekly_window_start,monthly_window_start)
 VALUES($1,'video',10,0.6,0.6,0.6,$2,$2,$2)`, qv.UserID, today)
	for _, snapshot := range []UserPlatformQuotaSnapshot{
		{UserID: qv.UserID, Platform: "video", DailyUsageUSD: .2, WeeklyUsageUSD: .2, MonthlyUsageUSD: .2, DailyWindowStart: today, WeeklyWindowStart: today, MonthlyWindowStart: today},
		{UserID: qv.UserID, Platform: "video", DailyUsageUSD: 9, WeeklyUsageUSD: 9, MonthlyUsageUSD: 9, DailyWindowStart: yesterday, WeeklyWindowStart: yesterday, MonthlyWindowStart: yesterday},
	} {
		if err := quotaRepo.BatchSnapshotUsage(ctx, []UserPlatformQuotaSnapshot{snapshot}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	var daily, weekly, monthly float64
	var start time.Time
	if err := db.QueryRow(`SELECT daily_usage_usd,weekly_usage_usd,monthly_usage_usd,daily_window_start FROM user_platform_quotas WHERE user_id=$1`, qv.UserID).Scan(&daily, &weekly, &monthly, &start); err != nil {
		t.Fatal(err)
	}
	if daily != .6 || weekly != .6 || monthly != .6 || !start.Equal(today) {
		t.Fatalf("flusher regressed counters: %f/%f/%f %v", daily, weekly, monthly, start)
	}
	// A genuinely newer window can reset usage; an equal-window newer value
	// advances the floor instead of adding it a second time.
	tomorrow := today.AddDate(0, 0, 1)
	for _, amount := range []float64{.1, .2, .15} {
		if err := quotaRepo.BatchSnapshotUsage(ctx, []UserPlatformQuotaSnapshot{{UserID: qv.UserID, Platform: "video", DailyUsageUSD: amount,
			WeeklyUsageUSD: amount, MonthlyUsageUSD: amount, DailyWindowStart: tomorrow, WeeklyWindowStart: tomorrow, MonthlyWindowStart: tomorrow}}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.QueryRow(`SELECT daily_usage_usd,daily_window_start FROM user_platform_quotas WHERE user_id=$1`, qv.UserID).Scan(&daily, &start); err != nil {
		t.Fatal(err)
	}
	if daily != .2 || !start.Equal(tomorrow) {
		t.Fatalf("window merge=%f %v", daily, start)
	}
	adapter := NewUserPlatformQuotaServiceAdapter(quotaRepo)
	scope, ok := adapter.(service.VideoBillingQuotaScopeRepository)
	if !ok {
		t.Fatal("production adapter lost durable scope interface")
	}
	exists, err := scope.HasVideoBillingScope(ctx, v.UserID, v.Platform)
	if err != nil || !exists {
		t.Fatalf("settled task must retain DB-authoritative scope: %v %v", exists, err)
	}
	exists, err = scope.HasVideoBillingScope(ctx, qv.UserID, qv.Platform)
	if err != nil || exists {
		t.Fatalf("ordinary scope incorrectly marked durable: %v %v", exists, err)
	}
}
