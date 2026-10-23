//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNormalizeGrokVideoSubmissionDefaultsAndIsIdempotent(t *testing.T) {
	body := []byte(`{"model":"video-alias","prompt":"A lake at sunrise","n":1}`)
	out, info, err := NormalizeGrokVideoSubmission(body, "application/json; charset=utf-8", GrokMediaEndpointVideosGenerations)
	require.NoError(t, err)
	require.Equal(t, 8, info.DurationSeconds)
	require.Equal(t, 8.0, info.DurationSecondsExact)
	require.Equal(t, "480p", info.Resolution)
	require.Equal(t, 1, info.N)
	require.Equal(t, "video-alias", info.Model)
	require.Equal(t, gjson.Number, gjson.GetBytes(out, "duration").Type)
	require.Equal(t, int64(8), gjson.GetBytes(out, "duration").Int())
	require.Equal(t, "480p", gjson.GetBytes(out, "resolution").String())
	require.False(t, gjson.GetBytes(out, "n").Exists())

	again, againInfo, err := NormalizeGrokVideoSubmission(out, "application/json", GrokMediaEndpointVideosGenerations)
	require.NoError(t, err)
	require.Equal(t, string(out), string(again))
	require.Equal(t, info, againInfo)
}

func TestNormalizeGrokVideoSubmissionOfficialDurationAliases(t *testing.T) {
	for _, tc := range []struct {
		name, fields string
		seconds      int64
	}{
		{"minimum", `"duration":1`, 1},
		{"maximum", `"duration":15`, 15},
		{"numeric_string", `"duration":"12"`, 12},
		{"seconds_number", `"seconds":10`, 10},
		{"seconds_string", `"seconds":"9"`, 9},
		{"same_aliases", `"seconds":"8","duration":8`, 8},
		{"null_duration", `"seconds":7,"duration":null`, 7},
		{"null_aliases", `"seconds":null,"duration":null`, 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, info, err := NormalizeGrokVideoSubmission([]byte(`{"model":"video",`+tc.fields+`}`), "", GrokMediaEndpointVideosGenerations)
			require.NoError(t, err)
			require.Equal(t, gjson.Number, gjson.GetBytes(out, "duration").Type)
			require.Equal(t, tc.seconds, gjson.GetBytes(out, "duration").Int())
			require.Equal(t, int(tc.seconds), info.DurationSeconds)
			require.Equal(t, float64(tc.seconds), info.DurationSecondsExact)
			require.False(t, gjson.GetBytes(out, "seconds").Exists())
		})
	}
}

func TestNormalizeGrokVideoSubmissionPreservesRelayResolutionTiers(t *testing.T) {
	for _, tc := range []struct{ input, canonical string }{
		{"480P", "480p"}, {"sd", "480p"}, {"720", "720p"}, {"HD", "720p"},
		{"768P", "768p"}, {"1080P", "1080p"}, {"FHD", "1080p"},
		{"full_hd", "1080p"}, {"2K", "2k"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			// Even official public names can map to relay models supporting 768p/2K.
			out, info, err := NormalizeGrokVideoSubmission([]byte(`{"model":"grok-imagine-video","resolution":"`+tc.input+`"}`), "application/json", GrokMediaEndpointVideosGenerations)
			require.NoError(t, err)
			require.Equal(t, tc.canonical, info.Resolution)
			require.Equal(t, info.Resolution, gjson.GetBytes(out, "resolution").String())
		})
	}
}

