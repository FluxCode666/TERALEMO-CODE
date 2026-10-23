//go:build unit

package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGrokMediaNoAccountErrorPointsToMatchingProtocolEndpoint(t *testing.T) {
	cases := []struct {
		endpoint service.GrokMediaEndpoint
		code     string
		prefix   string
		hints    []string
	}{
		{service.SeedanceEndpointCreate, "seedance_no_eligible_account", "No eligible Seedance accounts", []string{"/v2/video_generation", ":predictLongRunning"}},
		{service.SeedanceEndpointStatus, "seedance_no_eligible_account", "No eligible Seedance accounts", []string{"/v2/video_generation"}},
		{service.GeminiVideoEndpointCreate, "gemini_video_no_eligible_account", "No eligible Gemini video accounts", []string{"/api/v3/contents/generations/tasks", "/v2/video_generation"}},
		{service.MiniMaxVideoEndpointCreate, "minimax_video_no_eligible_account", "No eligible MiniMax video accounts", []string{"/api/v3/contents/generations/tasks", ":predictLongRunning"}},
	}
	for _, tc := range cases {
		code, message := grokMediaNoAccountError(tc.endpoint)
		require.Equal(t, tc.code, code, tc.endpoint)
		require.True(t, len(message) > len(tc.prefix) && message[:len(tc.prefix)] == tc.prefix, message)
		for _, hint := range tc.hints {
			require.Contains(t, message, hint, tc.endpoint)
		}
	}

	code, message := grokMediaNoAccountError(service.GrokMediaEndpointVideosGenerations)
	require.Equal(t, "grok_media_no_eligible_account", code)
	require.Equal(t, "No eligible Grok media accounts", message)
}
