//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type enterpriseOperationsHTTPStub struct {
	enterpriseManagementAPI
	actor      service.EnterpriseActor
	id         int64
	importRows []service.EnterpriseImportRow
	password   *string
	quotaInput service.EnterpriseBatchMemberQuotaInput
}

func (s *enterpriseOperationsHTTPStub) GetEnterprise(context.Context, service.EnterpriseActor, int64) (*service.Enterprise, error) {
	return &service.Enterprise{}, nil
}
func (s *enterpriseOperationsHTTPStub) PreviewImport(_ context.Context, a service.EnterpriseActor, id int64, rows []service.EnterpriseImportRow) (*service.EnterpriseImportPreview, error) {
	s.actor, s.id, s.importRows = a, id, rows
	return &service.EnterpriseImportPreview{Summary: service.EnterpriseImportPreviewSummary{Total: 1, Create: 1}, DepartmentsToCreate: []string{"A"}, Rows: []service.EnterpriseImportPreviewRow{{Row: 2, Status: "create", Email: "a@example.com", Role: "member", Messages: []string{}}}}, nil
}
func (s *enterpriseOperationsHTTPStub) CommitImport(_ context.Context, a service.EnterpriseActor, id int64, rows []service.EnterpriseImportRow) (*service.EnterpriseImportCommit, error) {
	s.actor, s.id, s.importRows = a, id, rows
	return &service.EnterpriseImportCommit{Summary: service.EnterpriseImportCommitSummary{Total: 1, Created: 1}, Rows: []service.EnterpriseImportCommitRow{{Row: 2, Status: "created", MemberID: 3, UserID: 4, GeneratedPassword: "generated-secret"}}}, nil
}
func (s *enterpriseOperationsHTTPStub) ResetMemberPassword(_ context.Context, a service.EnterpriseActor, id, memberID int64, password *string) (*service.EnterprisePasswordResetResult, error) {
	s.actor, s.id, s.password = a, id, password
	result := &service.EnterprisePasswordResetResult{}
	if password == nil {
		result.GeneratedPassword = "reset-secret"
	}
	return result, nil
}
func (s *enterpriseOperationsHTTPStub) BatchUpdateMemberQuotas(_ context.Context, a service.EnterpriseActor, id int64, input service.EnterpriseBatchMemberQuotaInput) (*service.EnterpriseBatchMemberQuotaResult, error) {
	s.actor, s.id, s.quotaInput = a, id, input
	return &service.EnterpriseBatchMemberQuotaResult{Updated: []int64{3}, Failed: []service.EnterpriseBatchMemberQuotaFailure{{MemberID: 99, Code: "ENTERPRISE_INVALID_SCOPE", Message: "missing"}}}, nil
}

func TestEnterpriseOperationsHTTPBothSurfacesAndContracts(t *testing.T) {
	for _, base := range []string{"/api/v1/enterprise", "/api/v1/admin/enterprises/8"} {
		t.Run(base, func(t *testing.T) {
			stub := &enterpriseOperationsHTTPStub{}
			h := &EnterpriseHandler{management: stub, memberReader: enterpriseHTTPMemberReader{member: &service.EnterpriseMemberContext{EnterpriseID: 8, MemberID: 7, Role: "owner", EnterpriseStatus: "active", MemberStatus: "active"}}}
			router := enterpriseTestRouter(h, service.RoleAdmin)
			body := `{"rows":[{"row":2,"email":"a@example.com","username":"a","department_path":"A","role":"员工","password":"provided-password"}]}`
			w := enterpriseTestRequest(router, http.MethodPost, base+"/import/preview", body)
			require.Equal(t, 200, w.Code, w.Body.String())
			require.NotContains(t, w.Body.String(), "password")
			require.Contains(t, w.Body.String(), `"departments_to_create":["A"]`)
			require.Equal(t, int64(8), stub.id)
			require.Equal(t, strings.Contains(base, "/admin/"), stub.actor.IsAdmin)
			require.Equal(t, "员工", stub.importRows[0].Role)
			w = enterpriseTestRequest(router, http.MethodPost, base+"/import/commit", body)
			require.Equal(t, 200, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), `"generated_password":"generated-secret"`)
			w = enterpriseTestRequest(router, http.MethodPost, base+"/members/3/reset-password", `{}`)
			require.Equal(t, 200, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), `"generated_password":"reset-secret"`)
			require.Nil(t, stub.password)
			w = enterpriseTestRequest(router, http.MethodPost, base+"/members/3/reset-password", `{"password":"explicit-password"}`)
			require.Equal(t, 200, w.Code, w.Body.String())
			require.NotContains(t, w.Body.String(), "password")
			require.Equal(t, "explicit-password", *stub.password)
			w = enterpriseTestRequest(router, http.MethodPut, base+"/quotas/members/batch", `{"member_ids":[3,99],"fields":["daily","weekly"],"daily_limit_usd":null,"weekly_limit_usd":0,"monthly_limit_usd":999}`)
			require.Equal(t, 200, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), `"updated":[3]`)
			require.Nil(t, stub.quotaInput.DailyLimitUSD)
			require.NotNil(t, stub.quotaInput.WeeklyLimitUSD)
			require.Zero(t, *stub.quotaInput.WeeklyLimitUSD)
			require.Nil(t, stub.quotaInput.MonthlyLimitUSD)
			w = enterpriseTestRequest(router, http.MethodPut, base+"/quotas/members/batch", `{"member_ids":[3],"fields":["daily"]}`)
			require.Equal(t, 400, w.Code, w.Body.String())
			for _, scope := range []string{"member", "department"} {
				w = enterpriseTestRequest(router, http.MethodPut, base+"/group-whitelists/"+scope+"/3", `{"group_ids":[]}`)
				require.Equal(t, 400, w.Code, w.Body.String())
				require.Contains(t, w.Body.String(), "ENTERPRISE_GROUP_SCOPE_DISABLED")
			}
			rows := make([]service.EnterpriseImportRow, 1001)
			raw, err := json.Marshal(map[string]any{"rows": rows})
			require.NoError(t, err)
			for _, phase := range []string{"preview", "commit"} {
				w = enterpriseTestRequest(router, http.MethodPost, base+"/import/"+phase, string(raw))
				require.Equal(t, 400, w.Code, w.Body.String())
			}
		})
	}
}

