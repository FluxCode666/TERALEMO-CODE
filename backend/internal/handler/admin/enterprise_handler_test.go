//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type enterpriseHTTPMemberReader struct {
	member *service.EnterpriseMemberContext
}

func (s enterpriseHTTPMemberReader) GetMemberContextByUserID(context.Context, int64) (*service.EnterpriseMemberContext, error) {
	return s.member, nil
}

type enterpriseHTTPManagementStub struct {
	enterpriseManagementAPI
	update       func(context.Context, service.EnterpriseActor, int64, service.UpdateEnterpriseInput) (*service.Enterprise, error)
	overview     func(context.Context, service.EnterpriseActor, int64) (*service.EnterpriseOverviewData, error)
	members      func(context.Context, service.EnterpriseActor, int64, pagination.PaginationParams, service.EnterpriseMemberListFilters) ([]service.EnterpriseMember, *pagination.PaginationResult, error)
	whitelist    func(context.Context, service.EnterpriseActor, int64, string, int64, []int64) error
	quota        func(context.Context, service.EnterpriseActor, int64, string, int64, service.EnterpriseQuotaLimitsInput) (*service.EnterpriseQuota, error)
	memberCreate func(context.Context, service.EnterpriseActor, int64, service.CreateEnterpriseMemberInput) (*service.EnterpriseMember, error)
}

func (s *enterpriseHTTPManagementStub) UpdateEnterprise(c context.Context, a service.EnterpriseActor, id int64, in service.UpdateEnterpriseInput) (*service.Enterprise, error) {
	return s.update(c, a, id, in)
}
func (s *enterpriseHTTPManagementStub) Overview(c context.Context, a service.EnterpriseActor, id int64) (*service.EnterpriseOverviewData, error) {
	return s.overview(c, a, id)
}
func (s *enterpriseHTTPManagementStub) ListMembers(c context.Context, a service.EnterpriseActor, id int64, p pagination.PaginationParams, f service.EnterpriseMemberListFilters) ([]service.EnterpriseMember, *pagination.PaginationResult, error) {
	return s.members(c, a, id, p, f)
}
func (s *enterpriseHTTPManagementStub) SetGroupWhitelist(c context.Context, a service.EnterpriseActor, id int64, scope string, scopeID int64, groups []int64) error {
	return s.whitelist(c, a, id, scope, scopeID, groups)
}
func (s *enterpriseHTTPManagementStub) UpdateQuota(c context.Context, a service.EnterpriseActor, id int64, scope string, scopeID int64, in service.EnterpriseQuotaLimitsInput) (*service.EnterpriseQuota, error) {
	return s.quota(c, a, id, scope, scopeID, in)
}
func (s *enterpriseHTTPManagementStub) CreateMember(c context.Context, a service.EnterpriseActor, id int64, in service.CreateEnterpriseMemberInput) (*service.EnterpriseMember, error) {
	return s.memberCreate(c, a, id, in)
}

type enterpriseHTTPUsageStub struct{ enterpriseUsageAPI }

func (enterpriseHTTPUsageStub) OverviewStats(context.Context, service.EnterpriseUsageScope) (float64, float64, error) {
	return 1.2, 30.5, nil
}
func enterpriseTestRouter(h *EnterpriseHandler, role string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
		c.Set(string(middleware.ContextKeyUserRole), role)
		c.Next()
	})
	h.RegisterAdminRoutes(r.Group("/api/v1/admin/enterprises"))
	h.RegisterEnterpriseRoutes(r.Group("/api/v1/enterprise"))
	return r
}
func enterpriseTestRequest(r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}
func enterpriseTestHandler(s enterpriseManagementAPI, role string) *gin.Engine {
	h := &EnterpriseHandler{management: s, usage: enterpriseHTTPUsageStub{}, memberReader: enterpriseHTTPMemberReader{member: &service.EnterpriseMemberContext{EnterpriseID: 8, MemberID: 7, Role: role, MemberStatus: "active", EnterpriseStatus: "active"}}}
	return enterpriseTestRouter(h, service.RoleAdmin)
}

