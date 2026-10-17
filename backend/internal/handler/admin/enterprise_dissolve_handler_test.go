//go:build unit

package admin

import (
	"context"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type enterpriseDissolveHTTPStub struct {
	enterpriseManagementAPI
	dissolved, deleted bool
}

func (s *enterpriseDissolveHTTPStub) DeleteEnterprise(context.Context, service.EnterpriseActor, int64) error {
	s.deleted = true
	return service.ErrEnterpriseNotEmpty
}
func (s *enterpriseDissolveHTTPStub) DissolveEnterprise(_ context.Context, a service.EnterpriseActor, id int64) error {
	s.dissolved = true
	return nil
}

func TestEnterpriseHTTPDissolveRequiresExplicitQuery(t *testing.T) {
	for _, query := range []string{"", "?dissolve=false", "?dissolve=1", "?dissolve=true"} {
		t.Run(query, func(t *testing.T) {
			s := &enterpriseDissolveHTTPStub{}
			w := enterpriseTestRequest(enterpriseTestHandler(s, "owner"), http.MethodDelete, "/api/v1/admin/enterprises/8"+query, "")
			if query == "?dissolve=true" {
				require.Equal(t, http.StatusOK, w.Code)
				require.True(t, s.dissolved)
				require.False(t, s.deleted)
			} else {
				require.Equal(t, http.StatusConflict, w.Code)
				require.True(t, s.deleted)
				require.False(t, s.dissolved)
				require.Contains(t, w.Body.String(), "ENTERPRISE_NOT_EMPTY")
			}
		})
	}
}

func TestEnterpriseHTTPDissolveRejectsNonAdmin(t *testing.T) {
	s := &enterpriseDissolveHTTPStub{}
	r := enterpriseTestRouter(&EnterpriseHandler{management: s}, service.RoleUser)
	w := enterpriseTestRequest(r, http.MethodDelete, "/api/v1/admin/enterprises/8?dissolve=true", "")
	require.Equal(t, http.StatusForbidden, w.Code)
	require.False(t, s.dissolved || s.deleted)
}
