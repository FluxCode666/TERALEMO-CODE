package service

import (
	"context"
	"errors"
	"time"
)

func (s *EnterpriseService) scopeTarget(ctx context.Context, id int64, scope string, scopeID int64) (EnterprisePermissionTarget, error) {
	target := EnterprisePermissionTarget{ScopeType: scope, ScopeID: scopeID}
	if scopeID <= 0 {
		return target, ErrEnterpriseInvalidScope
	}
	switch scope {
	case EnterpriseScopeEnterprise:
		if scopeID != id {
			return target, ErrEnterpriseInvalidScope
		}
	case EnterpriseScopeDepartment:
		if _, err := s.departments.GetByID(ctx, id, scopeID); err != nil {
			if errors.Is(err, ErrEnterpriseDepartmentNotFound) {
				return target, ErrEnterpriseInvalidScope
			}
			return target, err
		}
	case EnterpriseScopeMember:
		m, err := s.members.GetByID(ctx, id, scopeID)
		if err != nil {
			if errors.Is(err, ErrEnterpriseMemberNotFound) {
				return target, ErrEnterpriseInvalidScope
			}
			return target, err
		}
		target.Member = m
	default:
		return target, ErrEnterpriseInvalidScope
	}
	return target, nil
}
func (s *EnterpriseService) ListQuotas(ctx context.Context, a EnterpriseActor, id int64, scope string) ([]EnterpriseQuota, error) {
	e, c, err := s.EnterpriseAuthorize(ctx, a, id, false)
	if err != nil {
		return nil, err
	}
	if scope != "" && scope != EnterpriseScopeEnterprise && scope != EnterpriseScopeDepartment && scope != EnterpriseScopeMember {
		return nil, ErrEnterpriseInvalidScope
	}
	items, err := s.quotas.List(ctx, id, scope)
	if err != nil {
		return nil, err
	}
	out := make([]EnterpriseQuota, 0, len(items))
	for _, q := range items {
		if a.IsAdmin || c.Role == EnterpriseRoleOwner {
			out = append(out, q)
			continue
		}
		target := EnterprisePermissionTarget{ScopeType: q.ScopeType, ScopeID: q.ScopeID}
		if q.ScopeType == EnterpriseScopeMember {
			// Check the scope against its own current membership, never a caller supplied ID.
			m, err := s.members.GetByID(ctx, id, q.ScopeID)
			if err != nil {
				return nil, err
			}
			target.Member = m
		}
		if CheckEnterprisePermission(c, e.Settings, EnterprisePermissionViewQuota, target) == nil {
			out = append(out, q)
		}
	}
	return out, nil
}
func enterpriseQuotaLimits(q *EnterpriseQuota) []*float64 {
	return []*float64{q.DailyLimitUSD, q.WeeklyLimitUSD, q.MonthlyLimitUSD, q.TotalLimitUSD}
}
func enterpriseValidateQuotaParent(q, parent *EnterpriseQuota) error {
	if parent == nil {
		return nil
	}
	childLimits, parentLimits := enterpriseQuotaLimits(q), enterpriseQuotaLimits(parent)
	for i, child := range childLimits {
		if child != nil && parentLimits[i] != nil && *child > *parentLimits[i] {
			return ErrEnterpriseQuotaExceedsParent
		}
	}
	return nil
}
func (s *EnterpriseService) validateQuotaParents(ctx context.Context, id int64, target EnterprisePermissionTarget, q *EnterpriseQuota) error {
	if target.ScopeType == EnterpriseScopeEnterprise {
		return nil
	}
	scopes := []EnterpriseQuotaScope{{ScopeType: EnterpriseScopeEnterprise, ScopeID: id}}
	if target.Member != nil && target.Member.DepartmentID != nil {
		scopes = append(scopes, EnterpriseQuotaScope{ScopeType: EnterpriseScopeDepartment, ScopeID: *target.Member.DepartmentID})
	}
	parents, err := s.quotas.GetByScopes(ctx, id, scopes)
	if err != nil {
		return err
	}
	if len(parents) != len(scopes) {
		return ErrEnterpriseInvalidScope
	}
	for i := range parents {
		if err = enterpriseValidateQuotaParent(q, &parents[i]); err != nil {
			return err
		}
	}
	return nil
}
func (s *EnterpriseService) UpdateQuota(ctx context.Context, a EnterpriseActor, id int64, scope string, scopeID int64, in EnterpriseQuotaLimitsInput) (*EnterpriseQuota, error) {
	for _, v := range []*float64{in.DailyLimitUSD, in.WeeklyLimitUSD, in.MonthlyLimitUSD, in.TotalLimitUSD} {
		if !enterpriseValidNumber(v) {
			return nil, ErrEnterpriseInvalidInput
		}
	}
	var affectedUID int64
	err := s.mutate(ctx, a, id, func(ctx context.Context, e *Enterprise, c *EnterpriseMemberContext) error {
		target, err := s.scopeTarget(ctx, id, scope, scopeID)
		if err != nil {
			return err
		}
		if err = enterpriseCheck(a, c, e, EnterprisePermissionEditQuota, target); err != nil {
			return err
		}
		q, err := s.quotas.GetByScope(ctx, id, scope, scopeID)
		if err != nil {
			return err
		}
		if q == nil {
			return ErrEnterpriseInvalidScope
		}
		before := *q
		q.DailyLimitUSD = in.DailyLimitUSD
		q.WeeklyLimitUSD = in.WeeklyLimitUSD
		q.MonthlyLimitUSD = in.MonthlyLimitUSD
		q.TotalLimitUSD = in.TotalLimitUSD
		if err = s.validateQuotaParents(ctx, id, target, q); err != nil {
			return err
		}
		if err = s.quotas.UpdateLimits(ctx, q); err != nil {
			return err
		}
		if target.Member != nil {
			affectedUID = target.Member.UserID
		}
		return s.audit(ctx, a, c, id, EnterpriseAuditActionQuotaUpdate, scope, scopeID, &before, q)
	})
	if err != nil {
		return nil, err
	}
	if affectedUID > 0 {
		s.invalidate(ctx, id, affectedUID)
	} else {
		s.invalidate(ctx, id)
	}
	s.invalidateQuota(ctx, scope, scopeID)
	return s.quotas.GetByScope(ctx, id, scope, scopeID)
}
func (s *EnterpriseService) ResetQuota(ctx context.Context, a EnterpriseActor, id int64, scope string, scopeID int64, windows []string) error {
	if len(windows) == 0 {
		return ErrEnterpriseInvalidInput
	}
	seen := map[string]bool{}
	for _, w := range windows {
		switch w {
		case EnterpriseQuotaWindowDaily, EnterpriseQuotaWindowWeekly, EnterpriseQuotaWindowMonthly, EnterpriseQuotaWindowTotal:
		default:
			return ErrEnterpriseInvalidInput
		}
		if seen[w] {
			return ErrEnterpriseInvalidInput
		}
		seen[w] = true
	}
	var affectedUID int64
	err := s.mutate(ctx, a, id, func(ctx context.Context, e *Enterprise, c *EnterpriseMemberContext) error {
		target, err := s.scopeTarget(ctx, id, scope, scopeID)
		if err != nil {
			return err
		}
		if err = enterpriseCheck(a, c, e, EnterprisePermissionResetQuota, target); err != nil {
			return err
		}
		before, err := s.quotas.GetByScope(ctx, id, scope, scopeID)
		if err != nil {
			return err
		}
		if before == nil {
			return ErrEnterpriseInvalidScope
		}
		if err = s.quotas.ResetUsage(ctx, id, scope, scopeID, windows, time.Now()); err != nil {
			return err
		}
		after, err := s.quotas.GetByScope(ctx, id, scope, scopeID)
		if err != nil {
			return err
		}
		if target.Member != nil {
			affectedUID = target.Member.UserID
		}
		return s.audit(ctx, a, c, id, EnterpriseAuditActionQuotaReset, scope, scopeID, before, map[string]any{"quota": after, "windows": windows})
	})
	if err == nil {
		if affectedUID > 0 {
			s.invalidate(ctx, id, affectedUID)
		} else {
			s.invalidate(ctx, id)
		}
		s.invalidateQuota(ctx, scope, scopeID)
	}
	return err
}
