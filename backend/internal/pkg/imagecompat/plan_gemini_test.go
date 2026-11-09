//go:build unit

package imagecompat

import (
	"bytes"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

var (
	pngBytes  = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDRfakepng")
	jpegBytes = []byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00fakejpeg")
	gifBytes  = []byte("GIF89a\x01\x00\x01\x00fakegif")
)

func TestBuildPlanGeminiGenerateContentSizeMapping(t *testing.T) {
	cases := []struct {
		name          string
		model         string
		size          string
		wantAspect    string // "" → absent
		wantImageSize string // "" → absent
		wantTier      string // Plan.SizeTier
		wantErrParam  string // non-empty → expect 400 on this param
	}{
		{"auto no image config", "gemini-2.5-flash-image", "auto", "", "", "", ""},
		{"empty no image config", "gemini-2.5-flash-image", "", "", "", "", ""},
		{"auto on 3 pro sends nothing", "gemini-3-pro-image-preview", "AUTO", "", "", "", ""},
		{"square 1k on 2.5", "gemini-2.5-flash-image", "1024x1024", "1:1", "", "1K", ""},
		{"small square on 2.5", "gemini-2.5-flash-image", "512x512", "1:1", "", "1K", ""},
		{"16:9 1k on 2.5", "gemini-2.5-flash-image", "1024x576", "16:9", "", "1K", ""},
		{"3:2 1k on 2.5", "gemini-2.5-flash-image", "960x640", "3:2", "", "1K", ""},
		{"4:5 on 2.5", "gemini-2.5-flash-image", "816x1020", "4:5", "", "1K", ""},
		{"21:9 1k", "gemini-2.5-flash-image", "1008x432", "21:9", "", "1K", ""},
		{"ratio only on 2.5 defaults to 1K", "gemini-2.5-flash-image", "9:16", "9:16", "", "1K", ""},
		{"explicit 1K tier on 2.5", "gemini-2.5-flash-image", "1K", "", "", "1K", ""},
		// WxH above the model's highest tier: downgraded, ratio kept.
		{"2.5 1536x1024 downgraded 2K→1K", "gemini-2.5-flash-image", "1536x1024", "3:2", "", "1K", ""},
		{"2.5 2048x2048 downgraded 2K→1K", "gemini-2.5-flash-image", "2048x2048", "1:1", "", "1K", ""},
		{"2.5 2100x900 downgraded 4K→1K", "gemini-2.5-flash-image", "2100x900", "21:9", "", "1K", ""},
		{"2.5 3840x2160 downgraded 4K→1K", "gemini-2.5-flash-image", "3840x2160", "16:9", "", "1K", ""},
		{"2.5 preview alias downgraded", "gemini-2.5-flash-image-preview", "1024x1536", "2:3", "", "1K", ""},
		// Explicit tier strings the model cannot produce are still rejected.
		{"2K tier string rejected on 2.5", "gemini-2.5-flash-image", "2K", "", "", "", "size"},
		{"4k tier string rejected on 2.5 preview", "gemini-2.5-flash-image-preview", "4k", "", "", "", "size"},
		// Ratios must still match exactly, even when the tier is downgraded.
		{"1792x1024 is 7:4 not supported", "gemini-2.5-flash-image", "1792x1024", "", "", "", "size"},
		{"1:4 not supported on 2.5", "gemini-2.5-flash-image", "256x1024", "", "", "", "size"},
		{"2.5 inexact ratio at 4K still rejected", "gemini-2.5-flash-image", "4000x2001", "", "", "", "size"},
		{"3 pro 2K", "gemini-3-pro-image-preview", "2048x2048", "1:1", "2K", "2K", ""},
		{"3 pro 1536x1024 → 2K", "gemini-3-pro-image-preview", "1536x1024", "3:2", "2K", "2K", ""},
		{"3 pro 4K", "gemini-3-pro-image-preview", "3840x2160", "16:9", "4K", "4K", ""},
		{"3 pro 1K explicit", "gemini-3-pro-image-preview", "1024x1024", "1:1", "1K", "1K", ""},
		{"3 pro tier only", "gemini-3-pro-image-preview", "4K", "", "4K", "4K", ""},
		{"3 pro ratio only sends explicit 1K", "gemini-3-pro-image-preview", "16:9", "16:9", "1K", "1K", ""},
		{"3 pro 1:8 rejected", "gemini-3-pro-image-preview", "1:8", "", "", "", "size"},
		{"3.1 flash 1:8 supported", "gemini-3.1-flash-image-preview", "512x4096", "1:8", "4K", "4K", ""},
		{"3.1 flash 4:1 ratio only", "gemini-3.1-flash-image-preview", "4:1", "4:1", "1K", "1K", ""},
		{"inexact ratio rejected not rounded", "gemini-3-pro-image-preview", "1000x1001", "", "", "", "size"},
		{"uppercase X", "gemini-3-pro-image-preview", "1024X768", "4:3", "1K", "1K", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := BuildPlan(ProtocolGemini, Request{
				Operation: OperationGenerations, Model: tc.model, Prompt: "a lighthouse", Size: tc.size,
			})
			if tc.wantErrParam != "" {
				require.Nil(t, plan)
				apiErr := requireAPIError(t, err, 400, ErrorTypeInvalidRequest)
				require.Equal(t, tc.wantErrParam, apiErr.Param)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantTier, plan.SizeTier)
			require.Len(t, plan.Calls, 1)
			body := plan.Calls[0].Body
			aspect := gjson.GetBytes(body, "generationConfig.imageConfig.aspectRatio")
			imageSize := gjson.GetBytes(body, "generationConfig.imageConfig.imageSize")
			if tc.wantAspect == "" {
				require.False(t, aspect.Exists(), string(body))
			} else {
				require.Equal(t, tc.wantAspect, aspect.String())
			}
			if tc.wantImageSize == "" {
				require.False(t, imageSize.Exists(), string(body))
			} else {
				require.Equal(t, tc.wantImageSize, imageSize.String())
			}
			if tc.wantAspect == "" && tc.wantImageSize == "" {
				require.False(t, gjson.GetBytes(body, "generationConfig.imageConfig").Exists(), string(body))
			}
		})
	}
}

