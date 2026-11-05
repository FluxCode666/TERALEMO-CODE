//go:build unit

package service

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

func TestScheduledQuestionSkipReasonQuotaDimensionsAndReset(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	dailyStart := now.Add(-time.Hour)
	weeklyStart := now.Add(-24 * time.Hour)
	for _, test := range []struct {
		name      string
		extra     map[string]any
		resetAt   time.Time
		wantSkip  bool
		noResetAt bool
	}{
		{name: "no limits", extra: map[string]any{}},
		{name: "below total", extra: map[string]any{"quota_limit": 10, "quota_used": 9}},
		{name: "total", extra: map[string]any{"quota_limit": 10, "quota_used": 10}, wantSkip: true, noResetAt: true},
		{name: "daily", extra: map[string]any{
			"quota_daily_limit": 10, "quota_daily_used": 10, "quota_daily_start": dailyStart.Format(time.RFC3339),
		}, wantSkip: true, resetAt: dailyStart.Add(24 * time.Hour)},
		{name: "weekly", extra: map[string]any{
			"quota_weekly_limit": 10, "quota_weekly_used": 11, "quota_weekly_start": weeklyStart.Format(time.RFC3339),
		}, wantSkip: true, resetAt: weeklyStart.Add(7 * 24 * time.Hour)},
		{name: "both take latest", extra: map[string]any{
			"quota_daily_limit": 10, "quota_daily_used": 10, "quota_daily_start": dailyStart.Format(time.RFC3339),
			"quota_weekly_limit": 10, "quota_weekly_used": 10, "quota_weekly_start": weeklyStart.Format(time.RFC3339),
		}, wantSkip: true, resetAt: weeklyStart.Add(7 * 24 * time.Hour)},
		{name: "daily reset later than weekly", extra: map[string]any{
			"quota_daily_limit": 10, "quota_daily_used": 10, "quota_daily_start": dailyStart.Format(time.RFC3339),
			"quota_weekly_limit": 10, "quota_weekly_used": 10, "quota_weekly_start": now.Add(-6*24*time.Hour - 23*time.Hour).Format(time.RFC3339),
		}, wantSkip: true, resetAt: dailyStart.Add(24 * time.Hour)},
		{name: "total has no reset even with daily", extra: map[string]any{
			"quota_limit": 10, "quota_used": 10,
			"quota_daily_limit": 10, "quota_daily_used": 10, "quota_daily_start": dailyStart.Format(time.RFC3339),
		}, wantSkip: true, noResetAt: true},
		{name: "expired daily", extra: map[string]any{
			"quota_daily_limit": 10, "quota_daily_used": 100, "quota_daily_start": now.Add(-25 * time.Hour).Format(time.RFC3339),
		}},
		{name: "expired weekly", extra: map[string]any{
			"quota_weekly_limit": 10, "quota_weekly_used": 100, "quota_weekly_start": now.Add(-8 * 24 * time.Hour).Format(time.RFC3339),
		}},
		{name: "missing period start", extra: map[string]any{"quota_daily_limit": 10, "quota_daily_used": 100}},
		{name: "malformed period start", extra: map[string]any{
			"quota_weekly_limit": 10, "quota_weekly_used": 100, "quota_weekly_start": "not a date",
		}},
	} {
		for _, accountType := range []string{AccountTypeAPIKey, AccountTypeBedrock} {
			t.Run(test.name+"/"+accountType, func(t *testing.T) {
				account := &Account{ID: 41, Platform: PlatformAnthropic, Type: accountType, Extra: test.extra}
				reason, resetAt := (&ScheduledQuestionTestService{}).scheduledQuestionSkipReason(context.Background(), account, "")
				if !test.wantSkip {
					require.Empty(t, reason)
					require.Nil(t, resetAt)
					return
				}
				require.Equal(t, "quota_exceeded", reason)
				if test.noResetAt {
					require.Nil(t, resetAt)
				} else {
					require.NotNil(t, resetAt)
					require.True(t, test.resetAt.Equal(*resetAt), "reset: %v", resetAt)
				}
			})
		}
	}
}

