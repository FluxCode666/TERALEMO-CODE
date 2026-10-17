package service

import (
	"context"
	"fmt"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

// EnterpriseUsageScope is supplied by an authenticated enterprise handler. A nil
// Member represents a system administrator whose enterprise was already loaded.
type EnterpriseUsageScope struct {
	EnterpriseID int64
	UserID       int64
	Member       *EnterpriseMemberContext
}

type EnterpriseUsageUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
}
type EnterpriseUsageNamedResource struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// EnterpriseUsageLog deliberately contains only the enterprise-visible fields.
// Never serialize UsageLog or its full User/APIKey/Account associations here.
type EnterpriseUsageLog struct {
	ID                  int64                         `json:"id"`
	User                EnterpriseUsageUser           `json:"user"`
	DepartmentID        *int64                        `json:"department_id"`
	APIKey              *EnterpriseUsageNamedResource `json:"api_key"`
	Group               *EnterpriseUsageNamedResource `json:"group"`
	Model               string                        `json:"model"`
	InputTokens         int                           `json:"input_tokens"`
	OutputTokens        int                           `json:"output_tokens"`
	CacheCreationTokens int                           `json:"cache_creation_tokens"`
	CacheReadTokens     int                           `json:"cache_read_tokens"`
	ActualCost          float64                       `json:"actual_cost"`
	BillingType         int8                          `json:"billing_type"`
	RequestType         RequestType                   `json:"request_type"`
	Stream              bool                          `json:"stream"`
	DurationMs          int                           `json:"duration_ms"`
	FirstTokenMs        *int                          `json:"first_token_ms"`
	CreatedAt           time.Time                     `json:"created_at"`
}
type EnterpriseUsageStats struct {
	TotalRequests   int64   `json:"total_requests"`
	TotalTokens     int64   `json:"total_tokens"`
	TotalActualCost float64 `json:"total_actual_cost"`
}
type EnterpriseUsageTrend struct {
	Date        string  `json:"date"`
	Requests    int64   `json:"requests"`
	TotalTokens int64   `json:"total_tokens"`
	ActualCost  float64 `json:"actual_cost"`
}
type EnterpriseUsageModel struct {
	Model       string  `json:"model"`
	Requests    int64   `json:"requests"`
	TotalTokens int64   `json:"total_tokens"`
	ActualCost  float64 `json:"actual_cost"`
}
type EnterpriseMemberUsage struct {
	UserID         int64   `json:"user_id"`
	Username       string  `json:"username"`
	Email          string  `json:"email"`
	DepartmentID   *int64  `json:"department_id"`
	DepartmentName *string `json:"department_name"`
	Requests       int64   `json:"requests"`
	TotalTokens    int64   `json:"total_tokens"`
	ActualCost     float64 `json:"actual_cost"`
}
type EnterpriseDepartmentUsage struct {
	DepartmentID   *int64  `json:"department_id"`
	DepartmentName *string `json:"department_name"`
	Requests       int64   `json:"requests"`
	TotalTokens    int64   `json:"total_tokens"`
	ActualCost     float64 `json:"actual_cost"`
}

// These narrow capabilities keep enterprise queries out of UsageLogRepository.
type EnterpriseUsageLogReader interface {
	ListEnterpriseUsageLogs(context.Context, pagination.PaginationParams, usagestats.UsageLogFilters) ([]EnterpriseUsageLog, *pagination.PaginationResult, error)
}
type EnterpriseUsageRankingRepository interface {
	GetEnterpriseMemberUsage(context.Context, usagestats.UsageLogFilters) ([]EnterpriseMemberUsage, error)
	GetEnterpriseDepartmentUsage(context.Context, usagestats.UsageLogFilters) ([]EnterpriseDepartmentUsage, error)
}
type enterpriseUsageAnalyticsReader interface {
	GetUsageTrendWithUsageFilters(context.Context, time.Time, time.Time, string, usagestats.UsageLogFilters) ([]usagestats.TrendDataPoint, error)
	GetModelStatsWithUsageFiltersBySource(context.Context, time.Time, time.Time, usagestats.UsageLogFilters, string) ([]usagestats.ModelStat, error)
}

