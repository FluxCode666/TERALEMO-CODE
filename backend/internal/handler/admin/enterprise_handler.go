package admin

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// enterpriseManagementAPI is deliberately local: test doubles need only the
// management surface, without extending the application's existing interfaces.
type enterpriseManagementAPI interface {
	ListEnterprises(context.Context, service.EnterpriseActor, pagination.PaginationParams, service.EnterpriseListFilters) ([]service.Enterprise, *pagination.PaginationResult, error)
	CreateEnterprise(context.Context, service.EnterpriseActor, service.CreateEnterpriseInput) (*service.Enterprise, error)
	GetEnterprise(context.Context, service.EnterpriseActor, int64) (*service.Enterprise, error)
	UpdateEnterprise(context.Context, service.EnterpriseActor, int64, service.UpdateEnterpriseInput) (*service.Enterprise, error)
	DeleteEnterprise(context.Context, service.EnterpriseActor, int64) error
	DissolveEnterprise(context.Context, service.EnterpriseActor, int64) error
	AdjustBalance(context.Context, service.EnterpriseActor, int64, service.EnterpriseBalanceAdjustment) (*service.Enterprise, error)
	ListBalanceLogs(context.Context, service.EnterpriseActor, int64, pagination.PaginationParams) ([]service.EnterpriseBalanceLog, *pagination.PaginationResult, error)
	ListDepartments(context.Context, service.EnterpriseActor, int64) ([]service.EnterpriseDepartment, error)
	CreateDepartment(context.Context, service.EnterpriseActor, int64, service.EnterpriseDepartmentInput) (*service.EnterpriseDepartment, error)
	UpdateDepartment(context.Context, service.EnterpriseActor, int64, int64, service.UpdateEnterpriseDepartmentInput) (*service.EnterpriseDepartment, error)
	DeleteDepartment(context.Context, service.EnterpriseActor, int64, int64) error
	ListMembers(context.Context, service.EnterpriseActor, int64, pagination.PaginationParams, service.EnterpriseMemberListFilters) ([]service.EnterpriseMember, *pagination.PaginationResult, error)
	CreateMember(context.Context, service.EnterpriseActor, int64, service.CreateEnterpriseMemberInput) (*service.EnterpriseMember, error)
	UpdateMember(context.Context, service.EnterpriseActor, int64, int64, service.UpdateEnterpriseMemberInput) (*service.EnterpriseMember, error)
	RemoveMember(context.Context, service.EnterpriseActor, int64, int64) error
	ListQuotas(context.Context, service.EnterpriseActor, int64, string) ([]service.EnterpriseQuota, error)
	UpdateQuota(context.Context, service.EnterpriseActor, int64, string, int64, service.EnterpriseQuotaLimitsInput) (*service.EnterpriseQuota, error)
	ResetQuota(context.Context, service.EnterpriseActor, int64, string, int64, []string) error
	GetAllowedGroups(context.Context, service.EnterpriseActor, int64) ([]service.Group, error)
	SetAllowedGroups(context.Context, service.EnterpriseActor, int64, []int64) error
	ListGroupWhitelists(context.Context, service.EnterpriseActor, int64) ([]service.EnterpriseGroupWhitelist, error)
	SetGroupWhitelist(context.Context, service.EnterpriseActor, int64, string, int64, []int64) error
	GetSettings(context.Context, service.EnterpriseActor, int64) (service.EnterpriseSettings, error)
	UpdateSettings(context.Context, service.EnterpriseActor, int64, service.EnterpriseSettings) (service.EnterpriseSettings, error)
	ListAuditLogs(context.Context, service.EnterpriseActor, int64, pagination.PaginationParams, string) ([]service.EnterpriseAuditLog, *pagination.PaginationResult, error)
	Overview(context.Context, service.EnterpriseActor, int64) (*service.EnterpriseOverviewData, error)
}

