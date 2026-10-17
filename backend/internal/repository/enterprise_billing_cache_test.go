package repository

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newEnterpriseCacheTest(t *testing.T) (*miniredis.Miniredis, *enterpriseBillingCache) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return mr, &enterpriseBillingCache{rdb: rdb}
}

func TestEnterpriseBillingCacheBalance(t *testing.T) {
	mr, cache := newEnterpriseCacheTest(t)
	ctx := context.Background()
	require.NoError(t, cache.DeductEnterpriseBalance(ctx, 10, 2))
	require.False(t, mr.Exists(enterpriseBillingBalanceKey(10)))
	require.NoError(t, cache.SetEnterpriseBalance(ctx, 10, 5))
	require.NoError(t, cache.DeductEnterpriseBalance(ctx, 10, 2))
	balance, err := cache.GetEnterpriseBalance(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 3.0, balance)
	require.Positive(t, mr.TTL(enterpriseBillingBalanceKey(10)))
	require.NoError(t, cache.InvalidateEnterpriseBalance(ctx, 10))
	_, err = cache.GetEnterpriseBalance(ctx, 10)
	require.ErrorIs(t, err, redis.Nil)
}

func TestEnterpriseBillingCacheQuotaWindowsAndMissing(t *testing.T) {
	mr, cache := newEnterpriseCacheTest(t)
	ctx := context.Background()
	now := time.Date(2026, time.March, 31, 13, 0, 0, 0, timezone.Location())
	old := time.Date(2026, time.February, 28, 13, 0, 0, 0, timezone.Location())
	limit := 5.0
	q := &service.EnterpriseQuota{ID: 1, EnterpriseID: 10, ScopeType: service.EnterpriseScopeMember, ScopeID: 30, DailyLimitUSD: &limit, TotalLimitUSD: &limit, DailyUsageUSD: 4, WeeklyUsageUSD: 4, MonthlyUsageUSD: 4, TotalUsageUSD: 4, DailyWindowStart: &old, WeeklyWindowStart: &old, MonthlyWindowStart: &old}
	require.NoError(t, cache.IncrementEnterpriseQuotaUsage(ctx, q.ScopeType, q.ScopeID, 1, now))
	require.False(t, mr.Exists(enterpriseBillingQuotaKey(q.ScopeType, q.ScopeID)))
	require.NoError(t, cache.SetEnterpriseQuota(ctx, q))
	require.NoError(t, cache.IncrementEnterpriseQuotaUsage(ctx, q.ScopeType, q.ScopeID, 1, now))
	scopes := []service.EnterpriseQuotaScope{{ScopeType: q.ScopeType, ScopeID: q.ScopeID}, {ScopeType: service.EnterpriseScopeEnterprise, ScopeID: 10}}
	quotas, err := cache.GetEnterpriseQuotas(ctx, 10, scopes)
	require.NoError(t, err)
	require.Len(t, quotas, 1)
	got := quotas[0]
	require.Equal(t, q.DailyLimitUSD, got.DailyLimitUSD)
	require.Nil(t, got.MonthlyLimitUSD)
	require.Equal(t, 1.0, got.DailyUsageUSD)
	require.Equal(t, 1.0, got.WeeklyUsageUSD)
	require.Equal(t, 1.0, got.MonthlyUsageUSD)
	require.Equal(t, 5.0, got.TotalUsageUSD)
	require.True(t, timezone.StartOfMonth(now).Equal(*got.MonthlyWindowStart))
	require.NoError(t, cache.IncrementEnterpriseQuotaUsage(ctx, q.ScopeType, q.ScopeID, 2, now))
	quotas, err = cache.GetEnterpriseQuotas(ctx, 10, scopes)
	require.NoError(t, err)
	require.Equal(t, 3.0, quotas[0].MonthlyUsageUSD)
	require.Equal(t, 7.0, quotas[0].TotalUsageUSD)
	require.NoError(t, cache.InvalidateEnterpriseQuota(ctx, q.ScopeType, q.ScopeID))
	quotas, err = cache.GetEnterpriseQuotas(ctx, 10, scopes)
	require.NoError(t, err)
	require.Empty(t, quotas)
}

