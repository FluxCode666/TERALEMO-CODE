package service

import (
	"context"
	"errors"
	"slices"
)

func (s *EnterpriseService) allowedGroupRates(ctx context.Context, id int64) ([]EnterpriseAllowedGroup, error) {
	if repo, ok := s.enterpriseGroups.(EnterpriseGroupRateRepository); ok {
		return repo.GetAllowedGroups(ctx, id)
	}
	ids, err := s.enterpriseGroups.GetAllowedGroupIDs(ctx, id)
	if err != nil {
		return nil, err
	}
	groups := make([]EnterpriseAllowedGroup, 0, len(ids))
	for _, id := range ids {
		groups = append(groups, EnterpriseAllowedGroup{GroupID: id})
	}
	return groups, nil
}

func (s *EnterpriseService) GetAllowedGroups(ctx context.Context, a EnterpriseActor, id int64) ([]EnterpriseAllowedGroupView, error) {
	e, c, err := s.EnterpriseAuthorize(ctx, a, id, false)
	if err != nil {
		return nil, err
	}
	configured, err := s.allowedGroupRates(ctx, id)
	if err != nil {
		return nil, err
	}
	pool := make([]int64, 0, len(configured))
	overrides := make(map[int64]*float64, len(configured))
	for _, group := range configured {
		pool = append(pool, group.GroupID)
		overrides[group.GroupID] = group.RateMultiplier
	}
	if a.IsAdmin && len(pool) == 0 {
		return []EnterpriseAllowedGroupView{}, nil
	}
	if e.GroupPoolConfigured && len(pool) == 0 {
		return []EnterpriseAllowedGroupView{}, nil
	}
	active, err := s.groups.ListActive(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]EnterpriseAllowedGroupView, 0)
	var rates map[int64]float64
	if !a.IsAdmin {
		rates, err = resolveEffectiveUserGroupRates(ctx, s.groupRates, a.UserID)
		if err != nil {
			return nil, err
		}
	}
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
		view := EnterpriseAllowedGroupView{Group: g, EnterpriseRateMultiplier: overrides[g.ID], DefaultRateMultiplier: g.RateMultiplier, EffectiveRateMultiplier: g.RateMultiplier}
		if a.IsAdmin {
			if override := overrides[g.ID]; override != nil {
				view.EffectiveRateMultiplier = *override
			}
		} else if rate, ok := rates[g.ID]; ok {
			view.EffectiveRateMultiplier = rate
		}
		view.RateMultiplier = view.EffectiveRateMultiplier
		out = append(out, view)
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
	groups := make([]EnterpriseAllowedGroup, 0, len(ids))
	for _, id := range enterpriseUniqueGroupIDs(ids) {
		groups = append(groups, EnterpriseAllowedGroup{GroupID: id})
	}
	return s.setAllowedGroups(ctx, a, id, groups, true)
}

func (s *EnterpriseService) SetAllowedGroupRates(ctx context.Context, a EnterpriseActor, id int64, groups []EnterpriseAllowedGroup) error {
	return s.setAllowedGroups(ctx, a, id, groups, false)
}

func (s *EnterpriseService) setAllowedGroups(ctx context.Context, a EnterpriseActor, id int64, groups []EnterpriseAllowedGroup, preserveRates bool) error {
	if !a.IsAdmin {
		return ErrEnterprisePermissionDenied
	}
	groups = append([]EnterpriseAllowedGroup{}, groups...)
	slices.SortFunc(groups, func(a, b EnterpriseAllowedGroup) int {
		if a.GroupID < b.GroupID {
			return -1
		}
		if a.GroupID > b.GroupID {
			return 1
		}
		return 0
	})
	ids := make([]int64, 0, len(groups))
	for i, group := range groups {
		if !enterpriseValidNumber(group.RateMultiplier) || (group.RateMultiplier != nil && *group.RateMultiplier <= 0) || (i > 0 && groups[i-1].GroupID == group.GroupID) {
			return ErrEnterpriseInvalidInput
		}
		ids = append(ids, group.GroupID)
	}
	err := s.mutate(ctx, a, id, func(ctx context.Context, e *Enterprise, c *EnterpriseMemberContext) error {
		if err := s.validateGroupsActive(ctx, ids); err != nil {
			return err
		}
		before, err := s.allowedGroupRates(ctx, id)
		if err != nil {
			return err
		}
		if preserveRates {
			for i := range groups {
				for _, previous := range before {
					if groups[i].GroupID == previous.GroupID {
						groups[i].RateMultiplier = previous.RateMultiplier
						break
					}
				}
			}
			if err = s.enterpriseGroups.SetAllowedGroupIDs(ctx, id, ids); err != nil {
				return err
			}
		} else {
			repo, ok := s.enterpriseGroups.(EnterpriseGroupRateRepository)
			if !ok {
				return errors.New("enterprise group rate repository is unavailable")
			}
			if err = repo.SetAllowedGroups(ctx, id, groups); err != nil {
				return err
			}
		}
		return s.audit(ctx, a, c, id, EnterpriseAuditActionGroupPoolUpdate, EnterpriseScopeEnterprise, id, map[string]any{"groups": before}, map[string]any{"groups": groups})
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
	out := make([]EnterpriseGroupWhitelist, 0)
	for _, w := range items {
		if w.ScopeType == EnterpriseScopeEnterprise && w.ScopeID == id {
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
	parents, err := s.parentScopes(ctx, id, target)
	if err != nil {
		return err
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
	if scope == EnterpriseScopeDepartment || scope == EnterpriseScopeMember {
		return ErrEnterpriseGroupScopeDisabled
	}
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
