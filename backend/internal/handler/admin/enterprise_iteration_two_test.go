//go:build unit

package admin

import (
	"context"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type enterpriseIterationManagementStub struct {
	enterpriseManagementAPI
	effective func(service.EnterpriseActor, int64, int64) (*service.EnterpriseEffectiveGroups, error)
}

func (s enterpriseIterationManagementStub) GetEnterprise(_ context.Context, _ service.EnterpriseActor, id int64) (*service.Enterprise, error) {
	return &service.Enterprise{ID: id}, nil
}
func (s enterpriseIterationManagementStub) GetEffectiveGroups(_ context.Context, a service.EnterpriseActor, id, memberID int64) (*service.EnterpriseEffectiveGroups, error) {
	return s.effective(a, id, memberID)
}

type enterpriseIterationUsageStub struct {
	enterpriseUsageAPI
	params pagination.PaginationParams
}

func (s *enterpriseIterationUsageStub) List(_ context.Context, _ service.EnterpriseUsageScope, p pagination.PaginationParams, _ usagestats.UsageLogFilters) ([]service.EnterpriseUsageLog, *pagination.PaginationResult, error) {
	s.params = p
	return []service.EnterpriseUsageLog{{RequestType: "ws_v2"}}, &pagination.PaginationResult{Total: 1}, nil
}
func TestEnterpriseUsageSortBothRoutes(t *testing.T) {
	for _, base := range []string{"/api/v1/enterprise", "/api/v1/admin/enterprises/8"} {
		for _, tc := range []struct{ query, by, order string }{
			{"", "created_at", "desc"},
			{"?sort_by=model&sort_order=ASC", "model", "asc"},
			{"?sort_by=created_at&sort_order=asc", "created_at", "asc"},
			{"?sort_by=actual_cost&sort_order=asc", "created_at", "desc"},
			{"?sort_by=model&sort_order=invalid", "created_at", "desc"},
			{"?sort_by=model%3BDROP+TABLE+usage_logs&sort_order=asc", "created_at", "desc"},
		} {
			t.Run(base+tc.query, func(t *testing.T) {
				usage := &enterpriseIterationUsageStub{}
				h := &EnterpriseHandler{management: enterpriseIterationManagementStub{}, usage: usage, memberReader: enterpriseHTTPMemberReader{member: &service.EnterpriseMemberContext{EnterpriseID: 8, MemberID: 7, Role: "owner", EnterpriseStatus: "active", MemberStatus: "active"}}}
				w := enterpriseTestRequest(enterpriseTestRouter(h, service.RoleAdmin), http.MethodGet, base+"/usage"+tc.query, "")
				require.Equal(t, 200, w.Code, w.Body.String())
				require.Equal(t, tc.by, usage.params.SortBy)
				require.Equal(t, tc.order, usage.params.SortOrder)
				require.Contains(t, w.Body.String(), `"request_type":"ws_v2"`)
			})
		}
	}
}
func TestEnterpriseEffectiveGroupsBothRoutes(t *testing.T) {
	for _, base := range []string{"/api/v1/enterprise", "/api/v1/admin/enterprises/8"} {
		for _, tc := range []struct {
			query    string
			memberID int64
			status   int
		}{
			{"", 0, 200}, {"?member_id=7", 7, 200}, {"?member_id=99", 99, 403}, {"?member_id=0", 0, 400}, {"?member_id=-1", 0, 400}, {"?member_id=abc", 0, 400},
		} {
			t.Run(base+tc.query, func(t *testing.T) {
				called := false
				mgmt := enterpriseIterationManagementStub{effective: func(a service.EnterpriseActor, id, memberID int64) (*service.EnterpriseEffectiveGroups, error) {
					called = true
					require.EqualValues(t, 8, id)
					require.Equal(t, base == "/api/v1/admin/enterprises/8", a.IsAdmin)
					require.Equal(t, tc.memberID, memberID)
					if memberID == 99 {
						return nil, service.ErrEnterprisePermissionDenied
					}
					return &service.EnterpriseEffectiveGroups{MemberID: 7, Groups: []service.EnterpriseEffectiveGroup{}}, nil
				}}
				w := enterpriseTestRequest(enterpriseTestHandler(mgmt, "member"), http.MethodGet, base+"/group-whitelists/effective"+tc.query, "")
				require.Equal(t, tc.status, w.Code, w.Body.String())
				require.Equal(t, tc.status != 400, called)
				if tc.status == 403 {
					require.Contains(t, w.Body.String(), "ENTERPRISE_PERMISSION_DENIED")
				}
			})
		}
	}
}
