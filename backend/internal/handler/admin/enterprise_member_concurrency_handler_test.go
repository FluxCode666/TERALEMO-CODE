//go:build unit

package admin

import (
	"context"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type enterpriseConcurrencyHTTPStub struct {
	enterpriseManagementAPI
	update func(context.Context, service.EnterpriseActor, int64, *int) (*service.EnterpriseMemberConcurrencyResult, error)
}

func (s *enterpriseConcurrencyHTTPStub) UpdateMemberConcurrency(ctx context.Context, a service.EnterpriseActor, id int64, value *int) (*service.EnterpriseMemberConcurrencyResult, error) {
	return s.update(ctx, a, id, value)
}

func TestEnterpriseHTTPMemberConcurrencyContract(t *testing.T) {
	for _, tc := range []struct {
		body string
		code int
	}{
		{`{"concurrency":12}`, 200}, {`{"concurrency":1}`, 200},
		{`{"concurrency":2147483647}`, 200}, {`{"concurrency":null}`, 200},
		{`{}`, 400}, {`null`, 400}, {`{"concurrency":0}`, 400},
		{`{"concurrency":-1}`, 400}, {`{"concurrency":2147483648}`, 400},
		{`{"concurrency":1.5}`, 400}, {`{"concurrency":"12"}`, 400},
		{`{"concurrency":true}`, 400}, {`{"concurrency":[]}`, 400},
	} {
		t.Run(tc.body, func(t *testing.T) {
			called := false
			s := &enterpriseConcurrencyHTTPStub{update: func(_ context.Context, a service.EnterpriseActor, id int64, v *int) (*service.EnterpriseMemberConcurrencyResult, error) {
				called = true
				require.True(t, a.IsAdmin)
				require.Equal(t, int64(42), a.UserID)
				require.Equal(t, int64(8), id)
				count := 3
				if v == nil {
					count = 0
				}
				return &service.EnterpriseMemberConcurrencyResult{MemberConcurrency: v, UpdatedCount: count}, nil
			}}
			w := enterpriseTestRequest(enterpriseTestHandler(s, "owner"), http.MethodPut, "/api/v1/admin/enterprises/8/member-concurrency", tc.body)
			require.Equal(t, tc.code, w.Code, w.Body.String())
			require.Equal(t, tc.code == 200, called)
			if tc.code == 200 {
				require.Contains(t, w.Body.String(), `"member_concurrency":`)
				require.Contains(t, w.Body.String(), `"updated_count":`)
			} else {
				require.Contains(t, w.Body.String(), "INVALID_INPUT")
			}
		})
	}
}

func TestEnterpriseHTTPMemberConcurrencyAdminGuardAndErrors(t *testing.T) {
	s := &enterpriseConcurrencyHTTPStub{update: func(context.Context, service.EnterpriseActor, int64, *int) (*service.EnterpriseMemberConcurrencyResult, error) {
		return nil, service.ErrEnterpriseNotFound
	}}
	h := &EnterpriseHandler{management: s, memberReader: enterpriseHTTPMemberReader{member: &service.EnterpriseMemberContext{EnterpriseID: 8, Role: "owner", MemberStatus: "active", EnterpriseStatus: "active"}}}
	r := enterpriseTestRouter(h, service.RoleUser)
	w := enterpriseTestRequest(r, http.MethodPut, "/api/v1/admin/enterprises/8/member-concurrency", `{"concurrency":5}`)
	require.Equal(t, 403, w.Code)
	require.Contains(t, w.Body.String(), "ENTERPRISE_PERMISSION_DENIED")
	w = enterpriseTestRequest(r, http.MethodPut, "/api/v1/enterprise/member-concurrency", `{"concurrency":5}`)
	require.Equal(t, 404, w.Code)
	r = enterpriseTestHandler(s, "owner")
	w = enterpriseTestRequest(r, http.MethodPut, "/api/v1/admin/enterprises/8/member-concurrency", `{"concurrency":5}`)
	require.Equal(t, 404, w.Code)
	require.Contains(t, w.Body.String(), "ENTERPRISE_NOT_FOUND")
	w = enterpriseTestRequest(r, http.MethodPut, "/api/v1/admin/enterprises/invalid/member-concurrency", `{"concurrency":5}`)
	require.Equal(t, 400, w.Code)
}
