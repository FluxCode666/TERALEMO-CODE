//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func TestNativeVideoUnsupportedBillingControlsNeverReachUpstream(t *testing.T) {
	for _, protocol := range []string{VideoProtocolSeedance, "minimax-v1", "minimax-v2"} {
		for _, control := range []struct {
			path  string
			value any
		}{
			{"n", 2}, {"num", 2}, {"sampleCount", 8}, {"sample_count", 8},
			{"durationSeconds", 30}, {"size", "1920x1080"},
			{"parameters.duration", 30}, {"parameters.sampleCount", 8},
			{"generation_config.resolution", "1080p"}, {"extra_body.parameters.n", 8},
		} {
			t.Run(protocol+"/"+control.path, func(t *testing.T) {
				body := []byte(`{"model":"public-video","prompt":"waves"}`)
				account := miniMaxSubmissionAccount("v1", "MiniMax-Hailuo-2.3")
				if protocol == VideoProtocolSeedance {
					body = seedanceResolutionTestBody(t, "waves")
					account = seedanceTestAccount()
				} else if protocol == "minimax-v2" {
					body = []byte(`{"model":"public-video","content":[{"type":"text","text":"waves"}],"ratio":"16:9"}`)
					account = miniMaxSubmissionAccount("v2", "MiniMax-H3-Max")
				}
				body, err := sjson.SetBytes(body, control.path, control.value)
				require.NoError(t, err)
				upstream := &grokMediaContentUpstreamStub{}
				svc := &OpenAIGatewayService{httpUpstream: upstream}
				c, _ := grokMediaContentTestContext(http.MethodPost, "/video-create", nil)
				if protocol == VideoProtocolSeedance {
					_, err = ParseSeedanceRequest(body)
					require.Error(t, err)
					_, err = svc.ForwardSeedance(context.Background(), c, account, SeedanceEndpointCreate, "", body)
				} else {
					_, err = ParseMiniMaxVideoSchedulingRequest(body)
					require.Error(t, err)
					_, err = svc.ForwardMiniMaxVideo(context.Background(), c, account, MiniMaxVideoEndpointCreate, "", body)
				}
				require.Error(t, err)
				require.Empty(t, upstream.requests, "unsupported controls must fail before a paid create")
			})
		}
	}
}

func TestMiniMaxNativeControlsAlwaysHaveSingleOutputAndExplicitTimingAndResolution(t *testing.T) {
	for _, tc := range []struct {
		version, mapped, body string
	}{
		{"v1", "MiniMax-Hailuo-2.3", `{"model":"public-video","prompt":"a duration of 30 seconds at resolution 4K; n 8","future":{"keep":true}}`},
		{"v2", "MiniMax-H3-Max", `{"model":"public-video","prompt":"waves","future":{"keep":true}}`},
		{"v2", "MiniMax-H3-Max", `{"model":"public-video","content":[{"type":"text","text":"waves"}],"ratio":"16:9","future":{"keep":true}}`},
	} {
		t.Run(tc.version+"/"+tc.body, func(t *testing.T) {
			body := []byte(tc.body)
			scheduled, err := ParseMiniMaxVideoSchedulingRequest(body)
			require.NoError(t, err)
			require.Equal(t, 1, scheduled.N)
			prepared, info, err := NormalizeMiniMaxSubmission(body, miniMaxSubmissionAccount(tc.version, tc.mapped), false)
			require.NoError(t, err)
			require.Equal(t, 1, info.N)
			require.Equal(t, 6, info.DurationSeconds)
			require.Equal(t, VideoBillingResolution768P, info.Resolution)
			require.Equal(t, 6, int(gjson.GetBytes(prepared, "duration").Int()))
			require.Equal(t, "768P", gjson.GetBytes(prepared, "resolution").String())
			require.True(t, gjson.GetBytes(prepared, "future.keep").Bool())
		})
	}
}

func TestSeedanceAndMiniMaxRejectAmbiguousJSONBeforeNormalization(t *testing.T) {
	seedance := []byte(`{"model":"video","content":[{"type":"text","text":"waves"}],"duration":5,"duration":30}`)
	_, _, err := NormalizeSeedanceSubmission(seedance, "doubao-seedance-2-5-pro")
	require.ErrorContains(t, err, "duplicate")
	for _, version := range []string{"v1", "v2"} {
		body := []byte(`{"model":"public-video","prompt":"waves","resolution":"768P","resolution":"2K"}`)
		_, err := ParseMiniMaxVideoSchedulingRequest(body)
		require.ErrorContains(t, err, "duplicate")
		_, _, err = NormalizeMiniMaxSubmission(body, miniMaxSubmissionAccount(version, "MiniMax-H3-Max"), false)
		require.ErrorContains(t, err, "duplicate")
	}
}
