//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeminiVideoModelLimitsTableIsUnchanged(t *testing.T) {
	cases := []struct {
		name        string
		mappedModel string
		resolutions []string
		minDuration int
		maxDuration int
		durations   []int
	}{
		{
			name:        "veo 2.0",
			mappedModel: "veo-2.0-generate-001",
			resolutions: []string{VideoBillingResolution720P},
			minDuration: 5,
			maxDuration: 8,
		},
		{
			name:        "veo 3.0",
			mappedModel: "veo-3.0-generate-001",
			resolutions: []string{VideoBillingResolution720P, VideoBillingResolution1080P},
			minDuration: 8,
			maxDuration: 8,
		},
		{
			name:        "veo 3.1",
			mappedModel: "veo-3.1-generate-001",
			resolutions: []string{VideoBillingResolution720P, VideoBillingResolution1080P},
			minDuration: 4,
			maxDuration: 8,
			durations:   []int{4, 6, 8},
		},
		{
			name:        "unknown model keeps conservative defaults",
			mappedModel: "private-relay-video",
			resolutions: []string{VideoBillingResolution720P},
			minDuration: 8,
			maxDuration: 8,
		},
		{
			name:        "models prefix is trimmed",
			mappedModel: "models/veo-3.1-generate-001",
			resolutions: []string{VideoBillingResolution720P, VideoBillingResolution1080P},
			minDuration: 4,
			maxDuration: 8,
			durations:   []int{4, 6, 8},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			limits := geminiVideoModelLimits(tc.mappedModel)
			require.Equal(t, tc.resolutions, limits.Resolutions)
			require.Equal(t, tc.minDuration, limits.MinDuration)
			require.Equal(t, tc.maxDuration, limits.MaxDuration)
			require.Equal(t, tc.durations, limits.Durations)
			require.False(t, limits.ResolutionOverride)
			require.False(t, limits.DurationOverride)
		})
	}
}

func TestGeminiVideoModelFamilyMapping(t *testing.T) {
	require.Equal(t, "2", geminiVideoModelFamily("veo-2.0-generate-001"))
	require.Equal(t, "3", geminiVideoModelFamily("models/veo-3.0-fast-generate-001"))
	require.Equal(t, "3.1", geminiVideoModelFamily("veo-3.1-generate-001"))
	require.Equal(t, "unknown", geminiVideoModelFamily("veo-4.0-generate-001"))
	require.Equal(t, "unknown", geminiVideoModelFamily(""))
}
