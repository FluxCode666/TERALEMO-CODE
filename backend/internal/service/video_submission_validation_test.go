//go:build unit

package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVideoSubmissionJSONRejectsDuplicateKeysAndAmbiguousDocuments(t *testing.T) {
	for _, body := range []string{
		`{"duration":5,"duration":30}`,
		`{"duration":5,"\u0064uration":30}`,
		`{"parameters":{"sampleCount":1,"sampleCount":8}}`,
		`{"content":[{"text":"first","text":"last"}]}`,
		`{"future":{"keep":true,"keep":false}}`,
		`{"duration":5} {"duration":30}`,
		`{"duration":5} null`,
		`{"duration":5} trailing`,
		`{"content":[}`, `{"content":`, `{"duration":5,}`,
		`[]`, `"prompt"`, `null`, `5`, ``,
	} {
		require.Error(t, ValidateVideoSubmissionJSON([]byte(body)), body)
	}
	for _, body := range []string{
		`{}`,
		`{"duration":5,"content":[{"type":"text","text":"natural language n 2 duration 30"}]}`,
		`{"content":[{"text":"one"},{"text":"two"}],"future":{"text":"three"}}`,
		" \n {\"future\":[true,false,null,3.5,{\"x\":\"y\"}]} \t ",
	} {
		require.NoError(t, ValidateVideoSubmissionJSON([]byte(body)), body)
	}
	deep := `{"future":` + strings.Repeat(`[`, 130) + `0` + strings.Repeat(`]`, 130) + `}`
	require.ErrorContains(t, ValidateVideoSubmissionJSON([]byte(deep)), "depth")
}

func TestRejectUnsupportedVideoBillingFieldsLimitsItsInspection(t *testing.T) {
	for _, protocol := range []string{VideoProtocolSeedance, VideoProtocolMiniMax} {
		for _, body := range []string{
			`{"n":2}`, `{"num":2}`, `{"sampleCount":2}`, `{"sample_count":2}`,
			`{"num_videos":2}`, `{"number_of_videos":2}`, `{"video_count":2}`,
			`{"durationSeconds":15}`, `{"duration_seconds":15}`, `{"fps":60}`, `{"num_frames":600}`,
			`{"video_resolution":"1080p"}`, `{"size":"1920x1080"}`, `{"height":1080}`, `{"width":1920}`,
			`{"parameters":{"duration":15}}`, `{"parameters":{"resolution":"1080p"}}`,
			`{"generationConfig":{"sampleCount":8}}`, `{"generation_config":{"durationSeconds":15}}`,
			`{"video_config":{"frames":600}}`, `{"extra":{"sample_count":8}}`,
			`{"extra_body":{"parameters":{"resolution":"1080p"}}}`,
			`{"options":[{"duration":15}]}`,
		} {
			require.Error(t, RejectUnsupportedVideoBillingFields([]byte(body), protocol), protocol+" "+body)
		}
		for _, body := range []string{
			`{"duration":6,"resolution":"720p"}`,
			`{"prompt":"describe n 2 sampleCount 8 duration 30 resolution 4k"}`,
			`{"content":[{"type":"image_url","image_url":{"url":"https://image.example","width":1920,"height":1080}}]}`,
			`{"content":[{"type":"text","text":"waves","metadata":{"n":2,"duration":20}}]}`,
			`{"future":{"n":2,"duration":20},"extra":{"prompt_expansion_mode":"balanced"}}`,
		} {
			require.NoError(t, RejectUnsupportedVideoBillingFields([]byte(body), protocol), protocol+" "+body)
		}
	}
	require.NoError(t, RejectUnsupportedVideoBillingFields([]byte(`{"frames":49}`), VideoProtocolSeedance))
	require.Error(t, RejectUnsupportedVideoBillingFields([]byte(`{"frames":49}`), VideoProtocolMiniMax))
}