func TestEnterpriseBillingCacheCorruptQuotaFails(t *testing.T) {
	_, cache := newEnterpriseCacheTest(t)
	ctx := context.Background()
	require.NoError(t, cache.rdb.HSet(ctx, enterpriseBillingQuotaKey("member", 30), "version", "1", "id", "1", "enterprise_id", "10", "scope_type", "member", "scope_id", "30").Err())
	_, err := cache.GetEnterpriseQuotas(ctx, 10, []service.EnterpriseQuotaScope{{ScopeType: "member", ScopeID: 30}})
	require.Error(t, err)
}

func enterpriseInflightUsers() (*service.User, *service.User) {
	one := &service.User{ID: 1, Enterprise: &service.EnterpriseMemberContext{EnterpriseID: 10, MemberID: 30, EnterpriseStatus: service.EnterpriseStatusActive, MemberStatus: service.EnterpriseMemberStatusActive}}
	two := &service.User{ID: 2, Enterprise: &service.EnterpriseMemberContext{EnterpriseID: 10, MemberID: 31, EnterpriseStatus: service.EnterpriseStatusActive, MemberStatus: service.EnterpriseMemberStatusActive}}
	return one, two
}

func TestEnterpriseInflightSharesBalanceAcrossMembers(t *testing.T) {
	_, cache := newEnterpriseCacheTest(t)
	ctx := context.Background()
	require.NoError(t, cache.SetEnterpriseBalance(ctx, 10, 1))
	cfg := &config.Config{}
	cfg.Billing.InflightReservation.Enabled = true
	billing := &billingCache{rdb: cache.rdb}
	svc := service.NewBillingCacheService(billing, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(svc.Stop)
	svc.ConfigureEnterpriseBilling(cache, nil, nil)
	one, two := enterpriseInflightUsers()
	res, err := svc.ReserveInflight(ctx, one, nil, nil, 0.6)
	require.NoError(t, err)
	require.NotNil(t, res)
	_, err = svc.ReserveInflight(ctx, two, nil, nil, 0.6)
	require.ErrorIs(t, err, service.ErrEnterpriseBillingBalanceInsufficient)
	zkey, _ := billingInflightScopeKeys("ent:10")
	n, err := cache.rdb.ZCard(ctx, zkey).Result()
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	personal, _ := billingInflightKeys(one.ID)
	require.Zero(t, cache.rdb.Exists(ctx, personal).Val())
	res.Release()
	res, err = svc.ReserveInflight(ctx, two, nil, nil, 0.6)
	require.NoError(t, err)
	res.Release()
}

func TestEnterpriseInflightConcurrentMembers(t *testing.T) {
	_, cache := newEnterpriseCacheTest(t)
	ctx := context.Background()
	require.NoError(t, cache.SetEnterpriseBalance(ctx, 10, 1))
	cfg := &config.Config{}
	cfg.Billing.InflightReservation.Enabled = true
	svc := service.NewBillingCacheService(&billingCache{rdb: cache.rdb}, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(svc.Stop)
	svc.ConfigureEnterpriseBilling(cache, nil, nil)
	var wg sync.WaitGroup
	reservations := make(chan *service.InflightReservation, 20)
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			user, _ := enterpriseInflightUsers()
			res, err := svc.ReserveInflight(ctx, user, nil, nil, 0.4)
			if err != nil {
				errs <- err
			} else {
				reservations <- res
			}
		}()
	}
	wg.Wait()
	close(reservations)
	close(errs)
	admitted := 0
	for res := range reservations {
		admitted++
		res.Release()
	}
	require.LessOrEqual(t, admitted, 2)
	for err := range errs {
		require.ErrorIs(t, err, service.ErrEnterpriseBillingBalanceInsufficient)
	}
}

