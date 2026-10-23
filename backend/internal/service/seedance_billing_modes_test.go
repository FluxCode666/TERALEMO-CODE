//go:build unit

package service

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func TestSeedanceDraftModeRequiresKnownModelAndExplicit480P(t *testing.T) {
	for _, tc := range []struct {
		name, model, resolution string
		draft                   any
		invalid                 bool
	}{
		{"supported ordinary-price draft", "doubao-seedance-2-5-pro", "480p", true, false},
		{"draft does not silently override gateway default", "doubao-seedance-2-5-pro", "", true, true},
		{"draft cannot override 720P contract", "doubao-seedance-2-5-pro", "720p", true, true},
		{"draft cannot override 1080P contract", "doubao-seedance-2-5-pro", "1080p", true, true},
		{"wrong known family", "doubao-seedance-2-0", "480p", true, true},
		{"unknown deployment cannot infer draft mode", "ep-unknown", "480p", true, true},
		{"string is not boolean", "doubao-seedance-2-5-pro", "480p", "true", true},
		{"ordinary mode unchanged", "doubao-seedance-2-5-pro", "720p", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := seedanceResolutionTestBody(t, "waves")
			body, err := sjson.SetBytes(body, "draft", tc.draft)
			require.NoError(t, err)
			if tc.resolution != "" {
				body, err = sjson.SetBytes(body, "resolution", tc.resolution)
				require.NoError(t, err)
			}
			a := seedanceTestAccount()
			a.Credentials["model_mapping"] = map[string]any{"video": tc.model}
			upstream := &grokMediaContentUpstreamStub{response: grokMediaContentStatusResponse(`{"id":"task-1"}`)}
			svc := &OpenAIGatewayService{httpUpstream: upstream}
			c, _ := grokMediaContentTestContext(http.MethodPost, "/api/v3/contents/generations/tasks", nil)
			_, err = svc.ForwardSeedance(context.Background(), c, a, SeedanceEndpointCreate, "", body)
			if tc.invalid {
				require.Error(t, err)
				require.Empty(t, upstream.requests)
				return
			}
			require.NoError(t, err)
			require.Len(t, upstream.requests, 1)
		})
	}
	// The documented weak form is normalized to a strong 480p field too.
	body := seedanceResolutionTestBody(t, "waves --rs 480p")
	body, err := sjson.SetBytes(body, "draft", true)
	require.NoError(t, err)
	prepared, info, err := NormalizeSeedanceSubmission(body, "doubao-seedance-2-5-pro")
	require.NoError(t, err)
	require.Equal(t, VideoBillingResolution480P, info.Resolution)
	require.Equal(t, "480p", gjson.GetBytes(prepared, "resolution").String())
	require.True(t, gjson.GetBytes(prepared, "draft").Bool())
}

func TestSeedanceInheritedDraftTaskCannotBypassSubmissionContract(t *testing.T) {
	for _, body := range []string{
		`{"model":"video","content":[{"type":"draft_task","draft_task":{"id":"foreign-task"}}],"resolution":"1080p"}`,
		`{"model":"video","content":[{"type":"text","text":"waves"},{"draft_task":{"id":"foreign-task"}}],"duration":5,"resolution":"1080p"}`,
		`{"model":"video","content":[{"type":"text","text":"waves"}],"draft_task_id":"foreign-task"}`,
		`{"model":"video","content":[{"type":"text","text":"waves"}],"draftTaskId":"foreign-task"}`,
	} {
		_, _, err := NormalizeSeedanceSubmission([]byte(body), "doubao-seedance-2-5-pro")
		require.ErrorIs(t, err, ErrSeedanceDraftTaskUnavailable)
		upstream := &grokMediaContentUpstreamStub{}
		svc := &OpenAIGatewayService{httpUpstream: upstream}
		c, _ := grokMediaContentTestContext(http.MethodPost, "/api/v3/contents/generations/tasks", nil)
		_, err = svc.ForwardSeedance(context.Background(), c, seedanceTestAccount(), SeedanceEndpointCreate, "", []byte(body))
		require.ErrorIs(t, err, ErrSeedanceDraftTaskUnavailable)
		require.Empty(t, upstream.requests)
	}
}

func TestSeedanceReferenceEditAndExtendRetainExplicitTierAndSmartUpperBound(t *testing.T) {
	for _, tc := range []struct {
		mode     string
		duration int
		upper    int
	}{
		{"reference", 8, 8}, {"edit", -1, 30}, {"extend", 10, 10}, {"extend", -1, 30},
	} {
		t.Run(fmt.Sprintf("%s/%d", tc.mode, tc.duration), func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"model":"video","content":[{"type":"text","text":"waves"},{"type":"video_url","video_url":{"url":"https://media.example/input.mp4"},"role":"reference_video"},{"type":"audio_url","audio_url":{"url":"https://media.example/audio.mp3"},"role":"reference_audio"}],"duration":%d,"resolution":"1080p","ratio":"adaptive","omni_reference_task_type":"%s"}`, tc.duration, tc.mode))
			prepared, info, err := NormalizeSeedanceSubmission(body, "doubao-seedance-2-5-pro")
			require.NoError(t, err)
			require.Equal(t, tc.duration, info.DurationSeconds)
			require.Equal(t, tc.upper, SeedanceReservationDuration(info, "doubao-seedance-2-5-pro"))
			require.Equal(t, VideoBillingResolution1080P, info.Resolution)
			require.Equal(t, 1, info.N)
			for _, field := range []string{"duration", "resolution", "content", "ratio", "omni_reference_task_type"} {
				require.Equal(t, gjson.GetBytes(body, field).Raw, gjson.GetBytes(prepared, field).Raw)
			}
		})
	}
}

func TestSeedanceFramesTakePrecedenceOverBodyDuration(t *testing.T) {
	body := []byte(`{"model":"video","content":[{"type":"text","text":"waves"}],"duration":30,"frames":49}`)
	prepared, info, err := NormalizeSeedanceSubmission(body, "doubao-seedance-1-0-pro")
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(prepared, "duration").Exists())
	require.Equal(t, 49, int(gjson.GetBytes(prepared, "frames").Int()))
	require.Equal(t, 49.0/24, info.DurationSecondsExact)
	require.Equal(t, 3, info.DurationSeconds)
	require.Equal(t, "720p", gjson.GetBytes(prepared, "resolution").String())
}
