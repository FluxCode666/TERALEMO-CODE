//go:build unit

package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func geminiSubmissionTestAccount(mapped string) *Account {
	return &Account{ID: 1, Platform: PlatformVideo, Type: AccountTypeAPIKey, Credentials: map[string]any{
		"api_key": "mock-key", "video_protocol": VideoProtocolGemini, "base_url": "https://google.example/v1beta",
		"model_mapping": map[string]any{"public-video": mapped},
	}}
}

func TestParseGeminiVideoSchedulingEnvelope(t *testing.T) {
	for _, tc := range []struct {
		name, body      string
		duration, count int
	}{
		{"defaults", `{"model":"public-video","instances":[{"prompt":"waves"}]}`, 8, 1},
		{"mapped limits deferred", `{"model":"veo-3.0-generate-001","instances":[{}],"parameters":{"durationSeconds":5,"sampleCount":4}}`, 5, 4},
		{"decimal integers", `{"model":"public-video","instances":[{}],"parameters":{"durationSeconds":8.0,"sampleCount":2.0}}`, 8, 2},
		{"exponent integers", `{"model":"public-video","instances":[{}],"parameters":{"durationSeconds":8e0,"sampleCount":2E+0}}`, 8, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info, err := ParseGeminiVideoRequest([]byte(tc.body))
			require.NoError(t, err)
			require.Equal(t, tc.duration, info.DurationSeconds)
			require.Equal(t, float64(tc.duration), info.DurationSecondsExact)
			require.Equal(t, tc.count, info.N)
			require.Equal(t, VideoBillingResolution720P, info.Resolution)
		})
	}
	for _, body := range []string{
		`[]`, `{} {}`, `{"model":123,"instances":[{}]}`, `{"model":"veo","instances":[]}`,
		`{"model":"veo","instances":[{},{}]}`, `{"model":"veo","instances":["waves"]}`,
		`{"model":"veo","instances":[{}],"parameters":null}`,
		`{"model":"veo","model":"veo-2.0-generate-001","instances":[{}]}`,
		`{"model":"veo","instances":[{}],"parameters":{"sampleCount":1,"sampleCount":2}}`,
		`{"model":"veo","instances":[{}],"parameters":{"sampleCount":1,"sample\u0043ount":2}}`,
		`{"model":"veo","instances":[{}],"future":{"nested":1,"nested":2}}`,
		`{"model":"veo","instances":[{}],"parameters":{"resolution":null}}`,
		`{"model":"veo","instances":[{}],"parameters":{"resolution":"4k"}}`,
		`{"model":"veo","instances":[{}],"parameters":{"aspectRatio":"1:1"}}`,
	} {
		_, err := ParseGeminiVideoRequest([]byte(body))
		require.Error(t, err, body)
	}
	for _, field := range []string{"durationSeconds", "sampleCount"} {
		for _, value := range []string{`0`, `-1`, `1.5`, `"2"`, `null`, `true`, `{}`, `[]`, `NaN`, `1e999999999`, `1e-999999999`, `1.000000000000000000000000001`, `100000000000000000000`} {
			body := fmt.Sprintf(`{"model":"veo","instances":[{}],"parameters":{%q:%s}}`, field, value)
			_, err := ParseGeminiVideoRequest([]byte(body))
			require.Error(t, err, body)
		}
	}
}

func TestGeminiVideoRejectsQuantityAndTimingAliases(t *testing.T) {
	for _, body := range []string{
		`{"model":"veo","instances":[{}],"n":2}`,
		`{"model":"veo","instances":[{}],"sampleCount":2}`,
		`{"model":"veo","instances":[{}],"numberOfVideos":2}`,
		`{"model":"veo","instances":[{}],"parameters":{"numberOfVideos":2}}`,
		`{"model":"veo","instances":[{}],"parameters":{"number_of_videos":2}}`,
		`{"model":"veo","instances":[{}],"parameters":{"n":2}}`,
		`{"model":"veo","instances":[{}],"parameters":{"duration_seconds":8}}`,
		`{"model":"veo","instances":[{}],"parameters":{"extra_body":{"sampleCount":4}}}`,
		`{"model":"veo","instances":[{}],"generationConfig":{"durationSeconds":8}}`,
		`{"model":"veo","instances":[{"sampleCount":2}]}`,
	} {
		_, err := ParseGeminiVideoRequest([]byte(body))
		require.Error(t, err, body)
	}
}

