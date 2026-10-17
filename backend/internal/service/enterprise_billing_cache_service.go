package service

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/google/uuid"
)

// EnterpriseBillingRuntimeCache keeps the management invalidation contract small.
// Missing quota rows are never cached as unlimited sentinels.
type EnterpriseBillingRuntimeCache interface {
	EnterpriseBillingCache
	GetEnterpriseBalance(context.Context, int64) (float64, error)
	SetEnterpriseBalance(context.Context, int64, float64) error
	DeductEnterpriseBalance(context.Context, int64, float64) error
	GetEnterpriseQuotas(context.Context, int64, []EnterpriseQuotaScope) ([]EnterpriseQuota, error)
	SetEnterpriseQuota(context.Context, *EnterpriseQuota) error
	IncrementEnterpriseQuotaUsage(context.Context, string, int64, float64, time.Time) error
}

type EnterpriseBalanceReader interface {
	GetBalance(context.Context, int64) (float64, error)
}
type EnterpriseQuotaReader interface {
	GetByScopes(context.Context, int64, []EnterpriseQuotaScope) ([]EnterpriseQuota, error)
}

// ConfigureEnterpriseBilling is called once by dependency injection, before serving.
// Separate narrow dependencies preserve existing constructor and test stubs.
func (s *BillingCacheService) ConfigureEnterpriseBilling(cache EnterpriseBillingCache, balanceRepo EnterpriseBalanceReader, quotaRepo EnterpriseQuotaReader) {
	s.enterpriseCache, _ = cache.(EnterpriseBillingRuntimeCache)
	s.enterpriseBalanceRepo = balanceRepo
	s.enterpriseQuotaRepo = quotaRepo
}

func (s *BillingCacheService) GetEnterpriseBalance(ctx context.Context, enterpriseID int64) (float64, error) {
	if enterpriseID <= 0 {
		return 0, fmt.Errorf("invalid enterprise id")
	}
	if s.enterpriseCache != nil {
		if balance, err := s.enterpriseCache.GetEnterpriseBalance(ctx, enterpriseID); err == nil && !math.IsNaN(balance) && !math.IsInf(balance, 0) {
			return balance, nil
		}
	}
	if s.enterpriseBalanceRepo == nil {
		return 0, errBillingCacheUnavailable
	}
	result := s.balanceLoadSF.DoChan("enterprise:"+strconv.FormatInt(enterpriseID, 10), func() (any, error) {
		loadCtx, cancel := context.WithTimeout(context.Background(), balanceLoadTimeout)
		defer cancel()
		balance, err := s.enterpriseBalanceRepo.GetBalance(loadCtx, enterpriseID)
		if err != nil {
			return nil, err
		}
		if math.IsNaN(balance) || math.IsInf(balance, 0) {
			return nil, fmt.Errorf("invalid enterprise balance")
		}
		if s.enterpriseCache != nil {
			if err := s.enterpriseCache.SetEnterpriseBalance(loadCtx, enterpriseID, balance); err != nil {
				logger.LegacyPrintf("service.billing_cache", "Warning: cache enterprise balance %d: %v", enterpriseID, err)
			}
		}
		return balance, nil
	})
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case r := <-result:
		if r.Err != nil {
			return 0, r.Err
		}
		return r.Val.(float64), nil
	}
}

func enterpriseBillingScopes(member *EnterpriseMemberContext) []EnterpriseQuotaScope {
	scopes := []EnterpriseQuotaScope{{ScopeType: EnterpriseScopeMember, ScopeID: member.MemberID}}
	if member.DepartmentID != nil {
		scopes = append(scopes, EnterpriseQuotaScope{ScopeType: EnterpriseScopeDepartment, ScopeID: *member.DepartmentID})
	}
	return append(scopes, EnterpriseQuotaScope{ScopeType: EnterpriseScopeEnterprise, ScopeID: member.EnterpriseID})
}

func validateEnterpriseBillingQuotas(enterpriseID int64, scopes []EnterpriseQuotaScope, quotas []EnterpriseQuota) (map[EnterpriseQuotaScope]*EnterpriseQuota, error) {
	result := make(map[EnterpriseQuotaScope]*EnterpriseQuota, len(quotas))
	for i := range quotas {
		q := &quotas[i]
		if q.EnterpriseID != enterpriseID || q.DeletedAt != nil {
			return nil, fmt.Errorf("enterprise quota scope mismatch")
		}
		for _, v := range []*float64{q.DailyLimitUSD, q.WeeklyLimitUSD, q.MonthlyLimitUSD, q.TotalLimitUSD} {
			if v != nil && (*v < 0 || math.IsNaN(*v) || math.IsInf(*v, 0)) {
				return nil, fmt.Errorf("invalid enterprise quota limit")
			}
		}
		for _, v := range []float64{q.DailyUsageUSD, q.WeeklyUsageUSD, q.MonthlyUsageUSD, q.TotalUsageUSD} {
			if v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
				return nil, fmt.Errorf("invalid enterprise quota usage")
			}
		}
		scope := EnterpriseQuotaScope{ScopeType: q.ScopeType, ScopeID: q.ScopeID}
		if _, exists := result[scope]; exists {
			return nil, fmt.Errorf("duplicate enterprise quota")
		}
		result[scope] = q
	}
	for _, scope := range scopes {
		if scope.ScopeID <= 0 || result[scope] == nil {
			return nil, fmt.Errorf("required enterprise quota missing: %s/%d", scope.ScopeType, scope.ScopeID)
		}
	}
	return result, nil
}

