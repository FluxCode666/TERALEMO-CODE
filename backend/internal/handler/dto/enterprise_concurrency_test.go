package dto

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestEnterpriseConcurrencyResponseFields(t *testing.T) {
	value := 13
	for _, v := range []*int{nil, &value} {
		data, err := json.Marshal(EnterpriseFromService(&service.Enterprise{ID: 1, MemberConcurrency: v}))
		require.NoError(t, err)
		var fields map[string]any
		require.NoError(t, json.Unmarshal(data, &fields))
		require.Contains(t, fields, "member_concurrency")
		if v == nil {
			require.Nil(t, fields["member_concurrency"])
		} else {
			require.Equal(t, float64(*v), fields["member_concurrency"])
		}
	}
	member := EnterpriseMemberFromService(&service.EnterpriseMember{ID: 2, Concurrency: value})
	data, err := json.Marshal(member)
	require.NoError(t, err)
	require.Contains(t, string(data), `"concurrency":13`)
}