type EnterpriseUsageService struct{ repo UsageLogRepository }

func NewEnterpriseUsageService(repo UsageLogRepository) *EnterpriseUsageService {
	return &EnterpriseUsageService{repo: repo}
}

// ConstrainEnterpriseUsageFilters merges requested filters with mandatory access
// scope. It never joins current membership, preserving historical attribution.
func ConstrainEnterpriseUsageFilters(scope EnterpriseUsageScope, filters usagestats.UsageLogFilters, now time.Time) (usagestats.UsageLogFilters, error) {
	if scope.EnterpriseID <= 0 {
		return filters, ErrEnterpriseNotFound
	}
	if filters.EnterpriseID != 0 && filters.EnterpriseID != scope.EnterpriseID {
		return filters, ErrEnterprisePermissionDenied
	}
	filters.EnterpriseID = scope.EnterpriseID
	if member := scope.Member; member != nil {
		if member.EnterpriseID != scope.EnterpriseID {
			return filters, ErrEnterprisePermissionDenied
		}
		if member.MemberStatus != EnterpriseMemberStatusActive {
			return filters, ErrEnterpriseMemberDisabled
		}
		switch member.Role {
		case EnterpriseRoleOwner:
		case EnterpriseRoleManager:
			if member.DepartmentID == nil || *member.DepartmentID <= 0 {
				return filters, ErrEnterprisePermissionDenied
			}
			if filters.EnterpriseDepartmentID != 0 && filters.EnterpriseDepartmentID != *member.DepartmentID {
				return filters, ErrEnterprisePermissionDenied
			}
			filters.EnterpriseDepartmentID = *member.DepartmentID
		case EnterpriseRoleMember:
			if scope.UserID <= 0 || (filters.UserID != 0 && filters.UserID != scope.UserID) {
				return filters, ErrEnterprisePermissionDenied
			}
			filters.UserID = scope.UserID
		default:
			return filters, ErrEnterprisePermissionDenied
		}
	}
	if filters.UserID < 0 || filters.APIKeyID < 0 || filters.EnterpriseDepartmentID < 0 {
		return filters, infraerrors.BadRequest("INVALID_ARGUMENT", "usage filters must be non-negative")
	}
	end := now
	if filters.EndTime != nil {
		end = *filters.EndTime
	}
	start := end.AddDate(0, 0, -30)
	if filters.StartTime != nil {
		start = *filters.StartTime
	}
	if !start.Before(end) || end.Sub(start) > 366*24*time.Hour {
		return filters, infraerrors.BadRequest("INVALID_DATE_RANGE", "usage range must be positive and at most 366 days")
	}
	filters.StartTime, filters.EndTime = &start, &end
	filters.ExactTotal = true
	// Enterprise consumers see the client-requested model only.
	filters.ModelFilterSource = usagestats.ModelSourceRequested
	return filters, nil
}
func (s *EnterpriseUsageService) List(ctx context.Context, scope EnterpriseUsageScope, params pagination.PaginationParams, filters usagestats.UsageLogFilters) ([]EnterpriseUsageLog, *pagination.PaginationResult, error) {
	f, err := ConstrainEnterpriseUsageFilters(scope, filters, time.Now())
	if err != nil {
		return nil, nil, err
	}
	r, ok := s.repo.(EnterpriseUsageLogReader)
	if !ok {
		return nil, nil, fmt.Errorf("enterprise usage list repository unavailable")
	}
	return r.ListEnterpriseUsageLogs(ctx, params, f)
}
func (s *EnterpriseUsageService) Stats(ctx context.Context, scope EnterpriseUsageScope, filters usagestats.UsageLogFilters) (*EnterpriseUsageStats, error) {
	f, err := ConstrainEnterpriseUsageFilters(scope, filters, time.Now())
	if err != nil {
		return nil, err
	}
	row, err := s.repo.GetStatsWithFilters(ctx, f)
	if err != nil {
		return nil, err
	}
	return &EnterpriseUsageStats{TotalRequests: row.TotalRequests, TotalTokens: row.TotalTokens, TotalActualCost: row.TotalActualCost}, nil
}
func (s *EnterpriseUsageService) Trend(ctx context.Context, scope EnterpriseUsageScope, filters usagestats.UsageLogFilters, granularity string) ([]EnterpriseUsageTrend, error) {
	f, err := ConstrainEnterpriseUsageFilters(scope, filters, time.Now())
	if err != nil {
		return nil, err
	}
	if granularity == "" {
		granularity = "day"
	}
	if granularity != "hour" && granularity != "day" {
		return nil, infraerrors.BadRequest("INVALID_ARGUMENT", "granularity must be day or hour")
	}
	r, ok := s.repo.(enterpriseUsageAnalyticsReader)
	if !ok {
		return nil, fmt.Errorf("enterprise usage analytics repository unavailable")
	}
	rows, err := r.GetUsageTrendWithUsageFilters(ctx, *f.StartTime, *f.EndTime, granularity, f)
	if err != nil {
		return nil, err
	}
	out := make([]EnterpriseUsageTrend, 0, len(rows))
	for _, v := range rows {
		out = append(out, EnterpriseUsageTrend{v.Date, v.Requests, v.TotalTokens, v.ActualCost})
	}
	return out, nil
}
func (s *EnterpriseUsageService) Models(ctx context.Context, scope EnterpriseUsageScope, filters usagestats.UsageLogFilters) ([]EnterpriseUsageModel, error) {
	f, err := ConstrainEnterpriseUsageFilters(scope, filters, time.Now())
	if err != nil {
		return nil, err
	}
	r, ok := s.repo.(enterpriseUsageAnalyticsReader)
	if !ok {
		return nil, fmt.Errorf("enterprise usage analytics repository unavailable")
	}
	rows, err := r.GetModelStatsWithUsageFiltersBySource(ctx, *f.StartTime, *f.EndTime, f, usagestats.ModelSourceRequested)
	if err != nil {
		return nil, err
	}
	out := make([]EnterpriseUsageModel, 0, len(rows))
	for _, v := range rows {
		out = append(out, EnterpriseUsageModel{v.Model, v.Requests, v.TotalTokens, v.ActualCost})
	}
	return out, nil
}
func (s *EnterpriseUsageService) Members(ctx context.Context, scope EnterpriseUsageScope, filters usagestats.UsageLogFilters) ([]EnterpriseMemberUsage, error) {
	f, err := ConstrainEnterpriseUsageFilters(scope, filters, time.Now())
	if err != nil {
		return nil, err
	}
	r, ok := s.repo.(EnterpriseUsageRankingRepository)
	if !ok {
		return nil, fmt.Errorf("enterprise usage ranking repository unavailable")
	}
	return r.GetEnterpriseMemberUsage(ctx, f)
}
func (s *EnterpriseUsageService) Departments(ctx context.Context, scope EnterpriseUsageScope, filters usagestats.UsageLogFilters) ([]EnterpriseDepartmentUsage, error) {
	f, err := ConstrainEnterpriseUsageFilters(scope, filters, time.Now())
	if err != nil {
		return nil, err
	}
	if scope.Member != nil && scope.Member.Role != EnterpriseRoleOwner {
		return nil, ErrEnterprisePermissionDenied
	}
	r, ok := s.repo.(EnterpriseUsageRankingRepository)
	if !ok {
		return nil, fmt.Errorf("enterprise usage ranking repository unavailable")
	}
	return r.GetEnterpriseDepartmentUsage(ctx, f)
}
func (s *EnterpriseUsageService) OverviewStats(ctx context.Context, scope EnterpriseUsageScope) (todayCost, monthCost float64, err error) {
	now := time.Now().In(timezone.Location())
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	a, e := s.Stats(ctx, scope, usagestats.UsageLogFilters{StartTime: &today, EndTime: &now})
	if e != nil {
		return 0, 0, e
	}
	b, e := s.Stats(ctx, scope, usagestats.UsageLogFilters{StartTime: &month, EndTime: &now})
	if e != nil {
		return 0, 0, e
	}
	return a.TotalActualCost, b.TotalActualCost, nil
}