func TestScheduledQuestionSkipReasonFixedQuotaResets(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	location, err := time.LoadLocation("Asia/Singapore")
	require.NoError(t, err)
	hour := (now.In(location).Hour() + 6) % 24
	day := (int(now.In(location).Weekday()) + 3) % 7
	nearWeeklyReset := now.In(location).Add(2 * time.Hour)
	for _, test := range []struct {
		name    string
		extra   map[string]any
		resetAt time.Time
	}{
		{name: "daily configured timezone", extra: map[string]any{
			"quota_daily_limit": 1, "quota_daily_used": 1, "quota_daily_start": now.Format(time.RFC3339),
			"quota_daily_reset_mode": "fixed", "quota_daily_reset_hour": hour, "quota_reset_timezone": location.String(),
		}, resetAt: nextFixedDailyReset(hour, location, now)},
		{name: "weekly configured timezone", extra: map[string]any{
			"quota_weekly_limit": 1, "quota_weekly_used": 1, "quota_weekly_start": now.Format(time.RFC3339),
			"quota_weekly_reset_mode": "fixed", "quota_weekly_reset_day": day, "quota_weekly_reset_hour": hour,
			"quota_reset_timezone": location.String(),
		}, resetAt: nextFixedWeeklyReset(day, hour, location, now)},
		{name: "invalid timezone uses UTC", extra: map[string]any{
			"quota_daily_limit": 1, "quota_daily_used": 1, "quota_daily_start": now.Format(time.RFC3339),
			"quota_daily_reset_mode": "fixed", "quota_daily_reset_hour": (now.Hour() + 6) % 24, "quota_reset_timezone": "not/a/timezone",
		}, resetAt: nextFixedDailyReset((now.Hour()+6)%24, time.UTC, now)},
		{name: "expired fixed daily does not postpone weekly", extra: map[string]any{
			"quota_daily_limit": 1, "quota_daily_used": 100, "quota_daily_start": now.Add(-48 * time.Hour).Format(time.RFC3339),
			"quota_daily_reset_mode": "fixed", "quota_daily_reset_hour": hour,
			"quota_weekly_limit": 1, "quota_weekly_used": 1, "quota_weekly_start": now.Format(time.RFC3339),
			"quota_weekly_reset_mode": "fixed", "quota_weekly_reset_day": int(nearWeeklyReset.Weekday()), "quota_weekly_reset_hour": nearWeeklyReset.Hour(),
			"quota_reset_timezone": location.String(),
		}, resetAt: nextFixedWeeklyReset(int(nearWeeklyReset.Weekday()), nearWeeklyReset.Hour(), location, now)},
		{name: "expired fixed weekly does not postpone daily", extra: map[string]any{
			"quota_daily_limit": 1, "quota_daily_used": 1, "quota_daily_start": now.Format(time.RFC3339),
			"quota_daily_reset_mode": "fixed", "quota_daily_reset_hour": hour,
			"quota_weekly_limit": 1, "quota_weekly_used": 100, "quota_weekly_start": now.Add(-8 * 24 * time.Hour).Format(time.RFC3339),
			"quota_weekly_reset_mode": "fixed", "quota_weekly_reset_day": day, "quota_weekly_reset_hour": hour,
			"quota_reset_timezone": location.String(),
		}, resetAt: nextFixedDailyReset(hour, location, now)},
	} {
		t.Run(test.name, func(t *testing.T) {
			account := &Account{Type: AccountTypeAPIKey, Extra: test.extra}
			reason, resetAt := (&ScheduledQuestionTestService{}).scheduledQuestionSkipReason(context.Background(), account, "")
			require.Equal(t, "quota_exceeded", reason)
			require.NotNil(t, resetAt)
			require.True(t, test.resetAt.Equal(*resetAt), "reset: %v", resetAt)
		})
	}
}