func TestBuildPlanGeminiGenerateContentBody(t *testing.T) {
	plan, err := BuildPlan(ProtocolGemini, Request{
		Operation: OperationGenerations,
		Model:     "models/gemini-2.5-flash-image",
		Prompt:    "  a watercolor fox  ",
		N:         0,
	})
	require.NoError(t, err)
	require.Equal(t, ProtocolGemini, plan.Protocol)
	require.Equal(t, OperationGenerations, plan.Operation)
	require.Equal(t, "gemini-2.5-flash-image", plan.Model, "models/ prefix is stripped")
	require.Equal(t, 1, plan.RequestedN)
	require.Equal(t, "b64_json", plan.ResponseFormat)
	require.Empty(t, plan.SizeTier, "no size → upstream decides")
	require.Len(t, plan.Calls, 1)

	call := plan.Calls[0]
	require.Equal(t, "POST", call.Method)
	require.Equal(t, "/v1beta/models/gemini-2.5-flash-image:generateContent", call.Path)
	require.Equal(t, "application/json", call.ContentType)
	require.Equal(t, 1, call.ExpectImages)

	body := gjson.ParseBytes(call.Body)
	require.Equal(t, "user", body.Get("contents.0.role").String())
	require.Equal(t, "a watercolor fox", body.Get("contents.0.parts.0.text").String())
	require.Equal(t, 1, len(body.Get("contents.0.parts").Array()))
	require.Equal(t, `["IMAGE"]`, body.Get("generationConfig.responseModalities").Raw)
	require.False(t, body.Get("generationConfig.candidateCount").Exists())
}

func TestBuildPlanGeminiResponseModalitiesFor20(t *testing.T) {
	plan, err := BuildPlan(ProtocolGemini, Request{
		Operation: OperationGenerations, Model: "gemini-2.0-flash-preview-image-generation", Prompt: "cat",
	})
	require.NoError(t, err)
	require.Equal(t, `["TEXT","IMAGE"]`, gjson.GetBytes(plan.Calls[0].Body, "generationConfig.responseModalities").Raw)
}

