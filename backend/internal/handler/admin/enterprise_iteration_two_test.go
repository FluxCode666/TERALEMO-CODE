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
}

func (s enterpriseIterationManagementStub) GetEnterprise(_ context.Context, _ service.EnterpriseActor, id int64) (*service.Enterprise, error) {
	return &service.Enterprise{ID: id}, nil
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