type scheduledQuestionSkipUsageRepo struct {
	UsageLogRepository
	stats     *usagestats.AccountStats
	err       error
	calls     int
	accountID int64
	start     time.Time
}

func (r *scheduledQuestionSkipUsageRepo) GetAccountWindowStats(_ context.Context, accountID int64, start time.Time) (*usagestats.AccountStats, error) {
	r.calls++
	r.accountID, r.start = accountID, start
	return r.stats, r.err
}

type scheduledQuestionSkipCache struct {
	SessionLimitCache
	cost       float64
	hit        bool
	getErr     error
	setErr     error
	getCalls   int
	setCalls   int
	setAccount int64
	setCost    float64
}

func (c *scheduledQuestionSkipCache) GetWindowCost(context.Context, int64) (float64, bool, error) {
	c.getCalls++
	return c.cost, c.hit, c.getErr
}

func (c *scheduledQuestionSkipCache) SetWindowCost(_ context.Context, accountID int64, cost float64) error {
	c.setCalls++
	c.setAccount, c.setCost = accountID, cost
	return c.setErr
}

func TestScheduledQuestionSkipReasonWindowCostCacheAndFallback(t *testing.T) {
	start := time.Now().UTC().Truncate(time.Hour).Add(-time.Hour)
	end := start.Add(5 * time.Hour)
	for _, test := range []struct {
		name       string
		cache      *scheduledQuestionSkipCache
		repo       *scheduledQuestionSkipUsageRepo
		wantSkip   bool
		repoCalls  int
		cacheWrite bool
	}{
		{name: "cache below limit", cache: &scheduledQuestionSkipCache{cost: 9.99, hit: true}, repo: &scheduledQuestionSkipUsageRepo{}},
		{name: "cache sticky only", cache: &scheduledQuestionSkipCache{cost: 10, hit: true}, repo: &scheduledQuestionSkipUsageRepo{}, wantSkip: true},
		{name: "cache not schedulable", cache: &scheduledQuestionSkipCache{cost: 25, hit: true}, repo: &scheduledQuestionSkipUsageRepo{}, wantSkip: true},
		{name: "cache miss", cache: &scheduledQuestionSkipCache{}, repo: &scheduledQuestionSkipUsageRepo{stats: &usagestats.AccountStats{StandardCost: 10}}, wantSkip: true, repoCalls: 1, cacheWrite: true},
		{name: "cache failure falls back", cache: &scheduledQuestionSkipCache{cost: 0, hit: true, getErr: errors.New("private cache failure")}, repo: &scheduledQuestionSkipUsageRepo{stats: &usagestats.AccountStats{StandardCost: 10}}, wantSkip: true, repoCalls: 1, cacheWrite: true},
		{name: "database standard cost ignores billing multipliers", cache: &scheduledQuestionSkipCache{}, repo: &scheduledQuestionSkipUsageRepo{stats: &usagestats.AccountStats{StandardCost: 9, Cost: 900, UserCost: 900}}, repoCalls: 1, cacheWrite: true},
		{name: "cache write failure", cache: &scheduledQuestionSkipCache{setErr: errors.New("private write failure")}, repo: &scheduledQuestionSkipUsageRepo{stats: &usagestats.AccountStats{StandardCost: 10}}, wantSkip: true, repoCalls: 1, cacheWrite: true},
		{name: "database failure open", cache: &scheduledQuestionSkipCache{}, repo: &scheduledQuestionSkipUsageRepo{err: errors.New("private database failure")}, repoCalls: 1},
		{name: "nil stats open", repo: &scheduledQuestionSkipUsageRepo{}, repoCalls: 1},
		{name: "no cache", repo: &scheduledQuestionSkipUsageRepo{stats: &usagestats.AccountStats{StandardCost: 10}}, wantSkip: true, repoCalls: 1},
		{name: "cache without repository", cache: &scheduledQuestionSkipCache{cost: 10, hit: true}, wantSkip: true},
		{name: "missing dependencies open"},
	} {
		t.Run(test.name, func(t *testing.T) {
			account := &Account{ID: 41, Platform: PlatformAnthropic, Type: AccountTypeOAuth, SessionWindowStart: &start, SessionWindowEnd: &end,
				Extra: map[string]any{"window_cost_limit": 10, "window_cost_sticky_reserve": 10}}
			svc := &ScheduledQuestionTestService{}
			if test.cache != nil {
				svc.sessionLimitCache = test.cache
			}
			if test.repo != nil {
				svc.usageLogRepo = test.repo
			}
			reason, resetAt := svc.scheduledQuestionSkipReason(context.Background(), account, "")
			if test.wantSkip {
				require.Equal(t, "window_cost_exceeded", reason)
				require.NotNil(t, resetAt)
				require.True(t, end.Equal(*resetAt))
			} else {
				require.Empty(t, reason)
				require.Nil(t, resetAt)
			}
			if test.repo != nil {
				require.Equal(t, test.repoCalls, test.repo.calls)
				if test.repoCalls > 0 {
					require.Equal(t, account.ID, test.repo.accountID)
					require.True(t, start.Equal(test.repo.start))
				}
			}
			if test.cache != nil {
				require.Equal(t, 1, test.cache.getCalls)
				if test.cacheWrite {
					require.Equal(t, 1, test.cache.setCalls)
					require.Equal(t, account.ID, test.cache.setAccount)
					require.Equal(t, test.repo.stats.StandardCost, test.cache.setCost)
				} else {
					require.Zero(t, test.cache.setCalls)
				}
			}
		})
	}
}