func TestBuildPlanGeminiSplitsN(t *testing.T) {
	for n := 1; n <= 4; n++ {
		plan, err := BuildPlan(ProtocolGemini, Request{
			Operation: OperationGenerations, Model: "gemini-3-pro-image-preview", Prompt: "cat", N: n, Size: "2K",
		})
		require.NoError(t, err)
		require.Equal(t, n, plan.RequestedN)
		require.Equal(t, "2K", plan.SizeTier)
		require.Len(t, plan.Calls, n)
		for _, call := range plan.Calls {
			require.Equal(t, 1, call.ExpectImages)
			require.Equal(t, plan.Calls[0].Path, call.Path)
			require.True(t, bytes.Equal(plan.Calls[0].Body, call.Body))
			require.Equal(t, "2K", gjson.GetBytes(call.Body, "generationConfig.imageConfig.imageSize").String())
		}
	}

	_, err := BuildPlan(ProtocolGemini, Request{
		Operation: OperationGenerations, Model: "gemini-3-pro-image-preview", Prompt: "cat", N: 5,
	})
	apiErr := requireAPIError(t, err, 400, ErrorTypeInvalidRequest)
	require.Equal(t, "n", apiErr.Param)
	require.Contains(t, apiErr.Message, "between 1 and 4")
}

func TestBuildPlanGeminiModelPathSafety(t *testing.T) {
	bad := []string{
		"../../v1/files",
		"gemini-2.5-flash-image/../../x",
		"gemini:generateContent?key=x",
		"gemini%2F..",
		"gemini image",
		"gemini#frag",
		"models/../secret",
		"-leading-dash",
		".hidden",
		"gemini\n",
		"模型",
	}
	for _, model := range bad {
		t.Run(model, func(t *testing.T) {
			plan, err := BuildPlan(ProtocolGemini, Request{Operation: OperationGenerations, Model: model, Prompt: "cat"})
			if model == "gemini\n" {
				// surrounding whitespace is trimmed, so this one is valid
				require.NoError(t, err)
				require.Equal(t, "gemini", plan.Model)
				return
			}
			require.Nil(t, plan)
			apiErr := requireAPIError(t, err, 400, ErrorTypeInvalidRequest)
			require.Equal(t, "model", apiErr.Param)
		})
	}

	good := []string{"gemini-2.5-flash-image", "gemini-3-pro-image-preview", "imagen-4.0-ultra-generate-001", "tunedModels_abc.1"}
	for _, model := range good {
		_, err := BuildPlan(ProtocolGemini, Request{Operation: OperationGenerations, Model: model, Prompt: "cat"})
		require.NoError(t, err, model)
	}
}

func TestBuildPlanGeminiEdits(t *testing.T) {
	plan, err := BuildPlan(ProtocolGemini, Request{
		Operation: OperationEdits,
		Model:     "gemini-2.5-flash-image",
		Prompt:    "put a hat on the cat",
		N:         2,
		Size:      "1:1",
		Images: []InputImage{
			{Data: pngBytes, MIMEType: "image/png"},
			{Data: jpegBytes},                                      // sniffed
			{Data: jpegBytes, MIMEType: "image/jpg; q=1"},          // normalized
			{Data: pngBytes, MIMEType: "application/octet-stream"}, // sniffed
			{Data: []byte("heic-bytes"), MIMEType: "image/heic", URL: "https://example.com/ignored.heic"},
		},
	})
	require.NoError(t, err)
	require.Len(t, plan.Calls, 2)
	require.Equal(t, OperationEdits, plan.Operation)
	body := gjson.ParseBytes(plan.Calls[0].Body)
	parts := body.Get("contents.0.parts").Array()
	require.Len(t, parts, 6)
	require.Equal(t, "put a hat on the cat", parts[0].Get("text").String())
	wantMIMEs := []string{"image/png", "image/jpeg", "image/jpeg", "image/png", "image/heic"}
	wantData := [][]byte{pngBytes, jpegBytes, jpegBytes, pngBytes, []byte("heic-bytes")}
	for i, part := range parts[1:] {
		require.Equal(t, wantMIMEs[i], part.Get("inlineData.mimeType").String())
		require.Equal(t, base64.StdEncoding.EncodeToString(wantData[i]), part.Get("inlineData.data").String())
	}
	require.Equal(t, "1:1", body.Get("generationConfig.imageConfig.aspectRatio").String())
}