type EnterpriseHandler struct {
	management   enterpriseManagementAPI
	memberReader middleware.EnterpriseMemberContextReader
	// Usage is added through the dedicated usage implementation below.
	usage enterpriseUsageAPI
}

func NewEnterpriseHandler(management *service.EnterpriseService, usage *service.EnterpriseUsageService, members service.EnterpriseRepository) *EnterpriseHandler {
	return &EnterpriseHandler{management: management, usage: usage, memberReader: members}
}

// RegisterAdminRoutes must be mounted below the existing administrator auth
// group. The wrapper also checks the authenticated administrator role.
func (h *EnterpriseHandler) RegisterAdminRoutes(g *gin.RouterGroup) {
	g.GET("", h.bind(true, h.listEnterprises))
	g.POST("", h.bind(true, h.createEnterprise))
	g.GET("/:id", h.bind(true, h.getEnterprise))
	g.PUT("/:id", h.bind(true, h.updateEnterprise))
	g.DELETE("/:id", h.bind(true, h.deleteEnterprise))
	g.POST("/:id/balance", h.bind(true, h.adjustBalance))
	g.GET("/:id/balance-logs", h.bind(true, h.balanceLogs))
	resources := g.Group("/:id")
	h.registerResources(resources, true)
	resources.GET("/allowed-groups", h.bind(true, h.groups))
	resources.PUT("/allowed-groups", h.bind(true, h.setAllowedGroups))
}

// RegisterEnterpriseRoutes must follow JWT authentication. Membership and the
// disabled-enterprise write guard are installed before any resource handlers.
func (h *EnterpriseHandler) RegisterEnterpriseRoutes(g *gin.RouterGroup) {
	g.Use(middleware.EnterpriseMemberAuth(h.memberReader))
	g.GET("/overview", h.bind(false, h.overview))
	h.registerResources(g, false)
	g.GET("/groups", h.bind(false, h.groups))
	g.GET("/settings", h.bind(false, h.settings))
	g.PUT("/settings", h.bind(false, h.updateSettings))
}
func (h *EnterpriseHandler) registerResources(g *gin.RouterGroup, admin bool) {
	g.GET("/departments", h.bind(admin, h.departments))
	g.POST("/departments", h.bind(admin, h.createDepartment))
	g.PUT("/departments/:departmentId", h.bind(admin, h.updateDepartment))
	g.DELETE("/departments/:departmentId", h.bind(admin, h.deleteDepartment))
	g.GET("/members", h.bind(admin, h.members))
	g.POST("/members", h.bind(admin, h.createMember))
	g.PUT("/members/:memberId", h.bind(admin, h.updateMember))
	g.DELETE("/members/:memberId", h.bind(admin, h.removeMember))
	g.GET("/quotas", h.bind(admin, h.quotas))
	g.PUT("/quotas/:scopeType/:scopeId", h.bind(admin, h.updateQuota))
	g.POST("/quotas/:scopeType/:scopeId/reset", h.bind(admin, h.resetQuota))
	g.GET("/group-whitelists", h.bind(admin, h.whitelists))
	g.PUT("/group-whitelists/:scopeType/:scopeId", h.bind(admin, h.setWhitelist))
	g.GET("/audit-logs", h.bind(admin, h.auditLogs))
	h.registerUsageRoutes(g, admin)
}

type enterpriseEndpoint func(*gin.Context, service.EnterpriseActor, int64)

