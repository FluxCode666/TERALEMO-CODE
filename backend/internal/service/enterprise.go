package service

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"slices"
	"time"
)

type EnterpriseSettings = domain.EnterpriseSettings

func DefaultEnterpriseSettings() EnterpriseSettings { return domain.DefaultEnterpriseSettings() }

const (
	EnterpriseRoleOwner                             = domain.EnterpriseRoleOwner
	EnterpriseRoleManager                           = domain.EnterpriseRoleManager
	EnterpriseRoleMember                            = domain.EnterpriseRoleMember
	EnterpriseAuditActorRoleSystem                  = domain.EnterpriseAuditActorRoleSystem
	EnterpriseStatusActive                          = domain.EnterpriseStatusActive
	EnterpriseStatusDisabled                        = domain.EnterpriseStatusDisabled
	EnterpriseMemberStatusActive                    = domain.EnterpriseMemberStatusActive
	EnterpriseMemberStatusDisabled                  = domain.EnterpriseMemberStatusDisabled
	EnterpriseScopeEnterprise                       = domain.EnterpriseScopeEnterprise
	EnterpriseScopeDepartment                       = domain.EnterpriseScopeDepartment
	EnterpriseScopeMember                           = domain.EnterpriseScopeMember
	EnterpriseBalanceOperationAdd                   = domain.EnterpriseBalanceOperationAdd
	EnterpriseBalanceOperationSubtract              = domain.EnterpriseBalanceOperationSubtract
	EnterpriseQuotaWindowDaily                      = domain.EnterpriseQuotaWindowDaily
	EnterpriseQuotaWindowWeekly                     = domain.EnterpriseQuotaWindowWeekly
	EnterpriseQuotaWindowMonthly                    = domain.EnterpriseQuotaWindowMonthly
	EnterpriseQuotaWindowTotal                      = domain.EnterpriseQuotaWindowTotal
	EnterpriseAuditActionEnterpriseCreate           = domain.EnterpriseAuditActionEnterpriseCreate
	EnterpriseAuditActionEnterpriseUpdate           = domain.EnterpriseAuditActionEnterpriseUpdate
	EnterpriseAuditActionEnterpriseDelete           = domain.EnterpriseAuditActionEnterpriseDelete
	EnterpriseAuditActionBalanceAdjust              = domain.EnterpriseAuditActionBalanceAdjust
	EnterpriseAuditActionDepartmentCreate           = domain.EnterpriseAuditActionDepartmentCreate
	EnterpriseAuditActionDepartmentUpdate           = domain.EnterpriseAuditActionDepartmentUpdate
	EnterpriseAuditActionDepartmentDelete           = domain.EnterpriseAuditActionDepartmentDelete
	EnterpriseAuditActionMemberAdd                  = domain.EnterpriseAuditActionMemberAdd
	EnterpriseAuditActionMemberCreate               = domain.EnterpriseAuditActionMemberCreate
	EnterpriseAuditActionMemberUpdate               = domain.EnterpriseAuditActionMemberUpdate
	EnterpriseAuditActionMemberRemove               = domain.EnterpriseAuditActionMemberRemove
	EnterpriseAuditActionMembersConcurrencyUpdate   = domain.EnterpriseAuditActionMembersConcurrencyUpdate
	EnterpriseAuditActionMemberDisabledByModeration = domain.EnterpriseAuditActionMemberDisabledByModeration
	EnterpriseAuditActionQuotaUpdate                = domain.EnterpriseAuditActionQuotaUpdate
	EnterpriseAuditActionQuotaReset                 = domain.EnterpriseAuditActionQuotaReset
	EnterpriseAuditActionSettingsUpdate             = domain.EnterpriseAuditActionSettingsUpdate
	EnterpriseAuditActionGroupPoolUpdate            = domain.EnterpriseAuditActionGroupPoolUpdate
	EnterpriseAuditActionGroupWhitelistUpdate       = domain.EnterpriseAuditActionGroupWhitelistUpdate
)

