package service

import "context"

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
	if !member.AllowsGroup(group.ID) {
		return ErrEnterpriseGatewayGroupNotAllowed
	}
	if !group.IsSubscriptionType() {
		if !user.CanBindGroup(group.ID, group.IsExclusive) {
			return ErrEnterpriseGatewayGroupNotAllowed
		}
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