func (h *EnterpriseHandler) bind(admin bool, endpoint enterpriseEndpoint) gin.HandlerFunc {
	return func(c *gin.Context) {
		subject, ok := middleware.GetAuthSubjectFromContext(c)
		if !ok {
			response.Unauthorized(c, "User not found in context")
			return
		}
		actor := service.EnterpriseActor{UserID: subject.UserID, IsAdmin: admin}
		var id int64
		if admin {
			role, ok := middleware.GetUserRoleFromContext(c)
			if !ok || role != service.RoleAdmin {
				response.ErrorFrom(c, service.ErrEnterprisePermissionDenied)
				return
			}
			if c.Param("id") != "" {
				var valid bool
				id, valid = enterprisePathID(c, "id")
				if !valid {
					return
				}
			}
		} else {
			member, ok := middleware.GetEnterpriseMemberFromContext(c)
			if !ok {
				response.ErrorFrom(c, service.ErrEnterpriseNotMember)
				return
			}
			id = member.EnterpriseID
		}
		endpoint(c, actor, id)
	}
}
func enterprisePathID(c *gin.Context, key string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(key), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid "+key)
		return 0, false
	}
	return id, true
}
func enterprisePagination(c *gin.Context) pagination.PaginationParams {
	p, s := response.ParsePagination(c)
	return pagination.PaginationParams{Page: p, PageSize: s}
}
func enterpriseSearch(c *gin.Context) string {
	s := strings.TrimSpace(c.Query("search"))
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}
func enterpriseSuccess(c *gin.Context, value any, err error) {
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, value)
}
func enterprisePage(c *gin.Context, items any, page *pagination.PaginationResult, p pagination.PaginationParams, err error) {
	if response.ErrorFrom(c, err) {
		return
	}
	var total int64
	if page != nil {
		total = page.Total
	}
	response.Paginated(c, items, total, p.Page, p.PageSize)
}
func enterpriseMap[T any, R any](items []T, convert func(*T) *R) []R {
	out := make([]R, 0, len(items))
	for i := range items {
		out = append(out, *convert(&items[i]))
	}
	return out
}
func enterpriseBody(c *gin.Context, out any) bool {
	if err := c.ShouldBindJSON(out); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return false
	}
	return true
}

func (h *EnterpriseHandler) listEnterprises(c *gin.Context, a service.EnterpriseActor, _ int64) {
	p := enterprisePagination(c)
	items, page, err := h.management.ListEnterprises(c.Request.Context(), a, p, service.EnterpriseListFilters{Search: enterpriseSearch(c), Status: c.Query("status")})
	enterprisePage(c, enterpriseMap(items, dto.EnterpriseFromService), page, p, err)
}

type enterpriseOwnerRequest struct {
	UserID   int64  `json:"user_id" binding:"omitempty,gt=0"`
	Email    string `json:"email"`
	Password string `json:"password"`
	Username string `json:"username"`
}
type createEnterpriseRequest struct {
	Name                string                      `json:"name" binding:"required,max=100"`
	Status              string                      `json:"status" binding:"omitempty,oneof=active disabled"`
	Notes               string                      `json:"notes"`
	ContactName         string                      `json:"contact_name"`
	ContactEmail        string                      `json:"contact_email"`
	ContactPhone        string                      `json:"contact_phone"`
	LowBalanceThreshold *float64                    `json:"low_balance_threshold" binding:"omitempty,gte=0"`
	Settings            *service.EnterpriseSettings `json:"settings"`
	Owner               *enterpriseOwnerRequest     `json:"owner"`
}

func (h *EnterpriseHandler) createEnterprise(c *gin.Context, a service.EnterpriseActor, _ int64) {
	var r createEnterpriseRequest
	if !enterpriseBody(c, &r) {
		return
	}
	in := service.CreateEnterpriseInput{Name: r.Name, Status: r.Status, Notes: r.Notes, ContactName: r.ContactName, ContactEmail: r.ContactEmail, ContactPhone: r.ContactPhone, LowBalanceThreshold: r.LowBalanceThreshold, Settings: r.Settings}
	if r.Owner != nil {
		in.Owner = &service.EnterpriseOwnerInput{UserID: r.Owner.UserID, Email: r.Owner.Email, Password: r.Owner.Password, Username: r.Owner.Username}
	}
	item, err := h.management.CreateEnterprise(c.Request.Context(), a, in)
	enterpriseSuccess(c, dto.EnterpriseFromService(item), err)
}
func (h *EnterpriseHandler) getEnterprise(c *gin.Context, a service.EnterpriseActor, id int64) {
	item, err := h.management.GetEnterprise(c.Request.Context(), a, id)
	if response.ErrorFrom(c, err) {
		return
	}
	quotas, err := h.management.ListQuotas(c.Request.Context(), a, id, service.EnterpriseScopeEnterprise)
	if response.ErrorFrom(c, err) {
		return
	}
	out := dto.EnterpriseFromService(item)
	for i := range quotas {
		if quotas[i].ScopeID == id {
			out.Quota = dto.EnterpriseQuotaFromService(&quotas[i])
			break
		}
	}
	response.Success(c, out)
}

