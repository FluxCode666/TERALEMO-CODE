//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/stretchr/testify/require"
)

type enterpriseRuntimeCacheStub struct {
	EnterpriseBillingRuntimeCache
	balance                                          float64
	balanceErr, quotaErr                             error
	quotas                                           []EnterpriseQuota
	balanceReads, quotaReads, balanceSets, quotaSets int
}

func (c *enterpriseRuntimeCacheStub) GetEnterpriseBalance(context.Context, int64) (float64, error) {
	c.balanceReads++
	return c.balance, c.balanceErr
}
func (c *enterpriseRuntimeCacheStub) GetEnterpriseQuotas(context.Context, int64, []EnterpriseQuotaScope) ([]EnterpriseQuota, error) {
	c.quotaReads++
	return c.quotas, c.quotaErr
}
func (c *enterpriseRuntimeCacheStub) SetEnterpriseBalance(context.Context, int64, float64) error {
	c.balanceSets++
	return nil
}
func (c *enterpriseRuntimeCacheStub) SetEnterpriseQuota(context.Context, *EnterpriseQuota) error {
	c.quotaSets++
	return nil
}

type enterpriseBalanceReaderStub struct {
	balance float64
	err     error
	calls   int
}

func (r *enterpriseBalanceReaderStub) GetBalance(context.Context, int64) (float64, error) {
	r.calls++
	return r.balance, r.err
}

type enterpriseQuotaReaderStub struct {
	quotas []EnterpriseQuota
	err    error
	calls  int
	scopes []EnterpriseQuotaScope
}

func (r *enterpriseQuotaReaderStub) GetByScopes(_ context.Context, _ int64, scopes []EnterpriseQuotaScope) ([]EnterpriseQuota, error) {
	r.calls++
	r.scopes = scopes
	return r.quotas, r.err
}

func enterpriseEligibilityFixture() (*User, []EnterpriseQuota) {
	departmentID := int64(20)
	member := &EnterpriseMemberContext{EnterpriseID: 10, MemberID: 30, DepartmentID: &departmentID, EnterpriseStatus: EnterpriseStatusActive, MemberStatus: EnterpriseMemberStatusActive}
	quotas := make([]EnterpriseQuota, 0, 3)
	for i, scope := range enterpriseBillingScopes(member) {
		quotas = append(quotas, EnterpriseQuota{ID: int64(i + 1), EnterpriseID: 10, ScopeType: scope.ScopeType, ScopeID: scope.ScopeID})
	}
	return &User{ID: 1, Enterprise: member}, quotas
}

