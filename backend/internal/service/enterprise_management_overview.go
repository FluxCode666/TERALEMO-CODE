package service

import "context"

func (s *EnterpriseService) Overview(ctx context.Context, a EnterpriseActor, id int64) (*EnterpriseOverviewData, error) {
	e, c, err := s.EnterpriseAuthorize(ctx, a, id, false)
	if err != nil {
		return nil, err
	}
	// The workbench overview describes a member, rather than an administrator's
	// synthetic role. Admin detail is served by GetEnterprise.
	if c == nil {
		return nil, ErrEnterpriseNotMember
	}
	me, err := s.members.GetByID(ctx, id, c.MemberID)
	if err != nil {
		return nil, err
	}
	out := &EnterpriseOverviewData{Enterprise: e, Me: me, BalanceVisible: EnterpriseBalanceVisible(c.Role, e.Settings), Permissions: EnterprisePermissionsFor(c, e.Settings)}
	if out.BalanceVisible {
		out.Balance = &e.Balance
	}
	if c.Role == EnterpriseRoleOwner {
		out.Settings = &e.Settings
		out.EnterpriseQuota, err = s.quotas.GetByScope(ctx, id, EnterpriseScopeEnterprise, id)
		if err != nil {
			return nil, err
		}
	}
	out.DepartmentQuotaVisible = c.Role == EnterpriseRoleOwner || (c.Role == EnterpriseRoleManager && e.Settings.DepartmentQuotaVisibleToManager)
	out.MemberQuotaVisible = c.Role == EnterpriseRoleOwner || (c.Role == EnterpriseRoleManager && e.Settings.MemberQuotaVisibleToManager) || (c.Role == EnterpriseRoleMember && e.Settings.MemberQuotaVisibleToMember)
	if out.DepartmentQuotaVisible && c.DepartmentID != nil {
		out.DepartmentQuota, err = s.quotas.GetByScope(ctx, id, EnterpriseScopeDepartment, *c.DepartmentID)
		if err != nil {
			return nil, err
		}
	}
	if out.MemberQuotaVisible {
		out.MemberQuota, err = s.quotas.GetByScope(ctx, id, EnterpriseScopeMember, c.MemberID)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