type enterpriseSeriesHTTPRepo struct {
	service.UsageLogRepository
	filters                usagestats.UsageLogFilters
	dimension, granularity string
	top                    int
}

func (r *enterpriseSeriesHTTPRepo) GetEnterpriseUsageSeries(_ context.Context, f usagestats.UsageLogFilters, dimension, granularity string, top int) ([]service.EnterpriseUsageSeriesPoint, error) {
	r.filters, r.dimension, r.granularity, r.top = f, dimension, granularity, top
	return []service.EnterpriseUsageSeriesPoint{}, nil
}
func TestEnterpriseUsageSeriesHTTPFiltersPermissionsAndDates(t *testing.T) {
	for _, base := range []string{"/api/v1/enterprise", "/api/v1/admin/enterprises/8"} {
		repo := &enterpriseSeriesHTTPRepo{}
		h := &EnterpriseHandler{management: &enterpriseOperationsHTTPStub{}, usage: service.NewEnterpriseUsageService(repo), memberReader: enterpriseHTTPMemberReader{member: &service.EnterpriseMemberContext{EnterpriseID: 8, Role: "owner", MemberStatus: "active", EnterpriseStatus: "active"}}}
		router := enterpriseTestRouter(h, service.RoleAdmin)
		for _, dimension := range []string{"models", "members", "departments"} {
			w := enterpriseTestRequest(router, http.MethodGet, base+"/usage/"+dimension+"/trend?start_date=2026-10-01&end_date=2026-10-31&granularity=hour&top=20&model=m&user_id=42&api_key_id=7", "")
			require.Equal(t, 200, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), `"data":[]`)
			require.Equal(t, dimension, repo.dimension)
			require.Equal(t, "hour", repo.granularity)
			require.Equal(t, 20, repo.top)
			require.Equal(t, int64(8), repo.filters.EnterpriseID)
			require.Equal(t, int64(42), repo.filters.UserID)
			require.Equal(t, int64(7), repo.filters.APIKeyID)
			require.Equal(t, "m", repo.filters.Model)
			w = enterpriseTestRequest(router, http.MethodGet, base+"/usage/"+dimension+"/trend?start_date=2026-09-30&end_date=2026-10-31&granularity=hour", "")
			require.Equal(t, 400, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), "INVALID_DATE_RANGE")
		}
	}
	repo := &enterpriseSeriesHTTPRepo{}
	h := &EnterpriseHandler{usage: service.NewEnterpriseUsageService(repo), memberReader: enterpriseHTTPMemberReader{member: &service.EnterpriseMemberContext{EnterpriseID: 8, Role: "member", MemberStatus: "active", EnterpriseStatus: "active"}}}
	w := enterpriseTestRequest(enterpriseTestRouter(h, service.RoleUser), http.MethodGet, "/api/v1/enterprise/usage/departments/trend", "")
	require.Equal(t, 403, w.Code, w.Body.String())
}
