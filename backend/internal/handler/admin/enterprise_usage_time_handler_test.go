//go:build unit

package admin

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type enterpriseHTTPTimeRepo struct {
	enterpriseHTTPUsageRepo
	trendStart, trendEnd time.Time
}

func (r *enterpriseHTTPTimeRepo) ListEnterpriseUsageLogs(_ context.Context, _ pagination.PaginationParams, f usagestats.UsageLogFilters) ([]service.EnterpriseUsageLog, *pagination.PaginationResult, error) {
	r.filters = append(r.filters, f)
	return nil, &pagination.PaginationResult{}, nil
}
func (r *enterpriseHTTPTimeRepo) GetUsageTrendWithUsageFilters(_ context.Context, start, end time.Time, _ string, f usagestats.UsageLogFilters) ([]usagestats.TrendDataPoint, error) {
	r.filters = append(r.filters, f)
	r.trendStart, r.trendEnd = start, end
	return nil, nil
}
func (r *enterpriseHTTPTimeRepo) GetModelStatsWithUsageFiltersBySource(_ context.Context, _, _ time.Time, f usagestats.UsageLogFilters, _ string) ([]usagestats.ModelStat, error) {
	r.filters = append(r.filters, f)
	return nil, nil
}
func (r *enterpriseHTTPTimeRepo) GetEnterpriseUsageSeries(_ context.Context, f usagestats.UsageLogFilters, _, _ string, _ int) ([]service.EnterpriseUsageSeriesPoint, error) {
	r.filters = append(r.filters, f)
	return nil, nil
}
func (r *enterpriseHTTPTimeRepo) GetEnterpriseMemberUsage(_ context.Context, f usagestats.UsageLogFilters) ([]service.EnterpriseMemberUsage, error) {
	r.filters = append(r.filters, f)
	return nil, nil
}
func (r *enterpriseHTTPTimeRepo) GetEnterpriseDepartmentUsage(_ context.Context, f usagestats.UsageLogFilters) ([]service.EnterpriseDepartmentUsage, error) {
	r.filters = append(r.filters, f)
	return nil, nil
}
func (r *enterpriseHTTPTimeRepo) GetEnterpriseUsageModelOptions(_ context.Context, f usagestats.UsageLogFilters, _ string, _ int) ([]service.EnterpriseUsageModelOption, error) {
	r.filters = append(r.filters, f)
	return nil, nil
}
func (r *enterpriseHTTPTimeRepo) GetEnterpriseUsageAPIKeyOptions(_ context.Context, f usagestats.UsageLogFilters, _ string, _ int) ([]service.EnterpriseUsageAPIKeyOption, error) {
	r.filters = append(r.filters, f)
	return nil, nil
}

func TestEnterpriseUsagePreciseTimeBothRoutes(t *testing.T) {
	start, err := time.Parse(time.RFC3339Nano, "2026-01-01T10:25:30.123456+05:45")
	require.NoError(t, err)
	end := start.Add(24 * time.Hour)
	stamp := func(at time.Time) string {
		return at.In(time.FixedZone("request", -4*60*60)).Format(time.RFC3339Nano)
	}
	precise := func(end time.Time) url.Values {
		return url.Values{"start_time": {start.Format(time.RFC3339Nano)}, "end_time": {stamp(end)}}
	}
	for _, base := range []string{"/api/v1/enterprise", "/api/v1/admin/enterprises/8"} {
		for _, path := range []string{"", "/stats", "/trend", "/models", "/models/trend", "/members", "/members/trend", "/departments", "/departments/trend", "/model-options"} {
			for _, tc := range []struct {
				name  string
				query url.Values
				bad   bool
			}{
				{"precise", precise(end), false},
				{"date ignored", url.Values{"start_time": {stamp(start)}, "end_time": {stamp(end)}, "start_date": {"2020-01-01"}, "end_date": {"2020-01-02"}}, false},
				{"invalid date ignored", url.Values{"start_time": {stamp(start)}, "end_time": {stamp(end)}, "start_date": {"invalid"}, "end_date": {"invalid"}}, false},
				{"start only", url.Values{"start_time": {stamp(start)}, "end_date": {"2026-01-02"}}, true},
				{"end only", url.Values{"end_time": {stamp(end)}, "start_date": {"2026-01-01"}}, true},
				{"empty start only", url.Values{"start_time": {""}}, true},
				{"empty pair", url.Values{"start_time": {""}, "end_time": {""}}, true},
				{"invalid start", url.Values{"start_time": {"invalid"}, "end_time": {stamp(end)}}, true},
				{"invalid end", url.Values{"start_time": {stamp(start)}, "end_time": {"2026-01-02"}}, true},
				{"missing timezone", url.Values{"start_time": {"2026-01-01T10:25:30"}, "end_time": {stamp(end)}}, true},
				{"equal instants different offsets", precise(start), true},
				{"reversed", precise(start.Add(-time.Microsecond)), true},
				{"366 days", precise(start.Add(366 * 24 * time.Hour)), false},
				{"366 days plus microsecond", precise(start.Add(366*24*time.Hour + time.Microsecond)), true},
			} {
				t.Run(base+path+"/"+tc.name, func(t *testing.T) {
					repo := &enterpriseHTTPTimeRepo{}
					h := &EnterpriseHandler{management: enterpriseIterationManagementStub{}, usage: service.NewEnterpriseUsageService(repo), memberReader: enterpriseHTTPMemberReader{member: &service.EnterpriseMemberContext{EnterpriseID: 8, Role: service.EnterpriseRoleOwner, MemberStatus: "active", EnterpriseStatus: "active"}}}
					w := enterpriseTestRequest(enterpriseTestRouter(h, service.RoleAdmin), http.MethodGet, base+"/usage"+path+"?"+tc.query.Encode(), "")
					if tc.bad {
						require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
						require.Contains(t, w.Body.String(), "INVALID_DATE_RANGE")
						require.Empty(t, repo.filters)
						return
					}
					require.Equal(t, http.StatusOK, w.Code, w.Body.String())
					require.Len(t, repo.filters, 1)
					f := repo.filters[0]
					wantEnd, err := time.Parse(time.RFC3339Nano, tc.query.Get("end_time"))
					require.NoError(t, err)
					require.True(t, start.Equal(*f.StartTime))
					require.True(t, wantEnd.Equal(*f.EndTime))
					if path == "/trend" {
						require.True(t, start.Equal(repo.trendStart))
						require.True(t, wantEnd.Equal(repo.trendEnd))
					}
					require.EqualValues(t, 8, f.EnterpriseID)
				})
			}
			if !strings.HasSuffix(path, "/trend") {
				continue
			}
			for _, extra := range []time.Duration{0, time.Microsecond} {
				t.Run(base+path+"/hour limit/"+extra.String(), func(t *testing.T) {
					repo := &enterpriseHTTPTimeRepo{}
					h := &EnterpriseHandler{management: enterpriseIterationManagementStub{}, usage: service.NewEnterpriseUsageService(repo), memberReader: enterpriseHTTPMemberReader{member: &service.EnterpriseMemberContext{EnterpriseID: 8, Role: "owner", MemberStatus: "active", EnterpriseStatus: "active"}}}
					query := precise(start.Add(31*24*time.Hour + extra))
					query.Set("granularity", "hour")
					w := enterpriseTestRequest(enterpriseTestRouter(h, service.RoleAdmin), http.MethodGet, base+"/usage"+path+"?"+query.Encode(), "")
					if extra > 0 {
						require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
						require.Contains(t, w.Body.String(), "INVALID_DATE_RANGE")
						require.Empty(t, repo.filters)
					} else {
						require.Equal(t, http.StatusOK, w.Code, w.Body.String())
						require.Len(t, repo.filters, 1)
					}
				})
			}
		}
	}
}