func (s *BillingCacheService) getEnterpriseBillingQuotas(ctx context.Context, member *EnterpriseMemberContext) (map[EnterpriseQuotaScope]*EnterpriseQuota, error) {
	scopes := enterpriseBillingScopes(member)
	if s.enterpriseCache != nil {
		if quotas, err := s.enterpriseCache.GetEnterpriseQuotas(ctx, member.EnterpriseID, scopes); err == nil {
			if result, err := validateEnterpriseBillingQuotas(member.EnterpriseID, scopes, quotas); err == nil {
				return result, nil
			}
		}
	}
	if s.enterpriseQuotaRepo == nil {
		return nil, errBillingCacheUnavailable
	}
	var key strings.Builder
	fmt.Fprintf(&key, "enterprise:%d", member.EnterpriseID)
	for _, scope := range scopes {
		fmt.Fprintf(&key, ":%s:%d", scope.ScopeType, scope.ScopeID)
	}
	result := s.quotaLoadSF.DoChan(key.String(), func() (any, error) {
		loadCtx, cancel := context.WithTimeout(context.Background(), balanceLoadTimeout)
		defer cancel()
		// A partial cache miss reloads the complete scope set in one database query.
		quotas, err := s.enterpriseQuotaRepo.GetByScopes(loadCtx, member.EnterpriseID, scopes)
		if err != nil {
			return nil, err
		}
		validated, err := validateEnterpriseBillingQuotas(member.EnterpriseID, scopes, quotas)
		if err != nil {
			return nil, err
		}
		if s.enterpriseCache != nil {
			for i := range quotas {
				if err := s.enterpriseCache.SetEnterpriseQuota(loadCtx, &quotas[i]); err != nil {
					logger.LegacyPrintf("service.billing_cache", "Warning: cache enterprise quota %s/%d: %v", quotas[i].ScopeType, quotas[i].ScopeID, err)
				}
			}
		}
		return validated, nil
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-result:
		if r.Err != nil {
			return nil, r.Err
		}
		return r.Val.(map[EnterpriseQuotaScope]*EnterpriseQuota), nil
	}
}

func checkEnterpriseGatewayEligibility(member *EnterpriseMemberContext, group *Group) error {
	if member.EnterpriseStatus != EnterpriseStatusActive {
		return ErrEnterpriseGatewayDisabled
	}
	if member.MemberStatus != EnterpriseMemberStatusActive {
		return ErrEnterpriseGatewayMemberDisabled
	}
	groupID := int64(0)
	if group != nil {
		groupID = group.ID
	}
	if !member.AllowsGroup(groupID) {
		return ErrEnterpriseGatewayGroupNotAllowed
	}
	return nil
}

func (s *BillingCacheService) checkEnterpriseBillingEligibility(ctx context.Context, member *EnterpriseMemberContext, group *Group, pending float64) error {
	if err := checkEnterpriseGatewayEligibility(member, group); err != nil {
		return err
	}
	balance, err := s.GetEnterpriseBalance(ctx, member.EnterpriseID)
	if err != nil {
		return ErrBillingServiceUnavailable.WithCause(err)
	}
	if s.balanceBelowEligibilityThreshold(balance) || (pending > 0 && balance < pending) {
		return ErrEnterpriseBillingBalanceInsufficient
	}
	quotas, err := s.getEnterpriseBillingQuotas(ctx, member)
	if err != nil {
		return ErrBillingServiceUnavailable.WithCause(err)
	}
	now := timezone.Now()
	for _, scope := range enterpriseBillingScopes(member) {
		if err := checkEnterpriseQuota(quotas[scope], now, pending); err != nil {
			return err
		}
	}
	return nil
}

func checkEnterpriseQuota(q *EnterpriseQuota, now time.Time, pending float64) error {
	windows := []struct {
		name           string
		limit          *float64
		usage          float64
		start          *time.Time
		current, reset time.Time
	}{
		{EnterpriseQuotaWindowDaily, q.DailyLimitUSD, q.DailyUsageUSD, q.DailyWindowStart, timezone.StartOfDay(now), nextDailyReset(now)},
		{EnterpriseQuotaWindowWeekly, q.WeeklyLimitUSD, q.WeeklyUsageUSD, q.WeeklyWindowStart, timezone.StartOfWeek(now), nextWeeklyReset(now)},
		{EnterpriseQuotaWindowMonthly, q.MonthlyLimitUSD, q.MonthlyUsageUSD, q.MonthlyWindowStart, timezone.StartOfMonth(now), timezone.StartOfMonth(now).AddDate(0, 1, 0)},
		{EnterpriseQuotaWindowTotal, q.TotalLimitUSD, q.TotalUsageUSD, nil, time.Time{}, time.Time{}},
	}
	for _, w := range windows {
		usage := w.usage
		if w.name != EnterpriseQuotaWindowTotal && quotaWindowExpired(w.start, w.current) {
			usage = 0
		}
		if w.limit == nil || (pending == 0 && usage < *w.limit) || (pending > 0 && usage+pending <= *w.limit) {
			continue
		}
		appErr := ErrEnterpriseBillingQuotaExceeded
		switch q.ScopeType {
		case EnterpriseScopeDepartment:
			appErr = ErrEnterpriseBillingDepartmentQuotaExceeded
		case EnterpriseScopeMember:
			appErr = ErrEnterpriseBillingMemberQuotaExceeded
		}
		md := map[string]string{"scope": q.ScopeType, "window": w.name}
		if !w.reset.IsZero() {
			md["window_resets_at"] = w.reset.Format(time.RFC3339)
		}
		return infraerrors.FromError(appErr).WithMetadata(md)
	}
	return nil
}

// SyncEnterpriseUsage runs only after a successful, non-replayed transaction.
// Lua updates existing entries only; on failure invalidate rather than risk a stale allowance.
func (s *BillingCacheService) SyncEnterpriseUsage(ctx context.Context, member *EnterpriseMemberContext, cost float64) {
	if s == nil || s.enterpriseCache == nil || member == nil || cost <= 0 {
		return
	}
	syncCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cacheWriteTimeout)
	defer cancel()
	if err := s.enterpriseCache.DeductEnterpriseBalance(syncCtx, member.EnterpriseID, cost); err != nil {
		_ = s.enterpriseCache.InvalidateEnterpriseBalance(syncCtx, member.EnterpriseID)
		logger.LegacyPrintf("service.billing_cache", "Warning: sync enterprise balance %d: %v", member.EnterpriseID, err)
	}
	now := timezone.Now()
	for _, scope := range enterpriseBillingScopes(member) {
		if err := s.enterpriseCache.IncrementEnterpriseQuotaUsage(syncCtx, scope.ScopeType, scope.ScopeID, cost, now); err != nil {
			_ = s.enterpriseCache.InvalidateEnterpriseQuota(syncCtx, scope.ScopeType, scope.ScopeID)
			logger.LegacyPrintf("service.billing_cache", "Warning: sync enterprise quota %s/%d: %v", scope.ScopeType, scope.ScopeID, err)
		}
	}
}