func TestScheduledQuestionSkipReasonExpiredWindowUsesPredictedStart(t *testing.T) {
	now := time.Now()
	start, end := now.Add(-6*time.Hour), now.Add(-time.Hour)
	account := &Account{ID: 41, Platform: PlatformAnthropic, Type: AccountTypeOAuth, SessionWindowStart: &start, SessionWindowEnd: &end,
		Extra: map[string]any{"window_cost_limit": 10}}
	repo := &scheduledQuestionSkipUsageRepo{stats: &usagestats.AccountStats{StandardCost: 10}}
	svc := &ScheduledQuestionTestService{usageLogRepo: repo}
	before := account.GetCurrentWindowStartTime()
	reason, resetAt := svc.scheduledQuestionSkipReason(context.Background(), account, "")
	after := account.GetCurrentWindowStartTime()
	require.Equal(t, "window_cost_exceeded", reason)
	require.True(t, repo.start.Equal(before) || repo.start.Equal(after), "query must use the current predicted window, not an expired one")
	require.NotNil(t, resetAt)
	require.True(t, repo.start.Add(5*time.Hour).Equal(*resetAt))
}

func TestScheduledQuestionSkipReasonScopeAndPriority(t *testing.T) {
	reset := time.Now().Add(time.Hour)
	for _, test := range []struct {
		name     string
		account  *Account
		wantSkip string
	}{
		{name: "missing account"},
		{name: "OAuth quota ignored", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{"quota_limit": 1, "quota_used": 2}}},
		{name: "API key window ignored", account: &Account{Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Extra: map[string]any{"window_cost_limit": 1}}},
		{name: "other OAuth window ignored", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{"window_cost_limit": 1}}},
		{name: "disabled window ignored", account: &Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth, Extra: map[string]any{"window_cost_limit": 0}}},
		{name: "quota before rate", account: &Account{Type: AccountTypeAPIKey, RateLimitResetAt: &reset, Extra: map[string]any{"quota_limit": 1, "quota_used": 2}}, wantSkip: "quota_exceeded"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cache := &scheduledQuestionSkipCache{cost: 100, hit: true}
			repo := &scheduledQuestionSkipUsageRepo{}
			svc := &ScheduledQuestionTestService{sessionLimitCache: cache, usageLogRepo: repo}
			reason, resetAt := svc.scheduledQuestionSkipReason(context.Background(), test.account, "")
			require.Equal(t, test.wantSkip, reason)
			require.Nil(t, resetAt)
			require.Zero(t, cache.getCalls)
			require.Zero(t, repo.calls)
		})
	}
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeSetupToken} {
		account := &Account{Platform: PlatformAnthropic, Type: accountType, RateLimitResetAt: &reset, Extra: map[string]any{"window_cost_limit": 10}}
		svc := &ScheduledQuestionTestService{sessionLimitCache: &scheduledQuestionSkipCache{cost: 10, hit: true}}
		reason, resetAt := svc.scheduledQuestionSkipReason(context.Background(), account, "")
		require.Equal(t, "window_cost_exceeded", reason)
		require.NotNil(t, resetAt)
	}
}

