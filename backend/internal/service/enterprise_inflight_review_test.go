//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type enterpriseScopeReviewCache struct {
	BillingCache
	mu               sync.Mutex
	reserveErr       error
	pending          float64
	reservations     map[string]string
	releases, renews int
	lastScope        string
	balanceMiss      bool
}

func (c *enterpriseScopeReviewCache) ReserveEnterpriseInflight(ctx context.Context, enterpriseID int64, id string, amount float64, ttl time.Duration) (bool, bool, error) {
	if c.balanceMiss {
		return false, false, nil
	}
	pending, err := c.ReserveInflightScope(ctx, fmt.Sprintf("ent:%d", enterpriseID), id, amount, ttl)
	return true, 50-pending >= amount, err
}

func (c *enterpriseScopeReviewCache) ReserveInflightScope(_ context.Context, scope, id string, _ float64, _ time.Duration) (float64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.reservations == nil {
		c.reservations = make(map[string]string)
	}
	c.reservations[id] = scope // The Redis write may succeed even if its reply is lost.
	c.lastScope = scope
	return c.pending, c.reserveErr
}
func (c *enterpriseScopeReviewCache) ReleaseInflightScope(_ context.Context, scope, id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.releases++
	c.lastScope = scope
	delete(c.reservations, id)
	return nil
}
func (c *enterpriseScopeReviewCache) RenewInflightScope(_ context.Context, scope, id string, _ time.Duration) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.renews++
	c.lastScope = scope
	return c.reservations[id] == scope, nil
}
func (c *enterpriseScopeReviewCache) state() (active, releases, renews int, scope string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.reservations), c.releases, c.renews, c.lastScope
}

func newEnterpriseInflightReviewService(t *testing.T, cache BillingCache, balanceCache EnterpriseBillingCache, enabled bool) *BillingCacheService {
	t.Helper()
	cfg := &config.Config{}
	cfg.Billing.InflightReservation.Enabled = enabled
	cfg.Billing.InflightReservation.TTLSeconds = 1
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(svc.Stop)
	svc.ConfigureEnterpriseBilling(balanceCache, nil, nil)
	return svc
}

func TestEnterpriseInflightReservationDisabledOrUnavailableFailsOpen(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enabled bool
		cache   BillingCache
	}{
		{name: "disabled", cache: &enterpriseScopeReviewCache{}},
		{name: "unsupported cache", enabled: true, cache: &billingCacheWorkerStub{}},
		{name: "no cache", enabled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := newEnterpriseInflightReviewService(t, tc.cache, nil, tc.enabled)
			user, _ := enterpriseEligibilityFixture()
			res, err := svc.ReserveInflight(context.Background(), user, &Group{ID: 5}, nil, 1)
			require.NoError(t, err)
			require.Nil(t, res)
			res.Release()
			res.HandlerDone()
			res.Acquire()()
		})
	}
}

func TestEnterpriseInflightReservationRedisFailureReleasesAndFailsOpen(t *testing.T) {
	cache := &enterpriseScopeReviewCache{reserveErr: errors.New("redis reply lost")}
	svc := newEnterpriseInflightReviewService(t, cache, &enterpriseRuntimeCacheStub{balance: 50}, true)
	user, _ := enterpriseEligibilityFixture()
	res, err := svc.ReserveInflight(context.Background(), user, &Group{ID: 5}, nil, 1)
	require.NoError(t, err)
	require.Nil(t, res)
	active, releases, renews, scope := cache.state()
	require.Zero(t, active)
	require.Equal(t, 1, releases)
	require.Zero(t, renews)
	require.Equal(t, "ent:10", scope)
}

func TestEnterpriseInflightReservationBalanceReadStillFailsClosed(t *testing.T) {
	cache := &enterpriseScopeReviewCache{balanceMiss: true}
	svc := newEnterpriseInflightReviewService(t, cache, &enterpriseRuntimeCacheStub{balanceErr: errors.New("redis down")}, true)
	user, _ := enterpriseEligibilityFixture()
	res, err := svc.ReserveInflight(context.Background(), user, &Group{ID: 5}, nil, 1)
	require.ErrorIs(t, err, ErrBillingServiceUnavailable)
	require.Nil(t, res)
	active, releases, _, _ := cache.state()
	require.Zero(t, active)
	require.Zero(t, releases)
}

func TestEnterpriseFixedChargeReservationFailureDoesNotBypassEligibility(t *testing.T) {
	user, quotas := enterpriseEligibilityFixture()
	cache := &enterpriseScopeReviewCache{reserveErr: errors.New("redis down")}
	enterpriseCache := &enterpriseRuntimeCacheStub{balance: 50, quotas: quotas}
	svc := newEnterpriseInflightReviewService(t, cache, enterpriseCache, true)
	res, err := svc.ReserveFixedCharge(context.Background(), user, nil, &Group{ID: 5}, nil, 1)
	require.NoError(t, err)
	require.Nil(t, res)
	enterpriseCache.quotaErr = errors.New("quota unavailable")
	_, err = svc.ReserveFixedCharge(context.Background(), user, nil, &Group{ID: 5}, nil, 1)
	require.ErrorIs(t, err, ErrBillingServiceUnavailable)
	enterpriseCache.quotaErr = nil
	enterpriseCache.balanceErr = errors.New("balance unavailable")
	_, err = svc.ReserveFixedCharge(context.Background(), user, nil, &Group{ID: 5}, nil, 1)
	require.ErrorIs(t, err, ErrBillingServiceUnavailable)
}

func TestEnterpriseInflightReservationFrozenScopeRenewsAndReleasesOnce(t *testing.T) {
	cache := &enterpriseScopeReviewCache{}
	svc := newEnterpriseInflightReviewService(t, cache, &enterpriseRuntimeCacheStub{balance: 50}, true)
	user, _ := enterpriseEligibilityFixture()
	res, err := svc.ReserveInflight(context.Background(), user, &Group{ID: 5}, nil, 1)
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Eventually(t, func() bool { _, _, renews, scope := cache.state(); return renews > 0 && scope == "ent:10" }, 2*time.Second, 10*time.Millisecond)
	user.Enterprise.EnterpriseID = 999 // Releasing must never recompute scope from mutable membership.
	done := res.Acquire()
	res.HandlerDone()
	active, releases, renews, _ := cache.state()
	require.Equal(t, 1, active)
	require.Zero(t, releases)
	done()
	done()
	res.Release()
	res.HandlerDone()
	active, releases, renewsAfter, scope := cache.state()
	require.Zero(t, active)
	require.Equal(t, 1, releases)
	require.Equal(t, renews, renewsAfter)
	require.Equal(t, "ent:10", scope)
}

func TestEnterpriseInflightReservationInsufficientFundsReleasesOnce(t *testing.T) {
	cache := &enterpriseScopeReviewCache{pending: 49.5}
	svc := newEnterpriseInflightReviewService(t, cache, &enterpriseRuntimeCacheStub{balance: 50}, true)
	user, _ := enterpriseEligibilityFixture()
	res, err := svc.ReserveInflight(context.Background(), user, &Group{ID: 5}, nil, 1)
	require.ErrorIs(t, err, ErrEnterpriseBillingBalanceInsufficient)
	require.Nil(t, res)
	active, releases, _, scope := cache.state()
	require.Zero(t, active)
	require.Equal(t, 1, releases)
	require.Equal(t, "ent:10", scope)
}