func TestNormalizeGeminiVideoSubmissionFamilies(t *testing.T) {
	for _, tc := range []struct {
		name, mapped, parameters, extraInstance string
		count, duration                         int
		resolution                              string
	}{
		{"Veo2 defaults", "veo-2.0-generate-001", `{}`, ``, 1, 8, "720p"},
		{"Veo2 two results", "veo-2.0-generate-001", `{"sampleCount":2,"durationSeconds":5}`, ``, 2, 5, "720p"},
		{"Veo2 six seconds", "veo-2.0-generate-001", `{"sampleCount":2,"durationSeconds":6}`, ``, 2, 6, "720p"},
		{"Veo2 seven seconds", "veo-2.0-generate-001", `{"sampleCount":2,"durationSeconds":7}`, ``, 2, 7, "720p"},
		{"Veo3 defaults", "veo-3.0-generate-001", `{}`, ``, 1, 8, "720p"},
		{"Veo3 fast 1080p", "models/veo-3.0-fast-generate-001", `{"resolution":"1080P"}`, ``, 1, 8, "1080p"},
		{"Veo31 four seconds", "veo-3.1-generate-preview", `{"durationSeconds":4}`, ``, 1, 4, "720p"},
		{"Veo31 six seconds", "veo-3.1-fast-generate-preview", `{"durationSeconds":6}`, ``, 1, 6, "720p"},
		{"Veo31 portrait 1080p", "veo-3.1-generate-preview", `{"resolution":"1080p","aspectRatio":"9:16"}`, ``, 1, 8, "1080p"},
		{"Veo31 references", "veo-3.1-generate-preview", `{}`, `,"referenceImages":[{"image":{"gcsUri":"gs://bucket/reference.png"},"referenceType":"asset"}]`, 1, 8, "720p"},
		{"unknown defaults", "relay-private-veo", `{}`, ``, 1, 8, "720p"},
		{"exact numeric integers", "veo-2.0-generate-001", `{"sampleCount":2e0,"durationSeconds":8.0}`, ``, 2, 8, "720p"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(`{"model":"public-video","instances":[{"prompt":"waves","image":{"gcsUri":"gs://bucket/start.png"}` + tc.extraInstance + `}],"parameters":` + tc.parameters + `,"future":{"keep":true,"duration":99}}`)
			original := bytes.Clone(body)
			prepared, info, err := NormalizeGeminiVideoSubmission(body, geminiSubmissionTestAccount(tc.mapped))
			require.NoError(t, err)
			require.Equal(t, original, body, "normalizing an account candidate cannot mutate the original")
			require.Equal(t, "public-video", info.Model)
			require.Equal(t, "public-video", gjson.GetBytes(prepared, "model").String())
			require.Equal(t, tc.count, info.N)
			require.Equal(t, tc.duration, info.DurationSeconds)
			require.Equal(t, float64(tc.duration), info.DurationSecondsExact)
			require.Equal(t, tc.resolution, info.Resolution)
			require.Equal(t, int64(info.N), gjson.GetBytes(prepared, "parameters.sampleCount").Int())
			require.Equal(t, int64(info.DurationSeconds), gjson.GetBytes(prepared, "parameters.durationSeconds").Int())
			require.Equal(t, info.Resolution, gjson.GetBytes(prepared, "parameters.resolution").String())
			require.Equal(t, info.AspectRatio, gjson.GetBytes(prepared, "parameters.aspectRatio").String())
			require.Equal(t, "waves", info.Prompt)
			require.Equal(t, []string{"gs://bucket/start.png"}, info.InputImageURLs)
			require.Equal(t, gjson.GetBytes(original, "future").Raw, gjson.GetBytes(prepared, "future").Raw)
			again, againInfo, err := NormalizeGeminiVideoSubmission(prepared, geminiSubmissionTestAccount(tc.mapped))
			require.NoError(t, err)
			require.JSONEq(t, string(prepared), string(again))
			require.Equal(t, info, againInfo)
		})
	}
}

