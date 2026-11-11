//go:build unit

package imagecompat

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestBuildPlanGrokGenerations(t *testing.T) {
	cases := []struct {
		name           string
		req            Request
		wantFormat     string
		wantAspect     string
		wantResolution string
		wantTier       string
		wantQuality    string
		wantN          int64
	}{
		{"defaults", Request{}, "b64_json", "", "", "", "", 1},
		{"url kept", Request{ResponseFormat: "url"}, "url", "", "", "", "", 1},
		{"b64 explicit", Request{ResponseFormat: "B64_JSON"}, "b64_json", "", "", "", "", 1},
		{"square 1k", Request{Size: "1024x1024"}, "b64_json", "1:1", "1k", "1K", "", 1},
		{"3:2 2k", Request{Size: "1536x1024"}, "b64_json", "3:2", "2k", "2K", "", 1},
		{"16:9 2k", Request{Size: "2048x1152"}, "b64_json", "16:9", "2k", "2K", "", 1},
		{"19.5:9 via dims", Request{Size: "1950x900"}, "b64_json", "19.5:9", "2k", "2K", "", 1},
		{"4K dims downgraded to 2k", Request{Size: "3840x2160"}, "b64_json", "16:9", "2k", "2K", "", 1},
		{"4096 square downgraded to 2k", Request{Size: "4096x4096"}, "b64_json", "1:1", "2k", "2K", "", 1},
		{"9:19.5 ratio sends explicit 1k", Request{Size: "9:19.5"}, "b64_json", "9:19.5", "1k", "1K", "", 1},
		{"20:9 ratio sends explicit 1k", Request{Size: "20:9"}, "b64_json", "20:9", "1k", "1K", "", 1},
		{"tier only", Request{Size: "2k"}, "b64_json", "", "2k", "2K", "", 1},
		{"tier only 1K", Request{Size: "1K"}, "b64_json", "", "1k", "1K", "", 1},
		{"auto size", Request{Size: "auto"}, "b64_json", "", "", "", "", 1},
		{"quality low", Request{Quality: "low"}, "b64_json", "", "", "", "low", 1},
		{"quality medium", Request{Quality: "Medium"}, "b64_json", "", "", "", "medium", 1},
		{"quality auto omitted", Request{Quality: "auto"}, "b64_json", "", "", "", "", 1},
		{"n=10", Request{N: 10}, "b64_json", "", "", "", "", 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := tc.req
			req.Operation = OperationGenerations
			req.Model = " grok-imagine-image "
			req.Prompt = " neon city "
			plan, err := BuildPlan(ProtocolGrok, req)
			require.NoError(t, err)
			require.Equal(t, ProtocolGrok, plan.Protocol)
			require.Equal(t, "grok-imagine-image", plan.Model)
			require.Equal(t, tc.wantFormat, plan.ResponseFormat)
			require.Equal(t, tc.wantTier, plan.SizeTier)
			require.Len(t, plan.Calls, 1)
			call := plan.Calls[0]
			require.Equal(t, "POST", call.Method)
			require.Equal(t, "/v1/images/generations", call.Path)
			require.Equal(t, "application/json", call.ContentType)
			require.Equal(t, int(tc.wantN), call.ExpectImages)

			body := gjson.ParseBytes(call.Body)
			require.Equal(t, "grok-imagine-image", body.Get("model").String())
			require.Equal(t, "neon city", body.Get("prompt").String())
			require.Equal(t, tc.wantN, body.Get("n").Int())
			require.Equal(t, tc.wantFormat, body.Get("response_format").String())
			requireOptionalString(t, body, "aspect_ratio", tc.wantAspect)
			requireOptionalString(t, body, "resolution", tc.wantResolution)
			requireOptionalString(t, body, "quality", tc.wantQuality)
			for _, absent := range []string{"size", "image", "images", "style", "background", "output_format", "mask", "stream"} {
				require.False(t, body.Get(absent).Exists(), "unexpected %s in %s", absent, call.Body)
			}
		})
	}
}

func TestBuildPlanGrokRejections(t *testing.T) {
	cases := []struct {
		name      string
		req       Request
		wantParam string
	}{
		{"n=11", Request{N: 11}, "n"},
		{"4K tier string", Request{Size: "4K"}, "size"},
		{"4k tier string lowercase", Request{Size: "4k"}, "size"},
		{"4K dims with unsupported ratio", Request{Size: "4200x1800"}, "size"}, // 7:3 (21:9) not in xAI table
		{"21:9 not in xAI table", Request{Size: "2100x900"}, "size"},
		{"4:5 not in xAI table", Request{Size: "4:5"}, "size"},
		{"1792x1024 inexact", Request{Size: "1792x1024"}, "size"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := tc.req
			req.Operation = OperationGenerations
			req.Model = "grok-imagine-image"
			req.Prompt = "x"
			_, err := BuildPlan(ProtocolGrok, req)
			apiErr := requireAPIError(t, err, 400, ErrorTypeInvalidRequest)
			require.Equal(t, tc.wantParam, apiErr.Param)
		})
	}
}

