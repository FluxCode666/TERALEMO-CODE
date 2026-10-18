//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type enterpriseHTTPOptionsRepo struct {
	service.UsageLogRepository
	filters usagestats.UsageLogFilters
	search  string
	limit   int
	calls   int
}

func (r *enterpriseHTTPOptionsRepo) GetEnterpriseUsageAPIKeyOptions(_ context.Context, f usagestats.UsageLogFilters, search string, limit int) ([]service.EnterpriseUsageAPIKeyOption, error) {
	r.filters, r.search, r.limit = f, search, limit
	r.calls++
	return []service.EnterpriseUsageAPIKeyOption{{ID: 9, Name: "display", User: service.EnterpriseUsageUser{ID: 42, Username: "alice", Email: "alice@example.com"}}}, nil
}
func (r *enterpriseHTTPOptionsRepo) GetEnterpriseUsageModelOptions(_ context.Context, f usagestats.UsageLogFilters, search string, limit int) ([]service.EnterpriseUsageModelOption, error) {
	r.filters, r.search, r.limit = f, search, limit
	r.calls++
	return []service.EnterpriseUsageModelOption{{Model: "public", Requests: 3}}, nil
}

func TestEnterpriseUsageOptionsHTTPBothRoutes(t *testing.T) {
	for _, base := range []string{"/api/v1/enterprise", "/api/v1/admin/enterprises/8"} {
		for _, kind := range []string{"api-key-options", "model-options"} {
			for _, tc := range []struct {
				query         string
				status, limit int
			}{
				{"", 200, 20}, {"?search=alice&limit=999", 200, 100}, {"?limit=1", 200, 1},
				{"?limit=0", 400, 0}, {"?limit=-1", 400, 0}, {"?limit=bad", 400, 0}, {"?start_date=bad", 400, 0},
			} {
				t.Run(base+"/"+kind+tc.query, func(t *testing.T) {
					repo := &enterpriseHTTPOptionsRepo{}
					h := &EnterpriseHandler{management: enterpriseIterationManagementStub{}, usage: service.NewEnterpriseUsageService(repo), memberReader: enterpriseHTTPMemberReader{member: &service.EnterpriseMemberContext{EnterpriseID: 8, Role: service.EnterpriseRoleMember, MemberStatus: "active", EnterpriseStatus: "active"}}}
					w := enterpriseTestRequest(enterpriseTestRouter(h, service.RoleAdmin), http.MethodGet, base+"/usage/"+kind+tc.query, "")
					require.Equal(t, tc.status, w.Code, w.Body.String())
					if tc.status != 200 {
						require.Zero(t, repo.calls)
						return
					}
					require.Equal(t, tc.limit, repo.limit)
					require.EqualValues(t, 8, repo.filters.EnterpriseID)
					if base == "/api/v1/enterprise" {
						require.EqualValues(t, 42, repo.filters.UserID)
					} else {
						require.Zero(t, repo.filters.UserID)
					}
					if tc.limit == 100 {
						require.Equal(t, "alice", repo.search)
					}
					if kind == "model-options" {
						require.InDelta(t, 90, repo.filters.EndTime.Sub(*repo.filters.StartTime).Hours()/24, 0.01)
					}
					var body struct {
						Data []map[string]any `json:"data"`
					}
					require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
					require.Len(t, body.Data, 1)
					if kind == "api-key-options" {
						require.Len(t, body.Data[0], 3)
						require.Contains(t, body.Data[0], "id")
						require.Contains(t, body.Data[0], "name")
						require.Contains(t, body.Data[0], "user")
					} else {
						require.Equal(t, map[string]any{"model": "public", "requests": float64(3)}, body.Data[0])
					}
					require.NotContains(t, w.Body.String(), `"key":`)
				})
			}
		}
	}
}

func TestEnterpriseUsageOptionsHTTPRejectsScopeEscalation(t *testing.T) {
	for _, kind := range []string{"api-key-options", "model-options"} {
		for _, tc := range []struct {
			role, query string
			department  *int64
		}{
			{service.EnterpriseRoleMember, "?user_id=99", nil},
			{service.EnterpriseRoleManager, "?department_id=99", enterpriseInt64(3)},
		} {
			t.Run(kind+tc.role, func(t *testing.T) {
				repo := &enterpriseHTTPOptionsRepo{}
				h := &EnterpriseHandler{usage: service.NewEnterpriseUsageService(repo), memberReader: enterpriseHTTPMemberReader{member: &service.EnterpriseMemberContext{EnterpriseID: 8, Role: tc.role, DepartmentID: tc.department, MemberStatus: "active", EnterpriseStatus: "active"}}}
				w := enterpriseTestRequest(enterpriseTestRouter(h, service.RoleUser), http.MethodGet, "/api/v1/enterprise/usage/"+kind+tc.query, "")
				require.Equal(t, 403, w.Code, w.Body.String())
				require.Zero(t, repo.calls)
			})
		}
	}
}

func TestEnterpriseUsageModelOptionsHTTPDateRange(t *testing.T) {
	repo := &enterpriseHTTPOptionsRepo{}
	h := &EnterpriseHandler{management: enterpriseIterationManagementStub{}, usage: service.NewEnterpriseUsageService(repo)}
	w := enterpriseTestRequest(enterpriseTestRouter(h, service.RoleAdmin), http.MethodGet, "/api/v1/admin/enterprises/8/usage/model-options?start_date=2026-10-01T00:00:00Z&end_date=2026-10-03T00:00:00Z", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 48*time.Hour, repo.filters.EndTime.Sub(*repo.filters.StartTime))
}
