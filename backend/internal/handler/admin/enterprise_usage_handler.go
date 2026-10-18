package admin

import (
	"context"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type enterpriseUsageAPI interface {
	SeriesTrend(context.Context, service.EnterpriseUsageScope, usagestats.UsageLogFilters, string, string, int) ([]service.EnterpriseUsageSeriesPoint, error)
	List(context.Context, service.EnterpriseUsageScope, pagination.PaginationParams, usagestats.UsageLogFilters) ([]service.EnterpriseUsageLog, *pagination.PaginationResult, error)
	Stats(context.Context, service.EnterpriseUsageScope, usagestats.UsageLogFilters) (*service.EnterpriseUsageStats, error)
	Trend(context.Context, service.EnterpriseUsageScope, usagestats.UsageLogFilters, string) ([]service.EnterpriseUsageTrend, error)
	Models(context.Context, service.EnterpriseUsageScope, usagestats.UsageLogFilters) ([]service.EnterpriseUsageModel, error)
	APIKeyOptions(context.Context, service.EnterpriseUsageScope, usagestats.UsageLogFilters, string, int) ([]service.EnterpriseUsageAPIKeyOption, error)
	ModelOptions(context.Context, service.EnterpriseUsageScope, usagestats.UsageLogFilters, string, int) ([]service.EnterpriseUsageModelOption, error)
	Members(context.Context, service.EnterpriseUsageScope, usagestats.UsageLogFilters) ([]service.EnterpriseMemberUsage, error)
	Departments(context.Context, service.EnterpriseUsageScope, usagestats.UsageLogFilters) ([]service.EnterpriseDepartmentUsage, error)
	OverviewStats(context.Context, service.EnterpriseUsageScope) (float64, float64, error)
}

func (h *EnterpriseHandler) registerUsageRoutes(g *gin.RouterGroup, admin bool) {
	for _, route := range []struct{ path, kind string }{{"/usage/models/trend", "models-trend"}, {"/usage/members/trend", "members-trend"}, {"/usage/departments/trend", "departments-trend"}, {"/usage", "list"}, {"/usage/stats", "stats"}, {"/usage/trend", "trend"}, {"/usage/models", "models"}, {"/usage/api-key-options", "api-key-options"}, {"/usage/model-options", "model-options"}, {"/usage/members", "members"}, {"/usage/departments", "departments"}} {
		kind := route.kind
		g.GET(route.path, h.bind(admin, func(c *gin.Context, a service.EnterpriseActor, id int64) { h.usageQuery(c, a, id, kind) }))
	}
}

func enterpriseUsageScope(c *gin.Context, a service.EnterpriseActor, id int64) service.EnterpriseUsageScope {
	scope := service.EnterpriseUsageScope{EnterpriseID: id, UserID: a.UserID}
	if !a.IsAdmin {
		scope.Member, _ = middleware.GetEnterpriseMemberFromContext(c)
	}
	return scope
}

func enterpriseUsageFilters(c *gin.Context) (usagestats.UsageLogFilters, bool) {
	filters := usagestats.UsageLogFilters{Model: strings.TrimSpace(c.Query("model")), ExactTotal: true}
	for key, target := range map[string]*int64{"user_id": &filters.UserID, "department_id": &filters.EnterpriseDepartmentID, "api_key_id": &filters.APIKeyID} {
		if raw, exists := c.GetQuery(key); exists {
			value, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || value <= 0 {
				response.BadRequest(c, "Invalid "+key)
				return filters, false
			}
			*target = value
		}
	}
	if len(filters.Model) > 200 {
		response.BadRequest(c, "model is too long")
		return filters, false
	}
	query := c.Request.URL.Query()
	if query.Has("start_time") || query.Has("end_time") {
		if !query.Has("start_time") || !query.Has("end_time") {
			response.ErrorFrom(c, infraerrors.BadRequest("INVALID_DATE_RANGE", "start_time and end_time must be provided together"))
			return filters, false
		}
		for key, target := range map[string]**time.Time{"start_time": &filters.StartTime, "end_time": &filters.EndTime} {
			value, err := time.Parse(time.RFC3339, strings.TrimSpace(query.Get(key)))
			if err != nil {
				response.ErrorFrom(c, infraerrors.BadRequest("INVALID_DATE_RANGE", "Invalid "+key+"; use RFC3339 with a timezone"))
				return filters, false
			}
			*target = &value
		}
		// Precise times take precedence, including over invalid legacy dates.
		return filters, true
	}
	for key, target := range map[string]**time.Time{"start_date": &filters.StartTime, "end_date": &filters.EndTime} {
		raw := strings.TrimSpace(c.Query(key))
		if raw == "" {
			continue
		}
		value, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			value, err = timezone.ParseInLocation("2006-01-02", raw)
			if err == nil && key == "end_date" {
				value = value.AddDate(0, 0, 1)
			}
		}
		if err != nil {
			response.BadRequest(c, "Invalid "+key+"; use YYYY-MM-DD or RFC3339")
			return filters, false
		}
		*target = &value
	}
	return filters, true
}

