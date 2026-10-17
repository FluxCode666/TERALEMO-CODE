package domain

import (
	"encoding/json"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	EnterpriseRoleOwner                       = "owner"
	EnterpriseRoleManager                     = "manager"
	EnterpriseRoleMember                      = "member"
	EnterpriseStatusActive                    = "active"
	EnterpriseStatusDisabled                  = "disabled"
	EnterpriseMemberStatusActive              = "active"
	EnterpriseMemberStatusDisabled            = "disabled"
	EnterpriseScopeEnterprise                 = "enterprise"
	EnterpriseScopeDepartment                 = "department"
	EnterpriseScopeMember                     = "member"
	EnterpriseBalanceOperationAdd             = "add"
	EnterpriseBalanceOperationSubtract        = "subtract"
	EnterpriseQuotaWindowDaily                = "daily"
	EnterpriseQuotaWindowWeekly               = "weekly"
	EnterpriseQuotaWindowMonthly              = "monthly"
	EnterpriseQuotaWindowTotal                = "total"
	EnterpriseAuditActionEnterpriseCreate     = "enterprise.create"
	EnterpriseAuditActionEnterpriseUpdate     = "enterprise.update"
	EnterpriseAuditActionEnterpriseDelete     = "enterprise.delete"
	EnterpriseAuditActionBalanceAdjust        = "balance.adjust"
	EnterpriseAuditActionDepartmentCreate     = "department.create"
	EnterpriseAuditActionDepartmentUpdate     = "department.update"
	EnterpriseAuditActionDepartmentDelete     = "department.delete"
	EnterpriseAuditActionMemberAdd            = "member.add"
	EnterpriseAuditActionMemberCreate         = "member.create"
	EnterpriseAuditActionMemberUpdate         = "member.update"
	EnterpriseAuditActionMemberRemove         = "member.remove"
	EnterpriseAuditActionQuotaUpdate          = "quota.update"
	EnterpriseAuditActionQuotaReset           = "quota.reset"
	EnterpriseAuditActionSettingsUpdate       = "settings.update"
	EnterpriseAuditActionGroupPoolUpdate      = "group_pool.update"
	EnterpriseAuditActionGroupWhitelistUpdate = "group_whitelist.update"
)

const (
	EnterpriseErrorCodeNotFound                  = "ENTERPRISE_NOT_FOUND"
	EnterpriseErrorCodeDepartmentNotFound        = "ENTERPRISE_DEPARTMENT_NOT_FOUND"
	EnterpriseErrorCodeMemberNotFound            = "ENTERPRISE_MEMBER_NOT_FOUND"
	EnterpriseErrorCodeNameExists                = "ENTERPRISE_NAME_EXISTS"
	EnterpriseErrorCodeDepartmentNameExists      = "ENTERPRISE_DEPARTMENT_NAME_EXISTS"
	EnterpriseErrorCodeMemberExists              = "ENTERPRISE_MEMBER_EXISTS"
	EnterpriseErrorCodeMemberAdminForbidden      = "ENTERPRISE_MEMBER_ADMIN_FORBIDDEN"
	EnterpriseErrorCodeManagerRequiresDepartment = "ENTERPRISE_MANAGER_REQUIRES_DEPARTMENT"
	EnterpriseErrorCodeLastOwner                 = "ENTERPRISE_LAST_OWNER"
	EnterpriseErrorCodeDepartmentNotEmpty        = "ENTERPRISE_DEPARTMENT_NOT_EMPTY"
	EnterpriseErrorCodeNotEmpty                  = "ENTERPRISE_NOT_EMPTY"
	EnterpriseErrorCodeNotMember                 = "ENTERPRISE_NOT_MEMBER"
	EnterpriseErrorCodePermissionDenied          = "ENTERPRISE_PERMISSION_DENIED"
	EnterpriseErrorCodeDisabled                  = "ENTERPRISE_DISABLED"
	EnterpriseErrorCodeMemberDisabled            = "ENTERPRISE_MEMBER_DISABLED"
	EnterpriseErrorCodeBalanceInsufficient       = "ENTERPRISE_BALANCE_INSUFFICIENT"
	EnterpriseErrorCodeQuotaExceeded             = "ENTERPRISE_QUOTA_EXCEEDED"
	EnterpriseErrorCodeQuotaExceedsParent        = "ENTERPRISE_QUOTA_EXCEEDS_PARENT"
	EnterpriseErrorCodeGroupOutOfScope           = "ENTERPRISE_GROUP_OUT_OF_SCOPE"
	EnterpriseErrorCodeGroupNotAllowed           = "ENTERPRISE_GROUP_NOT_ALLOWED"
	EnterpriseErrorCodeMemberWalletDisabled      = "ENTERPRISE_MEMBER_WALLET_DISABLED"
	EnterpriseErrorCodeFeatureUnsupported        = "ENTERPRISE_FEATURE_UNSUPPORTED"
	EnterpriseErrorCodeInvalidScope              = "ENTERPRISE_INVALID_SCOPE"
	DepartmentErrorCodeQuotaExceeded             = "DEPARTMENT_QUOTA_EXCEEDED"
	MemberErrorCodeQuotaExceeded                 = "MEMBER_QUOTA_EXCEEDED"
)