func TestEnterpriseBillingEligibilityBalanceAndAccess(t *testing.T) {
	for _, tc := range []struct {
		name             string
		balance, reserve float64
		modify           func(*User)
		want             error
	}{
		{name: "personal zero ignored", balance: 50},
		{name: "enterprise zero", want: ErrEnterpriseBillingBalanceInsufficient},
		{name: "enterprise negative", balance: -1, want: ErrEnterpriseBillingBalanceInsufficient},
		{name: "reserve threshold", balance: 2, reserve: 3, want: ErrEnterpriseBillingBalanceInsufficient},
		{name: "disabled enterprise", balance: 50, modify: func(u *User) { u.Enterprise.EnterpriseStatus = EnterpriseStatusDisabled }, want: ErrEnterpriseGatewayDisabled},
		{name: "disabled member", balance: 50, modify: func(u *User) { u.Enterprise.MemberStatus = EnterpriseMemberStatusDisabled }, want: ErrEnterpriseGatewayMemberDisabled},
		{name: "group denied", balance: 50, modify: func(u *User) { u.Enterprise.GroupWhitelistConfigured = true }, want: ErrEnterpriseGatewayGroupNotAllowed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			user, quotas := enterpriseEligibilityFixture()
			if tc.modify != nil {
				tc.modify(user)
			}
			cache := &enterpriseRuntimeCacheStub{balance: tc.balance, quotas: quotas}
			svc := &BillingCacheService{cfg: &config.Config{}, enterpriseCache: cache}
			svc.cfg.Billing.MinimumBalanceReserve = tc.reserve
			err := svc.CheckBillingEligibility(context.Background(), user, nil, &Group{ID: 5}, nil, "")
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestEnterpriseBillingEligibilityAllQuotaScopesAndWindows(t *testing.T) {
	now := timezone.Now()
	for _, scope := range []string{EnterpriseScopeMember, EnterpriseScopeDepartment, EnterpriseScopeEnterprise} {
		for _, window := range []string{EnterpriseQuotaWindowDaily, EnterpriseQuotaWindowWeekly, EnterpriseQuotaWindowMonthly, EnterpriseQuotaWindowTotal} {
			t.Run(scope+"/"+window, func(t *testing.T) {
				user, quotas := enterpriseEligibilityFixture()
				limit := 1.0
				for i := range quotas {
					q := &quotas[i]
					if q.ScopeType != scope {
						continue
					}
					switch window {
					case EnterpriseQuotaWindowDaily:
						q.DailyLimitUSD = &limit
						q.DailyUsageUSD = 1
						q.DailyWindowStart = &now
					case EnterpriseQuotaWindowWeekly:
						q.WeeklyLimitUSD = &limit
						q.WeeklyUsageUSD = 1
						q.WeeklyWindowStart = &now
					case EnterpriseQuotaWindowMonthly:
						q.MonthlyLimitUSD = &limit
						q.MonthlyUsageUSD = 1
						q.MonthlyWindowStart = &now
					case EnterpriseQuotaWindowTotal:
						q.TotalLimitUSD = &limit
						q.TotalUsageUSD = 1
					}
				}
				svc := &BillingCacheService{enterpriseCache: &enterpriseRuntimeCacheStub{balance: 50, quotas: quotas}}
				err := svc.CheckBillingEligibility(context.Background(), user, nil, &Group{ID: 5}, nil, "")
				want := ErrEnterpriseBillingQuotaExceeded
				if scope == EnterpriseScopeMember {
					want = ErrEnterpriseBillingMemberQuotaExceeded
				}
				if scope == EnterpriseScopeDepartment {
					want = ErrEnterpriseBillingDepartmentQuotaExceeded
				}
				require.ErrorIs(t, err, want)
				md := infraerrors.FromError(err).Metadata
				require.Equal(t, scope, md["scope"])
				require.Equal(t, window, md["window"])
				if window == EnterpriseQuotaWindowTotal {
					require.NotContains(t, md, "window_resets_at")
				} else {
					reset, err := time.Parse(time.RFC3339, md["window_resets_at"])
					require.NoError(t, err)
					require.True(t, reset.After(now))
				}
			})
		}
	}
}

func TestEnterpriseBillingQuotaNaturalWindowExpiry(t *testing.T) {
	// A time at the end of March must use April 1, not a rolling 30-day window.
	now := time.Date(2026, time.March, 31, 13, 0, 0, 0, timezone.Location())
	old := time.Date(2026, time.February, 28, 13, 0, 0, 0, timezone.Location())
	limit := 1.0
	q := &EnterpriseQuota{ScopeType: EnterpriseScopeMember, DailyLimitUSD: &limit, WeeklyLimitUSD: &limit, MonthlyLimitUSD: &limit, DailyUsageUSD: 5, WeeklyUsageUSD: 5, MonthlyUsageUSD: 5, DailyWindowStart: &old, WeeklyWindowStart: &old, MonthlyWindowStart: &old}
	require.NoError(t, checkEnterpriseQuota(q, now, 0))
	q.MonthlyWindowStart = &now
	err := checkEnterpriseQuota(q, now, 0)
	require.Equal(t, timezone.StartOfMonth(now).AddDate(0, 1, 0).Format(time.RFC3339), infraerrors.FromError(err).Metadata["window_resets_at"])
	zero := 0.0
	q = &EnterpriseQuota{ScopeType: EnterpriseScopeMember, MonthlyLimitUSD: &zero}
	require.ErrorIs(t, checkEnterpriseQuota(q, now, 0), ErrEnterpriseBillingMemberQuotaExceeded, "expired or uninitialized zero-limit window still disables usage")
	q = &EnterpriseQuota{ScopeType: EnterpriseScopeMember, TotalLimitUSD: &limit, TotalUsageUSD: 5, DailyWindowStart: &old}
	require.ErrorIs(t, checkEnterpriseQuota(q, now, 0), ErrEnterpriseBillingMemberQuotaExceeded, "total never resets")
}

func TestEnterpriseBillingEligibilityCacheFallbackFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name            string
		cacheErr, dbErr error
		missing         bool
		want            error
	}{
		{name: "cache miss falls back", cacheErr: errors.New("miss")},
		{name: "redis failure falls back", cacheErr: errors.New("redis down")},
		{name: "partial cache reloads all scopes", missing: true},
		{name: "both fail", cacheErr: errors.New("redis down"), dbErr: errors.New("db down"), want: ErrBillingServiceUnavailable},
		{name: "missing required db quota", cacheErr: errors.New("miss"), missing: true, want: ErrBillingServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			user, quotas := enterpriseEligibilityFixture()
			cache := &enterpriseRuntimeCacheStub{balance: 50, quotas: quotas, balanceErr: tc.cacheErr, quotaErr: tc.cacheErr}
			balanceRepo := &enterpriseBalanceReaderStub{balance: 50, err: tc.dbErr}
			quotaRepo := &enterpriseQuotaReaderStub{quotas: quotas, err: tc.dbErr}
			if tc.missing {
				cache.quotas = quotas[:2]
				if tc.want != nil {
					quotaRepo.quotas = quotas[:2]
				}
			}
			svc := &BillingCacheService{}
			svc.ConfigureEnterpriseBilling(cache, balanceRepo, quotaRepo)
			err := svc.CheckBillingEligibility(context.Background(), user, nil, &Group{ID: 5}, nil, "")
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
			} else {
				require.NoError(t, err)
				require.Equal(t, 1, quotaRepo.calls)
				require.Len(t, quotaRepo.scopes, 3)
				require.Equal(t, 3, cache.quotaSets)
			}
		})
	}
}