func TestNormalizeGeminiVideoRejectsModelLimits(t *testing.T) {
	for _, tc := range []struct{ model, parameters, instance string }{
		{"veo-2.0-generate-001", `{"sampleCount":3}`, `{}`},
		{"veo-2.0-generate-001", `{"sampleCount":4}`, `{}`},
		{"veo-2.0-generate-001", `{"durationSeconds":4}`, `{}`},
		{"veo-2.0-generate-001", `{"durationSeconds":9}`, `{}`},
		{"veo-2.0-generate-001", `{"resolution":"1080p"}`, `{}`},
		{"veo-3.0-generate-001", `{"sampleCount":2}`, `{}`},
		{"veo-3.0-fast-generate-001", `{"durationSeconds":6}`, `{}`},
		{"veo-3.0-generate-001", `{"resolution":"1080p","aspectRatio":"9:16"}`, `{}`},
		{"veo-3.1-generate-preview", `{"sampleCount":2}`, `{}`},
		{"veo-3.1-generate-preview", `{"durationSeconds":5}`, `{}`},
		{"veo-3.1-generate-preview", `{"durationSeconds":7}`, `{}`},
		{"veo-3.1-generate-preview", `{"durationSeconds":6,"resolution":"1080p"}`, `{}`},
		{"veo-3.1-generate-preview", `{"durationSeconds":6}`, `{"referenceImages":[{}]}`},
		{"veo-3.1-generate-preview", `{}`, `{"referenceImages":[{},{},{},{}]}`},
		{"veo-3.1-generate-preview", `{}`, `{"video":{"uri":"https://google.example/a.mp4"}}`},
		{"veo-3.1-generate-preview", `{"resolution":"4k"}`, `{}`},
		{"veo-3.1-generate-preview", `{"resolution":"2K"}`, `{}`},
		{"veo-3.1-generate-preview", `{"resolution":"768p"}`, `{}`},
		{"unknown-model", `{"sampleCount":2}`, `{}`},
		{"unknown-model", `{"durationSeconds":6}`, `{}`},
		{"unknown-model", `{"resolution":"1080p"}`, `{}`},
		{"unknown-model", `{}`, `{"referenceImages":[{}]}`},
	} {
		body := []byte(`{"model":"public-video","instances":[` + tc.instance + `],"parameters":` + tc.parameters + `}`)
		prepared, _, err := NormalizeGeminiVideoSubmission(body, geminiSubmissionTestAccount(tc.model))
		require.Error(t, err, tc.model+": "+string(body))
		require.Nil(t, prepared)
	}
	_, _, err := NormalizeGeminiVideoSubmission([]byte(`{"model":"veo","instances":[{}]}`), nil)
	require.Error(t, err)
}

func TestGeminiVideoForwardNormalizesDefaultsAndMapsOnce(t *testing.T) {
	for _, normalizedFirst := range []bool{false, true} {
		for _, tc := range []struct {
			model, parameters string
			count, duration   int
		}{
			{"veo-3.0-generate-001", `{}`, 1, 8},
			{"veo-2.0-generate-001", `{"sampleCount":2,"durationSeconds":7}`, 2, 7},
			{"veo-3.1-generate-preview", `{"durationSeconds":4}`, 1, 4},
		} {
			t.Run(fmt.Sprintf("%s/prepared=%t", tc.model, normalizedFirst), func(t *testing.T) {
				account := geminiSubmissionTestAccount(tc.model)
				account.Credentials["model_mapping"].(map[string]any)[tc.model] = "must-not-map-a-second-time"
				upstream := &grokMediaContentUpstreamStub{response: grokMediaContentStatusResponse(`{"name":"models/` + tc.model + `/operations/mock"}`)}
				svc := &OpenAIGatewayService{httpUpstream: upstream}
				body := []byte(`{"model":"public-video","instances":[{"prompt":"waves"}],"parameters":` + tc.parameters + `}`)
				if normalizedFirst {
					var err error
					body, _, err = NormalizeGeminiVideoSubmission(body, account)
					require.NoError(t, err)
				}
				c, _ := grokMediaContentTestContext(http.MethodPost, "/v1beta/models/public-video:predictLongRunning", nil)
				result, err := svc.ForwardGeminiVideo(context.Background(), c, account, GeminiVideoEndpointCreate, "", body)
				require.NoError(t, err)
				require.Len(t, upstream.requests, 1)
				require.Equal(t, "/v1beta/models/"+tc.model+":predictLongRunning", upstream.request.URL.Path)
				sent, err := io.ReadAll(upstream.request.Body)
				require.NoError(t, err)
				require.False(t, gjson.GetBytes(sent, "model").Exists())
				require.Equal(t, int64(tc.count), gjson.GetBytes(sent, "parameters.sampleCount").Int())
				require.Equal(t, int64(tc.duration), gjson.GetBytes(sent, "parameters.durationSeconds").Int())
				require.Equal(t, "720p", gjson.GetBytes(sent, "parameters.resolution").String())
				require.Equal(t, "public-video", result.Model)
				require.Equal(t, tc.model, result.UpstreamModel)
				require.Equal(t, tc.duration, result.VideoDurationSeconds)
				require.Equal(t, "720p", result.VideoResolution)
				require.Zero(t, result.VideoCount, "submission must not be treated as completion")
			})
		}
	}
}