func TestScheduledQuestionSkipReasonRateLimitsAndExplicitModel(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	accountReset, modelReset := now.Add(time.Hour), now.Add(2*time.Hour)
	for _, test := range []struct {
		name         string
		model        string
		accountReset *time.Time
		modelReset   time.Time
		wantReset    *time.Time
	}{
		{name: "account only", accountReset: &accountReset, wantReset: &accountReset},
		{name: "model only", model: " requested-model ", modelReset: modelReset, wantReset: &modelReset},
		{name: "model not explicit", modelReset: modelReset},
		{name: "blank model", model: " \t ", modelReset: modelReset},
		{name: "other model ignored", model: "another-model", modelReset: modelReset},
		{name: "expired account", accountReset: scheduledQuestionSkipTimePointer(now.Add(-time.Hour))},
		{name: "expired model", model: "requested-model", modelReset: now.Add(-time.Hour)},
		{name: "latest model reset", model: "requested-model", accountReset: &accountReset, modelReset: modelReset, wantReset: &modelReset},
		{name: "latest account reset", model: "requested-model", accountReset: &modelReset, modelReset: accountReset, wantReset: &modelReset},
	} {
		t.Run(test.name, func(t *testing.T) {
			account := &Account{Type: AccountTypeOAuth, Platform: PlatformOpenAI, RateLimitResetAt: test.accountReset,
				Credentials: map[string]any{"model_mapping": map[string]any{"requested-model": "upstream-model"}},
				Extra: map[string]any{modelRateLimitsKey: map[string]any{
					"upstream-model": map[string]any{"rate_limit_reset_at": test.modelReset.Format(time.RFC3339)},
				}}}
			reason, resetAt := (&ScheduledQuestionTestService{}).scheduledQuestionSkipReason(context.Background(), account, test.model)
			if test.wantReset == nil {
				require.Empty(t, reason)
				require.Nil(t, resetAt)
			} else {
				require.Equal(t, "rate_limited", reason)
				require.NotNil(t, resetAt)
				require.True(t, test.wantReset.Equal(*resetAt))
			}
		})
	}
	account := &Account{Type: AccountTypeOAuth, Platform: PlatformAnthropic}
	setAccountModelRateLimitSnapshot(account, "claude-fable-5", modelReset, "", now)
	reason, resetAt := (&ScheduledQuestionTestService{}).scheduledQuestionSkipReason(context.Background(), account, "claude-fable-5[1m]")
	require.Equal(t, "rate_limited", reason, "reuse model-family scopes, not just exact model names")
	require.NotNil(t, resetAt)
	require.True(t, modelReset.Equal(*resetAt))
}

func scheduledQuestionSkipTimePointer(value time.Time) *time.Time { return &value }