type enterprisePersonalCacheStub struct {
	BillingCache
	balance      float64
	reads        int
	subscription *SubscriptionCacheData
	rate         *APIKeyRateLimitCacheData
}

func (c *enterprisePersonalCacheStub) GetUserBalance(context.Context, int64) (float64, error) {
	c.reads++
	return c.balance, nil
}
func (c *enterprisePersonalCacheStub) GetSubscriptionCache(context.Context, int64, int64) (*SubscriptionCacheData, error) {
	return c.subscription, nil
}
func (c *enterprisePersonalCacheStub) GetAPIKeyRateLimit(context.Context, int64) (*APIKeyRateLimitCacheData, error) {
	return c.rate, nil
}

func TestEnterpriseBillingEligibilityNonmemberAndSubscriptionUnchanged(t *testing.T) {
	for _, enterprise := range []bool{false, true} {
		t.Run(fmt.Sprint(enterprise), func(t *testing.T) {
			cache := &enterpriseRuntimeCacheStub{balanceErr: errors.New("must not read enterprise"), quotaErr: errors.New("must not read enterprise")}
			personal := &enterprisePersonalCacheStub{balance: 5, subscription: &SubscriptionCacheData{Status: SubscriptionStatusActive, ExpiresAt: time.Now().Add(time.Hour)}}
			svc := &BillingCacheService{cache: personal, enterpriseCache: cache}
			user := &User{ID: 1}
			var group *Group
			var subscription *UserSubscription
			if enterprise {
				user, _ = enterpriseEligibilityFixture()
				group = &Group{ID: 5, SubscriptionType: SubscriptionTypeSubscription}
				subscription = &UserSubscription{ID: 3}
			}
			require.NoError(t, svc.CheckBillingEligibility(context.Background(), user, nil, group, subscription, ""))
			require.Zero(t, cache.balanceReads)
			require.Zero(t, cache.quotaReads)
			if enterprise {
				require.Zero(t, personal.reads)
			} else {
				require.Equal(t, 1, personal.reads)
			}
		})
	}
}

func TestEnterpriseBillingEligibilityPersonalRPMStillEnforced(t *testing.T) {
	user, quotas := enterpriseEligibilityFixture()
	user.RPMLimit = 1
	svc := &BillingCacheService{enterpriseCache: &enterpriseRuntimeCacheStub{balance: 50, quotas: quotas}, userRPMCache: &userRPMCacheStub{userCounts: []int{2}}}
	require.ErrorIs(t, svc.CheckBillingEligibility(context.Background(), user, nil, &Group{ID: 5}, nil, ""), ErrUserRPMExceeded)
}