// InflightScopeReservationRenewer is optional for old scope-cache implementations.
// Redis implements it so enterprise reservations survive long streaming requests.
type InflightScopeReservationRenewer interface {
	RenewInflightScope(context.Context, string, string, time.Duration) (bool, error)
}

type enterpriseInflightRenewer struct {
	cache InflightScopeReservationRenewer
	scope string
}

func (r enterpriseInflightRenewer) RenewInflightBalance(ctx context.Context, _ int64, requestID string, ttl time.Duration) (bool, error) {
	return r.cache.RenewInflightScope(ctx, r.scope, requestID, ttl)
}

func (s *BillingCacheService) reserveEnterpriseInflight(ctx context.Context, user *User, amount float64, renew bool) (*InflightReservation, error) {
	cache, ok := s.cache.(InflightScopeReservationCache)
	if !ok || cache == nil {
		return nil, ErrBillingServiceUnavailable.WithCause(errBillingCacheUnavailable)
	}
	balance, err := s.GetEnterpriseBalance(ctx, user.Enterprise.EnterpriseID)
	if err != nil {
		return nil, ErrBillingServiceUnavailable.WithCause(err)
	}
	cfg, _ := s.inflightReservationConfig()
	ttl := defaultInflightReservationTTL
	if cfg.TTLSeconds > 0 {
		ttl = time.Duration(cfg.TTLSeconds) * time.Second
	}
	scope := "ent:" + strconv.FormatInt(user.Enterprise.EnterpriseID, 10)
	res := &InflightReservation{userID: user.ID, requestID: uuid.NewString(), amount: amount, ttl: ttl, scopeCache: cache, scopes: []string{scope}}
	res.refs.Store(1)
	reserveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), inflightReservationReserveTimeout)
	pending, err := cache.ReserveInflightScope(reserveCtx, scope, res.requestID, amount, ttl)
	cancel()
	if err != nil {
		res.Release()
		return nil, ErrBillingServiceUnavailable.WithCause(err)
	}
	if balance-pending < amount {
		res.Release()
		return nil, ErrEnterpriseBillingBalanceInsufficient
	}
	if r, ok := s.cache.(InflightScopeReservationRenewer); renew && ok {
		res.startRenewal(enterpriseInflightRenewer{cache: r, scope: scope})
	}
	return res, nil
}