type updateEnterpriseRequest struct {
	Name                *string                     `json:"name" binding:"omitempty,min=1,max=100"`
	Status              *string                     `json:"status" binding:"omitempty,oneof=active disabled"`
	Notes               *string                     `json:"notes"`
	ContactName         *string                     `json:"contact_name"`
	ContactEmail        *string                     `json:"contact_email"`
	ContactPhone        *string                     `json:"contact_phone"`
	LowBalanceThreshold json.RawMessage             `json:"low_balance_threshold"`
	Settings            *service.EnterpriseSettings `json:"settings"`
}

func (h *EnterpriseHandler) updateEnterprise(c *gin.Context, a service.EnterpriseActor, id int64) {
	var r updateEnterpriseRequest
	if !enterpriseBody(c, &r) {
		return
	}
	in := service.UpdateEnterpriseInput{Name: r.Name, Status: r.Status, Notes: r.Notes, ContactName: r.ContactName, ContactEmail: r.ContactEmail, ContactPhone: r.ContactPhone, Settings: r.Settings, LowBalanceThresholdSet: len(r.LowBalanceThreshold) > 0}
	if in.LowBalanceThresholdSet {
		if err := json.Unmarshal(r.LowBalanceThreshold, &in.LowBalanceThreshold); err != nil || in.LowBalanceThreshold != nil && *in.LowBalanceThreshold < 0 {
			response.BadRequest(c, "Invalid low_balance_threshold")
			return
		}
	}
	item, err := h.management.UpdateEnterprise(c.Request.Context(), a, id, in)
	enterpriseSuccess(c, dto.EnterpriseFromService(item), err)
}
func (h *EnterpriseHandler) deleteEnterprise(c *gin.Context, a service.EnterpriseActor, id int64) {
	var err error
	if c.Query("dissolve") == "true" {
		err = h.management.DissolveEnterprise(c.Request.Context(), a, id)
	} else {
		err = h.management.DeleteEnterprise(c.Request.Context(), a, id)
	}
	enterpriseSuccess(c, gin.H{"message": "Enterprise deleted"}, err)
}

type enterpriseBalanceRequest struct {
	Operation string  `json:"operation" binding:"required,oneof=add subtract"`
	Amount    float64 `json:"amount" binding:"required,gt=0"`
	Notes     string  `json:"notes"`
}

func (h *EnterpriseHandler) adjustBalance(c *gin.Context, a service.EnterpriseActor, id int64) {
	var r enterpriseBalanceRequest
	if !enterpriseBody(c, &r) {
		return
	}
	payload := struct {
		EnterpriseID int64                    `json:"enterprise_id"`
		Body         enterpriseBalanceRequest `json:"body"`
	}{id, r}
	executeAdminIdempotentJSON(c, "admin.enterprises.balance.adjust", payload, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		item, err := h.management.AdjustBalance(ctx, a, id, service.EnterpriseBalanceAdjustment{Operation: r.Operation, Amount: r.Amount, Notes: r.Notes, OperatorUserID: &a.UserID})
		if err != nil {
			return nil, err
		}
		return dto.EnterpriseFromService(item), nil
	})
}
func (h *EnterpriseHandler) balanceLogs(c *gin.Context, a service.EnterpriseActor, id int64) {
	p := enterprisePagination(c)
	items, page, err := h.management.ListBalanceLogs(c.Request.Context(), a, id, p)
	enterprisePage(c, enterpriseMap(items, dto.EnterpriseBalanceLogFromService), page, p, err)
}

