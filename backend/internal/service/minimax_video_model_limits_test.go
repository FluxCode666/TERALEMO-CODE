//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func miniMaxLimitsAccount(baseURL, protocol string) *Account {
	return &Account{
		Platform: PlatformVideo,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"video_protocol": protocol,
			"base_url":       baseURL,
		},
	}
}

func TestMiniMaxVideoModelLimitsTableIsUnchanged(t *testing.T) {
	v2Account := miniMaxLimitsAccount("https://api.minimax.io/v2", VideoProtocolMiniMax)
	v1Account := miniMaxLimitsAccount("https://api.minimax.io", VideoProtocolMiniMax)

	cases := []struct {
		name        string
		account     *Account
		mappedModel string
		resolutions []string
		minDuration int
		maxDuration int
		durations   []int
	}{
		{
			name:        "v2 default",
			account:     v2Account,
			mappedModel: "MiniMax-Hailuo-2.3",
			resolutions: []string{"480p", "768p", "2k"},
			minDuration: 4,
			maxDuration: 15,
		},
		{
			name:        "v2 MiniMax-H3",
			account:     v2Account,
			mappedModel: "MiniMax-H3",
			resolutions: []string{"768p", "2k"},
			minDuration: 4,
			maxDuration: 15,
		},
		{
			name:        "v2 MiniMax-H3-Max",
			account:     v2Account,
			mappedModel: "MiniMax-H3-Max",
			resolutions: []string{"480p", "768p"},
			minDuration: 5,
			maxDuration: 15,
		},
		{
			name:        "v1 legacy durations",
			account:     v1Account,
			mappedModel: "MiniMax-Hailuo-2.3",
			resolutions: []string{"480p", "720p", "768p", "1080p", "2k"},
			minDuration: 6,
			maxDuration: 10,
			durations:   []int{6, 10},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			limits, err := miniMaxVideoModelLimits(tc.account, tc.mappedModel)
			require.NoError(t, err)
			require.Equal(t, tc.resolutions, limits.Resolutions)
			require.Equal(t, tc.minDuration, limits.MinDuration)
			require.Equal(t, tc.maxDuration, limits.MaxDuration)
			require.Equal(t, tc.durations, limits.Durations)
		})
	}
}

func TestMiniMaxVideoModelLimitsHonorsAccountOverrides(t *testing.T) {
	account := miniMaxLimitsAccount("https://api.minimax.io/v2", VideoProtocolMiniMax)
	account.Credentials["video_resolutions"] = []string{"480p", "1080p"}
	account.Credentials["video_min_duration_seconds"] = 7
	account.Credentials["video_max_duration_seconds"] = 9

	limits, err := miniMaxVideoModelLimits(account, "MiniMax-H3")
	require.NoError(t, err)
	require.Equal(t, []string{"480p", "1080p"}, limits.Resolutions)
	require.True(t, limits.ResolutionOverride)
	require.Equal(t, 7, limits.MinDuration)
	require.Equal(t, 9, limits.MaxDuration)
	require.True(t, limits.DurationOverride)
	require.Nil(t, limits.Durations)
}
