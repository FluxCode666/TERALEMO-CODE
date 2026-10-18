//go:build unit

package domain

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnterpriseSettingsDefaultsAndMissingKeys(t *testing.T) {
	defaults := DefaultEnterpriseSettings()
	for _, input := range []string{`{}`, `null`, `{"future_setting":true}`, `{"balance_visible_to_manager":null}`} {
		t.Run(input, func(t *testing.T) {
			var settings EnterpriseSettings
			require.NoError(t, json.Unmarshal([]byte(input), &settings))
			require.Equal(t, defaults, settings)
		})
	}
	var settings EnterpriseSettings
	require.NoError(t, json.Unmarshal([]byte(`{"balance_visible_to_manager":false,"manager_can_edit_member_groups":true}`), &settings))
	require.False(t, settings.BalanceVisibleToManager)
	require.True(t, settings.ManagerCanEditMemberGroups)
	require.True(t, settings.MemberQuotaVisibleToMember)
	require.False(t, settings.BalanceVisibleToMember)

	// Reusing a value must not retain keys from a previous enterprise.
	require.NoError(t, json.Unmarshal([]byte(`{}`), &settings))
	require.Equal(t, defaults, settings)
	before := settings
	require.Error(t, json.Unmarshal([]byte(`{"balance_visible_to_member":"yes"}`), &settings))
	require.Equal(t, before, settings)
}

func TestEnterpriseSettingsExplicitFalseRoundTrip(t *testing.T) {
	settings := EnterpriseSettings{}
	data, err := json.Marshal(settings)
	require.NoError(t, err)
	var decoded EnterpriseSettings
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.Equal(t, settings, decoded, "explicit false must override true defaults")
}