var (
	ErrEnterpriseNotFound                      = domain.ErrEnterpriseNotFound
	ErrEnterpriseDepartmentNotFound            = domain.ErrEnterpriseDepartmentNotFound
	ErrEnterpriseMemberNotFound                = domain.ErrEnterpriseMemberNotFound
	ErrEnterpriseNameExists                    = domain.ErrEnterpriseNameExists
	ErrEnterpriseDepartmentNameExists          = domain.ErrEnterpriseDepartmentNameExists
	ErrEnterpriseMemberExists                  = domain.ErrEnterpriseMemberExists
	ErrEnterpriseMemberAdminForbidden          = domain.ErrEnterpriseMemberAdminForbidden
	ErrEnterpriseManagerRequiresDepartment     = domain.ErrEnterpriseManagerRequiresDepartment
	ErrEnterpriseLastOwner                     = domain.ErrEnterpriseLastOwner
	ErrEnterpriseDepartmentNotEmpty            = domain.ErrEnterpriseDepartmentNotEmpty
	ErrEnterpriseNotEmpty                      = domain.ErrEnterpriseNotEmpty
	ErrEnterpriseNotMember                     = domain.ErrEnterpriseNotMember
	ErrEnterprisePermissionDenied              = domain.ErrEnterprisePermissionDenied
	ErrEnterpriseDisabled                      = domain.ErrEnterpriseDisabled
	ErrEnterpriseMemberDisabled                = domain.ErrEnterpriseMemberDisabled
	ErrEnterpriseBalanceInsufficient           = domain.ErrEnterpriseBalanceInsufficient
	ErrEnterpriseQuotaExceeded                 = domain.ErrEnterpriseQuotaExceeded
	ErrEnterpriseQuotaExceedsParent            = domain.ErrEnterpriseQuotaExceedsParent
	ErrEnterpriseGroupOutOfScope               = domain.ErrEnterpriseGroupOutOfScope
	ErrEnterpriseGroupNotAllowed               = domain.ErrEnterpriseGroupNotAllowed
	ErrEnterpriseMemberWalletDisabled          = domain.ErrEnterpriseMemberWalletDisabled
	ErrEnterpriseFeatureUnsupported            = domain.ErrEnterpriseFeatureUnsupported
	ErrEnterpriseInvalidScope                  = domain.ErrEnterpriseInvalidScope
	ErrDepartmentQuotaExceeded                 = domain.ErrDepartmentQuotaExceeded
	ErrMemberQuotaExceeded                     = domain.ErrMemberQuotaExceeded
	ErrEnterpriseBalanceAdjustmentInsufficient = domain.ErrEnterpriseBalanceAdjustmentInsufficient
)

type Enterprise struct {
	ID                                             int64
	Name, Status                                   string
	GroupPoolConfigured                            bool
	MemberConcurrency                              *int
	Balance, FrozenBalance, TotalRecharged         float64
	LowBalanceThreshold                            *float64
	ContactName, ContactEmail, ContactPhone, Notes string
	Settings                                       EnterpriseSettings
	MemberCount, DepartmentCount                   int64
	ActiveOwnerCount                               int `json:"active_owner_count"`
	CreatedAt, UpdatedAt                           time.Time
	DeletedAt                                      *time.Time
}
type EnterpriseDepartment struct {
	ID, EnterpriseID     int64
	ParentID             *int64
	Path                 string
	PathIDs              []int64
	PathNames            []string
	Depth                int
	TotalMemberCount     int64
	Name                 string
	SortOrder            int
	MemberCount          int64
	CreatedAt, UpdatedAt time.Time
	DeletedAt            *time.Time
}
type EnterpriseMember struct {
	AccountCreatedByEnterprise                  bool
	Concurrency                                 int
	ID, EnterpriseID, UserID                    int64
	DepartmentPathIDs                           []int64
	DepartmentPathNames                         []string
	LastLoginAt                                 *time.Time
	DepartmentID                                *int64
	Role, Status, Title                         string
	Email, Username, UserStatus, DepartmentName string
	JoinedAt, CreatedAt, UpdatedAt              time.Time
	DeletedAt                                   *time.Time
}
type EnterpriseQuota struct {
	// UsageVisible is response-only metadata; nil preserves full visibility for internal callers.
	UsageVisible                                                  *bool `json:"-"`
	ID, EnterpriseID                                              int64
	ScopeType                                                     string
	ScopeID                                                       int64
	DailyLimitUSD, WeeklyLimitUSD, MonthlyLimitUSD, TotalLimitUSD *float64
	DailyUsageUSD, WeeklyUsageUSD, MonthlyUsageUSD, TotalUsageUSD float64
	DailyWindowStart, WeeklyWindowStart, MonthlyWindowStart       *time.Time
	CreatedAt, UpdatedAt                                          time.Time
	DeletedAt                                                     *time.Time
}
type EnterpriseQuotaScope struct {
	ScopeType string
	ScopeID   int64
}
type EnterpriseBalanceLog struct {
	ID, EnterpriseID                    int64
	Type                                string
	Amount, BalanceBefore, BalanceAfter float64
	OperatorUserID                      *int64
	OperatorEmail, Notes                string
	CreatedAt                           time.Time
}
type EnterpriseBalanceAdjustment struct {
	Operation      string
	Amount         float64
	OperatorUserID *int64
	Notes          string
}
type EnterpriseAuditLog struct {
	ID, EnterpriseID, ActorUserID             int64
	ActorRole, ActorEmail, Action, TargetType string
	TargetID                                  *int64
	Detail                                    map[string]any
	CreatedAt                                 time.Time
}
type EnterpriseAllowedGroup struct {
	GroupID        int64    `json:"group_id"`
	RateMultiplier *float64 `json:"rate_multiplier"`
}
type EnterpriseGroupWhitelist struct {
	EnterpriseID int64
	ScopeType    string
	ScopeID      int64
	GroupIDs     []int64
}

