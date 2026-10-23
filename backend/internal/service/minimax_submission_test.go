//go:build unit

package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func miniMaxSubmissionAccount(version, mappedModel string) *Account {
	a := miniMaxVideoTestAccount()
	a.Credentials["base_url"] = "https://proxy.example/minimax/" + version
	a.Credentials["model_mapping"] = map[string]any{"public-video": mappedModel}
	return a
}

func TestMiniMaxSchedulingAllowsAliasedV2LegacyRequests(t *testing.T) {
	for _, tc := range []struct {
		name       string
		body       string
		duration   int
		resolution string
	}{
		{"seven seconds", `{"model":"public-video","prompt":"waves","duration":7}`, 7, VideoBillingResolution768P},
		{"480P", `{"model":"public-video","prompt":"waves","resolution":"480P"}`, 6, VideoBillingResolution480P},
		{"four seconds", `{"model":"public-video","prompt":"waves","duration":4}`, 4, VideoBillingResolution768P},
		{"v1 image only", `{"model":"public-video","first_frame_image":"https://images.example/first.png"}`, 6, VideoBillingResolution768P},
		{"native content", `{"model":"public-video","content":[{"type":"text","text":"waves"}],"ratio":"16:9","duration":7}`, 7, VideoBillingResolution768P},
		{"known H3", `{"model":"MiniMax-H3","prompt":"waves","duration":4,"resolution":"2K"}`, 4, VideoBillingResolution2K},
		{"account extension deferred", `{"model":"MiniMax-H3","prompt":"waves","duration":20,"resolution":"1080P"}`, 20, VideoBillingResolution1080P},
		{"known Max can map to H3 for four seconds", `{"model":"MiniMax-H3-Max","prompt":"waves","duration":4}`, 4, VideoBillingResolution768P},
		{"known Max can map to H3 for 2K", `{"model":"MiniMax-H3-Max","prompt":"waves","resolution":"2K"}`, 6, VideoBillingResolution2K},
		{"known H3 can map to Max for 480P", `{"model":"MiniMax-H3","prompt":"waves","resolution":"480P"}`, 6, VideoBillingResolution480P},
		{"native known Max can map to H3", `{"model":"MiniMax-H3-Max","content":[{"type":"text","text":"waves"}],"ratio":"16:9","duration":4,"resolution":"2K"}`, 4, VideoBillingResolution2K},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(tc.body)
			original := bytes.Clone(body)
			info, err := ParseMiniMaxVideoSchedulingRequest(body)
			require.NoError(t, err)
			require.Equal(t, original, body)
			require.Equal(t, tc.duration, info.DurationSeconds)
			require.Equal(t, tc.resolution, info.Resolution)
			require.Equal(t, gjson.Get(tc.body, "model").String(), info.Model)
			if tc.name == "v1 image only" {
				require.Equal(t, []string{"https://images.example/first.png"}, info.InputImageURLs)
			} else {
				require.Equal(t, "waves", info.Prompt)
			}
		})
	}
	for _, body := range []string{
		`[]`, `{}`, `{"model":"public-video"}`,
		`{"model":"public-video","prompt":"waves","duration":7.5}`,
		`{"model":"public-video","prompt":"waves","duration":61}`,
		`{"model":"public-video","prompt":"waves","resolution":"4K"}`,
		`{"model":"public-video","content":[],"ratio":"16:9"}`,
		`{"model":"MiniMax-H3-Max","content":[],"ratio":"16:9"}`,
		`{"model":"MiniMax-H3-Max","content":[{"type":"text","text":"waves"}],"ratio":"2:1"}`,
		`{"model":"MiniMax-H3","prompt":"waves","ratio":"2:1"}`,
		`{"model":"MiniMax-H3-Max","prompt":"waves","duration":4.5}`,
	} {
		_, err := ParseMiniMaxVideoSchedulingRequest([]byte(body))
		require.Error(t, err, body)
	}
}

