//go:build unit

package dto

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestEnterpriseDTOIncludesActiveOwnerCount(t *testing.T) {
	for _, count := range []int{0, 2} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			company := EnterpriseFromService(&service.Enterprise{ID: 1, ActiveOwnerCount: count})
			require.Equal(t, count, company.ActiveOwnerCount)
			// Both the detail object and the list item use this DTO.
			for _, response := range []any{company, []Enterprise{*company}} {
				body, err := json.Marshal(response)
				require.NoError(t, err)
				require.Contains(t, string(body), `"active_owner_count":`+strconv.Itoa(count))
			}
		})
	}
}
