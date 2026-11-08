//go:build unit

package service

import (
	"encoding/base64"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func creationAssetURL(url string) CreationImageAsset {
	return CreationImageAsset{URL: url, ContentType: "image/png"}
}

func TestBuildCreationNativeRequestImageGenerate(t *testing.T) {
	native, err := BuildCreationNativeRequest(CreationNativeRequestInput{
		Mode: CreationModeImage, Protocol: CreationProtocolOpenAI, Model: "gpt-image-2", Prompt: "雪夜灯塔",
		Params: map[string]any{"size": "1536x1024", "quality": "high", "n": 2},
	})
	require.NoError(t, err)
	require.Equal(t, CreationPathImageGenerations, native.Path)
	require.Equal(t, CreationProtocolOpenAI, native.Protocol)
	require.Empty(t, native.RequestIDPrefix)
	require.JSONEq(t, `{"model":"gpt-image-2","prompt":"雪夜灯塔","size":"1536x1024","quality":"high","n":2}`, string(native.Body))
}

func TestBuildCreationNativeRequestImageEdit(t *testing.T) {
	source := []byte("fake-png-bytes")
	native, err := BuildCreationNativeRequest(CreationNativeRequestInput{
		Mode: CreationModeImage, Protocol: CreationProtocolOpenAI, Model: "gpt-image-2", Prompt: "把天空换成极光",
		Params: map[string]any{"size": "1024x1024", "quality": "auto", "n": 1},
		Images: map[string][]CreationImageAsset{"source": {{Data: source, ContentType: "image/png"}}},
	})
	require.NoError(t, err)
	require.Equal(t, CreationPathImageEdits, native.Path)
	expected := `{"model":"gpt-image-2","prompt":"把天空换成极光","size":"1024x1024","quality":"auto","n":1,
		"images":[{"image_url":"data:image/png;base64,` + base64.StdEncoding.EncodeToString(source) + `"}]}`
	require.JSONEq(t, expected, string(native.Body))
}

func TestBuildCreationNativeRequestGrokImage(t *testing.T) {
	native, err := BuildCreationNativeRequest(CreationNativeRequestInput{
		Mode: CreationModeImage, Protocol: CreationProtocolGrok, Model: "grok-imagine-image", Prompt: "p",
		Params: map[string]any{"aspect_ratio": "16:9", "resolution": "2k", "n": 1},
	})
	require.NoError(t, err)
	require.Equal(t, CreationPathImageGenerations, native.Path)
	require.JSONEq(t, `{"model":"grok-imagine-image","prompt":"p","aspect_ratio":"16:9","size":"2k","n":1}`, string(native.Body))
	require.False(t, gjson.GetBytes(native.Body, "resolution").Exists(), "grok billing tier travels in size, not resolution")
}

func TestBuildCreationNativeRequestGrokImageEdit(t *testing.T) {
	native, err := BuildCreationNativeRequest(CreationNativeRequestInput{
		Mode: CreationModeImage, Protocol: CreationProtocolGrok, Model: "grok-imagine-image", Prompt: "p",
		Params: map[string]any{"aspect_ratio": "1:1", "resolution": "1k", "n": 1},
		Images: map[string][]CreationImageAsset{"source": {{Data: []byte("x"), ContentType: "image/webp"}}},
	})
	require.NoError(t, err)
	require.Equal(t, CreationPathImageEdits, native.Path)
	require.Equal(t, "data:image/webp;base64,eA==", gjson.GetBytes(native.Body, "image.url").String())
	require.Equal(t, "1k", gjson.GetBytes(native.Body, "size").String())
}

func TestBuildCreationNativeRequestSeedanceFrames(t *testing.T) {
	native, err := BuildCreationNativeRequest(CreationNativeRequestInput{
		Mode: CreationModeVideo, Protocol: CreationProtocolSeedance, Model: "doubao-seedance-1-0-pro", Prompt: "夕阳下的海浪",
		Params: map[string]any{"resolution": "720p", "duration": 5, "ratio": "16:9", "watermark": "false"},
		Images: map[string][]CreationImageAsset{
			"first_frame": {creationAssetURL("https://cdn.example/first.png")},
			"last_frame":  {creationAssetURL("https://cdn.example/last.png")},
		},
	})
	require.NoError(t, err)
	require.Equal(t, CreationPathSeedance, native.Path)
	require.Equal(t, "seedance:", native.RequestIDPrefix)
	require.JSONEq(t, `{
		"model":"doubao-seedance-1-0-pro",
		"content":[
			{"type":"text","text":"夕阳下的海浪"},
			{"type":"image_url","image_url":{"url":"https://cdn.example/first.png"},"role":"first_frame"},
			{"type":"image_url","image_url":{"url":"https://cdn.example/last.png"},"role":"last_frame"}
		],
		"resolution":"720p","duration":5,"ratio":"16:9","watermark":false}`, string(native.Body))
	require.False(t, gjson.GetBytes(native.Body, "n").Exists())
	require.False(t, gjson.GetBytes(native.Body, "sampleCount").Exists())
}

func TestBuildCreationNativeRequestSeedanceReferences(t *testing.T) {
	native, err := BuildCreationNativeRequest(CreationNativeRequestInput{
		Mode: CreationModeVideo, Protocol: CreationProtocolSeedance, Model: "doubao-seedance-1-0-pro", Prompt: "p",
		Params: map[string]any{"resolution": "1080p", "duration": 8, "ratio": "9:16", "watermark": "true"},
		Images: map[string][]CreationImageAsset{"reference": {
			creationAssetURL("https://cdn.example/ref1.png"),
			creationAssetURL("https://cdn.example/ref2.png"),
		}},
	})
	require.NoError(t, err)
	require.Equal(t, "reference_image", gjson.GetBytes(native.Body, "content.1.role").String())
	require.Equal(t, "https://cdn.example/ref2.png", gjson.GetBytes(native.Body, "content.2.image_url.url").String())
	require.True(t, gjson.GetBytes(native.Body, "watermark").Bool())
}

func TestBuildCreationNativeRequestGeminiInlinesBytes(t *testing.T) {
	native, err := BuildCreationNativeRequest(CreationNativeRequestInput{
		Mode: CreationModeVideo, Protocol: CreationProtocolGemini, Model: "veo-3.1-generate-001", Prompt: "夕阳下的海浪",
		Params: map[string]any{"duration": 8, "resolution": "1080p", "aspect_ratio": "16:9"},
		Images: map[string][]CreationImageAsset{
			"first_frame": {{Data: []byte("hi"), ContentType: "image/png"}},
			"reference":   {{Data: []byte("ref"), ContentType: "image/jpeg"}},
		},
	})
	require.NoError(t, err)
	require.Equal(t, "/v1beta/models/veo-3.1-generate-001:predictLongRunning", native.Path)
	require.Equal(t, "gemini:", native.RequestIDPrefix)
	require.Equal(t, "aGk=", gjson.GetBytes(native.Body, "instances.0.image.bytesBase64Encoded").String())
	require.Equal(t, "image/png", gjson.GetBytes(native.Body, "instances.0.image.mimeType").String())
	require.Equal(t, "asset", gjson.GetBytes(native.Body, "instances.0.referenceImages.0.referenceType").String())
	require.Equal(t, "cmVm", gjson.GetBytes(native.Body, "instances.0.referenceImages.0.image.bytesBase64Encoded").String())
	require.Equal(t, int64(1), gjson.GetBytes(native.Body, "parameters.sampleCount").Int())
	require.Equal(t, int64(8), gjson.GetBytes(native.Body, "parameters.durationSeconds").Int())
	require.Equal(t, "1080p", gjson.GetBytes(native.Body, "parameters.resolution").String())
	require.Equal(t, "16:9", gjson.GetBytes(native.Body, "parameters.aspectRatio").String())
}

func TestBuildCreationNativeRequestMiniMaxV1(t *testing.T) {
	native, err := BuildCreationNativeRequest(CreationNativeRequestInput{
		Mode: CreationModeVideo, Protocol: CreationProtocolMiniMax, Model: "MiniMax-Hailuo-2.3", Prompt: "夕阳下的海浪",
		Params: map[string]any{"duration": 6, "resolution": "768p"},
		Images: map[string][]CreationImageAsset{
			"first_frame": {creationAssetURL("https://cdn.example/first.jpg")},
			"last_frame":  {creationAssetURL("https://cdn.example/last.jpg")},
		},
	})
	require.NoError(t, err)
	require.Equal(t, CreationPathMiniMaxV1, native.Path)
	require.Equal(t, "minimax:", native.RequestIDPrefix)
	require.JSONEq(t, `{"model":"MiniMax-Hailuo-2.3","prompt":"夕阳下的海浪","duration":6,"resolution":"768P",
		"first_frame_image":"https://cdn.example/first.jpg","last_frame_image":"https://cdn.example/last.jpg"}`, string(native.Body))
	require.False(t, gjson.GetBytes(native.Body, "n").Exists())
}

func TestBuildCreationNativeRequestMiniMaxV2(t *testing.T) {
	native, err := BuildCreationNativeRequest(CreationNativeRequestInput{
		Mode: CreationModeVideo, Protocol: CreationProtocolMiniMax, Model: "MiniMax-H3", Prompt: "夕阳下的海浪",
		Params: map[string]any{"duration": 6, "resolution": "768p", "ratio": "adaptive"},
		Images: map[string][]CreationImageAsset{
			"first_frame": {creationAssetURL("https://cdn.example/first.png")},
		},
	})
	require.NoError(t, err)
	require.Equal(t, CreationPathMiniMaxV2, native.Path)
	require.JSONEq(t, `{"model":"MiniMax-H3","content":[
		{"type":"text","text":"夕阳下的海浪"},
		{"type":"image_url","image_url":{"url":"https://cdn.example/first.png"},"role":"first_frame"}],
		"duration":6,"resolution":"768P","ratio":"adaptive"}`, string(native.Body))
	require.False(t, gjson.GetBytes(native.Body, "sampleCount").Exists())
}

func TestBuildCreationNativeRequestGrokVideo(t *testing.T) {
	native, err := BuildCreationNativeRequest(CreationNativeRequestInput{
		Mode: CreationModeVideo, Protocol: CreationProtocolGrok, Model: "grok-imagine-video", Prompt: "夕阳下的海浪",
		Params: map[string]any{"duration": 8, "resolution": "480p"},
		Images: map[string][]CreationImageAsset{
			"first_frame": {creationAssetURL("https://cdn.example/first.png")},
		},
	})
	require.NoError(t, err)
	require.Equal(t, CreationPathGrokVideo, native.Path)
	require.Empty(t, native.RequestIDPrefix)
	require.JSONEq(t, `{"model":"grok-imagine-video","prompt":"夕阳下的海浪","duration":8,"resolution":"480p",
		"image":{"url":"https://cdn.example/first.png"}}`, string(native.Body))
	require.False(t, gjson.GetBytes(native.Body, "n").Exists())
}

func TestBuildCreationNativeRequestWritesDefaults(t *testing.T) {
	openai, err := BuildCreationNativeRequest(CreationNativeRequestInput{
		Mode: CreationModeImage, Protocol: CreationProtocolOpenAI, Model: "gpt-image-2", Prompt: "p",
	})
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"gpt-image-2","prompt":"p","size":"1024x1024","quality":"auto","n":1}`, string(openai.Body))

	seedance, err := BuildCreationNativeRequest(CreationNativeRequestInput{
		Mode: CreationModeVideo, Protocol: CreationProtocolSeedance, Model: "doubao-seedance-1-0-pro", Prompt: "p",
	})
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"doubao-seedance-1-0-pro","content":[{"type":"text","text":"p"}],
		"resolution":"720p","duration":5,"ratio":"16:9","watermark":false}`, string(seedance.Body))

	gemini, err := BuildCreationNativeRequest(CreationNativeRequestInput{
		Mode: CreationModeVideo, Protocol: CreationProtocolGemini, Model: "veo-3.1-generate-001", Prompt: "p",
	})
	require.NoError(t, err)
	require.JSONEq(t, `{"instances":[{"prompt":"p"}],"parameters":{
		"durationSeconds":8,"resolution":"720p","aspectRatio":"16:9","sampleCount":1}}`, string(gemini.Body))

	minimax, err := BuildCreationNativeRequest(CreationNativeRequestInput{
		Mode: CreationModeVideo, Protocol: CreationProtocolMiniMax, Model: "MiniMax-Hailuo-2.3", Prompt: "p",
	})
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"MiniMax-Hailuo-2.3","prompt":"p","duration":6,"resolution":"768P"}`, string(minimax.Body))

	grok, err := BuildCreationNativeRequest(CreationNativeRequestInput{
		Mode: CreationModeVideo, Protocol: CreationProtocolGrok, Model: "grok-imagine-video", Prompt: "p",
	})
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"grok-imagine-video","prompt":"p","duration":8,"resolution":"480p"}`, string(grok.Body))
}