func TestNormalizeMiniMaxSubmissionMappedModelValidation(t *testing.T) {
	for _, tc := range []struct {
		name       string
		model      string
		parameters string
		wantError  bool
	}{
		{"H3 4 seconds", "MiniMax-H3", `"duration":4`, false},
		{"H3 15 seconds", "MiniMax-H3", `"duration":15`, false},
		{"Max 5 seconds", "MiniMax-H3-Max", `"duration":5`, false},
		{"Max 15 seconds", "MiniMax-H3-Max", `"duration":15`, false},
		{"Max 7 seconds", "MiniMax-H3-Max", `"duration":7`, false},
		{"Max rejects 4 seconds", "MiniMax-H3-Max", `"duration":4`, true},
		{"H3 rejects 3 seconds", "MiniMax-H3", `"duration":3`, true},
		{"H3 rejects 16 seconds", "MiniMax-H3", `"duration":16`, true},
		{"Max rejects fractions", "MiniMax-H3-Max", `"duration":7.5`, true},
		{"Max rejects string duration", "MiniMax-H3-Max", `"duration":"7"`, true},
		{"H3 2K", "MiniMax-H3", `"resolution":"2K"`, false},
		{"H3 rejects 480P", "MiniMax-H3", `"resolution":"480P"`, true},
		{"Max 480P", "MiniMax-H3-Max", `"resolution":" 480p "`, false},
		{"Max 768P", "MiniMax-H3-Max", `"resolution":"768P"`, false},
		{"Max rejects 2K", "minimax-h3-max", `"resolution":"2K"`, true},
		{"Max rejects v1 720P", "MiniMax-H3-Max", `"resolution":"720P"`, true},
		{"invalid ratio", "MiniMax-H3-Max", `"ratio":"2:1"`, true},
		{"text rejects adaptive", "MiniMax-H3-Max", `"ratio":"adaptive"`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(`{"model":"public-video","prompt":"waves",` + tc.parameters + `}`)
			original := bytes.Clone(body)
			prepared, info, err := NormalizeMiniMaxSubmission(body, miniMaxSubmissionAccount("v2", tc.model), false)
			require.Equal(t, original, body, "validation must not modify a body shared by scheduling attempts")
			if tc.wantError {
				require.Error(t, err)
				require.False(t, errors.Is(err, ErrMiniMaxVideoAccountIncompatible), "a bad mapped-model parameter is a client error")
				return
			}
			require.NoError(t, err)
			require.Equal(t, "public-video", info.Model)
			require.Equal(t, "public-video", gjson.GetBytes(prepared, "model").String())
			require.Equal(t, "waves", info.Prompt)
			require.Equal(t, "waves", gjson.GetBytes(prepared, "content.0.text").String())
			require.False(t, gjson.GetBytes(prepared, "prompt").Exists())
			require.Equal(t, info.DurationSeconds, int(gjson.GetBytes(prepared, "duration").Int()))
		})
	}
}

