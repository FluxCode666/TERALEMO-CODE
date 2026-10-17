package service

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// EnterpriseAllowsGroupForUser applies the same enterprise access predicate at
// explicit group changes as authentication does at the original request group.
func EnterpriseAllowsGroupForUser(user *User, group *Group) bool {
	if user == nil || user.Enterprise == nil {
		return true
	}
	if group == nil || !user.Enterprise.AllowsGroup(group.ID) {
		return false
	}
	return group.IsSubscriptionType() || user.CanBindGroup(group.ID, group.IsExclusive)
}

// CheckEnterpriseSessionEligibility rechecks a frozen turn without incrementing
// request RPM. Monetary windows and payer eligibility still apply to each turn.
func (s *BillingCacheService) CheckEnterpriseSessionEligibility(ctx context.Context, user *User, apiKey *APIKey, group *Group, subscription *UserSubscription, platform string) error {
	if user == nil || user.Enterprise == nil {
		return nil
	}
	if s == nil {
		return ErrBillingServiceUnavailable
	}
	if err := checkEnterpriseGatewayEligibility(user.Enterprise, group); err != nil {
		return err
	}
	if group == nil || !EnterpriseAllowsGroupForUser(user, group) {
		return ErrEnterpriseGatewayGroupNotAllowed
	}
	if s.cfg != nil && s.cfg.RunMode == config.RunModeSimple {
		if s.cfg.SimpleModeKeyRateLimitEnabled {
			return s.checkSimpleModeAPIKeyRateLimits(ctx, apiKey)
		}
		return nil
	}
	if s.circuitBreaker != nil && !s.circuitBreaker.Allow() {
		return ErrBillingServiceUnavailable
	}
	isSubscription := group.IsSubscriptionType() && subscription != nil
	if isSubscription {
		if err := s.checkSubscriptionEligibility(ctx, user.ID, group, subscription); err != nil {
			return err
		}
	} else {
		if err := s.checkEnterpriseBillingEligibility(ctx, user.Enterprise, group, 0); err != nil {
			return err
		}
		if err := s.checkUserPlatformQuotaEligibility(ctx, user.ID, platform); err != nil {
			return err
		}
	}
	if apiKey != nil && apiKey.HasRateLimits() {
		return s.checkAPIKeyRateLimits(ctx, apiKey)
	}
	return nil
}

type enterpriseGatewayUserContextKey struct{}

// WithEnterpriseGatewayUser carries the authenticated user's enterprise scope
// into account selection, where a configured fallback can change the group.
// A refreshed personal user also clears a prior membership on long-lived turns.
func WithEnterpriseGatewayUser(ctx context.Context, user *User) context.Context {
	if user == nil || (user.Enterprise == nil && ctx.Value(enterpriseGatewayUserContextKey{}) == nil) {
		return ctx
	}
	return context.WithValue(ctx, enterpriseGatewayUserContextKey{}, user)
}

func (s *GatewayService) checkEnterpriseResolvedGroup(ctx context.Context, group *Group, originalGroupID int64) error {
	user, _ := ctx.Value(enterpriseGatewayUserContextKey{}).(*User)
	if user == nil || user.Enterprise == nil {
		return nil
	}
	member := user.Enterprise
	if member.EnterpriseStatus != EnterpriseStatusActive {
		return ErrEnterpriseGatewayDisabled
	}
	if member.MemberStatus != EnterpriseMemberStatusActive {
		return ErrEnterpriseGatewayMemberDisabled
	}
	if !EnterpriseAllowsGroupForUser(user, group) {
		return ErrEnterpriseGatewayGroupNotAllowed
	}
	if !group.IsSubscriptionType() {
		return nil
	}
	// The original subscription was already authenticated. A fallback subscription
	// must not obtain access merely by being the target of an administrator's route.
	if group.ID != originalGroupID {
		if s.userSubRepo == nil {
			return ErrEnterpriseGatewayGroupNotAllowed
		}
		sub, err := s.userSubRepo.GetActiveByUserIDAndGroupID(ctx, user.ID, group.ID)
		if err != nil || sub == nil || !sub.IsActive() {
			return ErrEnterpriseGatewayGroupNotAllowed
		}
	}
	return nil
}