func TestNormalizeGrokVideoSubmissionPreservesNativeInputs(t *testing.T) {
	body := []byte(`{
		"model":"grok-imagine-video-1.5",
		"image":{"file_id":"file-first"},
		"last_frame":{"url":"https://media.example/last.png"},
		"keyframes":[{"image":{"file_id":"file-middle"},"timestamp_s":4}],
		"reference_images":[{"file_id":"file-ref"}],
		"reference_audios":[{"voice_id":"ara"},{"url":"https://media.example/audio.wav"}],
		"aspect_ratio":"16:9",
		"output":{"upload_url":"https://media.example/upload"},
		"storage_options":{"filename":"clip.mp4","expires_after":3600,"public_url":true},
		"user":"user-1",
		"future_options":{"seed":42,"opaque":["x",true]}
	}`)
	out, _, err := NormalizeGrokVideoSubmission(body, "application/json", GrokMediaEndpointVideosGenerations)
	require.NoError(t, err, "image-only generation does not require a prompt")
	for _, field := range []string{"model", "image", "last_frame", "keyframes", "reference_images", "reference_audios", "aspect_ratio", "output", "storage_options", "user", "future_options"} {
		require.JSONEq(t, gjson.GetBytes(body, field).Raw, gjson.GetBytes(out, field).Raw, field)
	}
	require.False(t, gjson.GetBytes(out, "prompt").Exists())
	for _, prompt := range []string{
		"Make a 15 seconds long scene in high resolution",
		"A narrative titled --narrative with a dramatic end",
		"在海边漫步十五秒，电影质感",
	} {
		_, _, err := NormalizeGrokVideoSubmission([]byte(`{"model":"video","prompt":"`+prompt+`"}`), "application/json", GrokMediaEndpointVideosGenerations)
		require.NoError(t, err)
	}
}

func TestNormalizeGrokVideoSubmissionRejectsAmbiguousOrUnpricedControls(t *testing.T) {
	for _, fields := range []string{
		`"duration":0`, `"duration":16`, `"duration":8.9`, `"duration":"8.5"`,
		`"duration":true`, `"duration":{}`, `"duration":1e400`,
		`"seconds":0`, `"seconds":"16"`, `"duration":8,"seconds":9`,
		`"n":2`, `"n":0`, `"n":"1"`, `"n":null`, `"n":1.9`,
		`"resolution":"4K"`, `"resolution":720`, `"resolution":null`,
		`"durationSeconds":15`, `"video_count":2`, `"fps":60`, `"frames":240`,
		`"parameters":{"duration":15}`, `"options":{"resolution":"1080p"}`,
		`"body":{"n":2}`, `"providerOptions":{"xai":{"duration":15}}`,
		`"extra":[{"sampleCount":2}]`, `"Duration":15`,
		`"video":{"url":"https://media.example/source.mp4"}`,
		`"providerOptions":{"xai":{"mode":"edit-video","videoUrl":"https://media.example/source.mp4"}}`,
		`"prompt":"waves --duration 15"`, `"prompt":"--dur=15 waves"`,
		`"prompt":"waves --n 3"`, `"prompt":"waves --number-of-videos 3"`,
		`"prompt":"waves --resolution:1080p"`,
		`"options":{"prompt":"waves --duration 15"}`, `"prompt":123`,
	} {
		t.Run(fields, func(t *testing.T) {
			_, _, err := NormalizeGrokVideoSubmission([]byte(`{"model":"video",`+fields+`}`), "application/json", GrokMediaEndpointVideosGenerations)
			require.Error(t, err)
		})
	}
	for _, body := range []string{
		`{"model":"video","duration":1,"duration":15}`,
		`{"model":"video","options":{"seed":1,"seed":2}}`,
		`{"model":"video"} {"duration":15}`, `[]`, `null`, `{`,
		`{}`, `{"model":1}`, `{"model":" "}`,
	} {
		_, _, err := NormalizeGrokVideoSubmission([]byte(body), "application/json", GrokMediaEndpointVideosGenerations)
		require.Error(t, err, body)
	}
	_, _, err := NormalizeGrokVideoSubmission([]byte(`{"model":"video"}`), "multipart/form-data; boundary=video", GrokMediaEndpointVideosGenerations)
	require.Error(t, err)
	_, _, err = NormalizeGrokVideoSubmission([]byte(`{"model":"video"}`), "application/json", GrokMediaEndpointImagesGenerations)
	require.Error(t, err)
}

func TestNormalizeGrokVideoSubmissionPreservesEditAndExtensionBehavior(t *testing.T) {
	for _, endpoint := range []GrokMediaEndpoint{GrokMediaEndpointVideosEdits, GrokMediaEndpointVideosExtensions} {
		body := []byte(`{"model":"video-alias","video":{"url":"https://media.example/input.mp4"},"prompt":"animate","duration":"6","resolution":"720P","providerOptions":{"xai":{"mode":"edit-video"}}}`)
		out, info, err := NormalizeGrokVideoSubmission(body, "application/json", endpoint)
		require.NoError(t, err)
		require.Equal(t, string(body), string(out))
		require.Equal(t, ParseGrokMediaRequest("application/json", body), info)
	}
}