func TestGeminiVideoForwardRejectsInvalidMetadataBeforeUpstream(t *testing.T) {
	for _, parameters := range []string{`{"sampleCount":2}`, `{"durationSeconds":6}`, `{"sampleCount":"1"}`, `{"sampleCount":1.5}`, `{"resolution":"4k"}`} {
		upstream := &grokMediaContentUpstreamStub{}
		svc := &OpenAIGatewayService{httpUpstream: upstream}
		c, _ := grokMediaContentTestContext(http.MethodPost, "/v1beta/models/public-video:predictLongRunning", nil)
		_, err := svc.ForwardGeminiVideo(context.Background(), c, geminiSubmissionTestAccount("veo-3.0-generate-001"), GeminiVideoEndpointCreate, "", []byte(`{"model":"public-video","instances":[{}],"parameters":`+parameters+`}`))
		require.Error(t, err)
		require.Empty(t, upstream.requests)
	}
}

func TestParseGeminiVideoAccountCapabilityEnvelope(t *testing.T) {
	for _, resolution := range []string{"480p", "720p", "768p", "1080p", "2k"} {
		for _, duration := range []int{1, 16, 60} {
			body := []byte(fmt.Sprintf(`{"model":"public-video","instances":[{}],"parameters":{"resolution":%q,"durationSeconds":%d}}`, resolution, duration))
			info, err := ParseGeminiVideoRequest(body)
			require.NoError(t, err)
			require.Equal(t, resolution, info.Resolution)
			require.Equal(t, duration, info.DurationSeconds)
		}
	}
	for _, parameters := range []string{
		`{"durationSeconds":61}`, `{"durationSeconds":0}`, `{"durationSeconds":null}`, `{"durationSeconds":"8"}`,
		`{"resolution":"4k"}`, `{"resolution":null}`, `{"resolution":720}`, `{"resolution":"hd"}`,
	} {
		_, err := ParseGeminiVideoRequest([]byte(`{"model":"public-video","instances":[{}],"parameters":` + parameters + `}`))
		require.Error(t, err, parameters)
	}
}

