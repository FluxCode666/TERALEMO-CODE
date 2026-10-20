//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type enterpriseHTTPGroupRatesStub struct {
	*enterpriseHTTPManagementStub
	get   func(context.Context, service.EnterpriseActor, int64) ([]service.EnterpriseAllowedGroupView, error)
	ids   func(context.Context, service.EnterpriseActor, int64, []int64) error
	rates func(context.Context, service.EnterpriseActor, int64, []service.EnterpriseAllowedGroup) error
}

func (s *enterpriseHTTPGroupRatesStub) GetAllowedGroups(ctx context.Context, actor service.EnterpriseActor, id int64) ([]service.EnterpriseAllowedGroupView, error) {
	if s.get != nil {
		return s.get(ctx, actor, id)
	}
	return []service.EnterpriseAllowedGroupView{}, nil
}

func (s *enterpriseHTTPGroupRatesStub) SetAllowedGroups(ctx context.Context, actor service.EnterpriseActor, id int64, groups []int64) error {
	return s.ids(ctx, actor, id, groups)
}

func (s *enterpriseHTTPGroupRatesStub) SetAllowedGroupRates(ctx context.Context, actor service.EnterpriseActor, id int64, groups []service.EnterpriseAllowedGroup) error {
	return s.rates(ctx, actor, id, groups)
}

func TestEnterpriseHTTPAllowedGroupsPayloadCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		newFormat  bool
		count      int
	}{
		{"rates and null", `{"groups":[{"group_id":1,"rate_multiplier":0.8},{"group_id":2,"rate_multiplier":null}]}`, true, 2},
		{"omitted rate follows default", `{"groups":[{"group_id":1}]}`, true, 1},
		{"new empty", `{"groups":[]}`, true, 0},
		{"legacy", `{"group_ids":[1,2]}`, false, 2},
		{"legacy empty", `{"group_ids":[]}`, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			s := &enterpriseHTTPGroupRatesStub{enterpriseHTTPManagementStub: &enterpriseHTTPManagementStub{}}
			s.ids = func(_ context.Context, actor service.EnterpriseActor, id int64, groups []int64) error {
				called = true
				require.False(t, tc.newFormat)
				require.True(t, actor.IsAdmin)
				require.Equal(t, int64(8), id)
				require.Len(t, groups, tc.count)
				return nil
			}
			s.rates = func(_ context.Context, actor service.EnterpriseActor, id int64, groups []service.EnterpriseAllowedGroup) error {
				called = true
				require.True(t, tc.newFormat)
				require.True(t, actor.IsAdmin)
				require.Equal(t, int64(8), id)
				require.Len(t, groups, tc.count)
				if tc.count == 2 {
					require.Equal(t, int64(1), groups[0].GroupID)
					require.Equal(t, 0.8, *groups[0].RateMultiplier)
					require.Nil(t, groups[1].RateMultiplier)
				} else if tc.count == 1 {
					require.Nil(t, groups[0].RateMultiplier)
				}
				return nil
			}
			w := enterpriseTestRequest(enterpriseTestHandler(s, "owner"), http.MethodPut, "/api/v1/admin/enterprises/8/allowed-groups", tc.body)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.True(t, called)
		})
	}
}

func TestEnterpriseHTTPAllowedGroupsRejectInvalidPayloads(t *testing.T) {
	for _, body := range []string{
		`{}`,
		`{"groups":null}`,
		`{"group_ids":null}`,
		`{"groups":[],"group_ids":[]}`,
		`{"groups":[{"rate_multiplier":1}]}`,
		`{"groups":[{"group_id":0,"rate_multiplier":1}]}`,
		`{"groups":[{"group_id":1,"rate_multiplier":-1}]}`,
		`{"groups":[{"group_id":1,"rate_multiplier":0}]}`,
		`{"groups":[{"group_id":1,"rate_multiplier":"0.8"}]}`,
		`{"groups":[{"group_id":1,"rate_multiplier":1e999}]}`,
		`{"groups":[{"group_id":1,"rate_multiplier":NaN}]}`,
		`{"group_ids":[0]}`,
	} {
		t.Run(body, func(t *testing.T) {
			called := false
			s := &enterpriseHTTPGroupRatesStub{enterpriseHTTPManagementStub: &enterpriseHTTPManagementStub{}}
			s.ids = func(context.Context, service.EnterpriseActor, int64, []int64) error { called = true; return nil }
			s.rates = func(context.Context, service.EnterpriseActor, int64, []service.EnterpriseAllowedGroup) error {
				called = true
				return nil
			}
			w := enterpriseTestRequest(enterpriseTestHandler(s, "owner"), http.MethodPut, "/api/v1/admin/enterprises/8/allowed-groups", body)
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			require.False(t, called)
		})
	}
}

func TestEnterpriseHTTPGroupRatesAdminNullableAndWorkspaceFinal(t *testing.T) {
	rate := 0.8
	s := &enterpriseHTTPGroupRatesStub{enterpriseHTTPManagementStub: &enterpriseHTTPManagementStub{}}
	s.get = func(_ context.Context, _ service.EnterpriseActor, id int64) ([]service.EnterpriseAllowedGroupView, error) {
		require.Equal(t, int64(8), id)
		return []service.EnterpriseAllowedGroupView{
			{Group: service.Group{ID: 1, RateMultiplier: 0.5}, DefaultRateMultiplier: 2, EffectiveRateMultiplier: 0.5},
			{Group: service.Group{ID: 2, RateMultiplier: rate}, EnterpriseRateMultiplier: &rate, DefaultRateMultiplier: 3, EffectiveRateMultiplier: rate},
		}, nil
	}
	for _, tc := range []struct {
		path  string
		admin bool
	}{
		{"/api/v1/admin/enterprises/8/allowed-groups", true},
		{"/api/v1/enterprise/groups", false},
	} {
		t.Run(tc.path, func(t *testing.T) {
			w := enterpriseTestRequest(enterpriseTestHandler(s, "manager"), http.MethodGet, tc.path, "")
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			var body struct {
				Data []map[string]any `json:"data"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			require.Len(t, body.Data, 2)
			if tc.admin {
				require.Nil(t, body.Data[0]["rate_multiplier"])
			} else {
				require.Equal(t, 0.5, body.Data[0]["rate_multiplier"])
			}
			require.Nil(t, body.Data[0]["enterprise_rate_multiplier"])
			require.Equal(t, 2.0, body.Data[0]["default_rate_multiplier"])
			require.Equal(t, 0.5, body.Data[0]["effective_rate_multiplier"])
			require.Equal(t, rate, body.Data[1]["rate_multiplier"])
			require.Equal(t, rate, body.Data[1]["enterprise_rate_multiplier"])
		})
	}
}

func TestEnterpriseHTTPGroupRatesOnlySystemAdminCanWrite(t *testing.T) {
	s := &enterpriseHTTPGroupRatesStub{enterpriseHTTPManagementStub: &enterpriseHTTPManagementStub{}}
	h := &EnterpriseHandler{management: s, memberReader: enterpriseHTTPMemberReader{member: &service.EnterpriseMemberContext{EnterpriseID: 8, MemberID: 7, Role: "owner", MemberStatus: "active", EnterpriseStatus: "active"}}}
	r := enterpriseTestRouter(h, service.RoleUser)
	w := enterpriseTestRequest(r, http.MethodPut, "/api/v1/admin/enterprises/8/allowed-groups", `{"groups":[]}`)
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	w = enterpriseTestRequest(r, http.MethodPut, "/api/v1/enterprise/groups", `{"groups":[]}`)
	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}
