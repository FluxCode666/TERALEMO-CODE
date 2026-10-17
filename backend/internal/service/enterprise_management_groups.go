package service

import (
	"context"
	"errors"
	"slices"
)

func (s *EnterpriseService) GetAllowedGroups(ctx context.Context, a EnterpriseActor, id int64) ([]Group, error) {
	e, c, err := s.EnterpriseAuthorize(ctx, a, id, false)
	if err != nil {
		return nil, err
	}
	pool, err := s.enterpriseGroups.GetAllowedGroupIDs(ctx, id)
	if err != nil {
		return nil, err
	}
	if a.IsAdmin && len(pool) == 0 {
		return []Group{}, nil
	}
	if e.GroupPoolConfigured && len(pool) == 0 {
		return []Group{}, nil
	}
	active, err := s.groups.ListActive(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Group, 0)
	var user *User
	member := !a.IsAdmin && c.Role == EnterpriseRoleMember
	if member && !e.GroupPoolConfigured {
		user, err = s.users.GetByID(ctx, a.UserID)
		if err != nil {
			return nil, err
		}
	}
	for _, g := range active {
		if !g.IsActive() {
			continue
		}
		if e.GroupPoolConfigured {
			if !slices.Contains(pool, g.ID) {
				continue
			}
		} else if member {
			if (g.IsExclusive || user.RestrictPublicGroups) && !slices.Contains(user.AllowedGroups, g.ID) {
				continue
			}
		} else if g.IsExclusive {
			continue
		}
		if member && !c.AllowsGroup(g.ID) {
			continue
		}
		out = append(out, g)
	}
	return out, nil
}
func (s *EnterpriseService) validateGroupsActive(ctx context.Context, ids []int64) error {
	for _, id := range ids {
		if id <= 0 {
			return ErrEnterpriseGroupOutOfScope
		}
		g, err := s.groups.GetByID(ctx, id)
		if err != nil {
			if errors.Is(err, ErrGroupNotFound) {
				return ErrEnterpriseGroupOutOfScope
			}
			return err
		}
		if g == nil || !g.IsActive() {
			return ErrEnterpriseGroupOutOfScope
		}
	}
	return nil
}
func enterpriseUniqueGroupIDs(ids []int64) []int64 {
	ids = slices.Clone(ids)
	slices.Sort(ids)
	return slices.Compact(ids)
}
func (s *EnterpriseService) SetAllowedGroups(ctx context.Context, a EnterpriseActor, id int64, ids []int64) error {
	if !a.IsAdmin {
		return ErrEnterprisePermissionDenied
	}
	ids = enterpriseUniqueGroupIDs(ids)
	err := s.mutate(ctx, a, id, func(ctx context.Context, e *Enterprise, c *EnterpriseMemberContext) error {
		if err := s.validateGroupsActive(ctx, ids); err != nil {
			return err
		}
		before, err := s.enterpriseGroups.GetAllowedGroupIDs(ctx, id)
		if err != nil {
			return err
		}
		if err = s.enterpriseGroups.SetAllowedGroupIDs(ctx, id, ids); err != nil {
			return err
		}
		return s.audit(ctx, a, c, id, EnterpriseAuditActionGroupPoolUpdate, EnterpriseScopeEnterprise, id, before, ids)
	})
	if err == nil {
		s.invalidate(ctx, id)
	}
	return err
}
func (s *EnterpriseService) ListGroupWhitelists(ctx context.Context, a EnterpriseActor, id int64) ([]EnterpriseGroupWhitelist, error) {
	e, c, err := s.EnterpriseAuthorize(ctx, a, id, false)
	if err != nil {
		return nil, err
	}
	if err = enterpriseCheck(a, c, e, EnterprisePermissionViewWhitelists, EnterprisePermissionTarget{}); err != nil {
		return nil, err
	}
	items, err := s.enterpriseGroups.ListWhitelists(ctx, id)
	if err != nil {
		return nil, err
	}
	if a.IsAdmin || c.Role == EnterpriseRoleOwner {
		return items, nil
	}
	out := make([]EnterpriseGroupWhitelist, 0)
	for _, w := range items {
		if w.ScopeType != EnterpriseScopeMember {
			continue
		}
		m, err := s.members.GetByID(ctx, id, w.ScopeID)
		if err != nil {
			return nil, err
		}
		if enterpriseSameDepartment(c, m) {
			out = append(out, w)
		}
	}
	return out, nil
}
func enterpriseGroupsSubset(ids, allowed []int64) bool {
	for _, id := range ids {
		if !slices.Contains(allowed, id) {
			return false
		}
	}
	return true
}
func (s *EnterpriseService) validateWhitelistParents(ctx context.Context, e *Enterprise, target EnterprisePermissionTarget, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	id := e.ID
	pool, err := s.enterpriseGroups.GetAllowedGroupIDs(ctx, id)
	if err != nil {
		return err
	}
	// An unconfigured pool imposes no enterprise restriction; personal public /
	// exclusive rules remain enforced when members resolve their effective groups.
	if e.GroupPoolConfigured && !enterpriseGroupsSubset(ids, pool) {
		return ErrEnterpriseGroupOutOfScope
	}
	parents := []EnterpriseQuotaScope{}
	if target.ScopeType != EnterpriseScopeEnterprise {
		parents = append(parents, EnterpriseQuotaScope{ScopeType: EnterpriseScopeEnterprise, ScopeID: id})
	}
	if target.Member != nil && target.Member.DepartmentID != nil {
		parents = append(parents, EnterpriseQuotaScope{ScopeType: EnterpriseScopeDepartment, ScopeID: *target.Member.DepartmentID})
	}
	for _, p := range parents {
		w, err := s.enterpriseGroups.GetWhitelist(ctx, id, p.ScopeType, p.ScopeID)
		if err != nil {
			return err
		}
		if w != nil && !enterpriseGroupsSubset(ids, w.GroupIDs) {
			return ErrEnterpriseGroupOutOfScope
		}
	}
	return nil
}
func (s *EnterpriseService) SetGroupWhitelist(ctx context.Context, a EnterpriseActor, id int64, scope string, scopeID int64, ids []int64) error {
	ids = enterpriseUniqueGroupIDs(ids)
	var affectedUID int64
	err := s.mutate(ctx, a, id, func(ctx context.Context, e *Enterprise, c *EnterpriseMemberContext) error {
		target, err := s.scopeTarget(ctx, id, scope, scopeID)
		if err != nil {
			return err
		}
		if err = enterpriseCheck(a, c, e, EnterprisePermissionEditWhitelist, target); err != nil {
			return err
		}
		if err = s.validateGroupsActive(ctx, ids); err != nil {
			return err
		}
		if err = s.validateWhitelistParents(ctx, e, target, ids); err != nil {
			return err
		}
		before, err := s.enterpriseGroups.GetWhitelist(ctx, id, scope, scopeID)
		if err != nil {
			return err
		}
		if err = s.enterpriseGroups.SetWhitelist(ctx, id, scope, scopeID, ids); err != nil {
			return err
		}
		if target.Member != nil {
			affectedUID = target.Member.UserID
		}
		return s.audit(ctx, a, c, id, EnterpriseAuditActionGroupWhitelistUpdate, scope, scopeID, before, EnterpriseGroupWhitelist{EnterpriseID: id, ScopeType: scope, ScopeID: scopeID, GroupIDs: ids})
	})
	if err == nil {
		if affectedUID > 0 {
			s.invalidate(ctx, id, affectedUID)
		} else {
			s.invalidate(ctx, id)
		}
	}
	return err
}
