package service

import (
	"context"
	"fmt"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

// Pointer dimensions preserve zero for the remainder while omitting unrelated dimensions.
type EnterpriseUsageSeriesPoint struct {
	Date           string  `json:"date"`
	Model          *string `json:"model,omitempty"`
	UserID         *int64  `json:"user_id,omitempty"`
	Username       *string `json:"username,omitempty"`
	DepartmentID   *int64  `json:"department_id,omitempty"`
	DepartmentName *string `json:"department_name,omitempty"`
	Requests       int64   `json:"requests"`
	TotalTokens    int64   `json:"total_tokens"`
	ActualCost     float64 `json:"actual_cost"`
}

type EnterpriseUsageSeriesRepository interface {
	GetEnterpriseUsageSeries(context.Context, usagestats.UsageLogFilters, string, string, int) ([]EnterpriseUsageSeriesPoint, error)
}

func (s *EnterpriseUsageService) SeriesTrend(ctx context.Context, scope EnterpriseUsageScope, filters usagestats.UsageLogFilters, dimension, granularity string, top int) ([]EnterpriseUsageSeriesPoint, error) {
	switch dimension {
	case "models", "members":
	case "departments":
		if scope.Member != nil && scope.Member.Role == EnterpriseRoleMember {
			return nil, ErrEnterprisePermissionDenied
		}
	default:
		return nil, ErrEnterpriseInvalidInput
	}
	f, err := s.constrainFilters(ctx, scope, filters, time.Now())
	if err != nil {
		return nil, err
	}
	if granularity == "" {
		granularity = "day"
	}
	if granularity != "day" && granularity != "hour" {
		return nil, infraerrors.BadRequest("INVALID_ARGUMENT", "granularity must be day or hour")
	}
	if granularity == "hour" && f.EndTime.Sub(*f.StartTime) > 31*24*time.Hour {
		return nil, infraerrors.BadRequest("INVALID_DATE_RANGE", "hourly usage range must be at most 31 days")
	}
	if top <= 0 {
		top = 8
	}
	if top > 20 {
		top = 20
	}
	reader, ok := s.repo.(EnterpriseUsageSeriesRepository)
	if !ok {
		return nil, fmt.Errorf("enterprise usage series repository unavailable")
	}
	return reader.GetEnterpriseUsageSeries(ctx, f, dimension, granularity, top)
}
