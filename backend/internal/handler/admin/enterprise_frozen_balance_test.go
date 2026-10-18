//go:build unit

package admin

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
)

type enterpriseFrozenHTTPStub struct{ enterpriseManagementAPI }

func (enterpriseFrozenHTTPStub) ListEnterprises(context.Context, service.EnterpriseActor, pagination.PaginationParams, service.EnterpriseListFilters) ([]service.Enterprise, *pagination.PaginationResult, error) {
	return []service.Enterprise{{ID: 8, Balance: 12, FrozenBalance: 3}}, &pagination.PaginationResult{Total: 1}, nil
}
func (enterpriseFrozenHTTPStub) GetEnterprise(context.Context, service.EnterpriseActor, int64) (*service.Enterprise, error) {
	return &service.Enterprise{ID: 8, Balance: 12, FrozenBalance: 3}, nil
}
func (enterpriseFrozenHTTPStub) ListQuotas(context.Context, service.EnterpriseActor, int64, string) ([]service.EnterpriseQuota, error) {
	return nil, nil
}
func (enterpriseFrozenHTTPStub) DissolveEnterprise(context.Context, service.EnterpriseActor, int64) error {
	return service.ErrEnterpriseFrozenBalancePending
}

func TestEnterpriseHTTPFrozenBalanceAdminAndDissolve(t *testing.T) {
	h := enterpriseTestHandler(enterpriseFrozenHTTPStub{}, "owner")
	for _, path := range []string{"/api/v1/admin/enterprises", "/api/v1/admin/enterprises/8"} {
		w := enterpriseTestRequest(h, http.MethodGet, path, "")
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), `"frozen_balance":3`)
	}
	w := enterpriseTestRequest(h, http.MethodDelete, "/api/v1/admin/enterprises/8?dissolve=true", "")
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "ENTERPRISE_FROZEN_BALANCE_PENDING")
}

func TestEnterpriseEffectiveGroupRoutesRemoved(t *testing.T) {
	for _, path := range []string{"/api/v1/enterprise/group-whitelists/effective", "/api/v1/admin/enterprises/8/group-whitelists/effective"} {
		w := enterpriseTestRequest(enterpriseTestHandler(enterpriseFrozenHTTPStub{}, "owner"), http.MethodGet, path, "")
		require.Equal(t, http.StatusNotFound, w.Code)
	}
}

func TestEnterpriseHTTPOverviewFrozenBalanceVisibility(t *testing.T) {
	for _, visible := range []bool{false, true} {
		management := &enterpriseHTTPManagementStub{overview: func(context.Context, service.EnterpriseActor, int64) (*service.EnterpriseOverviewData, error) {
			out := &service.EnterpriseOverviewData{Enterprise: &service.Enterprise{ID: 8}, Me: &service.EnterpriseMember{ID: 7}, BalanceVisible: visible}
			if visible {
				balance, frozen := 12.0, 3.0
				out.Balance, out.FrozenBalance = &balance, &frozen
			}
			return out, nil
		}}
		w := enterpriseTestRequest(enterpriseTestHandler(management, "owner"), http.MethodGet, "/api/v1/enterprise/overview", "")
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		want := `"frozen_balance":null`
		if visible {
			want = `"frozen_balance":3`
		}
		require.Contains(t, w.Body.String(), want)
	}
}