func TestEnterpriseInflightRedisReservationDownFailsOpen(t *testing.T) {
	mr, cache := newEnterpriseCacheTest(t)
	_, balanceCache := newEnterpriseCacheTest(t)
	ctx := context.Background()
	require.NoError(t, balanceCache.SetEnterpriseBalance(ctx, 10, 1))
	cfg := &config.Config{}
	cfg.Billing.InflightReservation.Enabled = true
	svc := service.NewBillingCacheService(&billingCache{rdb: cache.rdb}, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(svc.Stop)
	svc.ConfigureEnterpriseBilling(balanceCache, nil, nil)
	mr.Close()
	user, _ := enterpriseInflightUsers()
	res, err := svc.ReserveInflight(ctx, user, nil, nil, 0.1)
	require.NoError(t, err)
	require.Nil(t, res)
}

func TestEnterpriseReserveFixedChargeUsesEnterpriseBalanceAndQuotas(t *testing.T) {
	_, cache := newEnterpriseCacheTest(t)
	ctx := context.Background()
	require.NoError(t, cache.SetEnterpriseBalance(ctx, 10, 1))
	user, _ := enterpriseInflightUsers()
	limit := 0.5
	for _, q := range []*service.EnterpriseQuota{{ID: 1, EnterpriseID: 10, ScopeType: "enterprise", ScopeID: 10}, {ID: 2, EnterpriseID: 10, ScopeType: "member", ScopeID: 30, TotalLimitUSD: &limit}} {
		require.NoError(t, cache.SetEnterpriseQuota(ctx, q))
	}
	cfg := &config.Config{}
	cfg.Billing.InflightReservation.Enabled = true
	svc := service.NewBillingCacheService(&billingCache{rdb: cache.rdb}, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(svc.Stop)
	svc.ConfigureEnterpriseBilling(cache, nil, nil)
	_, err := svc.ReserveFixedCharge(ctx, user, nil, &service.Group{ID: 5}, nil, 0.6)
	require.ErrorIs(t, err, service.ErrEnterpriseBillingMemberQuotaExceeded)
	res, err := svc.ReserveFixedCharge(ctx, user, nil, &service.Group{ID: 5}, nil, 0.4)
	require.NoError(t, err)
	require.NotNil(t, res)
	res.Release()
	_, err = svc.ReserveFixedCharge(ctx, user, nil, &service.Group{ID: 5}, nil, 2)
	require.ErrorIs(t, err, service.ErrEnterpriseBillingBalanceInsufficient)
}

func TestEnterpriseInflightRenewScopeAndNoResurrection(t *testing.T) {
	_, cache := newEnterpriseCacheTest(t)
	billing := &billingCache{rdb: cache.rdb}
	ctx := context.Background()
	_, err := billing.ReserveInflightScope(ctx, "ent:10", "r1", 1, time.Minute)
	require.NoError(t, err)
	alive, err := billing.RenewInflightScope(ctx, "ent:10", "r1", 2*time.Minute)
	require.NoError(t, err)
	require.True(t, alive)
	require.NoError(t, billing.ReleaseInflightScope(ctx, "ent:10", "r1"))
	alive, err = billing.RenewInflightScope(ctx, "ent:10", "r1", time.Minute)
	require.NoError(t, err)
	require.False(t, alive)
}

func TestEnterpriseSyncUsageOnlyExistingEntries(t *testing.T) {
	mr, cache := newEnterpriseCacheTest(t)
	ctx := context.Background()
	require.NoError(t, cache.SetEnterpriseBalance(ctx, 10, 5))
	now := timezone.Now()
	q := &service.EnterpriseQuota{ID: 1, EnterpriseID: 10, ScopeType: "enterprise", ScopeID: 10, DailyWindowStart: &now, WeeklyWindowStart: &now, MonthlyWindowStart: &now}
	require.NoError(t, cache.SetEnterpriseQuota(ctx, q))
	svc := &service.BillingCacheService{}
	svc.ConfigureEnterpriseBilling(cache, nil, nil)
	user, _ := enterpriseInflightUsers()
	svc.SyncEnterpriseUsage(ctx, user.Enterprise, 0.25)
	balance, err := cache.GetEnterpriseBalance(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 4.75, balance)
	quotas, err := cache.GetEnterpriseQuotas(ctx, 10, []service.EnterpriseQuotaScope{{ScopeType: "enterprise", ScopeID: 10}})
	require.NoError(t, err)
	require.Equal(t, 0.25, quotas[0].TotalUsageUSD)
	require.False(t, mr.Exists(enterpriseBillingQuotaKey("member", 30)))
	require.False(t, mr.Exists(billingBalanceKey(user.ID)))
}
