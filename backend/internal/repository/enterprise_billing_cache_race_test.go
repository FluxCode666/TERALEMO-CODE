package repository

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type enterpriseBalanceReadFunc func(context.Context, int64) (float64, error)

func (f enterpriseBalanceReadFunc) GetBalance(ctx context.Context, id int64) (float64, error) {
	return f(ctx, id)
}

type enterpriseQuotaReadFunc func(context.Context, int64, []service.EnterpriseQuotaScope) ([]service.EnterpriseQuota, error)

func (f enterpriseQuotaReadFunc) GetByScopes(ctx context.Context, id int64, scopes []service.EnterpriseQuotaScope) ([]service.EnterpriseQuota, error) {
	return f(ctx, id, scopes)
}

func enterpriseTestWindows(now time.Time) *service.EnterpriseBillingWindowStarts {
	return &service.EnterpriseBillingWindowStarts{Daily: timezone.StartOfDay(now), Weekly: timezone.StartOfWeek(now), Monthly: timezone.StartOfMonth(now)}
}

func enterpriseTestQuota(scope string, id int64, windows *service.EnterpriseBillingWindowStarts) service.EnterpriseQuota {
	return service.EnterpriseQuota{ID: id, EnterpriseID: 10, ScopeType: scope, ScopeID: id,
		DailyWindowStart: &windows.Daily, WeeklyWindowStart: &windows.Weekly, MonthlyWindowStart: &windows.Monthly}
}

func TestEnterpriseBalanceReloadFencedByInvalidationAndSettlement(t *testing.T) {
	for _, settlement := range []bool{false, true} {
		name := "management invalidation"
		if settlement {
			name = "settlement while cache absent"
		}
		t.Run(name, func(t *testing.T) {
			_, cache := newEnterpriseCacheTest(t)
			ctx := context.Background()
			started, resume := make(chan struct{}), make(chan struct{})
			var calls atomic.Int32
			repo := enterpriseBalanceReadFunc(func(ctx context.Context, _ int64) (float64, error) {
				if calls.Add(1) == 1 {
					close(started) // DB has already returned its old snapshot.
					select {
					case <-resume:
						return 1, nil
					case <-ctx.Done():
						return 0, ctx.Err()
					}
				}
				return 0.2, nil // management/settlement has committed in the meantime.
			})
			svc := &service.BillingCacheService{}
			svc.ConfigureEnterpriseBilling(cache, repo, nil)
			type result struct {
				balance float64
				err     error
			}
			done := make(chan result, 1)
			go func() { balance, err := svc.GetEnterpriseBalance(ctx, 10); done <- result{balance, err} }()
			<-started
			if settlement {
				require.NoError(t, cache.DeductEnterpriseBalance(ctx, 10, 0.8))
			} else {
				require.NoError(t, cache.InvalidateEnterpriseBalance(ctx, 10))
			}
			close(resume)
			got := <-done
			require.NoError(t, got.err)
			require.Equal(t, 0.2, got.balance)
			require.EqualValues(t, 2, calls.Load(), "a fenced snapshot must be read again")
			balance, err := cache.GetEnterpriseBalance(ctx, 10)
			require.NoError(t, err)
			require.Equal(t, 0.2, balance)
			require.Equal(t, time.Duration(-1), cache.rdb.TTL(ctx, enterpriseBillingGenerationKey(enterpriseBillingBalanceKey(10))).Val())
		})
	}
}