func TestBuildPlanGeminiEditsErrors(t *testing.T) {
	base := func(images ...InputImage) Request {
		return Request{Operation: OperationEdits, Model: "gemini-3-pro-image-preview", Prompt: "edit", Images: images}
	}

	t.Run("url only image is a caller contract violation", func(t *testing.T) {
		_, err := BuildPlan(ProtocolGemini, base(InputImage{URL: "https://example.com/a.png"}))
		require.Error(t, err)
		var apiErr *Error
		require.NotErrorAs(t, err, &apiErr)
	})
	t.Run("empty image", func(t *testing.T) {
		_, err := BuildPlan(ProtocolGemini, base(InputImage{}))
		apiErr := requireAPIError(t, err, 400, ErrorTypeInvalidRequest)
		require.Equal(t, "image", apiErr.Param)
	})
	t.Run("gif rejected", func(t *testing.T) {
		_, err := BuildPlan(ProtocolGemini, base(InputImage{Data: gifBytes}))
		apiErr := requireAPIError(t, err, 400, ErrorTypeInvalidRequest)
		require.Equal(t, "image", apiErr.Param)
		require.Contains(t, apiErr.Message, "image/gif")
	})
	t.Run("non image bytes rejected", func(t *testing.T) {
		_, err := BuildPlan(ProtocolGemini, base(InputImage{Data: []byte("hello world"), MIMEType: "text/plain"}))
		apiErr := requireAPIError(t, err, 400, ErrorTypeInvalidRequest)
		require.Equal(t, "image", apiErr.Param)
	})
	t.Run("too many reference images", func(t *testing.T) {
		images := make([]InputImage, 15)
		for i := range images {
			images[i] = InputImage{Data: pngBytes}
		}
		_, err := BuildPlan(ProtocolGemini, base(images...))
		apiErr := requireAPIError(t, err, 400, ErrorTypeInvalidRequest)
		require.Equal(t, "image", apiErr.Param)
		require.Contains(t, apiErr.Message, "14")
	})
	t.Run("14 reference images accepted", func(t *testing.T) {
		images := make([]InputImage, 14)
		for i := range images {
			images[i] = InputImage{Data: pngBytes}
		}
		_, err := BuildPlan(ProtocolGemini, base(images...))
		require.NoError(t, err)
	})
	t.Run("mask rejected on edits", func(t *testing.T) {
		req := base(InputImage{Data: pngBytes})
		req.HasMask = true
		_, err := BuildPlan(ProtocolGemini, req)
		apiErr := requireAPIError(t, err, 400, ErrorTypeInvalidRequest)
		require.Equal(t, "mask", apiErr.Param)
	})
}