var (
	ErrEnterpriseNotFound                      = infraerrors.New(404, EnterpriseErrorCodeNotFound, "enterprise not found")
	ErrEnterpriseDepartmentNotFound            = infraerrors.New(404, EnterpriseErrorCodeDepartmentNotFound, "enterprise department not found")
	ErrEnterpriseMemberNotFound                = infraerrors.New(404, EnterpriseErrorCodeMemberNotFound, "enterprise member not found")
	ErrEnterpriseNameExists                    = infraerrors.New(409, EnterpriseErrorCodeNameExists, "enterprise name already exists")
	ErrEnterpriseDepartmentNameExists          = infraerrors.New(409, EnterpriseErrorCodeDepartmentNameExists, "department name already exists")
	ErrEnterpriseMemberExists                  = infraerrors.New(409, EnterpriseErrorCodeMemberExists, "user already belongs to an enterprise")
	ErrEnterpriseMemberAdminForbidden          = infraerrors.New(400, EnterpriseErrorCodeMemberAdminForbidden, "system administrators cannot join an enterprise")
	ErrEnterpriseManagerRequiresDepartment     = infraerrors.New(400, EnterpriseErrorCodeManagerRequiresDepartment, "manager requires a department")
	ErrEnterpriseLastOwner                     = infraerrors.New(409, EnterpriseErrorCodeLastOwner, "enterprise must retain an active owner")
	ErrEnterpriseDepartmentNotEmpty            = infraerrors.New(409, EnterpriseErrorCodeDepartmentNotEmpty, "department still has members")
	ErrEnterpriseNotEmpty                      = infraerrors.New(409, EnterpriseErrorCodeNotEmpty, "enterprise still has members")
	ErrEnterpriseNotMember                     = infraerrors.New(403, EnterpriseErrorCodeNotMember, "user is not an enterprise member")
	ErrEnterprisePermissionDenied              = infraerrors.New(403, EnterpriseErrorCodePermissionDenied, "enterprise permission denied")
	ErrEnterpriseDisabled                      = infraerrors.New(403, EnterpriseErrorCodeDisabled, "enterprise is disabled")
	ErrEnterpriseMemberDisabled                = infraerrors.New(403, EnterpriseErrorCodeMemberDisabled, "enterprise member is disabled")
	ErrEnterpriseBalanceInsufficient           = infraerrors.New(403, EnterpriseErrorCodeBalanceInsufficient, "enterprise balance is insufficient")
	ErrEnterpriseQuotaExceeded                 = infraerrors.New(429, EnterpriseErrorCodeQuotaExceeded, "enterprise quota exceeded")
	ErrEnterpriseQuotaExceedsParent            = infraerrors.New(400, EnterpriseErrorCodeQuotaExceedsParent, "quota exceeds parent limit")
	ErrEnterpriseGroupOutOfScope               = infraerrors.New(400, EnterpriseErrorCodeGroupOutOfScope, "group is outside the allowed scope")
	ErrEnterpriseGroupNotAllowed               = infraerrors.New(403, EnterpriseErrorCodeGroupNotAllowed, "enterprise group is not allowed")
	ErrEnterpriseMemberWalletDisabled          = infraerrors.New(403, EnterpriseErrorCodeMemberWalletDisabled, "personal wallet is disabled for enterprise members")
	ErrEnterpriseFeatureUnsupported            = infraerrors.New(403, EnterpriseErrorCodeFeatureUnsupported, "feature is unavailable for enterprise members")
	ErrEnterpriseInvalidScope                  = infraerrors.New(400, EnterpriseErrorCodeInvalidScope, "invalid enterprise scope")
	ErrDepartmentQuotaExceeded                 = infraerrors.New(429, DepartmentErrorCodeQuotaExceeded, "department quota exceeded")
	ErrMemberQuotaExceeded                     = infraerrors.New(429, MemberErrorCodeQuotaExceeded, "member quota exceeded")
	ErrEnterpriseBalanceAdjustmentInsufficient = infraerrors.BadRequest(EnterpriseErrorCodeBalanceInsufficient, "enterprise balance is insufficient")
)

// EnterpriseSettings must be initialized with DefaultEnterpriseSettings for a new enterprise.
// UnmarshalJSON fills absent or null keys from the documented defaults.
type EnterpriseSettings struct {
	BalanceVisibleToManager         bool `json:"balance_visible_to_manager"`
	BalanceVisibleToMember          bool `json:"balance_visible_to_member"`
	MemberQuotaVisibleToMember      bool `json:"member_quota_visible_to_member"`
	MemberQuotaVisibleToManager     bool `json:"member_quota_visible_to_manager"`
	DepartmentQuotaVisibleToManager bool `json:"department_quota_visible_to_manager"`
	ManagerCanManageMembers         bool `json:"manager_can_manage_members"`
	ManagerCanEditMemberQuota       bool `json:"manager_can_edit_member_quota"`
	ManagerCanEditMemberGroups      bool `json:"manager_can_edit_member_groups"`
}

func DefaultEnterpriseSettings() EnterpriseSettings {
	return EnterpriseSettings{BalanceVisibleToManager: true, MemberQuotaVisibleToMember: true, MemberQuotaVisibleToManager: true, DepartmentQuotaVisibleToManager: true}
}
func (s *EnterpriseSettings) UnmarshalJSON(data []byte) error {
	type settings EnterpriseSettings
	v := settings(DefaultEnterpriseSettings())
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*s = EnterpriseSettings(v)
	return nil
}
