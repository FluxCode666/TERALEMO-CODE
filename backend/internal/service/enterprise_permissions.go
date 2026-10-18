package service

// EnterprisePermission is an operation, rather than an HTTP route, so the same
// policy guards both service calls and the workbench permission hints.
type EnterprisePermission string

const (
	EnterprisePermissionRead              EnterprisePermission = "read"
	EnterprisePermissionManageDepartments EnterprisePermission = "manage_departments"
	EnterprisePermissionViewMembers       EnterprisePermission = "view_members"
	EnterprisePermissionManageMember      EnterprisePermission = "manage_member"
	EnterprisePermissionCreateMember      EnterprisePermission = "create_member"
	EnterprisePermissionViewQuota         EnterprisePermission = "view_quota"
	EnterprisePermissionEditQuota         EnterprisePermission = "edit_quota"
	EnterprisePermissionResetQuota        EnterprisePermission = "reset_quota"
	EnterprisePermissionViewWhitelists    EnterprisePermission = "view_whitelists"
	EnterprisePermissionEditWhitelist     EnterprisePermission = "edit_whitelist"
	EnterprisePermissionSettings          EnterprisePermission = "settings"
	EnterprisePermissionAudit             EnterprisePermission = "audit"
	EnterprisePermissionDepartmentUsage   EnterprisePermission = "department_usage"
)

type EnterprisePermissionTarget struct {
	ScopeType string
	ScopeID   int64
	Member    *EnterpriseMember
}

func enterprisePermissionWrites(p EnterprisePermission) bool {
	switch p {
	case EnterprisePermissionManageDepartments, EnterprisePermissionManageMember, EnterprisePermissionCreateMember, EnterprisePermissionEditQuota, EnterprisePermissionResetQuota, EnterprisePermissionEditWhitelist, EnterprisePermissionSettings:
		return true
	}
	return false
}
func enterpriseSameDepartment(c *EnterpriseMemberContext, m *EnterpriseMember) bool {
	return c != nil && c.DepartmentID != nil && m != nil && m.DepartmentID != nil && *c.DepartmentID == *m.DepartmentID
}

// CheckEnterprisePermission is pure. It never trusts client-provided roles or
// changes the target. The service separately verifies target ownership.
func CheckEnterprisePermission(c *EnterpriseMemberContext, settings EnterpriseSettings, p EnterprisePermission, target EnterprisePermissionTarget) error {
	if c == nil {
		return ErrEnterpriseNotMember
	}
	if c.MemberStatus != EnterpriseMemberStatusActive {
		return ErrEnterpriseMemberDisabled
	}
	if enterprisePermissionWrites(p) && c.EnterpriseStatus != EnterpriseStatusActive {
		return ErrEnterpriseDisabled
	}
	if !enterpriseValidRole(c.Role) || (target.Member != nil && target.Member.EnterpriseID != 0 && target.Member.EnterpriseID != c.EnterpriseID) {
		return ErrEnterprisePermissionDenied
	}
	if c.Role == EnterpriseRoleOwner {
		return nil
	}
	allowed := false
	switch p {
	case EnterprisePermissionRead:
		allowed = true
	case EnterprisePermissionViewMembers, EnterprisePermissionViewWhitelists:
		allowed = c.Role == EnterpriseRoleManager && c.DepartmentID != nil
	case EnterprisePermissionCreateMember, EnterprisePermissionManageMember:
		allowed = c.Role == EnterpriseRoleManager && settings.ManagerCanManageMembers && enterpriseSameDepartment(c, target.Member) && target.Member.Role == EnterpriseRoleMember
	case EnterprisePermissionViewQuota:
		switch target.ScopeType {
		case EnterpriseScopeDepartment:
			allowed = c.Role == EnterpriseRoleManager && settings.DepartmentQuotaVisibleToManager && c.DepartmentID != nil && *c.DepartmentID == target.ScopeID
		case EnterpriseScopeMember:
			allowed = (c.Role == EnterpriseRoleManager && settings.MemberQuotaVisibleToManager && enterpriseSameDepartment(c, target.Member)) || (c.Role == EnterpriseRoleMember && settings.MemberQuotaVisibleToMember && c.MemberID == target.ScopeID)
		}
	case EnterprisePermissionEditQuota:
		allowed = c.Role == EnterpriseRoleManager && settings.ManagerCanEditMemberQuota && target.ScopeType == EnterpriseScopeMember && enterpriseSameDepartment(c, target.Member) && target.Member.Role == EnterpriseRoleMember
	case EnterprisePermissionEditWhitelist:
		allowed = c.Role == EnterpriseRoleManager && settings.ManagerCanEditMemberGroups && target.ScopeType == EnterpriseScopeMember && enterpriseSameDepartment(c, target.Member) && target.Member.Role == EnterpriseRoleMember
	}
	if !allowed {
		return ErrEnterprisePermissionDenied
	}
	return nil
}
func EnterpriseBalanceVisible(role string, settings EnterpriseSettings) bool {
	switch role {
	case EnterpriseRoleOwner:
		return true
	case EnterpriseRoleManager:
		return settings.BalanceVisibleToManager
	case EnterpriseRoleMember:
		return settings.BalanceVisibleToMember
	}
	return false
}
func EnterprisePermissionsFor(c *EnterpriseMemberContext, settings EnterpriseSettings) EnterprisePermissions {
	if c == nil || c.MemberStatus != EnterpriseMemberStatusActive {
		return EnterprisePermissions{}
	}
	owner := c.Role == EnterpriseRoleOwner
	manager := c.Role == EnterpriseRoleManager && c.DepartmentID != nil
	write := c.EnterpriseStatus == EnterpriseStatusActive
	return EnterprisePermissions{ManageDepartments: owner && write, ManageMembers: write && (owner || (manager && settings.ManagerCanManageMembers)), CreateMembers: write && (owner || (manager && settings.ManagerCanManageMembers)), EditMemberQuota: write && (owner || (manager && settings.ManagerCanEditMemberQuota)), EditMemberGroups: write && (owner || (manager && settings.ManagerCanEditMemberGroups)), EditQuotas: owner && write, EditSettings: owner && write, ViewMembers: owner || manager, ViewAuditLogs: owner, ViewDepartmentUsage: owner}
}