func TestBuildPlanImagen(t *testing.T) {
	cases := []struct {
		name           string
		model          string
		n              int
		size           string
		wantCount      int
		wantAspect     string
		wantSampleSize string
		wantTier       string
		wantErrParam   string
	}{
		{"defaults", "imagen-4.0-generate-001", 0, "", 1, "", "", "", ""},
		{"auto", "imagen-4.0-generate-001", 1, "auto", 1, "", "", "", ""},
		{"n=4 16:9", "imagen-4.0-generate-001", 4, "1024x576", 4, "16:9", "1K", "1K", ""},
		{"2K on standard", "imagen-4.0-generate-001", 2, "2048x2048", 2, "1:1", "2K", "2K", ""},
		{"2K on ultra", "imagen-4.0-ultra-generate-001", 1, "2K", 1, "", "2K", "2K", ""},
		{"ratio only sends explicit 1K", "imagen-4.0-generate-001", 1, "9:16", 1, "9:16", "1K", "1K", ""},
		// WxH above the model's highest tier: downgraded, ratio kept.
		{"standard 3840x2160 downgraded 4K→2K", "imagen-4.0-generate-001", 1, "3840x2160", 1, "16:9", "2K", "2K", ""},
		{"ultra 4096x4096 downgraded 4K→2K", "imagen-4.0-ultra-generate-001", 1, "4096x4096", 1, "1:1", "2K", "2K", ""},
		{"fast 2048x1536 downgraded 2K→1K", "imagen-4.0-fast-generate-001", 1, "2048x1536", 1, "4:3", "", "1K", ""},
		{"fast 3840x2160 downgraded 4K→1K", "imagen-4.0-fast-generate-001", 1, "3840x2160", 1, "16:9", "", "1K", ""},
		{"imagen 3 2048x2048 downgraded 2K→1K", "imagen-3.0-generate-002", 1, "2048x2048", 1, "1:1", "", "1K", ""},
		{"1K tier on fast omits size", "imagen-4.0-fast-generate-001", 1, "1024x768", 1, "4:3", "", "1K", ""},
		{"ratio only on fast", "imagen-4.0-fast-generate-001", 1, "3:4", 1, "3:4", "", "1K", ""},
		// Explicit tier strings the model cannot produce are still rejected.
		{"4K tier string rejected on standard", "imagen-4.0-generate-001", 1, "4K", 0, "", "", "", "size"},
		{"2K tier string rejected on fast", "imagen-4.0-fast-generate-001", 1, "2K", 0, "", "", "", "size"},
		{"2K tier string rejected on imagen 3", "imagen-3.0-generate-002", 1, "2K", 0, "", "", "", "size"},
		{"3:2 unsupported", "imagen-4.0-generate-001", 1, "1536x1024", 0, "", "", "", "size"},
		{"3:2 unsupported even when downgraded", "imagen-4.0-generate-001", 1, "3840x2560", 0, "", "", "", "size"},
		{"21:9 unsupported", "imagen-4.0-generate-001", 1, "21:9", 0, "", "", "", "size"},
		{"n=5 rejected", "imagen-4.0-generate-001", 5, "", 0, "", "", "", "n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := BuildPlan(ProtocolGemini, Request{
				Operation: OperationGenerations, Model: tc.model, Prompt: " a koi pond ", N: tc.n, Size: tc.size,
			})
			if tc.wantErrParam != "" {
				require.Nil(t, plan)
				apiErr := requireAPIError(t, err, 400, ErrorTypeInvalidRequest)
				require.Equal(t, tc.wantErrParam, apiErr.Param)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantTier, plan.SizeTier)
			require.Len(t, plan.Calls, 1, "imagen uses sampleCount, not split calls")
			call := plan.Calls[0]
			require.Equal(t, "/v1beta/models/"+tc.model+":predict", call.Path)
			require.Equal(t, tc.wantCount, call.ExpectImages)
			require.Equal(t, tc.wantCount, plan.RequestedN)
			body := gjson.ParseBytes(call.Body)
			require.Equal(t, "a koi pond", body.Get("instances.0.prompt").String())
			require.Equal(t, int64(tc.wantCount), body.Get("parameters.sampleCount").Int())
			if tc.wantAspect == "" {
				require.False(t, body.Get("parameters.aspectRatio").Exists())
			} else {
				require.Equal(t, tc.wantAspect, body.Get("parameters.aspectRatio").String())
			}
			if tc.wantSampleSize == "" {
				require.False(t, body.Get("parameters.sampleImageSize").Exists())
			} else {
				require.Equal(t, tc.wantSampleSize, body.Get("parameters.sampleImageSize").String())
			}
		})
	}
}

func TestBuildPlanImagenRejectsEdits(t *testing.T) {
	for _, images := range [][]InputImage{
		{{Data: pngBytes}},
		{{URL: "https://example.com/a.png"}}, // rejected before any download requirement
	} {
		_, err := BuildPlan(ProtocolGemini, Request{
			Operation: OperationEdits, Model: "imagen-4.0-generate-001", Prompt: "edit", Images: images,
		})
		apiErr := requireAPIError(t, err, 400, ErrorTypeInvalidRequest)
		require.Equal(t, "model", apiErr.Param)
		require.Contains(t, apiErr.Message, "do not support image edits")
	}
}

func TestBuildPlanMessagesDoNotLeakMappedModel(t *testing.T) {
	cases := []struct {
		p   Protocol
		req Request
	}{
		{ProtocolGemini, Request{Operation: OperationGenerations, Model: "gemini-2.5-flash-image-secretmap", Prompt: "x", Size: "2K"}},
		{ProtocolGemini, Request{Operation: OperationGenerations, Model: "imagen-4.0-fast-secretmap", Prompt: "x", Size: "2K"}},
		{ProtocolGemini, Request{Operation: OperationEdits, Model: "imagen-4.0-secretmap", Prompt: "x", Images: []InputImage{{Data: pngBytes}}}},
		{ProtocolGemini, Request{Operation: OperationGenerations, Model: "secretmap/../x", Prompt: "x"}},
		{ProtocolGrok, Request{Operation: OperationGenerations, Model: "grok-secretmap", Prompt: "x", Size: "4K"}},
	}
	for _, tc := range cases {
		_, err := BuildPlan(tc.p, tc.req)
		apiErr := requireAPIError(t, err, 400, ErrorTypeInvalidRequest)
		require.NotContains(t, apiErr.Message, "secretmap", apiErr.Message)
	}
}