func TestEnterpriseUsagePreciseTimePreservesScope(t *testing.T) {
	for _, tc := range []struct {
		role             string
		user, department int64
	}{
		{service.EnterpriseRoleOwner, 0, 0},
		{service.EnterpriseRoleManager, 0, 3},
		{service.EnterpriseRoleMember, 42, 0},
	} {
		t.Run(tc.role, func(t *testing.T) {
			repo := &enterpriseHTTPTimeRepo{}
			h := &EnterpriseHandler{usage: service.NewEnterpriseUsageService(repo), memberReader: enterpriseHTTPMemberReader{member: &service.EnterpriseMemberContext{EnterpriseID: 8, Role: tc.role, DepartmentID: enterpriseInt64(3), MemberStatus: "active", EnterpriseStatus: "active"}}}
			query := url.Values{"start_time": {"2026-10-17T10:25:00+08:00"}, "end_time": {"2026-10-18T10:25:00+08:00"}}
			w := enterpriseTestRequest(enterpriseTestRouter(h, service.RoleUser), http.MethodGet, "/api/v1/enterprise/usage/stats?"+query.Encode(), "")
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.Len(t, repo.filters, 1)
			require.Equal(t, tc.user, repo.filters[0].UserID)
			require.Equal(t, tc.department, repo.filters[0].EnterpriseDepartmentID)
			require.Equal(t, tc.department, repo.filters[0].EnterpriseDepartmentScopeID)
		})
	}
}

func TestEnterpriseUsageLegacyDatesAndDefaultsBothRoutes(t *testing.T) {
	for _, base := range []string{"/api/v1/enterprise", "/api/v1/admin/enterprises/8"} {
		for _, path := range []string{"/stats", "/model-options"} {
			repo := &enterpriseHTTPTimeRepo{}
			h := &EnterpriseHandler{management: enterpriseIterationManagementStub{}, usage: service.NewEnterpriseUsageService(repo), memberReader: enterpriseHTTPMemberReader{member: &service.EnterpriseMemberContext{EnterpriseID: 8, Role: "owner", MemberStatus: "active", EnterpriseStatus: "active"}}}
			router := enterpriseTestRouter(h, service.RoleAdmin)
			w := enterpriseTestRequest(router, http.MethodGet, base+"/usage"+path+"?start_date=2026-10-17&end_date=2026-10-18", "")
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			start, err := timezone.ParseInLocation("2006-01-02", "2026-10-17")
			require.NoError(t, err)
			require.Equal(t, start, *repo.filters[0].StartTime)
			require.Equal(t, start.AddDate(0, 0, 2), *repo.filters[0].EndTime)
			w = enterpriseTestRequest(router, http.MethodGet, base+"/usage"+path, "")
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			f := repo.filters[1]
			days := 30
			if path == "/model-options" {
				days = 90
			}
			require.WithinDuration(t, f.EndTime.AddDate(0, 0, -days), *f.StartTime, time.Second)
		}
	}
}