func TestEnterpriseBillingEligibilityPersonalPlatformAndKeyStillEnforced(t *testing.T) {
	user, quotas := enterpriseEligibilityFixture()
	enterpriseCache := &enterpriseRuntimeCacheStub{balance: 50, quotas: quotas}
	platformCache := &fakeZeroQuotaCache{}
	svc := &BillingCacheService{cache: platformCache, enterpriseCache: enterpriseCache, userPlatformQuotaRepo: &fakeQuotaRepo{}, cfg: &config.Config{}}
	require.ErrorIs(t, svc.CheckBillingEligibility(context.Background(), user, nil, &Group{ID: 5}, nil, "anthropic"), ErrUserPlatformDailyQuotaExhausted)
	require.True(t, platformCache.called)
	now := time.Now().Unix()
	personal := &enterprisePersonalCacheStub{rate: &APIKeyRateLimitCacheData{Usage5h: 2, Window5h: now, Window1d: now, Window7d: now}}
	svc = &BillingCacheService{cache: personal, enterpriseCache: enterpriseCache}
	require.ErrorIs(t, svc.CheckBillingEligibility(context.Background(), user, &APIKey{ID: 2, RateLimit5h: 1}, &Group{ID: 5}, nil, ""), ErrAPIKeyRateLimit5hExceeded)
}

func TestEnterpriseBillingAccessChecksBeforeSubscriptionAndSimpleBypass(t *testing.T) {
	for _, mode := range []string{"simple", "subscription"} {
		for _, tc := range []struct {
			name   string
			modify func(*EnterpriseMemberContext)
			want   error
		}{
			{name: "enterprise disabled", modify: func(m *EnterpriseMemberContext) { m.EnterpriseStatus = EnterpriseStatusDisabled }, want: ErrEnterpriseGatewayDisabled},
			{name: "member disabled", modify: func(m *EnterpriseMemberContext) { m.MemberStatus = EnterpriseMemberStatusDisabled }, want: ErrEnterpriseGatewayMemberDisabled},
			{name: "group denied", modify: func(m *EnterpriseMemberContext) { m.GroupPoolConfigured = true }, want: ErrEnterpriseGatewayGroupNotAllowed},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				user, _ := enterpriseEligibilityFixture()
				tc.modify(user.Enterprise)
				svc := &BillingCacheService{cfg: &config.Config{}}
				group := &Group{ID: 5}
				var subscription *UserSubscription
				if mode == "simple" {
					svc.cfg.RunMode = config.RunModeSimple
				} else {
					group.SubscriptionType = SubscriptionTypeSubscription
					subscription = &UserSubscription{ID: 1}
				}
				require.ErrorIs(t, svc.CheckBillingEligibility(context.Background(), user, nil, group, subscription, ""), tc.want)
				_, err := svc.ReserveFixedCharge(context.Background(), user, nil, group, subscription, 1)
				require.ErrorIs(t, err, tc.want)
			})
		}
	}
}

func TestEnterpriseBillingUngroupedKeyCannotBypassRestrictions(t *testing.T) {
	for _, configured := range []string{"none", "pool", "whitelist"} {
		for _, invalidGroup := range []struct {
			name  string
			group *Group
		}{
			{name: "missing"},
			{name: "zero", group: &Group{}},
			{name: "negative", group: &Group{ID: -1}},
		} {
			for _, mode := range []string{"balance", "simple", "subscription"} {
				t.Run(configured+"/"+invalidGroup.name+"/"+mode, func(t *testing.T) {
					user, _ := enterpriseEligibilityFixture()
					user.Enterprise.GroupPoolConfigured = configured == "pool"
					user.Enterprise.GroupWhitelistConfigured = configured == "whitelist"
					cache := &enterpriseRuntimeCacheStub{balanceErr: errors.New("must not read balance"), quotaErr: errors.New("must not read quota")}
					svc := &BillingCacheService{cfg: &config.Config{}, enterpriseCache: cache}
					var subscription *UserSubscription
					if mode == "simple" {
						svc.cfg.RunMode = config.RunModeSimple
					} else if mode == "subscription" {
						subscription = &UserSubscription{ID: 1}
					}
					group := invalidGroup.group
					if group != nil && mode == "subscription" {
						group = &Group{ID: group.ID, SubscriptionType: SubscriptionTypeSubscription}
					}
					require.ErrorIs(t, checkEnterpriseGatewayEligibility(user.Enterprise, group), ErrEnterpriseGatewayGroupNotAllowed)
					require.ErrorIs(t, svc.CheckBillingEligibility(context.Background(), user, nil, group, subscription, ""), ErrEnterpriseGatewayGroupNotAllowed)
					res, err := svc.ReserveFixedCharge(context.Background(), user, nil, group, subscription, 1)
					require.ErrorIs(t, err, ErrEnterpriseGatewayGroupNotAllowed)
					require.Nil(t, res)
					require.Zero(t, cache.balanceReads)
					require.Zero(t, cache.quotaReads)
				})
			}
		}
	}
}