func TestGeminiVideoAccountCapabilityOverrides(t *testing.T) {
	for _, tc := range []struct {
		name, model, parameters, instance string
		capabilities                      map[string]any
		resolution                        string
		duration                          int
	}{
		{"lowest configured tier", "veo-3.0-generate-001", `{}`, `{}`, map[string]any{"video_resolutions": []string{"2k", "1080p", "480p"}}, "480p", 8},
		{"only configured high tier", "veo-3.0-generate-001", `{}`, `{}`, map[string]any{"video_resolutions": []string{"1080p"}}, "1080p", 8},
		{"unknown default lower bound", "private-veo", `{}`, `{}`, map[string]any{"video_resolutions": []string{"768p"}, "video_min_duration_seconds": 12, "video_max_duration_seconds": 20}, "768p", 12},
		{"unknown explicit expanded bounds", "private-veo", `{"durationSeconds":60,"resolution":"2k"}`, `{}`, map[string]any{"video_resolutions": []string{"2k"}, "video_min_duration_seconds": 1, "video_max_duration_seconds": 60}, "2k", 60},
		{"duration override clears discrete set", "veo-3.1-generate-preview", `{"durationSeconds":5}`, `{}`, map[string]any{"video_min_duration_seconds": 1, "video_max_duration_seconds": 60}, "720p", 5},
		{"default clamped into non discrete range", "veo-3.1-generate-preview", `{}`, `{}`, map[string]any{"video_min_duration_seconds": 5, "video_max_duration_seconds": 7}, "720p", 7},
		{"max only default clamp", "veo-3.1-generate-preview", `{}`, `{}`, map[string]any{"video_max_duration_seconds": 6}, "720p", 6},
		{"min only clears discrete set", "veo-3.1-generate-preview", `{"durationSeconds":5}`, `{}`, map[string]any{"video_min_duration_seconds": 5}, "720p", 5},
		{"default lower clamp", "veo-3.0-generate-001", `{}`, `{}`, map[string]any{"video_min_duration_seconds": 10, "video_max_duration_seconds": 12}, "720p", 10},
		{"Veo2 extended duration", "veo-2.0-generate-001", `{"sampleCount":2,"durationSeconds":20}`, `{}`, map[string]any{"video_min_duration_seconds": 1, "video_max_duration_seconds": 60}, "720p", 20},
		{"resolution override permits portrait", "veo-3.0-generate-001", `{"resolution":"1080p","aspectRatio":"9:16"}`, `{}`, map[string]any{"video_resolutions": []string{"1080p"}}, "1080p", 8},
		{"resolution override permits short 1080p", "veo-3.1-generate-preview", `{"resolution":"1080p","durationSeconds":6}`, `{}`, map[string]any{"video_resolutions": []string{"1080p"}}, "1080p", 6},
		{"duration override permits long 1080p", "veo-3.1-generate-preview", `{"resolution":"1080p","durationSeconds":10}`, `{}`, map[string]any{"video_min_duration_seconds": 4, "video_max_duration_seconds": 12}, "1080p", 10},
		{"duration override permits short references", "veo-3.1-generate-preview", `{}`, `{"referenceImages":[{}]}`, map[string]any{"video_max_duration_seconds": 6}, "720p", 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := geminiSubmissionTestAccount(tc.model)
			for key, value := range tc.capabilities {
				account.Credentials[key] = value
			}
			body := []byte(`{"model":"public-video","instances":[` + tc.instance + `],"parameters":` + tc.parameters + `}`)
			original := bytes.Clone(body)
			prepared, info, err := NormalizeGeminiVideoSubmission(body, account)
			require.NoError(t, err)
			require.Equal(t, original, body)
			require.Equal(t, tc.resolution, info.Resolution)
			require.Equal(t, tc.duration, info.DurationSeconds)
			require.Equal(t, float64(tc.duration), info.DurationSecondsExact)
			require.Equal(t, info.Resolution, gjson.GetBytes(prepared, "parameters.resolution").String())
			require.Equal(t, int64(info.DurationSeconds), gjson.GetBytes(prepared, "parameters.durationSeconds").Int())
			require.Equal(t, int64(info.N), gjson.GetBytes(prepared, "parameters.sampleCount").Int())
			second, secondInfo, err := NormalizeGeminiVideoSubmission(prepared, account)
			require.NoError(t, err)
			require.JSONEq(t, string(prepared), string(second))
			require.Equal(t, info, secondInfo)
		})
	}
}