func (h *EnterpriseHandler) departments(c *gin.Context, a service.EnterpriseActor, id int64) {
	items, err := h.management.ListDepartments(c.Request.Context(), a, id)
	enterpriseSuccess(c, enterpriseMap(items, dto.EnterpriseDepartmentFromService), err)
}

type enterpriseDepartmentRequest struct {
	ParentID  *int64  `json:"parent_id" binding:"omitempty,gte=0"`
	Name      *string `json:"name" binding:"omitempty,min=1,max=100"`
	SortOrder *int    `json:"sort_order"`
}

func (h *EnterpriseHandler) createDepartment(c *gin.Context, a service.EnterpriseActor, id int64) {
	var r enterpriseDepartmentRequest
	if !enterpriseBody(c, &r) {
		return
	}
	if r.Name == nil {
		response.BadRequest(c, "name is required")
		return
	}
	in := service.EnterpriseDepartmentInput{Name: *r.Name, ParentID: r.ParentID}
	if r.SortOrder != nil {
		in.SortOrder = *r.SortOrder
	}
	item, err := h.management.CreateDepartment(c.Request.Context(), a, id, in)
	enterpriseSuccess(c, dto.EnterpriseDepartmentFromService(item), err)
}
func (h *EnterpriseHandler) updateDepartment(c *gin.Context, a service.EnterpriseActor, id int64) {
	departmentID, ok := enterprisePathID(c, "departmentId")
	if !ok {
		return
	}
	var r enterpriseDepartmentRequest
	if !enterpriseBody(c, &r) {
		return
	}
	in := service.UpdateEnterpriseDepartmentInput{Name: r.Name, SortOrder: r.SortOrder, ParentID: r.ParentID}
	item, err := h.management.UpdateDepartment(c.Request.Context(), a, id, departmentID, in)
	enterpriseSuccess(c, dto.EnterpriseDepartmentFromService(item), err)
}
func (h *EnterpriseHandler) deleteDepartment(c *gin.Context, a service.EnterpriseActor, id int64) {
	departmentID, ok := enterprisePathID(c, "departmentId")
	if !ok {
		return
	}
	enterpriseSuccess(c, gin.H{"message": "Department deleted"}, h.management.DeleteDepartment(c.Request.Context(), a, id, departmentID))
}

func (h *EnterpriseHandler) members(c *gin.Context, a service.EnterpriseActor, id int64) {
	p := enterprisePagination(c)
	f := service.EnterpriseMemberListFilters{Search: enterpriseSearch(c), Role: c.Query("role"), Status: c.Query("status")}
	if raw, present := c.GetQuery("department_id"); present {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 0 {
			response.BadRequest(c, "Invalid department_id")
			return
		}
		f.DepartmentID = &n
	}
	items, page, err := h.management.ListMembers(c.Request.Context(), a, id, p, f)
	enterprisePage(c, enterpriseMap(items, dto.EnterpriseMemberFromService), page, p, err)
}

type enterpriseMemberCreateRequest struct {
	UserID       int64  `json:"user_id" binding:"omitempty,gt=0"`
	Email        string `json:"email"`
	Password     string `json:"password"`
	Username     string `json:"username"`
	DepartmentID *int64 `json:"department_id" binding:"omitempty,gte=0"`
	Role         string `json:"role" binding:"required,oneof=owner manager member"`
	Title        string `json:"title"`
}

func (h *EnterpriseHandler) createMember(c *gin.Context, a service.EnterpriseActor, id int64) {
	var r enterpriseMemberCreateRequest
	if !enterpriseBody(c, &r) {
		return
	}
	item, err := h.management.CreateMember(c.Request.Context(), a, id, service.CreateEnterpriseMemberInput{UserID: r.UserID, Email: r.Email, Password: r.Password, Username: r.Username, DepartmentID: r.DepartmentID, Role: r.Role, Title: r.Title})
	enterpriseSuccess(c, dto.EnterpriseMemberFromService(item), err)
}

