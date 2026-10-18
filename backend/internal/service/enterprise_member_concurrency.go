package service

import (
	"context"
	"math"
)

// User concurrency is stored in a PostgreSQL INT. Existing user management has
// no smaller application limit; enterprise overrides must additionally be positive.
func EnterpriseMemberConcurrencyValid(value *int) bool {
	return value == nil || (*value > 0 && *value <= math.MaxInt32)
}

type EnterpriseMemberConcurrencyResult struct {
	MemberConcurrency *int `json:"member_concurrency"`
	UpdatedCount      int  `json:"updated_count"`
}

func (s *EnterpriseService) UpdateMemberConcurrency(ctx context.Context, a EnterpriseActor, id int64, concurrency *int) (*EnterpriseMemberConcurrencyResult, error) {
	if !a.IsAdmin {
		return nil, ErrEnterprisePermissionDenied
	}
	if !EnterpriseMemberConcurrencyValid(concurrency) {
		return nil, ErrEnterpriseInvalidInput
	}
	result := &EnterpriseMemberConcurrencyResult{MemberConcurrency: concurrency}
	var userIDs []int64
	err := s.mutate(ctx, a, id, func(ctx context.Context, e *Enterprise, _ *EnterpriseMemberContext) error {
		before := e.MemberConcurrency
		e.MemberConcurrency = concurrency
		if err := s.enterprises.Update(ctx, e); err != nil {
			return err
		}
		if concurrency != nil {
			var err error
			userIDs, err = s.enterprises.ListMemberUserIDs(ctx, id)
			if err != nil {
				return err
			}
			// Reuse the same repository operation as administrator batch updates.
			// Cache invalidation must wait until the enterprise and audit commit.
			result.UpdatedCount, err = s.users.BatchSetConcurrency(ctx, userIDs, *concurrency)
			if err != nil {
				return err
			}
		}
		return s.auditLogs.Create(ctx, &EnterpriseAuditLog{
			EnterpriseID: id, ActorUserID: a.UserID, ActorRole: RoleAdmin,
			Action: EnterpriseAuditActionMembersConcurrencyUpdate, TargetType: EnterpriseScopeEnterprise, TargetID: &id,
			Detail: map[string]any{"old_value": before, "new_value": concurrency, "updated_count": result.UpdatedCount},
		})
	})
	if err != nil {
		return nil, err
	}
	invalidateEnterpriseMembersAuthCache(ctx, userIDs, s.authCache)
	return result, nil
}