// EnterpriseMemberContext is a snapshot of the current, non-deleted membership.
// Disabled enterprises/members remain present so callers can reject them explicitly.
type EnterpriseMemberContext struct {
	EnterpriseID                     int64
	EnterpriseName, EnterpriseStatus string
	MemberID                         int64
	MemberStatus, Role               string
	DepartmentID                     *int64
	DepartmentPathIDs                []int64 // Frozen root-to-leaf billing scopes.
	DepartmentSubtreeIDs             []int64 // Current management/usage access scope.
	GroupPoolConfigured              bool
	GroupPoolIDs                     []int64
	GroupWhitelistConfigured         bool
	GroupWhitelistIDs                []int64
}

func (c *EnterpriseMemberContext) Active() bool {
	return c != nil && c.EnterpriseStatus == EnterpriseStatusActive && c.MemberStatus == EnterpriseMemberStatusActive
}

// AllowsGroup only applies enterprise restrictions; callers apply personal group rules
// when GroupPoolConfigured is false. A configured but empty intersection denies all.
func (c *EnterpriseMemberContext) AllowsGroup(id int64) bool {
	return c == nil || ((!c.GroupPoolConfigured || slices.Contains(c.GroupPoolIDs, id)) && (!c.GroupWhitelistConfigured || slices.Contains(c.GroupWhitelistIDs, id)))
}

// ApplyGroupWhitelist combines one configured scope into the authentication snapshot.
// An empty configured scope is a restriction that denies every group.
func (c *EnterpriseMemberContext) ApplyGroupWhitelist(ids []int64) {
	if !c.GroupWhitelistConfigured {
		c.GroupWhitelistConfigured = true
		c.GroupWhitelistIDs = append([]int64{}, ids...)
		return
	}
	c.GroupWhitelistIDs = slices.DeleteFunc(c.GroupWhitelistIDs, func(id int64) bool { return !slices.Contains(ids, id) })
}

type EnterpriseListFilters struct{ Search, Status string }
type EnterpriseMemberListFilters struct {
	Search, Role, Status string
	DepartmentScopeID    int64  // Server-only mandatory manager subtree.
	DepartmentID         *int64 // nil: all departments; pointer to 0: no department
}