func TestEnterpriseQuotaReloadCannotRestoreOldLimitOrOverwriteUsage(t *testing.T) {
	for _, settlement := range []bool{false, true} {
		name := "management tightens limit"
		if settlement {
			name = "settlement increments during partial reload"
		}
		t.Run(name, func(t *testing.T) {
			_, cache := newEnterpriseCacheTest(t)
			ctx := context.Background()
			windows := enterpriseTestWindows(timezone.Now())
			enterprise := enterpriseTestQuota("enterprise", 10, windows)
			member := enterpriseTestQuota("member", 30, windows)
			oldLimit, newLimit := 10.0, 0.5
			member.TotalLimitUSD = &oldLimit
			// The enterprise scope is cached, while the member scope is missing.
			require.NoError(t, cache.SetEnterpriseBalance(ctx, 10, 5))
			require.NoError(t, cache.SetEnterpriseQuota(ctx, &enterprise))
			started, resume := make(chan struct{}), make(chan struct{})
			var calls atomic.Int32
			repo := enterpriseQuotaReadFunc(func(ctx context.Context, _ int64, _ []service.EnterpriseQuotaScope) ([]service.EnterpriseQuota, error) {
				if calls.Add(1) == 1 {
					close(started)
					select {
					case <-resume:
						return []service.EnterpriseQuota{member, enterprise}, nil
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				}
				currentMember, currentEnterprise := member, enterprise
				if settlement {
					currentMember.TotalUsageUSD, currentEnterprise.TotalUsageUSD = 0.75, 0.75
					currentMember.TotalLimitUSD = &newLimit
				} else {
					currentMember.TotalLimitUSD = &newLimit
					currentMember.TotalUsageUSD = 0.75
				}
				return []service.EnterpriseQuota{currentMember, currentEnterprise}, nil
			})
			svc := &service.BillingCacheService{}
			svc.ConfigureEnterpriseBilling(cache, nil, repo)
			user, _ := enterpriseInflightUsers()
			done := make(chan error, 1)
			go func() { done <- svc.CheckBillingEligibility(ctx, user, nil, &service.Group{ID: 5}, nil, "") }()
			<-started
			if settlement {
				svc.SyncEnterpriseUsage(ctx, user.Enterprise, 0.75, windows)
			} else {
				require.NoError(t, cache.InvalidateEnterpriseQuota(ctx, "member", 30))
			}
			close(resume)
			require.ErrorIs(t, <-done, service.ErrEnterpriseBillingMemberQuotaExceeded)
			require.EqualValues(t, 2, calls.Load())
			quotas, err := cache.GetEnterpriseQuotas(ctx, 10, []service.EnterpriseQuotaScope{{ScopeType: "enterprise", ScopeID: 10}, {ScopeType: "member", ScopeID: 30}})
			require.NoError(t, err)
			require.Len(t, quotas, 2)
			require.Equal(t, 0.75, quotas[1].TotalUsageUSD)
			require.Equal(t, newLimit, *quotas[1].TotalLimitUSD)
			if settlement {
				require.Equal(t, 0.75, quotas[0].TotalUsageUSD, "stale whole-scope reload must not erase a committed increment")
			}
		})
	}
}

func TestEnterpriseUsageIncrementsCannotExtendConfigurationFreshness(t *testing.T) {
	mr, cache := newEnterpriseCacheTest(t)
	ctx := context.Background()
	windows := enterpriseTestWindows(timezone.Now())
	q := enterpriseTestQuota("enterprise", 10, windows)
	require.NoError(t, cache.SetEnterpriseQuota(ctx, &q))
	require.NoError(t, cache.SetEnterpriseBalance(ctx, 10, 5))
	quotaKey, balanceKey := enterpriseBillingQuotaKey("enterprise", 10), enterpriseBillingBalanceKey(10)
	initialQuotaTTL, initialBalanceTTL := mr.TTL(quotaKey), mr.TTL(balanceKey)
	mr.FastForward(time.Minute)
	for i := 0; i < 10; i++ {
		require.NoError(t, cache.IncrementEnterpriseQuotaUsageForWindows(ctx, "enterprise", 10, 0.01, *windows))
		require.NoError(t, cache.DeductEnterpriseBalance(ctx, 10, 0.01))
	}
	require.LessOrEqual(t, mr.TTL(quotaKey), initialQuotaTTL-time.Minute)
	require.LessOrEqual(t, mr.TTL(balanceKey), initialBalanceTTL-time.Minute)
	// Even an old entry whose TTL was extended by an older deployment is a miss.
	staleLoaded := time.Now().Add(-enterpriseBillingMaxFreshness - time.Second).UnixMilli()
	require.NoError(t, cache.rdb.HSet(ctx, quotaKey, "loaded_at", staleLoaded).Err())
	require.NoError(t, cache.rdb.Set(ctx, enterpriseBillingLoadedAtKey(balanceKey), staleLoaded, time.Hour).Err())
	quotas, err := cache.GetEnterpriseQuotas(ctx, 10, []service.EnterpriseQuotaScope{{ScopeType: "enterprise", ScopeID: 10}})
	require.NoError(t, err)
	require.Empty(t, quotas)
	_, err = cache.GetEnterpriseBalance(ctx, 10)
	require.ErrorIs(t, err, redis.Nil)
	require.NoError(t, cache.IncrementEnterpriseQuotaUsageForWindows(ctx, "enterprise", 10, 0.1, *windows))
	require.False(t, mr.Exists(quotaKey))
}

type pausedEnterpriseInflightCache struct {
	*billingCache
	balances *enterpriseBillingCache
	started  chan float64
	resume   chan struct{}
}

func (c *pausedEnterpriseInflightCache) pause(ctx context.Context) {
	balance, _ := c.balances.GetEnterpriseBalance(ctx, 10)
	c.started <- balance
	<-c.resume
}
func (c *pausedEnterpriseInflightCache) ReserveEnterpriseInflight(ctx context.Context, id int64, requestID string, amount float64, ttl time.Duration) (bool, bool, error) {
	c.pause(ctx)
	return c.billingCache.ReserveEnterpriseInflight(ctx, id, requestID, amount, ttl)
}

// Also pause the old path, so this regression reproduces the former read-then-
// reserve timing and fails if the service reverts to ReserveInflightScope.
func (c *pausedEnterpriseInflightCache) ReserveInflightScope(ctx context.Context, scope, requestID string, amount float64, ttl time.Duration) (float64, error) {
	c.pause(ctx)
	return c.billingCache.ReserveInflightScope(ctx, scope, requestID, amount, ttl)
}

func TestEnterpriseInflightRechecksBalanceAtomicallyAfterConcurrentSettlement(t *testing.T) {
	_, cache := newEnterpriseCacheTest(t)
	ctx := context.Background()
	require.NoError(t, cache.SetEnterpriseBalance(ctx, 10, 1))
	cfg := &config.Config{}
	cfg.Billing.InflightReservation.Enabled = true
	billing := &billingCache{rdb: cache.rdb}
	a := service.NewBillingCacheService(billing, nil, nil, nil, nil, nil, cfg, nil)
	a.ConfigureEnterpriseBilling(cache, nil, nil)
	t.Cleanup(a.Stop)
	paused := &pausedEnterpriseInflightCache{billingCache: billing, balances: cache, started: make(chan float64, 1), resume: make(chan struct{})}
	b := service.NewBillingCacheService(paused, nil, nil, nil, nil, nil, cfg, nil)
	b.ConfigureEnterpriseBilling(cache, nil, nil)
	t.Cleanup(b.Stop)
	one, two := enterpriseInflightUsers()
	first, err := a.ReserveInflight(ctx, one, nil, nil, 0.8)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { res, err := b.ReserveInflight(ctx, two, nil, nil, 0.8); res.Release(); done <- err }()
	require.Equal(t, 1.0, <-paused.started, "B has observed the old balance before A settles")
	require.NoError(t, cache.DeductEnterpriseBalance(ctx, 10, 0.8))
	first.Release()
	close(paused.resume)
	require.ErrorIs(t, <-done, service.ErrEnterpriseBillingBalanceInsufficient)
	zkey, _ := billingInflightScopeKeys("ent:10")
	require.Zero(t, cache.rdb.ZCard(ctx, zkey).Val(), "rejected B must not leave a reservation")
}

func TestEnterpriseInflightBalanceMissReloadsBeforeAtomicAdmission(t *testing.T) {
	_, cache := newEnterpriseCacheTest(t)
	cfg := &config.Config{}
	cfg.Billing.InflightReservation.Enabled = true
	svc := service.NewBillingCacheService(&billingCache{rdb: cache.rdb}, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(svc.Stop)
	var reads atomic.Int32
	svc.ConfigureEnterpriseBilling(cache, enterpriseBalanceReadFunc(func(context.Context, int64) (float64, error) { reads.Add(1); return 0.2, nil }), nil)
	user, _ := enterpriseInflightUsers()
	res, err := svc.ReserveInflight(context.Background(), user, nil, nil, 0.8)
	require.ErrorIs(t, err, service.ErrEnterpriseBillingBalanceInsufficient)
	require.Nil(t, res)
	require.EqualValues(t, 1, reads.Load())
}

func TestEnterpriseSyncUsageInvalidatesDifferentCommittedWindows(t *testing.T) {
	// Transaction starts before midnight/month/week rollover; cache is reloaded
	// for the new window before that old transaction's post-commit sync runs.
	for _, tc := range []struct {
		name   string
		before time.Time
	}{
		{"daily", time.Date(2026, time.October, 17, 23, 59, 59, 0, timezone.Location())},
		{"weekly", time.Date(2026, time.October, 18, 23, 59, 59, 0, timezone.Location())},
		{"monthly", time.Date(2026, time.October, 31, 23, 59, 59, 0, timezone.Location())},
	} {
		before := tc.before
		after := before.Add(2 * time.Second)
		for _, cacheTime := range []time.Time{after, before} {
			t.Run(tc.name+"/"+cacheTime.Format(time.RFC3339), func(t *testing.T) {
				mr, cache := newEnterpriseCacheTest(t)
				ctx := context.Background()
				committed := enterpriseTestWindows(before)
				q := enterpriseTestQuota("enterprise", 10, enterpriseTestWindows(cacheTime))
				q.TotalUsageUSD = 3
				require.NoError(t, cache.SetEnterpriseQuota(ctx, &q))
				svc := &service.BillingCacheService{}
				svc.ConfigureEnterpriseBilling(cache, nil, nil)
				user, _ := enterpriseInflightUsers()
				svc.SyncEnterpriseUsage(ctx, user.Enterprise, 0.5, committed)
				key := enterpriseBillingQuotaKey("enterprise", 10)
				if cacheTime.Equal(after) {
					require.False(t, mr.Exists(key), "old transaction cannot charge the new cached window")
				} else {
					quotas, err := cache.GetEnterpriseQuotas(ctx, 10, []service.EnterpriseQuotaScope{{ScopeType: "enterprise", ScopeID: 10}})
					require.NoError(t, err)
					require.Len(t, quotas, 1)
					require.Equal(t, 0.5, quotas[0].DailyUsageUSD)
					require.Equal(t, 3.5, quotas[0].TotalUsageUSD)
					require.True(t, committed.Daily.Equal(*quotas[0].DailyWindowStart), "sync uses Apply's window even though wall clock is months later")
				}
			})
		}
	}
}
