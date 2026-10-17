package service

import (
	"context"
	"fmt"
)

// EnterpriseDissolveRepository archives organization rows without erasing the
// quota rows still referenced by requests admitted before dissolution.
type EnterpriseDissolveRepository interface {
	Dissolve(ctx context.Context, enterpriseID int64) error
}

func (s *EnterpriseService) DeleteEnterprise(ctx context.Context, a EnterpriseActor, id int64) error {
	return s.deleteEnterprise(ctx, a, id, false)
}

// DissolveEnterprise is an explicit administrator-only exception to member
// operations' last-owner protection. All organization rows are archived together.
func (s *EnterpriseService) DissolveEnterprise(ctx context.Context, a EnterpriseActor, id int64) error {
	return s.deleteEnterprise(ctx, a, id, true)
}

func (s *EnterpriseService) deleteEnterprise(ctx context.Context, a EnterpriseActor, id int64, dissolve bool) error {
	if !a.IsAdmin {
		return ErrEnterprisePermissionDenied
	}
	var userIDs []int64
	err := s.mutate(ctx, a, id, func(ctx context.Context, e *Enterprise, _ *EnterpriseMemberContext) error {
		var err error
		userIDs, err = s.enterprises.ListMemberUserIDs(ctx, id)
		if err != nil {
			return err
		}
		if !dissolve && len(userIDs) > 0 {
			return ErrEnterpriseNotEmpty
		}
		if dissolve {
			repo, ok := s.enterprises.(EnterpriseDissolveRepository)
			if !ok {
				return fmt.Errorf("enterprise dissolution repository is unavailable")
			}
			err = repo.Dissolve(ctx, id)
		} else {
			err = s.enterprises.Delete(ctx, id)
		}
		if err != nil {
			return err
		}
		return s.auditLogs.Create(ctx, &EnterpriseAuditLog{
			EnterpriseID: id, ActorUserID: a.UserID, ActorRole: RoleAdmin,
			Action: EnterpriseAuditActionEnterpriseDelete, TargetType: EnterpriseScopeEnterprise, TargetID: &id,
			Detail: map[string]any{"before": e, "after": nil, "dissolve": dissolve,
				"member_count": len(userIDs), "remaining_balance": e.Balance},
		})
	})
	if err != nil {
		return err
	}
	// Listing after commit would return no members: retain the transaction's IDs.
	if len(userIDs) > 0 {
		s.invalidate(ctx, id, userIDs...)
	}
	s.invalidateQuota(ctx, EnterpriseScopeEnterprise, id)
	return nil
}
