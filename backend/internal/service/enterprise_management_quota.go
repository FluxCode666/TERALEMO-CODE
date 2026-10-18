package service

import (
	"context"
	"errors"
	"slices"
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
		d, err := s.departments.GetByID(ctx, id, scopeID)
		if err != nil {
			if errors.Is(err, ErrEnterpriseDepartmentNotFound) {
				return target, ErrEnterpriseInvalidScope
			}
			return target, err
		}
		target.Department = d
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
			visible := true
			q.UsageVisible = &visible
			out = append(out, q)
			continue
		}
		target, err := s.scopeTarget(ctx, id, q.ScopeType, q.ScopeID)
		if err != nil {
			return nil, err
		}
		visible := CheckEnterprisePermission(c, e.Settings, EnterprisePermissionViewQuota, target) == nil
		// Editors need the limit values even when usage visibility is disabled.
		// Keep the existing ownership/role rules: a manager cannot gain access to
		// another department or to an owner's/manager's limits through this path.
		editable := CheckEnterprisePermission(c, e.Settings, EnterprisePermissionEditQuota, target) == nil
		if visible || editable {
			q.UsageVisible = &visible
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
	scopes, err := s.parentScopes(ctx, id, target)
	if err != nil {
		return err
	}
	if len(scopes) == 0 {
		return nil
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
	var usageVisible bool
	err := s.mutate(ctx, a, id, func(ctx context.Context, e *Enterprise, c *EnterpriseMemberContext) error {
		target, err := s.scopeTarget(ctx, id, scope, scopeID)
		if err != nil {
			return err
		}
		if err = enterpriseCheck(a, c, e, EnterprisePermissionEditQuota, target); err != nil {
			return err
		}
		usageVisible = enterpriseCheck(a, c, e, EnterprisePermissionViewQuota, target) == nil
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
		if err = s.validateQuotaDescendants(ctx, id, target, q); err != nil {
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
	q, err := s.quotas.GetByScope(ctx, id, scope, scopeID)
	if err != nil || q == nil {
		return q, err
	}
	// Never persist redacted counters: visibility applies only to the returned
	// response and leaves the repository's accounting values untouched.
	response := *q
	response.UsageVisible = &usageVisible
	return &response, nil
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

// parentScopes returns every configured ancestor scope, skipping the target.
// Comparing against all ancestors preserves a non-null grandparent cap even when
// an intermediate department intentionally leaves the same window unlimited.
func (s *EnterpriseService) parentScopes(ctx context.Context, id int64, target EnterprisePermissionTarget) ([]EnterpriseQuotaScope, error) {
	if target.ScopeType == EnterpriseScopeEnterprise {
		return nil, nil
	}
	scopes := []EnterpriseQuotaScope{{ScopeType: EnterpriseScopeEnterprise, ScopeID: id}}
	var path []int64
	if target.Department != nil {
		path = target.Department.PathIDs
		if len(path) > 0 {
			path = path[:len(path)-1]
		}
	} else if target.Member != nil && target.Member.DepartmentID != nil {
		path = target.Member.DepartmentPathIDs
		if len(path) == 0 {
			d, err := s.departments.GetByID(ctx, id, *target.Member.DepartmentID)
			if err != nil {
				return nil, err
			}
			path = FrozenEnterpriseDepartmentIDs(d.PathIDs, d.ID)
		}
	}
	for _, departmentID := range path {
		scopes = append(scopes, EnterpriseQuotaScope{ScopeType: EnterpriseScopeDepartment, ScopeID: departmentID})
	}
	return scopes, nil
}

// Lowering a parent cannot silently invalidate an already configured child cap.
func (s *EnterpriseService) validateQuotaDescendants(ctx context.Context, id int64, target EnterprisePermissionTarget, q *EnterpriseQuota) error {
	if target.ScopeType == EnterpriseScopeMember {
		return nil
	}
	items, err := s.quotas.List(ctx, id, "")
	if err != nil {
		return err
	}
	for i := range items {
		child := &items[i]
		if child.ScopeType == EnterpriseScopeEnterprise || (child.ScopeType == q.ScopeType && child.ScopeID == q.ScopeID) {
			continue
		}
		if target.ScopeType == EnterpriseScopeDepartment {
			ct, err := s.scopeTarget(ctx, id, child.ScopeType, child.ScopeID)
			if err != nil {
				return err
			}
			var path []int64
			if ct.Department != nil {
				path = ct.Department.PathIDs
			} else if ct.Member != nil {
				path = ct.Member.DepartmentPathIDs
				if len(path) == 0 && ct.Member.DepartmentID != nil {
					path = []int64{*ct.Member.DepartmentID}
				}
			}
			if !slices.Contains(path, target.ScopeID) {
				continue
			}
		}
		if err := enterpriseValidateQuotaParent(child, q); err != nil {
			return err
		}
	}
	return nil
}

func (s *EnterpriseService) validateDepartmentSubtreeQuotaParents(ctx context.Context, id, departmentID int64) error {
	items, err := s.quotas.List(ctx, id, "")
	if err != nil {
		return err
	}
	for i := range items {
		q := &items[i]
		if q.ScopeType == EnterpriseScopeEnterprise {
			continue
		}
		target, err := s.scopeTarget(ctx, id, q.ScopeType, q.ScopeID)
		if err != nil {
			return err
		}
		var path []int64
		if target.Department != nil {
			path = target.Department.PathIDs
		} else if target.Member != nil {
			path = target.Member.DepartmentPathIDs
		}
		if slices.Contains(path, departmentID) {
			if err := s.validateQuotaParents(ctx, id, target, q); err != nil {
				return err
			}
		}
	}
	return nil
}
