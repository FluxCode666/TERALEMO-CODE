//go:build unit

package admin

import (
	"context"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type enterpriseHTTPUsageRepo struct {
	service.UsageLogRepository
	filters []usagestats.UsageLogFilters
}

func (r *enterpriseHTTPUsageRepo) GetStatsWithFilters(_ context.Context, f usagestats.UsageLogFilters) (*usagestats.UsageStats, error) {
	r.filters = append(r.filters, f)
	return &usagestats.UsageStats{TotalRequests: 2, TotalTokens: 10, TotalActualCost: 1.5}, nil
}
func TestEnterpriseHTTPUsageScopeConvergence(t *testing.T) {
	for _, tc := range []struct {
		name, role, query        string
		departmentID             *int64
		status                   int
		userID, filterDepartment int64
	}{
		{name: "owner enterprise scope", role: "owner", status: 200},
		{name: "member own scope", role: "member", status: 200, userID: 42},
		{name: "member own explicit filter", role: "member", query: "?user_id=42", status: 200, userID: 42},
		{name: "member forbidden user", role: "member", query: "?user_id=99", status: 403},
		{name: "manager own department", role: "manager", departmentID: enterpriseInt64(3), status: 200, filterDepartment: 3},
		{name: "manager narrowed user", role: "manager", departmentID: enterpriseInt64(3), query: "?user_id=99", status: 200, userID: 99, filterDepartment: 3},
		{name: "manager forbidden department", role: "manager", departmentID: enterpriseInt64(3), query: "?department_id=4", status: 403},
		{name: "member invalid date", role: "member", query: "?start_date=invalid", status: 400},
		{name: "owner excessive range", role: "owner", query: "?start_date=2020-01-01&end_date=2022-01-01", status: 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &enterpriseHTTPUsageRepo{}
			h := &EnterpriseHandler{usage: service.NewEnterpriseUsageService(repo), memberReader: enterpriseHTTPMemberReader{member: &service.EnterpriseMemberContext{EnterpriseID: 8, MemberID: 7, Role: tc.role, DepartmentID: tc.departmentID, EnterpriseStatus: "active", MemberStatus: "active"}}}
			w := enterpriseTestRequest(enterpriseTestRouter(h, service.RoleUser), http.MethodGet, "/api/v1/enterprise/usage/stats"+tc.query, "")
			require.Equal(t, tc.status, w.Code, w.Body.String())
			if tc.status != 200 {
				require.Empty(t, repo.filters)
				return
			}
			require.Len(t, repo.filters, 1)
			f := repo.filters[0]
			require.Equal(t, int64(8), f.EnterpriseID)
			require.Equal(t, tc.userID, f.UserID)
			require.Equal(t, tc.filterDepartment, f.EnterpriseDepartmentID)
			require.NotNil(t, f.StartTime)
			require.NotNil(t, f.EndTime)
			require.InDelta(t, 30, f.EndTime.Sub(*f.StartTime).Hours()/24, 0.1)
		})
	}
}
func enterpriseInt64(n int64) *int64 { return &n }
