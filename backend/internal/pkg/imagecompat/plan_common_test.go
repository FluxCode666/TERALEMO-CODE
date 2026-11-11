//go:build unit

package imagecompat

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseProtocol(t *testing.T) {
	cases := []struct {
		in      string
		want    Protocol
		wantErr bool
	}{
		{"", ProtocolOpenAI, false},
		{"   ", ProtocolOpenAI, false},
		{"openai", ProtocolOpenAI, false},
		{" OpenAI ", ProtocolOpenAI, false},
		{"gemini", ProtocolGemini, false},
		{"GEMINI", ProtocolGemini, false},
		{"grok", ProtocolGrok, false},
		{"\tGrok\n", ProtocolGrok, false},
		{"xai", "", true},
		{"imagen", "", true},
		{"claude", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseProtocol(tc.in)
			if tc.wantErr {
				require.Error(t, err)
				require.Empty(t, got)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestNeedsInlineImageData(t *testing.T) {
	require.True(t, NeedsInlineImageData(ProtocolGemini))
	require.False(t, NeedsInlineImageData(ProtocolGrok))
	require.False(t, NeedsInlineImageData(ProtocolOpenAI))
	require.False(t, NeedsInlineImageData("unknown"))
}

func TestBuildPlanCallerContractErrors(t *testing.T) {
	base := Request{Operation: OperationGenerations, Model: "gemini-2.5-flash-image", Prompt: "a cat"}

	cases := []struct {
		name string
		p    Protocol
		req  Request
	}{
		{"openai passthrough", ProtocolOpenAI, base},
		{"unknown protocol", Protocol("midjourney"), base},
		{"unknown operation", ProtocolGemini, func() Request { r := base; r.Operation = "variations"; return r }()},
		{"empty operation", ProtocolGrok, func() Request { r := base; r.Operation = ""; return r }()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := BuildPlan(tc.p, tc.req)
			require.Error(t, err)
			require.Nil(t, plan)
			var apiErr *Error
			require.False(t, errors.As(err, &apiErr), "caller contract violations must not be client *Error: %v", err)
		})
	}
}

func TestBuildPlanCommonRequiredFields(t *testing.T) {
	for _, p := range []Protocol{ProtocolGemini, ProtocolGrok} {
		for _, model := range []string{"gemini-2.5-flash-image", "imagen-4.0-generate-001", "grok-imagine-image"} {
			cases := []struct {
				name      string
				req       Request
				wantParam string
			}{
				{"empty prompt", Request{Operation: OperationGenerations, Model: model, Prompt: ""}, "prompt"},
				{"blank prompt", Request{Operation: OperationGenerations, Model: model, Prompt: " \n\t "}, "prompt"},
				{"empty model", Request{Operation: OperationGenerations, Model: "  ", Prompt: "cat"}, "model"},
				{"edits without image", Request{Operation: OperationEdits, Model: model, Prompt: "cat"}, "image"},
				{"generations with image", Request{Operation: OperationGenerations, Model: model, Prompt: "cat",
					Images: []InputImage{{URL: "https://example.com/a.png"}}}, "image"},
			}
			for _, tc := range cases {
				t.Run(string(p)+"/"+model+"/"+tc.name, func(t *testing.T) {
					_, err := BuildPlan(p, tc.req)
					apiErr := requireAPIError(t, err, 400, ErrorTypeInvalidRequest)
					require.Equal(t, tc.wantParam, apiErr.Param)
				})
			}
		}
	}
}

// unsupportedParamCase mutates a valid request so that it carries exactly one
// parameter the target protocol must reject.
type unsupportedParamCase struct {
	name      string
	mutate    func(*Request)
	wantParam string
}

func intPtr(v int) *int { return &v }

func commonUnsupportedCases() []unsupportedParamCase {
	return []unsupportedParamCase{
		{"stream", func(r *Request) { r.Stream = true }, "stream"},
		{"partial_images", func(r *Request) { r.PartialImages = intPtr(2) }, "partial_images"},
		{"mask", func(r *Request) { r.HasMask = true }, "mask"},
		{"output_compression", func(r *Request) { r.OutputCompression = intPtr(80) }, "output_compression"},
		{"output_compression 100", func(r *Request) { r.OutputCompression = intPtr(100) }, "output_compression"},
		{"background transparent", func(r *Request) { r.Background = "transparent" }, "background"},
		{"background opaque", func(r *Request) { r.Background = "opaque" }, "background"},
		{"output_format png", func(r *Request) { r.OutputFormat = "png" }, "output_format"},
		{"output_format webp", func(r *Request) { r.OutputFormat = "webp" }, "output_format"},
		{"style vivid", func(r *Request) { r.Style = "vivid" }, "style"},
		{"style natural", func(r *Request) { r.Style = "natural" }, "style"},
		{"moderation low", func(r *Request) { r.Moderation = "low" }, "moderation"},
		{"input_fidelity high", func(r *Request) { r.InputFidelity = "high" }, "input_fidelity"},
		{"input_fidelity low", func(r *Request) { r.InputFidelity = "low" }, "input_fidelity"},
		{"response_format garbage", func(r *Request) { r.ResponseFormat = "base64" }, "response_format"},
		{"size garbage", func(r *Request) { r.Size = "big" }, "size"},
		{"size zero", func(r *Request) { r.Size = "0x1024" }, "size"},
	}
}

func TestBuildPlanRejectsUnsupportedParameters(t *testing.T) {
	targets := []struct {
		name  string
		p     Protocol
		model string
		extra []unsupportedParamCase
	}{
		{"gemini generateContent", ProtocolGemini, "gemini-2.5-flash-image", []unsupportedParamCase{
			{"quality high", func(r *Request) { r.Quality = "high" }, "quality"},
			{"quality low", func(r *Request) { r.Quality = "low" }, "quality"},
			{"quality standard", func(r *Request) { r.Quality = "standard" }, "quality"},
			{"response_format url", func(r *Request) { r.ResponseFormat = "url" }, "response_format"},
		}},
		{"gemini imagen", ProtocolGemini, "imagen-4.0-generate-001", []unsupportedParamCase{
			{"quality hd", func(r *Request) { r.Quality = "hd" }, "quality"},
			{"quality medium", func(r *Request) { r.Quality = "medium" }, "quality"},
			{"response_format url", func(r *Request) { r.ResponseFormat = "URL" }, "response_format"},
		}},
		{"grok", ProtocolGrok, "grok-imagine-image", []unsupportedParamCase{
			{"quality high", func(r *Request) { r.Quality = "high" }, "quality"},
			{"quality hd", func(r *Request) { r.Quality = "hd" }, "quality"},
			{"quality standard", func(r *Request) { r.Quality = "standard" }, "quality"},
		}},
	}
	for _, target := range targets {
		for _, tc := range append(commonUnsupportedCases(), target.extra...) {
			t.Run(target.name+"/"+tc.name, func(t *testing.T) {
				req := Request{Operation: OperationGenerations, Model: target.model, Prompt: "a red fox"}
				tc.mutate(&req)
				plan, err := BuildPlan(target.p, req)
				require.Nil(t, plan)
				apiErr := requireAPIError(t, err, 400, ErrorTypeInvalidRequest)
				require.Equal(t, tc.wantParam, apiErr.Param)
				require.NotEmpty(t, apiErr.Code)
				require.Contains(t, apiErr.Message, tc.wantParam)
			})
		}
	}
}

func TestBuildPlanAcceptsEquivalentDefaults(t *testing.T) {
	targets := []struct {
		p     Protocol
		model string
	}{
		{ProtocolGemini, "gemini-2.5-flash-image"},
		{ProtocolGemini, "imagen-4.0-generate-001"},
		{ProtocolGrok, "grok-imagine-image"},
	}
	for _, target := range targets {
		t.Run(target.model, func(t *testing.T) {
			req := Request{
				Operation:      OperationGenerations,
				Model:          target.model,
				Prompt:         "a red fox",
				Size:           "auto",
				Quality:        "auto",
				ResponseFormat: "b64_json",
				Background:     "auto",
				OutputFormat:   "",
				Style:          "",
				Moderation:     "AUTO",
				InputFidelity:  "",
				PartialImages:  intPtr(0),
				Stream:         false,
			}
			plan, err := BuildPlan(target.p, req)
			require.NoError(t, err)
			require.Equal(t, "b64_json", plan.ResponseFormat)
			require.Equal(t, 1, plan.RequestedN)
			require.Empty(t, plan.SizeTier, "size auto → upstream decides")
		})
	}
}

func requireAPIError(t *testing.T, err error, status int, typ string) *Error {
	t.Helper()
	require.Error(t, err)
	var apiErr *Error
	require.True(t, errors.As(err, &apiErr), "expected *Error, got %T: %v", err, err)
	require.Equal(t, status, apiErr.HTTPStatus, apiErr.Message)
	require.Equal(t, typ, apiErr.Type, apiErr.Message)
	return apiErr
}

// TestPlanSizeTierDowngradeContract pins the user decision (CONTRACT-CHANGES
// 2026-11-09): a WIDTHxHEIGHT size above the model's highest tier is served at
// the highest tier and billed via Plan.SizeTier; a literal tier string the
// model cannot produce is still rejected.
func TestPlanSizeTierDowngradeContract(t *testing.T) {
	cases := []struct {
		name     string
		p        Protocol
		model    string
		size     string
		wantTier string // "" → expect 400 on size
	}{
		{"gemini 2.5 2K dims → 1K", ProtocolGemini, "gemini-2.5-flash-image", "1536x1024", "1K"},
		{"gemini 2.5 4K dims → 1K", ProtocolGemini, "gemini-2.5-flash-image", "3840x2160", "1K"},
		{"gemini 2.5 2K string → 400", ProtocolGemini, "gemini-2.5-flash-image", "2K", ""},
		{"gemini 2.5 4K string → 400", ProtocolGemini, "gemini-2.5-flash-image", "4K", ""},
		{"gemini 3 pro 4K dims kept", ProtocolGemini, "gemini-3-pro-image-preview", "3840x2160", "4K"},
		{"imagen fast 2K dims → 1K", ProtocolGemini, "imagen-4.0-fast-generate-001", "2048x2048", "1K"},
		{"imagen fast 4K dims → 1K", ProtocolGemini, "imagen-4.0-fast-generate-001", "3840x2160", "1K"},
		{"imagen fast 2K string → 400", ProtocolGemini, "imagen-4.0-fast-generate-001", "2K", ""},
		{"imagen 3 2K dims → 1K", ProtocolGemini, "imagen-3.0-generate-002", "2048x1536", "1K"},
		{"imagen 3 2K string → 400", ProtocolGemini, "imagen-3.0-generate-002", "2K", ""},
		{"imagen 4 4K dims → 2K", ProtocolGemini, "imagen-4.0-generate-001", "3840x2160", "2K"},
		{"imagen 4 ultra 4K dims → 2K", ProtocolGemini, "imagen-4.0-ultra-generate-001", "4096x3072", "2K"},
		{"imagen 4 4K string → 400", ProtocolGemini, "imagen-4.0-generate-001", "4K", ""},
		{"grok 4K dims → 2K", ProtocolGrok, "grok-imagine-image", "3840x2160", "2K"},
		{"grok 4K string → 400", ProtocolGrok, "grok-imagine-image", "4K", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := BuildPlan(tc.p, Request{Operation: OperationGenerations, Model: tc.model, Prompt: "x", Size: tc.size})
			if tc.wantTier == "" {
				require.Nil(t, plan)
				apiErr := requireAPIError(t, err, 400, ErrorTypeInvalidRequest)
				require.Equal(t, "size", apiErr.Param)
				require.Contains(t, apiErr.Message, "does not support")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantTier, plan.SizeTier)
		})
	}
}

// TestBuildPlanErrorsHaveNoCallIndex pins R2-6 for validation errors.
func TestBuildPlanErrorsHaveNoCallIndex(t *testing.T) {
	cases := []struct {
		p   Protocol
		req Request
	}{
		{ProtocolGemini, Request{Operation: OperationGenerations, Model: "gemini-2.5-flash-image"}},
		{ProtocolGemini, Request{Operation: OperationGenerations, Model: "gemini-2.5-flash-image", Prompt: "x", Quality: "high"}},
		{ProtocolGemini, Request{Operation: OperationGenerations, Model: "gemini-2.5-flash-image", Prompt: "x", Size: "1792x1024"}},
		{ProtocolGemini, Request{Operation: OperationGenerations, Model: "../x", Prompt: "x"}},
		{ProtocolGemini, Request{Operation: OperationEdits, Model: "imagen-4.0-generate-001", Prompt: "x", Images: []InputImage{{Data: []byte("x")}}}},
		{ProtocolGemini, Request{Operation: OperationGenerations, Model: "imagen-4.0-generate-001", Prompt: "x", N: 9}},
		{ProtocolGrok, Request{Operation: OperationGenerations, Model: "grok-imagine-image", Prompt: "x", Size: "4K"}},
		{ProtocolGrok, Request{Operation: OperationEdits, Model: "grok-imagine-image", Prompt: "x"}},
		{ProtocolGrok, Request{Operation: OperationGenerations, Model: "grok-imagine-image", Prompt: "x", ResponseFormat: "jpeg"}},
	}
	for i, tc := range cases {
		_, err := BuildPlan(tc.p, tc.req)
		var apiErr *Error
		require.True(t, errors.As(err, &apiErr), "case %d: %v", i, err)
		require.Equal(t, -1, apiErr.CallIndex, "case %d: %s", i, apiErr.Message)
	}
}