func TestForwardGrokMediaCanonicalizesVideoSubmissionBeforeUpstream(t *testing.T) {
	for _, resolution := range []string{"768P", "2K"} {
		t.Run(resolution, func(t *testing.T) {
			upstream := &grokMediaContentUpstreamStub{response: grokMediaContentStatusResponse(`{"request_id":"task-1"}`)}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
			account := grokMediaContentTestAccount()
			account.Credentials["model_mapping"] = map[string]any{"grok-imagine-video": "relay-video"}
			c, _ := grokMediaContentTestContext(http.MethodPost, "https://api.example/v1/videos/generations", nil)
			result, err := svc.ForwardGrokMedia(context.Background(), c, account, GrokMediaEndpointVideosGenerations, "", []byte(`{"model":"grok-imagine-video","prompt":"waves","seconds":"12","resolution":"`+resolution+`","n":1,"image":{"file_id":"file-first"}}`), "application/json")
			require.NoError(t, err)
			require.Len(t, upstream.requests, 1)
			forwarded, err := io.ReadAll(upstream.request.Body)
			require.NoError(t, err)
			require.Equal(t, "relay-video", gjson.GetBytes(forwarded, "model").String())
			require.Equal(t, gjson.Number, gjson.GetBytes(forwarded, "duration").Type)
			require.Equal(t, int64(12), gjson.GetBytes(forwarded, "duration").Int())
			require.Equal(t, strings.ToLower(resolution), gjson.GetBytes(forwarded, "resolution").String())
			require.Equal(t, "file-first", gjson.GetBytes(forwarded, "image.file_id").String())
			require.False(t, gjson.GetBytes(forwarded, "seconds").Exists())
			require.False(t, gjson.GetBytes(forwarded, "n").Exists())
			require.Equal(t, 12, result.VideoDurationSeconds)
			require.Equal(t, strings.ToLower(resolution), result.VideoResolution)
			require.Zero(t, result.VideoCount, "accepted asynchronous tasks settle on completion")
		})
	}
}

func TestForwardGrokMediaRejectsUnpricedVideoControlsWithoutUpstreamCall(t *testing.T) {
	for _, fields := range []string{`"duration":16`, `"n":2`, `"providerOptions":{"xai":{"duration":15}}`, `"prompt":"waves --duration 15"`} {
		upstream := &grokMediaContentUpstreamStub{}
		svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
		c, _ := grokMediaContentTestContext(http.MethodPost, "https://api.example/v1/videos/generations", nil)
		result, err := svc.ForwardGrokMedia(context.Background(), c, grokMediaContentTestAccount(), GrokMediaEndpointVideosGenerations, "", []byte(`{"model":"video",`+fields+`}`), "application/json")
		require.Error(t, err)
		require.Nil(t, result)
		require.Empty(t, upstream.requests)
	}
}

func TestForwardGrokMediaPreservesEditAndExtensionRequests(t *testing.T) {
	for _, endpoint := range []GrokMediaEndpoint{GrokMediaEndpointVideosEdits, GrokMediaEndpointVideosExtensions} {
		upstream := &grokMediaContentUpstreamStub{response: grokMediaContentStatusResponse(`{"request_id":"task-1"}`)}
		svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
		c, _ := grokMediaContentTestContext(http.MethodPost, "https://api.example/v1/videos/edits", nil)
		body := []byte(`{"model":"grok-imagine-video","prompt":"animate","video":{"url":"https://media.example/input.mp4"},"duration":6}`)
		_, err := svc.ForwardGrokMedia(context.Background(), c, grokMediaContentTestAccount(), endpoint, "", body, "application/json")
		require.NoError(t, err)
		require.Len(t, upstream.requests, 1)
		forwarded, err := io.ReadAll(upstream.request.Body)
		require.NoError(t, err)
		require.JSONEq(t, string(body), string(forwarded))
	}
}
