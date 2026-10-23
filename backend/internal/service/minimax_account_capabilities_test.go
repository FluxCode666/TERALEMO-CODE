//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestMiniMaxVideoAccountCapabilitiesOverrideOfficialLimits(t *testing.T) {
	for _, version := range []string{"v1", "v2"} {
		for _, resolution := range []string{"480P", "720P", "768P", "1080P", "2K"} {
			t.Run(version+"/"+resolution, func(t *testing.T) {
				model := "MiniMax-H3"
				if version == "v1" {
					model = "MiniMax-Hailuo-2.3"
				}
				a := miniMaxSubmissionAccount(version, model)
				a.Credentials["video_resolutions"] = []string{"480p", "720p", "768p", "1080p", "2k"}
				a.Credentials["video_min_duration_seconds"], a.Credentials["video_max_duration_seconds"] = 4, 30
				body := []byte(`{"model":"public-video","prompt":"waves","duration":20,"resolution":"` + resolution + `"}`)
				_, err := ParseMiniMaxVideoSchedulingRequest(body)
				require.NoError(t, err, "scheduling cannot apply a yet unknown account's limits")
				prepared, info, err := NormalizeMiniMaxSubmission(body, a, false)
				require.NoError(t, err)
				require.Equal(t, 20, info.DurationSeconds)
				require.Equal(t, 20.0, info.DurationSecondsExact)
				require.Equal(t, resolution, gjson.GetBytes(prepared, "resolution").String())
				require.EqualValues(t, 20, gjson.GetBytes(prepared, "duration").Int())
				again, againInfo, err := NormalizeMiniMaxSubmission(prepared, a, false)
				require.NoError(t, err)
				require.JSONEq(t, string(prepared), string(again))
				require.Equal(t, info, againInfo)
			})
		}
	}
}

func TestMiniMaxVideoAccountCapabilitiesDefaultsAndSingleBound(t *testing.T) {
	for _, tc := range []struct {
		name       string
		caps       map[string]any
		seconds    int
		resolution string
	}{
		{"default remains supported", map[string]any{"video_resolutions": []string{"480p", "768p"}}, 6, "768P"},
		{"lowest fallback", map[string]any{"video_resolutions": []string{"2k", "1080p"}}, 6, "1080P"},
		{"lower clamp", map[string]any{"video_min_duration_seconds": 20, "video_max_duration_seconds": 30}, 20, "768P"},
		{"upper clamp", map[string]any{"video_max_duration_seconds": 5}, 5, "768P"},
		{"single minimum", map[string]any{"video_min_duration_seconds": 7}, 7, "768P"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := miniMaxSubmissionAccount("v2", "MiniMax-H3")
			for k, v := range tc.caps {
				a.Credentials[k] = v
			}
			prepared, info, err := NormalizeMiniMaxSubmission([]byte(`{"model":"public-video","prompt":"waves"}`), a, false)
			require.NoError(t, err)
			require.Equal(t, tc.seconds, info.DurationSeconds)
			require.Equal(t, tc.resolution, gjson.GetBytes(prepared, "resolution").String())
			require.EqualValues(t, tc.seconds, gjson.GetBytes(prepared, "duration").Int())
		})
	}
}

func TestMiniMaxVideoAccountCapabilitiesKeepOfficialAndHardRules(t *testing.T) {
	for _, model := range []string{"MiniMax-H3", "MiniMax-H3-Max"} {
		a := miniMaxSubmissionAccount("v2", model)
		_, _, err := NormalizeMiniMaxSubmission([]byte(`{"model":"public-video","prompt":"waves","duration":20,"resolution":"1080P"}`), a, false)
		require.ErrorIs(t, err, ErrVideoAccountCapabilityIncompatible)
		a.Credentials["video_resolutions"] = []string{"1080p"}
		a.Credentials["video_min_duration_seconds"], a.Credentials["video_max_duration_seconds"] = 4, 30
		for _, body := range []string{
			`{"model":"public-video","prompt":"waves","duration":3,"resolution":"1080P"}`,
			`{"model":"public-video","prompt":"waves","duration":31,"resolution":"1080P"}`,
			`{"model":"public-video","prompt":"waves","duration":20,"resolution":"768P"}`,
		} {
			_, _, err = NormalizeMiniMaxSubmission([]byte(body), a, false)
			require.ErrorIs(t, err, ErrVideoAccountCapabilityIncompatible)
			require.Contains(t, err.Error(), "1080p")
		}
		for _, body := range []string{
			`{"model":"public-video","prompt":"waves","duration":20.5}`,
			`{"model":"public-video","prompt":"waves","duration":"20"}`,
			`{"model":"public-video","prompt":"waves","ratio":"2:1"}`,
			`{"model":"public-video","content":[{"type":"image_url","image_url":{"url":"https://img.example/start"}}]}`,
		} {
			_, _, err = NormalizeMiniMaxSubmission([]byte(body), a, false)
			require.Error(t, err)
		}
	}
}