type enterpriseMemberUpdateRequest struct {
	DepartmentID *int64  `json:"department_id" binding:"omitempty,gte=0"`
	Role         *string `json:"role" binding:"omitempty,oneof=owner manager member"`
	Status       *string `json:"status" binding:"omitempty,oneof=active disabled"`
	Title        *string `json:"title"`
}

func (h *EnterpriseHandler) updateMember(c *gin.Context, a service.EnterpriseActor, id int64) {
	memberID, ok := enterprisePathID(c, "memberId")
	if !ok {
		return
	}
	var r enterpriseMemberUpdateRequest
	if !enterpriseBody(c, &r) {
		return
	}
	item, err := h.management.UpdateMember(c.Request.Context(), a, id, memberID, service.UpdateEnterpriseMemberInput{DepartmentID: r.DepartmentID, Role: r.Role, Status: r.Status, Title: r.Title})
	enterpriseSuccess(c, dto.EnterpriseMemberFromService(item), err)
}
func (h *EnterpriseHandler) removeMember(c *gin.Context, a service.EnterpriseActor, id int64) {
	memberID, ok := enterprisePathID(c, "memberId")
	if !ok {
		return
	}
	enterpriseSuccess(c, gin.H{"message": "Member removed"}, h.management.RemoveMember(c.Request.Context(), a, id, memberID))
}

func (h *EnterpriseHandler) quotas(c *gin.Context, a service.EnterpriseActor, id int64) {
	items, err := h.management.ListQuotas(c.Request.Context(), a, id, c.Query("scope_type"))
	enterpriseSuccess(c, enterpriseMap(items, dto.EnterpriseQuotaFromService), err)
}
func enterpriseScope(c *gin.Context) (string, int64, bool) {
	scope := c.Param("scopeType")
	switch scope {
	case service.EnterpriseScopeEnterprise, service.EnterpriseScopeDepartment, service.EnterpriseScopeMember:
	default:
		response.ErrorFrom(c, service.ErrEnterpriseInvalidScope)
		return "", 0, false
	}
	id, ok := enterprisePathID(c, "scopeId")
	return scope, id, ok
}
func (h *EnterpriseHandler) updateQuota(c *gin.Context, a service.EnterpriseActor, id int64) {
	scope, scopeID, ok := enterpriseScope(c)
	if !ok {
		return
	}
	var fields map[string]json.RawMessage
	if !enterpriseBody(c, &fields) {
		return
	}
	limits := service.EnterpriseQuotaLimitsInput{}
	for key, ptr := range map[string]**float64{"daily_limit_usd": &limits.DailyLimitUSD, "weekly_limit_usd": &limits.WeeklyLimitUSD, "monthly_limit_usd": &limits.MonthlyLimitUSD, "total_limit_usd": &limits.TotalLimitUSD} {
		value, exists := fields[key]
		if !exists || json.Unmarshal(value, ptr) != nil || *ptr != nil && **ptr < 0 {
			response.BadRequest(c, "Invalid or missing "+key)
			return
		}
	}
	item, err := h.management.UpdateQuota(c.Request.Context(), a, id, scope, scopeID, limits)
	enterpriseSuccess(c, dto.EnterpriseQuotaFromService(item), err)
}
func (h *EnterpriseHandler) resetQuota(c *gin.Context, a service.EnterpriseActor, id int64) {
	scope, scopeID, ok := enterpriseScope(c)
	if !ok {
		return
	}
	var r struct {
		Windows []string `json:"windows" binding:"required,min=1,dive,oneof=daily weekly monthly total"`
	}
	if !enterpriseBody(c, &r) {
		return
	}
	if response.ErrorFrom(c, h.management.ResetQuota(c.Request.Context(), a, id, scope, scopeID, r.Windows)) {
		return
	}
	items, err := h.management.ListQuotas(c.Request.Context(), a, id, scope)
	if response.ErrorFrom(c, err) {
		return
	}
	for i := range items {
		if items[i].ScopeID == scopeID {
			response.Success(c, dto.EnterpriseQuotaFromService(&items[i]))
			return
		}
	}
	response.ErrorFrom(c, service.ErrEnterpriseInvalidScope)
}

