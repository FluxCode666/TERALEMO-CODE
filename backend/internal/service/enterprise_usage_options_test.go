//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

type enterpriseOptionsCapture struct {
	UsageLogRepository
	filters     usagestats.UsageLogFilters
	search      string
	limit       int
	granularity string
}

func (r *enterpriseOptionsCapture) GetEnterpriseUsageAPIKeyOptions(_ context.Context, f usagestats.UsageLogFilters, search string, limit int) ([]EnterpriseUsageAPIKeyOption, error) {
	r.filters, r.search, r.limit = f, search, limit
	return nil, nil
}
func (r *enterpriseOptionsCapture) GetEnterpriseUsageModelOptions(_ context.Context, f usagestats.UsageLogFilters, search string, limit int) ([]EnterpriseUsageModelOption, error) {
	r.filters, r.search, r.limit = f, search, limit
	return nil, nil
}
func (r *enterpriseOptionsCapture) GetUsageTrendWithUsageFilters(_ context.Context, _, _ time.Time, granularity string, f usagestats.UsageLogFilters) ([]usagestats.TrendDataPoint, error) {
	r.filters, r.granularity = f, granularity
	return nil, nil
}
func (*enterpriseOptionsCapture) GetModelStatsWithUsageFiltersBySource(context.Context, time.Time, time.Time, usagestats.UsageLogFilters, string) ([]usagestats.ModelStat, error) {
	return nil, nil
}

func TestEnterpriseUsageOptionsConstrainScope(t *testing.T) {
	dept := int64(8)
	for _, role := range []string{"", EnterpriseRoleOwner, EnterpriseRoleManager, EnterpriseRoleMember} {
		t.Run("role="+role, func(t *testing.T) {
			scope := EnterpriseUsageScope{EnterpriseID: 3, UserID: 7}
			if role != "" {
				scope.Member = &EnterpriseMemberContext{EnterpriseID: 3, Role: role, DepartmentID: &dept, MemberStatus: EnterpriseMemberStatusActive}
			}
			r := &enterpriseOptionsCapture{}
			svc := NewEnterpriseUsageService(r)
			for _, query := range []func(context.Context, EnterpriseUsageScope, usagestats.UsageLogFilters, string, int) error{
				func(c context.Context, s EnterpriseUsageScope, f usagestats.UsageLogFilters, search string, limit int) error {
					_, err := svc.APIKeyOptions(c, s, f, search, limit)
					return err
				},
				func(c context.Context, s EnterpriseUsageScope, f usagestats.UsageLogFilters, search string, limit int) error {
					_, err := svc.ModelOptions(c, s, f, search, limit)
					return err
				},
			} {
				require.NoError(t, query(context.Background(), scope, usagestats.UsageLogFilters{}, " term ", 999))
				require.EqualValues(t, 3, r.filters.EnterpriseID)
				require.Equal(t, "term", r.search)
				require.Equal(t, 100, r.limit)
				if role == EnterpriseRoleMember {
					require.EqualValues(t, 7, r.filters.UserID)
				} else {
					require.Zero(t, r.filters.UserID)
				}
				if role == EnterpriseRoleManager {
					require.Equal(t, dept, r.filters.EnterpriseDepartmentID)
				} else {
					require.Zero(t, r.filters.EnterpriseDepartmentID)
				}
				require.ErrorIs(t, query(context.Background(), scope, usagestats.UsageLogFilters{EnterpriseID: 99}, "", 20), ErrEnterprisePermissionDenied)
			}
		})
	}
}

func TestEnterpriseUsageModelOptionsDefaultsAndValidation(t *testing.T) {
	r := &enterpriseOptionsCapture{}
	svc := NewEnterpriseUsageService(r)
	scope := EnterpriseUsageScope{EnterpriseID: 1}
	end := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	_, err := svc.ModelOptions(context.Background(), scope, usagestats.UsageLogFilters{EndTime: &end}, "", 0)
	require.NoError(t, err)
	require.Equal(t, end.AddDate(0, 0, -90), *r.filters.StartTime)
	require.Equal(t, end, *r.filters.EndTime)
	require.Equal(t, 20, r.limit)
	start := end.AddDate(0, 0, -7)
	_, err = svc.ModelOptions(context.Background(), scope, usagestats.UsageLogFilters{StartTime: &start, EndTime: &end}, "", 1)
	require.NoError(t, err)
	require.Equal(t, start, *r.filters.StartTime)
	require.Equal(t, 1, r.limit)
	start = end.AddDate(0, 0, -367)
	_, err = svc.ModelOptions(context.Background(), scope, usagestats.UsageLogFilters{StartTime: &start, EndTime: &end}, "", 20)
	require.Error(t, err)
}

func TestEnterpriseUsageTrendGranularityAndFilters(t *testing.T) {
	r := &enterpriseOptionsCapture{}
	svc := NewEnterpriseUsageService(r)
	dept := int64(8)
	scope := EnterpriseUsageScope{EnterpriseID: 1, Member: &EnterpriseMemberContext{EnterpriseID: 1, Role: EnterpriseRoleManager, DepartmentID: &dept, MemberStatus: EnterpriseMemberStatusActive}}
	end := time.Now()
	start := end.Add(-time.Hour)
	filters := usagestats.UsageLogFilters{UserID: 5, APIKeyID: 7, Model: "public", StartTime: &start, EndTime: &end}
	for _, granularity := range []string{"", "day", "hour", "month"} {
		_, err := svc.Trend(context.Background(), scope, filters, granularity)
		if granularity == "month" {
			require.Error(t, err)
			continue
		}
		require.NoError(t, err)
		want := granularity
		if want == "" {
			want = "day"
		}
		require.Equal(t, want, r.granularity)
		require.Equal(t, filters.UserID, r.filters.UserID)
		require.Equal(t, filters.APIKeyID, r.filters.APIKeyID)
		require.Equal(t, filters.Model, r.filters.Model)
		require.Equal(t, start, *r.filters.StartTime)
		require.Equal(t, end, *r.filters.EndTime)
		require.Equal(t, dept, r.filters.EnterpriseDepartmentID)
		require.EqualValues(t, 1, r.filters.EnterpriseID)
		require.Equal(t, usagestats.ModelSourceRequested, r.filters.ModelFilterSource)
	}
}
