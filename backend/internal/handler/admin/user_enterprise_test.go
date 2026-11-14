package admin

import (
	"context"
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
)

type enterpriseUserListStub struct {
	service.AdminService
	filters service.UserListFilters
}

func (s *enterpriseUserListStub) ListUsers(_ context.Context, _, _ int, f service.UserListFilters, _, _ string) ([]service.User, int64, error) {
	s.filters = f
	return []service.User{{ID: 7}}, 1, nil
}

type enterpriseUserSummaryRepo struct{ service.EnterpriseRepository }

func (enterpriseUserSummaryRepo) GetMemberContextsByUserIDs(context.Context, []int64) (map[int64]*service.EnterpriseMemberContext, error) {
	return map[int64]*service.EnterpriseMemberContext{7: {EnterpriseID: 3, EnterpriseName: "Acme", Role: "owner", EnterpriseBalance: 12.5}}, nil
}
func TestAdminUserListEnterpriseFilterAndSummary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, query := range []string{"?enterprise_id=3", "?enterprise_id=invalid", "?enterprise_id=-1"} {
		stub := &enterpriseUserListStub{}
		h := NewUserHandler(stub, nil, nil, nil, nil, nil, nil)
		h.SetEnterpriseRepository(enterpriseUserSummaryRepo{})
		r := gin.New()
		r.GET("/users", h.List)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users"+query, nil))
		if query != "?enterprise_id=3" {
			require.Equal(t, http.StatusBadRequest, w.Code)
			continue
		}
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.EqualValues(t, 3, stub.filters.EnterpriseID)
		var body struct {
			Data struct {
				Items []struct {
					Enterprise *userEnterpriseSummary `json:"enterprise"`
				} `json:"items"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		bal := 12.5
		require.Equal(t, &userEnterpriseSummary{ID: 3, Name: "Acme", Role: "owner", Balance: &bal}, body.Data.Items[0].Enterprise)
	}
}

func TestNewUserEnterpriseSummaryHidesBalanceForNonOwner(t *testing.T) {
	require.Nil(t, newUserEnterpriseSummary(nil))
	s := newUserEnterpriseSummary(&service.EnterpriseMemberContext{EnterpriseID: 1, Role: "manager", EnterpriseBalance: 9})
	require.Nil(t, s.Balance)
	s = newUserEnterpriseSummary(&service.EnterpriseMemberContext{EnterpriseID: 1, Role: "owner", EnterpriseBalance: 9})
	require.NotNil(t, s.Balance)
}