func (h *EnterpriseHandler) groups(c *gin.Context, a service.EnterpriseActor, id int64) {
	items, err := h.management.GetAllowedGroups(c.Request.Context(), a, id)
	enterpriseSuccess(c, enterpriseMap(items, dto.EnterpriseGroupFromService), err)
}

type enterpriseGroupsRequest struct {
	GroupIDs []int64 `json:"group_ids" binding:"required,dive,gt=0"`
}

func (h *EnterpriseHandler) setAllowedGroups(c *gin.Context, a service.EnterpriseActor, id int64) {
	var r enterpriseGroupsRequest
	if !enterpriseBody(c, &r) {
		return
	}
	if response.ErrorFrom(c, h.management.SetAllowedGroups(c.Request.Context(), a, id, r.GroupIDs)) {
		return
	}
	h.groups(c, a, id)
}
func (h *EnterpriseHandler) whitelists(c *gin.Context, a service.EnterpriseActor, id int64) {
	items, err := h.management.ListGroupWhitelists(c.Request.Context(), a, id)
	enterpriseSuccess(c, enterpriseMap(items, dto.EnterpriseGroupWhitelistFromService), err)
}
func (h *EnterpriseHandler) setWhitelist(c *gin.Context, a service.EnterpriseActor, id int64) {
	scope, scopeID, ok := enterpriseScope(c)
	if !ok {
		return
	}
	var r enterpriseGroupsRequest
	if !enterpriseBody(c, &r) {
		return
	}
	if response.ErrorFrom(c, h.management.SetGroupWhitelist(c.Request.Context(), a, id, scope, scopeID, r.GroupIDs)) {
		return
	}
	response.Success(c, dto.EnterpriseGroupWhitelist{ScopeType: scope, ScopeID: scopeID, GroupIDs: r.GroupIDs})
}
func (h *EnterpriseHandler) settings(c *gin.Context, a service.EnterpriseActor, id int64) {
	item, err := h.management.GetSettings(c.Request.Context(), a, id)
	enterpriseSuccess(c, item, err)
}
func (h *EnterpriseHandler) updateSettings(c *gin.Context, a service.EnterpriseActor, id int64) {
	var r service.EnterpriseSettings
	if !enterpriseBody(c, &r) {
		return
	}
	item, err := h.management.UpdateSettings(c.Request.Context(), a, id, r)
	enterpriseSuccess(c, item, err)
}
func (h *EnterpriseHandler) auditLogs(c *gin.Context, a service.EnterpriseActor, id int64) {
	p := enterprisePagination(c)
	items, page, err := h.management.ListAuditLogs(c.Request.Context(), a, id, p, c.Query("action"))
	enterprisePage(c, enterpriseMap(items, dto.EnterpriseAuditLogFromService), page, p, err)
}

func (h *EnterpriseHandler) overview(c *gin.Context, a service.EnterpriseActor, id int64) {
	item, err := h.management.Overview(c.Request.Context(), a, id)
	if response.ErrorFrom(c, err) {
		return
	}
	out := dto.EnterpriseOverviewFromService(item)
	today, month, err := h.usage.OverviewStats(c.Request.Context(), enterpriseUsageScope(c, a, id))
	if response.ErrorFrom(c, err) {
		return
	}
	out.Stats = dto.EnterpriseOverviewStats{TodayCost: today, MonthCost: month}
	response.Success(c, out)
}
