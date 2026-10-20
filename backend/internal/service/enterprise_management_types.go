package service

import (
	"context"
	dbent "github.com/Wei-Shaw/sub2api/ent"
)

// EnterpriseActor identifies the authenticated caller. IsAdmin is set only by
// the protected admin handlers. Enterprise roles are always loaded from storage.
type EnterpriseActor struct {
	UserID  int64
	IsAdmin bool
}

type EnterpriseOwnerInput struct {
	UserID                    int64
	Email, Password, Username string
}
type CreateEnterpriseInput struct {
	Name, Status, Notes, ContactName, ContactEmail, ContactPhone string
	LowBalanceThreshold                                          *float64
	Settings                                                     *EnterpriseSettings
	Owner                                                        *EnterpriseOwnerInput
}
type UpdateEnterpriseInput struct {
	Name, Status, Notes, ContactName, ContactEmail, ContactPhone *string
	LowBalanceThreshold                                          *float64
	LowBalanceThresholdSet                                       bool
	Settings                                                     *EnterpriseSettings
}
type EnterpriseDepartmentInput struct {
	ParentID  *int64
	Name      string
	SortOrder int
}
type UpdateEnterpriseDepartmentInput struct {
	ParentID  *int64 // nil leaves unchanged; 0 moves to root.
	Name      *string
	SortOrder *int
}

type CreateEnterpriseMemberInput struct {
	UserID                    int64
	Email, Password, Username string
	DepartmentID              *int64
	Role, Title               string
}
type UpdateEnterpriseMemberInput struct {
	DepartmentID        *int64 // nil leaves unchanged; 0 removes department.
	Role, Status, Title *string
}
type EnterpriseQuotaLimitsInput struct{ DailyLimitUSD, WeeklyLimitUSD, MonthlyLimitUSD, TotalLimitUSD *float64 }

type EnterpriseAllowedGroupView struct {
	Group
	EnterpriseRateMultiplier *float64
	DefaultRateMultiplier    float64
	EffectiveRateMultiplier  float64
}

type EnterprisePermissions struct {
	ManageDepartments   bool `json:"manage_departments"`
	ManageMembers       bool `json:"manage_members"`
	CreateMembers       bool `json:"create_members"`
	EditMemberQuota     bool `json:"edit_member_quota"`
	EditMemberGroups    bool `json:"edit_member_groups"`
	EditQuotas          bool `json:"edit_quotas"`
	EditSettings        bool `json:"edit_settings"`
	ViewMembers         bool `json:"view_members"`
	ViewAuditLogs       bool `json:"view_audit_logs"`
	ViewDepartmentUsage bool `json:"view_department_usage"`
}
type EnterpriseOverviewData struct {
	Enterprise                                    *Enterprise
	Me                                            *EnterpriseMember
	BalanceVisible                                bool
	Balance, FrozenBalance                        *float64
	EnterpriseQuota, DepartmentQuota, MemberQuota *EnterpriseQuota
	DepartmentQuotaVisible, MemberQuotaVisible    bool
	Settings                                      *EnterpriseSettings
	Permissions                                   EnterprisePermissions
}
type EnterpriseIdentity struct {
	ID               int64    `json:"id"`
	Name             string   `json:"name"`
	Role             string   `json:"role"`
	DepartmentID     *int64   `json:"department_id"`
	DepartmentName   *string  `json:"department_name"`
	MemberStatus     string   `json:"member_status"`
	EnterpriseStatus string   `json:"enterprise_status"`
	BalanceVisible   bool     `json:"balance_visible"`
	Balance          *float64 `json:"balance"`
	FrozenBalance    *float64 `json:"frozen_balance"`
	// MemberQuota is the caller's own member quota. It is always populated (nil
	// only when no quota row exists yet) and never gated by visibility settings.
	MemberQuota *EnterpriseQuota `json:"-"`
}

type enterpriseManagementUserReader interface {
	GetByID(context.Context, int64) (*User, error)
	BatchSetConcurrency(context.Context, []int64, int) (int, error)
}
type enterpriseManagementConcurrencyDefaults interface {
	GetDefaultConcurrency(context.Context) int
}
type enterpriseManagementUserCreator interface {
	CreateUser(context.Context, *CreateUserInput) (*User, error)
}
type enterpriseManagementGroupReader interface {
	GetByID(context.Context, int64) (*Group, error)
	ListActive(context.Context) ([]Group, error)
}
type enterpriseManagementAuthInvalidator interface{ InvalidateAuthCacheByUserID(context.Context, int64) }

type EnterpriseService struct {
	client           *dbent.Client
	enterprises      EnterpriseRepository
	departments      EnterpriseDepartmentRepository
	members          EnterpriseMemberRepository
	quotas           EnterpriseQuotaRepository
	balanceLogs      EnterpriseBalanceLogRepository
	auditLogs        EnterpriseAuditLogRepository
	enterpriseGroups EnterpriseGroupRepository
	users            enterpriseManagementUserReader
	groups           enterpriseManagementGroupReader
	groupRates       UserGroupRateRepository
	admin            enterpriseManagementUserCreator
	settings         enterpriseManagementConcurrencyDefaults
	sessions         enterpriseSessionRevoker
	authCache        enterpriseManagementAuthInvalidator
	billingCache     EnterpriseBillingCache
	// transact can be replaced in unit tests. Production always uses Ent transactions.
	transact func(context.Context, func(context.Context) error) error
}

func NewEnterpriseService(client *dbent.Client, enterprises EnterpriseRepository, departments EnterpriseDepartmentRepository, members EnterpriseMemberRepository, quotas EnterpriseQuotaRepository, balanceLogs EnterpriseBalanceLogRepository, auditLogs EnterpriseAuditLogRepository, enterpriseGroups EnterpriseGroupRepository, users UserRepository, groups GroupRepository, admin AdminService, authCache APIKeyAuthCacheInvalidator, billingCache EnterpriseBillingCache, settings *SettingService) *EnterpriseService {
	s := &EnterpriseService{client: client, enterprises: enterprises, departments: departments, members: members, quotas: quotas, balanceLogs: balanceLogs, auditLogs: auditLogs, enterpriseGroups: enterpriseGroups, users: users, groups: groups, admin: admin, authCache: authCache, billingCache: billingCache}
	if settings != nil {
		s.settings = settings
	}
	return s
}