func TestGeminiVideoAccountCapabilitiesRejectExplicitMismatch(t *testing.T) {
	for _, tc := range []struct {
		name, model, parameters, instance string
		capabilities                      map[string]any
	}{
		{"explicit unsupported resolution", "veo-3.0-generate-001", `{"resolution":"720p"}`, `{}`, map[string]any{"video_resolutions": []string{"480p"}}},
		{"explicit below minimum", "veo-3.1-generate-preview", `{"durationSeconds":4}`, `{}`, map[string]any{"video_min_duration_seconds": 5}},
		{"explicit above maximum", "veo-3.1-generate-preview", `{"durationSeconds":8}`, `{}`, map[string]any{"video_max_duration_seconds": 6}},
		{"resolution only preserves official duration", "veo-3.0-generate-001", `{"resolution":"480p","durationSeconds":6}`, `{}`, map[string]any{"video_resolutions": []string{"480p"}}},
		{"duration only preserves official resolutions", "veo-3.1-generate-preview", `{"resolution":"768p","durationSeconds":10}`, `{}`, map[string]any{"video_min_duration_seconds": 1, "video_max_duration_seconds": 60}},
		{"resolution only preserves discrete durations", "veo-3.1-generate-preview", `{"durationSeconds":5}`, `{}`, map[string]any{"video_resolutions": []string{"720p"}}},
		{"resolution only preserves reference duration", "veo-3.1-generate-preview", `{"durationSeconds":6}`, `{"referenceImages":[{}]}`, map[string]any{"video_resolutions": []string{"720p"}}},
		{"duration only preserves portrait limit", "veo-3.0-generate-001", `{"resolution":"1080p","aspectRatio":"9:16"}`, `{}`, map[string]any{"video_min_duration_seconds": 1, "video_max_duration_seconds": 60}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := geminiSubmissionTestAccount(tc.model)
			for key, value := range tc.capabilities {
				account.Credentials[key] = value
			}
			prepared, _, err := NormalizeGeminiVideoSubmission([]byte(`{"model":"public-video","instances":[`+tc.instance+`],"parameters":`+tc.parameters+`}`), account)
			require.ErrorIs(t, err, ErrVideoAccountCapabilityIncompatible)
			require.Nil(t, prepared)
		})
	}
}

func TestGeminiVideoCapabilitiesKeepNonUsageRules(t *testing.T) {
	for _, tc := range []struct{ model, parameters, instances string }{
		{"veo-2.0-generate-001", `{"sampleCount":3}`, `[{}]`},
		{"veo-3.0-generate-001", `{"sampleCount":2}`, `[{}]`},
		{"veo-3.1-generate-preview", `{"sampleCount":2}`, `[{}]`},
		{"private-veo", `{"sampleCount":2}`, `[{}]`},
		{"veo-3.1-generate-preview", `{}`, `[{"video":{"uri":"https://google.example/video.mp4"}}]`},
		{"veo-3.1-generate-preview", `{}`, `[{"referenceImages":[{},{},{},{}]}]`},
		{"veo-2.0-generate-001", `{}`, `[{"referenceImages":[{}]}]`},
		{"veo-3.1-generate-preview", `{}`, `[{},{}]`},
		{"veo-3.1-generate-preview", `{"resolution":"4k"}`, `[{}]`},
	} {
		account := geminiSubmissionTestAccount(tc.model)
		account.Credentials["video_resolutions"] = []string{"480p", "720p", "768p", "1080p", "2k"}
		account.Credentials["video_min_duration_seconds"], account.Credentials["video_max_duration_seconds"] = 1, 60
		_, _, err := NormalizeGeminiVideoSubmission([]byte(`{"model":"public-video","instances":`+tc.instances+`,"parameters":`+tc.parameters+`}`), account)
		require.Error(t, err, tc.model+tc.instances+tc.parameters)
	}
}

func TestGeminiVideoForwardUsesConfiguredCapabilityDefaults(t *testing.T) {
	account := geminiSubmissionTestAccount("veo-3.0-generate-001")
	account.Credentials["video_resolutions"] = []string{"2k"}
	account.Credentials["video_min_duration_seconds"], account.Credentials["video_max_duration_seconds"] = 20, 30
	upstream := &grokMediaContentUpstreamStub{response: grokMediaContentStatusResponse(`{"name":"models/veo-3.0-generate-001/operations/mock"}`)}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	c, _ := grokMediaContentTestContext(http.MethodPost, "/v1beta/models/public-video:predictLongRunning", nil)
	result, err := svc.ForwardGeminiVideo(context.Background(), c, account, GeminiVideoEndpointCreate, "", []byte(`{"model":"public-video","instances":[{}]}`))
	require.NoError(t, err)
	require.Len(t, upstream.requests, 1)
	sent, err := io.ReadAll(upstream.request.Body)
	require.NoError(t, err)
	require.Equal(t, "2k", gjson.GetBytes(sent, "parameters.resolution").String())
	require.Equal(t, int64(20), gjson.GetBytes(sent, "parameters.durationSeconds").Int())
	require.Equal(t, int64(1), gjson.GetBytes(sent, "parameters.sampleCount").Int())
	require.Equal(t, "2k", result.VideoResolution)
	require.Equal(t, 20, result.VideoDurationSeconds)
	require.Zero(t, result.VideoCount)
}
