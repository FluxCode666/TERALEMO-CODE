//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestSeedanceStatusPrefersExactFramesToTruncatedDuration(t *testing.T) {
	for _, tc := range []struct {
		name, fields string
		exact        float64
		display      int
	}{
		{"289 frames", `"frames":289,"duration":12`, 289.0 / 24, 13},
		{"29 frames", `"frames":29,"duration":1`, 29.0 / 24, 2},
		{"fractional seconds", `"duration":6.25`, 6.25, 7},
		{"integer seconds", `"duration":6`, 6, 6},
		{"invalid frames do not truncate duration", `"frames":29.5,"duration":6.25`, 6.25, 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"id":"task-1","status":"succeeded","content":{"video_url":"https://cdn.example/video.mp4"},` + tc.fields + `}`
			upstream := &grokMediaContentUpstreamStub{response: grokMediaContentStatusResponse(body)}
			svc := &OpenAIGatewayService{httpUpstream: upstream}
			c, _ := grokMediaContentTestContext(http.MethodGet, "/api/v3/contents/generations/tasks/task-1", nil)
			result, err := svc.ForwardSeedance(context.Background(), c, seedanceTestAccount(), SeedanceEndpointStatus, "seedance:task-1", nil)
			require.NoError(t, err)
			require.True(t, HTTPUpstreamRedirectsDisabled(upstream.request.Context()))
			require.Equal(t, tc.exact, result.VideoDurationSecondsExact)
			require.Equal(t, tc.display, result.VideoDurationSeconds)
		})
	}
}

func TestSeedanceForwardNormalizesTimingAfterModelMapping(t *testing.T) {
	for _, tc := range []struct {
		name, mapped, timing string
		wantDuration         int
		wantFrames           int
		wantErr              bool
	}{
		{"2.5 official smart default", "doubao-seedance-2-5-pro", "", -1, 0, false},
		{"1.0 frames", "doubao-seedance-1-0-pro-fast", `,"frames":289`, 0, 289, false},
		{"unknown deployment cannot guess smart cap", "ep-unknown", `,"duration":-1`, 0, 0, true},
		{"mapped 1.0 cannot accept 30 seconds", "doubao-seedance-1-0-pro", `,"duration":30`, 0, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := seedanceTestAccount()
			a.Credentials["model_mapping"] = map[string]any{"video": tc.mapped}
			upstream := &grokMediaContentUpstreamStub{response: grokMediaContentStatusResponse(`{"id":"task-1"}`)}
			svc := &OpenAIGatewayService{httpUpstream: upstream}
			c, _ := grokMediaContentTestContext(http.MethodPost, "/api/v3/contents/generations/tasks", nil)
			result, err := svc.ForwardSeedance(context.Background(), c, a, SeedanceEndpointCreate, "", []byte(`{"model":"video","content":[{"type":"text","text":"waves"}]`+tc.timing+`}`))
			if tc.wantErr {
				require.Error(t, err)
				require.Empty(t, upstream.requests)
				return
			}
			require.NoError(t, err)
			require.True(t, HTTPUpstreamRedirectsDisabled(upstream.request.Context()))
			forwarded, err := io.ReadAll(upstream.request.Body)
			require.NoError(t, err)
			require.Equal(t, tc.mapped, gjson.GetBytes(forwarded, "model").String())
			if tc.wantFrames > 0 {
				require.False(t, gjson.GetBytes(forwarded, "duration").Exists())
				require.Equal(t, tc.wantFrames, int(gjson.GetBytes(forwarded, "frames").Int()))
				require.Equal(t, float64(tc.wantFrames)/24, result.VideoDurationSecondsExact)
			} else {
				require.Equal(t, tc.wantDuration, int(gjson.GetBytes(forwarded, "duration").Int()))
			}
		})
	}
}