func TestBuildPlanGrokEdits(t *testing.T) {
	t.Run("single image uses image field", func(t *testing.T) {
		plan, err := BuildPlan(ProtocolGrok, Request{
			Operation: OperationEdits, Model: "grok-imagine-image-2.0", Prompt: "make it snowy",
			Images: []InputImage{{URL: "https://cdn.example.com/a.png?sig=1&x=2"}},
		})
		require.NoError(t, err)
		call := plan.Calls[0]
		require.Equal(t, "/v1/images/edits", call.Path)
		body := gjson.ParseBytes(call.Body)
		require.Equal(t, "image_url", body.Get("image.type").String())
		require.Equal(t, "https://cdn.example.com/a.png?sig=1&x=2", body.Get("image.url").String())
		require.False(t, body.Get("images").Exists(), "image and images are mutually exclusive")
		require.NotContains(t, string(call.Body), "\\"+"u0026", "URLs must not be HTML-escaped")
	})

	t.Run("multiple images use images field and data URLs", func(t *testing.T) {
		plan, err := BuildPlan(ProtocolGrok, Request{
			Operation: OperationEdits, Model: "grok-imagine-image-2.0", Prompt: "merge",
			Images: []InputImage{
				{Data: pngBytes, MIMEType: "image/png"},
				{Data: jpegBytes},
				{URL: "data:image/webp;base64,UklGRg=="},
				{URL: "http://example.com/b.jpg"},
				{Data: pngBytes, URL: "https://ignored.example.com/x.png"},
			},
		})
		require.NoError(t, err)
		body := gjson.ParseBytes(plan.Calls[0].Body)
		require.False(t, body.Get("image").Exists())
		images := body.Get("images").Array()
		require.Len(t, images, 5)
		require.Equal(t, "data:image/png;base64,"+base64.StdEncoding.EncodeToString(pngBytes), images[0].Get("url").String())
		require.Equal(t, "data:image/jpeg;base64,"+base64.StdEncoding.EncodeToString(jpegBytes), images[1].Get("url").String())
		require.Equal(t, "data:image/webp;base64,UklGRg==", images[2].Get("url").String())
		require.Equal(t, "http://example.com/b.jpg", images[3].Get("url").String())
		require.Equal(t, "data:image/png;base64,"+base64.StdEncoding.EncodeToString(pngBytes), images[4].Get("url").String())
		for _, img := range images {
			require.Equal(t, "image_url", img.Get("type").String())
		}
	})

	errorCases := []struct {
		name   string
		images []InputImage
	}{
		{"six images", []InputImage{{URL: "https://a/1"}, {URL: "https://a/2"}, {URL: "https://a/3"}, {URL: "https://a/4"}, {URL: "https://a/5"}, {URL: "https://a/6"}}},
		{"empty image", []InputImage{{}}},
		{"file scheme", []InputImage{{URL: "file:///etc/passwd"}}},
		{"ftp scheme", []InputImage{{URL: "ftp://example.com/a.png"}}},
		{"relative url", []InputImage{{URL: "/tmp/a.png"}}},
		{"data url without base64", []InputImage{{URL: "data:image/png,rawbytes"}}},
		{"gif bytes", []InputImage{{Data: gifBytes}}},
		{"heic not accepted by xAI", []InputImage{{Data: []byte("heic"), MIMEType: "image/heic"}}},
	}
	for _, tc := range errorCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := BuildPlan(ProtocolGrok, Request{
				Operation: OperationEdits, Model: "grok-imagine-image", Prompt: "edit", Images: tc.images,
			})
			apiErr := requireAPIError(t, err, 400, ErrorTypeInvalidRequest)
			require.Equal(t, "image", apiErr.Param)
		})
	}
}

func requireOptionalString(t *testing.T, body gjson.Result, path, want string) {
	t.Helper()
	value := body.Get(path)
	if want == "" {
		require.False(t, value.Exists(), "%s should be absent in %s", path, body.Raw)
		return
	}
	require.Equal(t, want, value.String(), body.Raw)
}