func TestNormalizeMiniMaxSubmissionAccountCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name         string
		body         string
		mapped       string
		requireV2    bool
		incompatible bool
		invalid      bool
	}{
		{"explicit v2 route", `{"model":"public-video","prompt":"waves"}`, "MiniMax-Hailuo-2.3", true, true, false},
		{"native content", `{"model":"public-video","content":[{"type":"text","text":"waves"}],"ratio":"16:9"}`, "MiniMax-Hailuo-2.3", false, true, false},
		{"native H3", `{"model":"MiniMax-H3","prompt":"waves"}`, "MiniMax-Hailuo-2.3", false, true, false},
		{"mapped H3", `{"model":"public-video","prompt":"waves"}`, "MiniMax-H3", false, true, false},
		{"mapped H3-Max", `{"model":"public-video","prompt":"waves"}`, "MiniMax-H3-Max", false, true, false},
		{"seven seconds needs v2", `{"model":"public-video","prompt":"waves","duration":7}`, "MiniMax-Hailuo-2.3", false, true, false},
		{"480P needs v2", `{"model":"public-video","prompt":"waves","resolution":"480P"}`, "MiniMax-Hailuo-2.3", false, true, false},
		{"v1 accepted", `{"model":"public-video","prompt":"waves","duration":10,"resolution":"720P"}`, "MiniMax-Hailuo-2.3", false, false, false},
		{"bad for both versions", `{"model":"public-video","prompt":"waves","duration":16}`, "MiniMax-Hailuo-2.3", false, false, true},
		{"bad JSON", `[]`, "MiniMax-Hailuo-2.3", false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(tc.body)
			original := bytes.Clone(body)
			_, _, err := NormalizeMiniMaxSubmission(body, miniMaxSubmissionAccount("v1", tc.mapped), tc.requireV2)
			require.Equal(t, original, body)
			if tc.incompatible {
				require.ErrorIs(t, err, ErrMiniMaxVideoAccountIncompatible)
			} else if tc.invalid {
				require.Error(t, err)
				require.NotErrorIs(t, err, ErrMiniMaxVideoAccountIncompatible)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestNormalizeMiniMaxSubmissionDefaultsPassthroughAndIdempotence(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version string
		body    string
	}{
		{"v1 legacy", "v1", `{"model":"public-video","prompt":"waves","prompt_optimizer":false,"future":{"preserve":[1,true,"x"]}}`},
		{"v1 native fields", "v1", `{"model":"public-video","first_frame_image":"https://img.example/first","last_frame_image":"https://img.example/last","future":{"preserve":[1,true,"x"]}}`},
		{"v2 legacy", "v2", `{"model":"public-video","prompt":"waves","first_frame_image":"https://img.example/first","last_frame_image":"https://img.example/last","future":{"preserve":[1,true,"x"]}}`},
		{"v2 native", "v2", `{"model":"public-video","content":[{"type":"text","text":"waves"},{"type":"image_url","image_url":{"url":"https://img.example/reference"},"role":"reference_image","future":"preserved"}],"ratio":"adaptive","future":{"preserve":[1,true,"x"]}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := miniMaxSubmissionAccount(tc.version, "MiniMax-H3-Max")
			if tc.version == "v1" {
				a = miniMaxSubmissionAccount("v1", "MiniMax-Hailuo-2.3")
			}
			body := make([]byte, len(tc.body), len(tc.body)+1024)
			copy(body, tc.body)
			original := bytes.Clone(body)
			prepared, info, err := NormalizeMiniMaxSubmission(body, a, false)
			require.NoError(t, err)
			require.Equal(t, original, body)
			require.Equal(t, 6, info.DurationSeconds)
			require.Equal(t, VideoBillingResolution768P, info.Resolution)
			require.Equal(t, 6, int(gjson.GetBytes(prepared, "duration").Int()))
			require.Equal(t, "768P", gjson.GetBytes(prepared, "resolution").String())
			require.Equal(t, "public-video", gjson.GetBytes(prepared, "model").String())
			require.Equal(t, gjson.Get(tc.body, "future").Raw, gjson.GetBytes(prepared, "future").Raw)
			if tc.version == "v1" {
				require.False(t, gjson.GetBytes(prepared, "content").Exists())
				require.Equal(t, gjson.Get(tc.body, "first_frame_image").String(), gjson.GetBytes(prepared, "first_frame_image").String())
			} else if tc.name == "v2 legacy" {
				require.Equal(t, "adaptive", gjson.GetBytes(prepared, "ratio").String())
				require.Equal(t, "first_frame", gjson.GetBytes(prepared, "content.1.role").String())
				require.Equal(t, "last_frame", gjson.GetBytes(prepared, "content.2.role").String())
				require.False(t, gjson.GetBytes(prepared, "first_frame_image").Exists())
			} else {
				require.Equal(t, gjson.Get(tc.body, "content").Raw, gjson.GetBytes(prepared, "content").Raw)
			}
			second, secondInfo, err := NormalizeMiniMaxSubmission(prepared, a, false)
			require.NoError(t, err)
			require.Equal(t, prepared, second)
			require.Equal(t, info, secondInfo)
		})
	}
}

func TestMiniMaxSubmissionForwardMapsModelOnceAndPreservesNormalizedMetadata(t *testing.T) {
	for _, parameters := range []string{`"duration":7`, `"resolution":"480P"`} {
		a := miniMaxSubmissionAccount("v2", "MiniMax-H3-Max")
		// Re-applying the mapping to a normalized upstream model would both pick
		// the wrong model and reject this request's 480P case.
		a.Credentials["model_mapping"] = map[string]any{"public-video": "MiniMax-H3-Max", "MiniMax-H3-Max": "MiniMax-H3"}
		body := []byte(`{"model":"public-video","prompt":"waves","future":{"keep":true},` + parameters + `}`)
		prepared, info, err := NormalizeMiniMaxSubmission(body, a, false)
		require.NoError(t, err)
		u := &grokMediaContentUpstreamStub{response: grokMediaContentStatusResponse(`{"task_id":"task-1"}`)}
		svc := &OpenAIGatewayService{httpUpstream: u}
		c, _ := grokMediaContentTestContext(http.MethodPost, "/v2/video_generation", nil)
		result, err := svc.ForwardMiniMaxVideo(context.Background(), c, a, MiniMaxVideoEndpointCreate, "", prepared)
		require.NoError(t, err)
		require.Len(t, u.requests, 1)
		payload, err := io.ReadAll(u.request.Body)
		require.NoError(t, err)
		require.Equal(t, "MiniMax-H3-Max", gjson.GetBytes(payload, "model").String())
		require.Equal(t, "MiniMax-H3-Max", result.UpstreamModel)
		require.Equal(t, "public-video", result.Model)
		require.Equal(t, "public-video", result.BillingModel)
		require.Equal(t, info.DurationSeconds, result.VideoDurationSeconds)
		require.Equal(t, info.Resolution, result.VideoResolution)
		require.Equal(t, info.DurationSeconds, int(gjson.GetBytes(payload, "duration").Int()))
		require.Equal(t, "waves", gjson.GetBytes(payload, "content.0.text").String())
		require.True(t, gjson.GetBytes(payload, "future.keep").Bool())
		require.Equal(t, "public-video", gjson.GetBytes(prepared, "model").String(), "forwarding must not mutate the public submission")
	}
}

func TestMiniMaxSubmissionKnownPublicModelsUseMappedLimitsExactlyOnce(t *testing.T) {
	for _, tc := range []struct {
		name       string
		public     string
		mapped     string
		fields     string
		duration   int
		resolution string
	}{
		{"Max name maps to H3 for four seconds", "MiniMax-H3-Max", "MiniMax-H3", `"prompt":"waves","duration":4`, 4, VideoBillingResolution768P},
		{"Max name maps to H3 for 2K", "MiniMax-H3-Max", "MiniMax-H3", `"prompt":"waves","resolution":"2K"`, 6, VideoBillingResolution2K},
		{"H3 name maps to Max for 480P", "MiniMax-H3", "MiniMax-H3-Max", `"prompt":"waves","resolution":"480P"`, 6, VideoBillingResolution480P},
		{"native Max name maps to H3", "MiniMax-H3-Max", "MiniMax-H3", `"content":[{"type":"text","text":"waves"}],"ratio":"16:9","duration":4,"resolution":"2K"`, 4, VideoBillingResolution2K},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := miniMaxSubmissionAccount("v2", tc.mapped)
			a.Credentials["model_mapping"] = map[string]any{tc.public: tc.mapped, tc.mapped: tc.public}
			body := []byte(`{"model":"` + tc.public + `",` + tc.fields + `}`)
			original := bytes.Clone(body)
			scheduling, err := ParseMiniMaxVideoSchedulingRequest(body)
			require.NoError(t, err)
			require.Equal(t, tc.public, scheduling.Model)
			prepared, info, err := NormalizeMiniMaxSubmission(body, a, false)
			require.NoError(t, err)
			require.Equal(t, original, body)
			require.Equal(t, tc.public, info.Model)
			require.Equal(t, tc.duration, info.DurationSeconds)
			require.Equal(t, tc.resolution, info.Resolution)
			u := &grokMediaContentUpstreamStub{response: grokMediaContentStatusResponse(`{"task_id":"task-1"}`)}
			svc := &OpenAIGatewayService{httpUpstream: u}
			c, _ := grokMediaContentTestContext(http.MethodPost, "/v2/video_generation", nil)
			result, err := svc.ForwardMiniMaxVideo(context.Background(), c, a, MiniMaxVideoEndpointCreate, "", prepared)
			require.NoError(t, err)
			require.Len(t, u.requests, 1)
			payload, err := io.ReadAll(u.request.Body)
			require.NoError(t, err)
			require.Equal(t, tc.mapped, gjson.GetBytes(payload, "model").String())
			require.Equal(t, tc.mapped, result.UpstreamModel)
			require.Equal(t, tc.public, result.Model)
			require.Equal(t, tc.public, result.BillingModel)
			require.Equal(t, tc.duration, result.VideoDurationSeconds)
			require.Equal(t, tc.resolution, result.VideoResolution)
			require.Equal(t, tc.duration, int(gjson.GetBytes(payload, "duration").Int()))
			require.Equal(t, tc.public, gjson.GetBytes(prepared, "model").String())
		})
	}
}