// Create/Delete also create/soft-delete the matching quota in the same transaction.
// Update writes management fields, never Balance or TotalRecharged. Callers pass a
// complete management model; API patch handling belongs in the service layer.
type EnterpriseRepository interface {
	Create(ctx context.Context, enterprise *Enterprise) error
	GetByID(ctx context.Context, id int64) (*Enterprise, error)
	Update(ctx context.Context, enterprise *Enterprise) error
	Delete(ctx context.Context, id int64) error
	List(ctx context.Context, params pagination.PaginationParams, filters EnterpriseListFilters) ([]Enterprise, *pagination.PaginationResult, error)
	GetBalance(ctx context.Context, enterpriseID int64) (float64, error)
	AdjustBalance(ctx context.Context, enterpriseID int64, adjustment EnterpriseBalanceAdjustment) (*EnterpriseBalanceLog, error)
	GetMemberContextByUserID(ctx context.Context, userID int64) (*EnterpriseMemberContext, error)
	GetMemberContextsByUserIDs(ctx context.Context, userIDs []int64) (map[int64]*EnterpriseMemberContext, error)
	ListMemberUserIDs(ctx context.Context, enterpriseID int64) ([]int64, error)
}
type EnterpriseDepartmentRepository interface {
	Create(ctx context.Context, department *EnterpriseDepartment) error
	GetByID(ctx context.Context, enterpriseID, departmentID int64) (*EnterpriseDepartment, error)
	Update(ctx context.Context, department *EnterpriseDepartment) error
	Delete(ctx context.Context, enterpriseID, departmentID int64) error
	List(ctx context.Context, enterpriseID int64) ([]EnterpriseDepartment, error)
}
type EnterpriseMemberRepository interface {
	Create(ctx context.Context, member *EnterpriseMember) error
	GetByID(ctx context.Context, enterpriseID, memberID int64) (*EnterpriseMember, error)
	GetByUserID(ctx context.Context, userID int64) (*EnterpriseMember, error)
	Update(ctx context.Context, member *EnterpriseMember) error
	Delete(ctx context.Context, enterpriseID, memberID int64) error
	List(ctx context.Context, enterpriseID int64, params pagination.PaginationParams, filters EnterpriseMemberListFilters) ([]EnterpriseMember, *pagination.PaginationResult, error)
	// CountActiveOwners locks the enterprise row. Use an outer Ent transaction
	// encompassing this count and owner mutations to protect the last owner.
	CountActiveOwners(ctx context.Context, enterpriseID int64) (int64, error)
}

// Limits are full replacements. UpdateLimits must not overwrite usage/windows.
// GetByScopes omits missing/deleted rows; callers must reject missing required scopes.
type EnterpriseQuotaRepository interface {
	GetByScope(ctx context.Context, enterpriseID int64, scopeType string, scopeID int64) (*EnterpriseQuota, error)
	GetByScopes(ctx context.Context, enterpriseID int64, scopes []EnterpriseQuotaScope) ([]EnterpriseQuota, error)
	List(ctx context.Context, enterpriseID int64, scopeType string) ([]EnterpriseQuota, error)
	UpdateLimits(ctx context.Context, quota *EnterpriseQuota) error
	ResetUsage(ctx context.Context, enterpriseID int64, scopeType string, scopeID int64, windows []string, now time.Time) error
}
type EnterpriseBalanceLogRepository interface {
	List(ctx context.Context, enterpriseID int64, params pagination.PaginationParams) ([]EnterpriseBalanceLog, *pagination.PaginationResult, error)
}
type EnterpriseAuditLogRepository interface {
	Create(ctx context.Context, log *EnterpriseAuditLog) error
	List(ctx context.Context, enterpriseID int64, params pagination.PaginationParams, action string) ([]EnterpriseAuditLog, *pagination.PaginationResult, error)
}
type EnterpriseGroupRepository interface {
	GetAllowedGroupIDs(ctx context.Context, enterpriseID int64) ([]int64, error)
	SetAllowedGroupIDs(ctx context.Context, enterpriseID int64, groupIDs []int64) error
	GetWhitelist(ctx context.Context, enterpriseID int64, scopeType string, scopeID int64) (*EnterpriseGroupWhitelist, error)
	ListWhitelists(ctx context.Context, enterpriseID int64) ([]EnterpriseGroupWhitelist, error)
	SetWhitelist(ctx context.Context, enterpriseID int64, scopeType string, scopeID int64, groupIDs []int64) error
}

// EnterpriseGroupRateRepository extends group pools with optional billing overrides.
type EnterpriseGroupRateRepository interface {
	GetAllowedGroups(ctx context.Context, enterpriseID int64) ([]EnterpriseAllowedGroup, error)
	SetAllowedGroups(ctx context.Context, enterpriseID int64, groups []EnterpriseAllowedGroup) error
}

type EnterpriseBillingCache interface {
	InvalidateEnterpriseBalance(ctx context.Context, enterpriseID int64) error
	InvalidateEnterpriseQuota(ctx context.Context, scopeType string, scopeID int64) error
}
type NoopEnterpriseBillingCache struct{}

func NewNoopEnterpriseBillingCache() EnterpriseBillingCache { return NoopEnterpriseBillingCache{} }
func (NoopEnterpriseBillingCache) InvalidateEnterpriseBalance(context.Context, int64) error {
	return nil
}
func (NoopEnterpriseBillingCache) InvalidateEnterpriseQuota(context.Context, string, int64) error {
	return nil
}