func TestBuildCreationNativeRequestRejectsUnsafePaths(t *testing.T) {
	require.True(t, creationStudioAllowedPath(CreationPathImageEdits))
	require.True(t, creationStudioAllowedPath("/v1beta/models/veo-3.1-generate-001:predictLongRunning"))
	require.False(t, creationStudioAllowedPath("/v1/chat/completions"))
	require.False(t, creationStudioAllowedPath("/v1beta/models/../secrets:predictLongRunning"))
	require.False(t, creationStudioAllowedPath("/v1beta/models/a/b:predictLongRunning"))

	_, err := BuildCreationNativeRequest(CreationNativeRequestInput{
		Mode: CreationModeVideo, Protocol: CreationProtocolGemini, Model: "veo/3.1", Prompt: "p",
	})
	require.Error(t, err)
	require.Equal(t, "INVALID_CREATION_REQUEST", infraerrors.Reason(err))
}

func TestBuildCreationNativeRequestRejectsMissingImageBytes(t *testing.T) {
	_, err := BuildCreationNativeRequest(CreationNativeRequestInput{
		Mode: CreationModeImage, Protocol: CreationProtocolOpenAI, Model: "gpt-image-2", Prompt: "p",
		Images: map[string][]CreationImageAsset{"source": {{ContentType: "image/png"}}},
	})
	require.Error(t, err)

	_, err = BuildCreationNativeRequest(CreationNativeRequestInput{
		Mode: CreationModeVideo, Protocol: CreationProtocolGemini, Model: "veo-3.1-generate-001", Prompt: "p",
		Images: map[string][]CreationImageAsset{"first_frame": {{ContentType: "image/png"}}},
	})
	require.Error(t, err)

	_, err = BuildCreationNativeRequest(CreationNativeRequestInput{
		Mode: CreationModeVideo, Protocol: CreationProtocolSeedance, Model: "doubao-seedance-1-0-pro", Prompt: "p",
		Images: map[string][]CreationImageAsset{"reference": {{ContentType: "image/png"}}},
	})
	require.Error(t, err)
}