func TestScheduledQuestionSkipReasonWindowLookupFailureLogsNoRawErrors(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(previous)
	rawError := errors.New("https://user:password@private.example/hook/private-token?secret=private-secret")
	reset := time.Now().Add(time.Hour)
	account := &Account{ID: 41, Platform: PlatformAnthropic, Type: AccountTypeOAuth, RateLimitResetAt: &reset,
		Extra: map[string]any{"window_cost_limit": 10}}
	svc := &ScheduledQuestionTestService{
		sessionLimitCache: &scheduledQuestionSkipCache{getErr: rawError},
		usageLogRepo:      &scheduledQuestionSkipUsageRepo{err: rawError},
	}
	reason, resetAt := svc.scheduledQuestionSkipReason(context.Background(), account, "")
	require.Equal(t, "rate_limited", reason, "window lookup fails open without hiding an independent active limit")
	require.NotNil(t, resetAt)
	require.True(t, reset.Equal(*resetAt))
	require.Contains(t, logs.String(), "scheduled_question_window_cost_unavailable")
	require.Contains(t, logs.String(), `"account_id":41`)
	for _, private := range []string{"private.example", "password", "private-token", "private-secret"} {
		require.NotContains(t, logs.String(), private)
	}
}

func TestScheduledQuestionAllErrorsRateLimitedPrefersStructuredStatus(t *testing.T) {
	item := func(code int, message string) AccountBatchTestItemResult {
		return AccountBatchTestItemResult{AccountID: 41, Status: "error", StatusCode: code, Error: message}
	}
	for _, test := range []struct {
		name     string
		count    int
		items    []AccountBatchTestItemResult
		wantSkip bool
	}{
		{name: "structured", count: 2, items: []AccountBatchTestItemResult{item(http.StatusTooManyRequests, "private upstream body"), item(http.StatusTooManyRequests, "")}, wantSkip: true},
		{name: "fallback", count: 1, items: []AccountBatchTestItemResult{item(0, "API returned 429: usage_limit_reached")}, wantSkip: true},
		{name: "fallback exact", count: 1, items: []AccountBatchTestItemResult{item(0, "API returned 429")}, wantSkip: true},
		{name: "mixed structured and fallback", count: 2, items: []AccountBatchTestItemResult{item(http.StatusTooManyRequests, ""), item(0, "API returned 429: limit")}, wantSkip: true},
		{name: "structured takes precedence", count: 1, items: []AccountBatchTestItemResult{item(http.StatusInternalServerError, "API returned 429: forged body")}},
		{name: "other status", count: 2, items: []AccountBatchTestItemResult{item(http.StatusTooManyRequests, ""), item(http.StatusForbidden, "forbidden")}},
		{name: "not a status prefix", count: 1, items: []AccountBatchTestItemResult{item(0, "body mentions API returned 429: not a real status")}},
		{name: "wrong status prefix", count: 1, items: []AccountBatchTestItemResult{item(0, "API returned 500: body API returned 429")}},
		{name: "number boundary", count: 1, items: []AccountBatchTestItemResult{item(0, "API returned 4290: not 429")}},
		{name: "word boundary", count: 1, items: []AccountBatchTestItemResult{item(0, "API returned 429oops")}},
		{name: "no results", count: 1},
		{name: "no questions"},
		{name: "incomplete", count: 2, items: []AccountBatchTestItemResult{item(http.StatusTooManyRequests, "")}},
		{name: "too many results", count: 1, items: []AccountBatchTestItemResult{item(http.StatusTooManyRequests, ""), item(http.StatusTooManyRequests, "")}},
		{name: "not error", count: 1, items: []AccountBatchTestItemResult{{AccountID: 41, Status: "failed", StatusCode: http.StatusTooManyRequests}}},
		{name: "judged", count: 1, items: []AccountBatchTestItemResult{{AccountID: 41, Status: "error", Judged: true, StatusCode: http.StatusTooManyRequests}}},
		{name: "other accounts ignored", count: 1, items: []AccountBatchTestItemResult{item(http.StatusTooManyRequests, ""), {AccountID: 99, Status: "error", StatusCode: http.StatusInternalServerError}}, wantSkip: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.wantSkip, scheduledQuestionAllErrorsRateLimited(41, test.count, test.items))
		})
	}
}
