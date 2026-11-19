package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAdminUserList_ParsesHideEnterpriseEmployees(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name  string
		query string
		want  bool
	}{
		{"missing defaults off", "", false},
		{"true", "?hide_enterprise_employees=true", true},
		{"one", "?hide_enterprise_employees=1", true},
		{"false", "?hide_enterprise_employees=false", false},
		{"zero", "?hide_enterprise_employees=0", false},
		{"invalid defaults off", "?hide_enterprise_employees=abc", false},
		{"empty defaults off", "?hide_enterprise_employees=", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &listUsersFilterStub{AdminService: newStubAdminService()}
			r := gin.New()
			h := NewUserHandler(stub, nil, nil, nil, nil, nil, nil)
			r.GET("/admin/users", h.List)

			w := httptest.NewRecorder()
			req, _ := http.NewRequest(http.MethodGet, "/admin/users"+tc.query, nil)
			r.ServeHTTP(w, req)

			require.Equal(t, http.StatusOK, w.Code)
			require.Equal(t, tc.want, stub.captured.HideEnterpriseEmployees)
		})
	}
}

func TestAdminUserList_HideEnterpriseEmployeesCombinesWithEnterpriseID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &listUsersFilterStub{AdminService: newStubAdminService()}
	r := gin.New()
	h := NewUserHandler(stub, nil, nil, nil, nil, nil, nil)
	r.GET("/admin/users", h.List)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/admin/users?enterprise_id=7&hide_enterprise_employees=true", nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.EqualValues(t, 7, stub.captured.EnterpriseID)
	require.True(t, stub.captured.HideEnterpriseEmployees)
}