func TestEnterpriseHTTPUpdateThresholdPresence(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		set        bool
		value      *float64
	}{{name: "omitted", body: `{"name":"Acme"}`}, {name: "explicit null", body: `{"low_balance_threshold":null}`, set: true}, {name: "zero", body: `{"low_balance_threshold":0}`, set: true, value: new(float64)}} {
		t.Run(tc.name, func(t *testing.T) {
			s := &enterpriseHTTPManagementStub{update: func(_ context.Context, a service.EnterpriseActor, id int64, in service.UpdateEnterpriseInput) (*service.Enterprise, error) {
				require.True(t, a.IsAdmin)
				require.Equal(t, int64(8), id)
				require.Equal(t, tc.set, in.LowBalanceThresholdSet)
				require.Equal(t, tc.value, in.LowBalanceThreshold)
				return &service.Enterprise{ID: id}, nil
			}}
			w := enterpriseTestRequest(enterpriseTestHandler(s, "owner"), http.MethodPut, "/api/v1/admin/enterprises/8", tc.body)
			require.Equal(t, 200, w.Code, w.Body.String())
		})
	}
}
func TestEnterpriseHTTPOverviewHidesSensitiveFields(t *testing.T) {
	s := &enterpriseHTTPManagementStub{overview: func(_ context.Context, a service.EnterpriseActor, id int64) (*service.EnterpriseOverviewData, error) {
		require.False(t, a.IsAdmin)
		require.Equal(t, int64(8), id)
		return &service.EnterpriseOverviewData{Enterprise: &service.Enterprise{ID: 8, Name: "Acme", Status: "active"}, Me: &service.EnterpriseMember{ID: 7, UserID: 42, Role: "member", Status: "active"}, Permissions: service.EnterprisePermissions{}}, nil
	}}
	w := enterpriseTestRequest(enterpriseTestHandler(s, "member"), http.MethodGet, "/api/v1/enterprise/overview?enterprise_id=999", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	var body struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	for _, key := range []string{"balance", "frozen_balance", "enterprise_quota", "department_quota", "member_quota", "settings"} {
		value, ok := body.Data[key]
		require.True(t, ok, key)
		require.Nil(t, value, key)
	}
	require.Equal(t, 1.2, body.Data["stats"].(map[string]any)["today_cost"])
	require.Contains(t, body.Data["permissions"], "view_department_usage")
}
func TestEnterpriseHTTPPermissionFailureAndAdminGuard(t *testing.T) {
	s := &enterpriseHTTPManagementStub{members: func(_ context.Context, a service.EnterpriseActor, id int64, _ pagination.PaginationParams, _ service.EnterpriseMemberListFilters) ([]service.EnterpriseMember, *pagination.PaginationResult, error) {
		require.False(t, a.IsAdmin)
		require.Equal(t, int64(8), id)
		return nil, nil, service.ErrEnterprisePermissionDenied
	}}
	w := enterpriseTestRequest(enterpriseTestHandler(s, "member"), http.MethodGet, "/api/v1/enterprise/members", "")
	require.Equal(t, 403, w.Code)
	require.Contains(t, w.Body.String(), "ENTERPRISE_PERMISSION_DENIED")
	h := &EnterpriseHandler{management: s, memberReader: enterpriseHTTPMemberReader{}}
	w = enterpriseTestRequest(enterpriseTestRouter(h, service.RoleUser), http.MethodGet, "/api/v1/admin/enterprises/8", "")
	require.Equal(t, 403, w.Code)
}
func TestEnterpriseHTTPWhitelistEmptyArray(t *testing.T) {
	called := false
	s := &enterpriseHTTPManagementStub{whitelist: func(_ context.Context, a service.EnterpriseActor, id int64, scope string, scopeID int64, groups []int64) error {
		called = true
		require.False(t, a.IsAdmin)
		require.Equal(t, int64(8), id)
		require.Equal(t, "member", scope)
		require.Equal(t, int64(7), scopeID)
		require.Empty(t, groups)
		return nil
	}}
	w := enterpriseTestRequest(enterpriseTestHandler(s, "owner"), http.MethodPut, "/api/v1/enterprise/group-whitelists/member/7", `{"group_ids":[]}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.True(t, called)
	require.Contains(t, w.Body.String(), `"group_ids":[]`)
}
func TestEnterpriseHTTPQuotaRequiresAllWindowsPreservesNullZero(t *testing.T) {
	called := false
	s := &enterpriseHTTPManagementStub{quota: func(_ context.Context, _ service.EnterpriseActor, id int64, scope string, scopeID int64, in service.EnterpriseQuotaLimitsInput) (*service.EnterpriseQuota, error) {
		called = true
		require.Nil(t, in.DailyLimitUSD)
		require.NotNil(t, in.WeeklyLimitUSD)
		require.Zero(t, *in.WeeklyLimitUSD)
		return &service.EnterpriseQuota{ScopeType: scope, ScopeID: scopeID, DailyLimitUSD: in.DailyLimitUSD, WeeklyLimitUSD: in.WeeklyLimitUSD}, nil
	}}
	r := enterpriseTestHandler(s, "owner")
	w := enterpriseTestRequest(r, http.MethodPut, "/api/v1/enterprise/quotas/member/7", `{"daily_limit_usd":null,"weekly_limit_usd":0,"monthly_limit_usd":1,"total_limit_usd":null}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.True(t, called)
	called = false
	w = enterpriseTestRequest(r, http.MethodPut, "/api/v1/enterprise/quotas/member/7", `{"daily_limit_usd":null}`)
	require.Equal(t, 400, w.Code)
	require.False(t, called)
}
func TestEnterpriseHTTPMemberDepartmentZero(t *testing.T) {
	s := &enterpriseHTTPManagementStub{memberCreate: func(_ context.Context, a service.EnterpriseActor, id int64, in service.CreateEnterpriseMemberInput) (*service.EnterpriseMember, error) {
		require.False(t, a.IsAdmin)
		require.Equal(t, int64(8), id)
		require.Equal(t, "member", in.Role)
		require.NotNil(t, in.DepartmentID)
		require.Zero(t, *in.DepartmentID)
		return &service.EnterpriseMember{ID: 7, EnterpriseID: id, UserID: 4, Role: in.Role}, nil
	}}
	w := enterpriseTestRequest(enterpriseTestHandler(s, "owner"), http.MethodPost, "/api/v1/enterprise/members", `{"email":"a@x.com","password":"12345678","role":"member","department_id":0}`)
	require.Equal(t, 200, w.Code, w.Body.String())
}

func TestEnterpriseHTTPRoutesMatchW4Contract(t *testing.T) {
	h := &EnterpriseHandler{memberReader: enterpriseHTTPMemberReader{}}
	r := enterpriseTestRouter(h, service.RoleUser)
	expected := map[string]bool{}
	add := func(base string, routes []string) {
		for _, route := range routes {
			parts := strings.SplitN(route, " ", 2)
			expected[parts[0]+" "+base+parts[1]] = true
		}
	}
	add("/api/v1/admin/enterprises", []string{
		"GET ", "POST ", "GET /:id", "PUT /:id", "DELETE /:id", "POST /:id/balance", "GET /:id/balance-logs",
		"GET /:id/departments", "POST /:id/departments", "PUT /:id/departments/:departmentId", "DELETE /:id/departments/:departmentId",
		"GET /:id/members", "POST /:id/members", "PUT /:id/members/:memberId", "DELETE /:id/members/:memberId",
		"GET /:id/quotas", "PUT /:id/quotas/:scopeType/:scopeId", "POST /:id/quotas/:scopeType/:scopeId/reset",
		"GET /:id/allowed-groups", "PUT /:id/allowed-groups", "GET /:id/group-whitelists", "PUT /:id/group-whitelists/:scopeType/:scopeId",
		"GET /:id/usage", "GET /:id/usage/stats", "GET /:id/usage/trend", "GET /:id/usage/models", "GET /:id/usage/members", "GET /:id/usage/departments", "GET /:id/audit-logs",
		"GET /:id/usage/api-key-options", "GET /:id/usage/model-options",
	})
	add("/api/v1/enterprise", []string{
		"GET /overview", "GET /departments", "POST /departments", "PUT /departments/:departmentId", "DELETE /departments/:departmentId",
		"GET /members", "POST /members", "PUT /members/:memberId", "DELETE /members/:memberId",
		"GET /quotas", "PUT /quotas/:scopeType/:scopeId", "POST /quotas/:scopeType/:scopeId/reset", "GET /groups",
		"GET /group-whitelists", "PUT /group-whitelists/:scopeType/:scopeId", "GET /settings", "PUT /settings",
		"GET /usage", "GET /usage/stats", "GET /usage/trend", "GET /usage/models", "GET /usage/members", "GET /usage/departments", "GET /audit-logs",
		"GET /usage/api-key-options", "GET /usage/model-options",
	})
	require.Len(t, expected, 57)
	require.Len(t, r.Routes(), 57)
	for _, route := range r.Routes() {
		key := route.Method + " " + route.Path
		require.True(t, expected[key], "unexpected route: %s", key)
		delete(expected, key)
	}
	require.Empty(t, expected)
}