func (h *EnterpriseHandler) usageQuery(c *gin.Context, a service.EnterpriseActor, id int64, kind string) {
	if a.IsAdmin {
		if _, err := h.management.GetEnterprise(c.Request.Context(), a, id); response.ErrorFrom(c, err) {
			return
		}
	}
	filters, ok := enterpriseUsageFilters(c)
	if !ok {
		return
	}
	scope := enterpriseUsageScope(c, a, id)
	ctx := c.Request.Context()
	switch kind {
	case "models-trend", "members-trend", "departments-trend":
		top := 8
		if raw, exists := c.GetQuery("top"); exists {
			value, err := strconv.Atoi(raw)
			if err != nil || value <= 0 {
				response.BadRequest(c, "Invalid top")
				return
			}
			top = value
		}
		items, err := h.usage.SeriesTrend(ctx, scope, filters, strings.TrimSuffix(kind, "-trend"), c.DefaultQuery("granularity", "day"), top)
		if items == nil {
			items = []service.EnterpriseUsageSeriesPoint{}
		}
		enterpriseSuccess(c, items, err)
	case "api-key-options", "model-options":
		limit := 20
		if raw, exists := c.GetQuery("limit"); exists {
			value, err := strconv.Atoi(raw)
			if err != nil || value <= 0 {
				response.BadRequest(c, "Invalid limit")
				return
			}
			limit = service.NormalizeEnterpriseUsageOptionsLimit(value)
		}
		search := strings.TrimSpace(c.Query("search"))
		if kind == "api-key-options" {
			items, err := h.usage.APIKeyOptions(ctx, scope, filters, search, limit)
			if items == nil {
				items = []service.EnterpriseUsageAPIKeyOption{}
			}
			enterpriseSuccess(c, items, err)
		} else {
			items, err := h.usage.ModelOptions(ctx, scope, filters, search, limit)
			if items == nil {
				items = []service.EnterpriseUsageModelOption{}
			}
			enterpriseSuccess(c, items, err)
		}
	case "list":
		p := enterprisePagination(c)
		p.SortBy, p.SortOrder = c.Query("sort_by"), c.Query("sort_order")
		p = service.NormalizeEnterpriseUsageSort(p)
		items, page, err := h.usage.List(ctx, scope, p, filters)
		if items == nil {
			items = []service.EnterpriseUsageLog{}
		}
		enterprisePage(c, items, page, p, err)
	case "stats":
		item, err := h.usage.Stats(ctx, scope, filters)
		enterpriseSuccess(c, item, err)
	case "trend":
		items, err := h.usage.Trend(ctx, scope, filters, c.DefaultQuery("granularity", "day"))
		if items == nil {
			items = []service.EnterpriseUsageTrend{}
		}
		enterpriseSuccess(c, items, err)
	case "models":
		items, err := h.usage.Models(ctx, scope, filters)
		if items == nil {
			items = []service.EnterpriseUsageModel{}
		}
		enterpriseSuccess(c, items, err)
	case "members":
		items, err := h.usage.Members(ctx, scope, filters)
		if items == nil {
			items = []service.EnterpriseMemberUsage{}
		}
		enterpriseSuccess(c, items, err)
	case "departments":
		items, err := h.usage.Departments(ctx, scope, filters)
		if items == nil {
			items = []service.EnterpriseDepartmentUsage{}
		}
		enterpriseSuccess(c, items, err)
	}
}
