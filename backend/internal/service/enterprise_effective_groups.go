package service

import (
	"context"
	"errors"
)

// EnterpriseGroupRestriction distinguishes an unconfigured scope from a configured
// empty scope. All group_ids arrays are non-null, including unconfigured scopes.
type EnterpriseGroupRestriction struct {
	Configured bool    `json:"configured"`
	GroupIDs   []int64 `json:"group_ids"`
}
type EnterpriseEffectiveGroupLevels struct {
	Enterprise EnterpriseGroupRestriction  `json:"enterprise"`
	Department *EnterpriseGroupRestriction `json:"department"`
	Member     EnterpriseGroupRestriction  `json:"member"`
}
type EnterpriseEffectiveGroup struct {
	ID               int64   `json:"id"`
	Name             string  `json:"name"`
	Platform         string  `json:"platform"`
	IsExclusive      bool    `json:"is_exclusive"`
	SubscriptionType string  `json:"subscription_type"`
	Status           string  `json:"status"`
	RateMultiplier   float64 `json:"rate_multiplier"`
}
type EnterpriseEffectiveGroups struct {
	MemberID          int64                          `json:"member_id"`
	UserID            int64                          `json:"user_id"`
	DepartmentID      *int64                         `json:"department_id"`
	Pool              EnterpriseGroupRestriction     `json:"pool"`
	Levels            EnterpriseEffectiveGroupLevels `json:"levels"`
	Restricted        bool                           `json:"restricted"`
	EffectiveGroupIDs []int64                        `json:"effective_group_ids"`
	Groups            []EnterpriseEffectiveGroup     `json:"groups"`
}

func (s *EnterpriseService) GetEffectiveGroups(ctx context.Context, a EnterpriseActor, id, memberID int64) (*EnterpriseEffectiveGroups, error) {
	e, caller, err := s.EnterpriseAuthorize(ctx, a, id, false)
	if err != nil {
		return nil, err
	}
	if memberID == 0 {
		if caller != nil {
			memberID = caller.MemberID
		} else {
			m, err := s.members.GetByUserID(ctx, a.UserID)
			if err != nil {
				return nil, err
			}
			if m.EnterpriseID != id {
				return nil, ErrEnterpriseMemberNotFound
			}
			memberID = m.ID
		}
	}
	m, err := s.members.GetByID(ctx, id, memberID)
	if err != nil {
		if !a.IsAdmin && errors.Is(err, ErrEnterpriseMemberNotFound) {
			return nil, ErrEnterprisePermissionDenied
		}
		return nil, err
	}
	if m.EnterpriseID != id {
		return nil, ErrEnterprisePermissionDenied
	}
	target := EnterprisePermissionTarget{ScopeType: EnterpriseScopeMember, ScopeID: m.ID, Member: m}
	if err := enterpriseCheck(a, caller, e, EnterprisePermissionViewEffectiveGroups, target); err != nil {
		return nil, err
	}
	pool, err := s.enterpriseGroups.GetAllowedGroupIDs(ctx, id)
	if err != nil {
		return nil, err
	}
	snapshot := &EnterpriseMemberContext{GroupPoolConfigured: e.GroupPoolConfigured, GroupPoolIDs: pool}
	out := &EnterpriseEffectiveGroups{
		MemberID: m.ID, UserID: m.UserID, DepartmentID: m.DepartmentID,
		Pool:   EnterpriseGroupRestriction{Configured: e.GroupPoolConfigured, GroupIDs: append([]int64{}, pool...)},
		Groups: []EnterpriseEffectiveGroup{},
	}
	load := func(scope string, scopeID int64) (EnterpriseGroupRestriction, error) {
		level := EnterpriseGroupRestriction{GroupIDs: []int64{}}
		w, err := s.enterpriseGroups.GetWhitelist(ctx, id, scope, scopeID)
		if err != nil {
			return level, err
		}
		if w != nil {
			level.Configured = true
			level.GroupIDs = append(level.GroupIDs, enterpriseUniqueGroupIDs(w.GroupIDs)...)
			snapshot.ApplyGroupWhitelist(level.GroupIDs)
		}
		return level, nil
	}
	if out.Levels.Enterprise, err = load(EnterpriseScopeEnterprise, id); err != nil {
		return nil, err
	}
	if m.DepartmentID != nil {
		level, err := load(EnterpriseScopeDepartment, *m.DepartmentID)
		if err != nil {
			return nil, err
		}
		out.Levels.Department = &level
	}
	if out.Levels.Member, err = load(EnterpriseScopeMember, m.ID); err != nil {
		return nil, err
	}
	out.Restricted = snapshot.GroupPoolConfigured || snapshot.GroupWhitelistConfigured
	out.EffectiveGroupIDs = snapshot.EffectiveGroupIDs()
	// With no enterprise restriction, personal public/exclusive rules still apply;
	// groups is empty because this endpoint describes enterprise policy only.
	for _, groupID := range out.EffectiveGroupIDs {
		g, err := s.groups.GetByID(ctx, groupID)
		if errors.Is(err, ErrGroupNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out.Groups = append(out.Groups, EnterpriseEffectiveGroup{ID: g.ID, Name: g.Name, Platform: g.Platform, IsExclusive: g.IsExclusive, SubscriptionType: g.SubscriptionType, Status: g.Status, RateMultiplier: g.RateMultiplier})
	}
	return out, nil
}
