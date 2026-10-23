//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func seedanceResolutionTestBody(t *testing.T, prompt string) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{"model": "video", "content": []map[string]string{{"type": "text", "text": prompt}}})
	require.NoError(t, err)
	return body
}

func TestSeedanceResolutionStrongBodyAndWeakFlagsAgree(t *testing.T) {
	for _, tc := range []struct {
		name, prompt, bodyResolution, want string
	}{
		{"gateway default", "waves", "", "720p"},
		{"long flag", "waves --resolution 480p", "", "480p"},
		{"short flag", "waves --rs 1080p", "", "1080p"},
		{"strong uppercase value", "waves", "1080P", "1080p"},
		{"proxy 768p compatibility", "waves", "768P", "768p"},
		{"proxy 2K compatibility", "waves", "2K", "2k"},
		{"strong matches weak", "waves --rs 1080p", "1080P", "1080p"},
		{"same weak expression repeated", "waves --rs 1080p --resolution 1080p", "", "1080p"},
		{"existing billing alias becomes explicit native value", "waves", "FHD", "1080p"},
		{"natural language has no structural meaning", "resolution 1080p duration 30 sampleCount 8", "", "720p"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := seedanceResolutionTestBody(t, tc.prompt)
			if tc.bodyResolution != "" {
				var err error
				body, err = sjson.SetBytes(body, "resolution", tc.bodyResolution)
				require.NoError(t, err)
			}
			original := bytes.Clone(body)
			prepared, info, err := NormalizeSeedanceSubmission(body, "doubao-seedance-1-0-pro-250528")
			require.NoError(t, err)
			require.Equal(t, original, body)
			require.Equal(t, tc.want, info.Resolution)
			require.Equal(t, tc.want, gjson.GetBytes(prepared, "resolution").String())
			require.Equal(t, 1, info.N)
			require.Equal(t, 5, info.DurationSeconds)
			require.Equal(t, tc.prompt, gjson.GetBytes(prepared, "content.0.text").String())
			second, secondInfo, err := NormalizeSeedanceSubmission(prepared, "doubao-seedance-1-0-pro-250528")
			require.NoError(t, err)
			require.Equal(t, prepared, second)
			require.Equal(t, info, secondInfo)
		})
	}
}

func TestSeedanceResolutionRejectsConflictsAndAmbiguousControls(t *testing.T) {
	for _, tc := range []struct {
		prompt string
		value  any
	}{
		{"waves --rs 1080p", "720p"},
		{"waves --rs 480p --resolution 1080p", nil},
		{"waves --rs", nil},
		{"waves --resolution=1080p", nil},
		{"waves --rs:1080p", nil},
		{"waves --rs 4k", nil},
		{"waves --RS 1080p", nil},
		{"waves --n 2", nil},
		{"waves --sampleCount 8", nil},
		{"waves", "4k"}, {"waves", ""}, {"waves", 1080}, {"waves", false},
	} {
		body := seedanceResolutionTestBody(t, tc.prompt)
		if tc.value != nil {
			var err error
			body, err = sjson.SetBytes(body, "resolution", tc.value)
			require.NoError(t, err)
		}
		_, err := ParseSeedanceRequest(body)
		require.Error(t, err, string(body))
		_, _, err = NormalizeSeedanceSubmission(body, "doubao-seedance-1-0-pro-250528")
		require.Error(t, err, string(body))
	}
}

func TestSeedanceResolutionIsValidatedAfterMappingAndSentExplicitly(t *testing.T) {
	for _, tc := range []struct {
		model, resolution string
		invalid           bool
	}{
		{"doubao-seedance-1-0-pro-250528", "", false},
		{"doubao-seedance-1-0-pro-fast", "1080p", false},
		{"doubao-seedance-2-0-fast", "720p", false},
		{"doubao-seedance-2-0-mini", "480p", false},
		{"doubao-seedance-2-0-fast", "1080p", true},
		{"doubao-seedance-2-0-mini", "1080p", true},
		{"doubao-seedance-2-0", "1080p", false},
	} {
		t.Run(tc.model+"/"+tc.resolution, func(t *testing.T) {
			body := seedanceResolutionTestBody(t, "waves")
			if tc.resolution != "" {
				var err error
				body, err = sjson.SetBytes(body, "resolution", tc.resolution)
				require.NoError(t, err)
			}
			a := seedanceTestAccount()
			a.Credentials["model_mapping"] = map[string]any{"video": tc.model}
			upstream := &grokMediaContentUpstreamStub{response: grokMediaContentStatusResponse(`{"id":"task-1"}`)}
			svc := &OpenAIGatewayService{httpUpstream: upstream}
			c, _ := grokMediaContentTestContext(http.MethodPost, "/api/v3/contents/generations/tasks", nil)
			result, err := svc.ForwardSeedance(context.Background(), c, a, SeedanceEndpointCreate, "", body)
			if tc.invalid {
				require.Error(t, err)
				require.Empty(t, upstream.requests)
				return
			}
			require.NoError(t, err)
			require.Len(t, upstream.requests, 1)
			payload, err := io.ReadAll(upstream.request.Body)
			require.NoError(t, err)
			want := tc.resolution
			if want == "" {
				want = "720p"
			}
			require.Equal(t, want, result.VideoResolution)
			require.Equal(t, want, gjson.GetBytes(payload, "resolution").String())
		})
	}
}
