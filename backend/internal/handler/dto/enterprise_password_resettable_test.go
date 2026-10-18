package dto

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestEnterpriseMemberPasswordResettableProjection(t *testing.T) {
	for _, created := range []bool{true, false} {
		member := &service.EnterpriseMember{AccountCreatedByEnterprise: created}
		dto := EnterpriseMemberFromService(member)
		require.Equal(t, created, dto.PasswordResettable)
		raw, err := json.Marshal(dto)
		require.NoError(t, err)
		var body map[string]any
		require.NoError(t, json.Unmarshal(raw, &body))
		require.Equal(t, created, body["password_resettable"])
		require.NotContains(t, body, "account_created_by_enterprise")
	}
}
