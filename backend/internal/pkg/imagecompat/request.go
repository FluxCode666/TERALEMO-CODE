package imagecompat

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// InputImage is a reference image. At least one of Data and URL is set.
// Callers decode multipart uploads and data: URLs into Data+MIMEType and keep
// http(s) URLs in URL.
type InputImage struct {
	Data     []byte
	MIMEType string
	URL      string
}

// Request is the protocol-neutral form of an OpenAI images request. Model is
// already the account-mapped upstream model name.
type Request struct {
	Operation         Operation
	Model             string
	Prompt            string
	N                 int    // <=0 means 1
	Size              string // "", "auto", "1024x1024", "1K"/"2K"/"4K", "16:9" ...
	Quality           string
	ResponseFormat    string // "", "url", "b64_json"
	Background        string
	OutputFormat      string
	Style             string
	Moderation        string
	InputFidelity     string
	OutputCompression *int
	PartialImages     *int
	Stream            bool
	HasMask           bool
	Images            []InputImage
}

// UpstreamCall is one upstream HTTP call. Path starts with "/" and is appended
// to the account base URL by the caller, which also adds authentication
// (gemini: x-goog-api-key; grok: Authorization: Bearer). Calls of one plan may
// share the same Body slice; treat it as read-only.
type UpstreamCall struct {
	Method       string
	Path         string
	ContentType  string
	Body         []byte
	ExpectImages int
}

// Plan lists the upstream calls for one OpenAI request. Gemini generateContent
// yields one image per call, so n>1 is split into n calls.
type Plan struct {
	Protocol       Protocol
	Operation      Operation
	Model          string
	RequestedN     int
	ResponseFormat string // final format returned to the client: "b64_json" or "url"
	// SizeTier is the resolution tier the upstream will actually produce:
	// "1K" / "2K" / "4K" (same literals as the service image billing tiers).
	// It can be lower than the tier implied by a WIDTHxHEIGHT size when the
	// model cannot produce that tier (downgraded, billed at the actual tier).
	// Empty when size is empty or "auto": the upstream decides.
	SizeTier string
	Calls    []UpstreamCall
}

const (
	responseFormatB64 = "b64_json"
	responseFormatURL = "url"
	contentTypeJSON   = "application/json"
)

// errOpenAIPassthrough is returned by BuildPlan for ProtocolOpenAI.
var errOpenAIPassthrough = errors.New("imagecompat: the openai protocol is passed through and must not be converted")

// BuildPlan validates req for protocol p and builds the upstream calls.
// Unsupported parameters or combinations return *Error (HTTP 400,
// invalid_request_error, Param set). Caller contract violations (openai or
// unknown protocol, unknown operation, URL-only reference images for a
// protocol that needs inline data) return a plain error.
func BuildPlan(p Protocol, req Request) (*Plan, error) {
	switch p {
	case ProtocolOpenAI:
		return nil, errOpenAIPassthrough
	case ProtocolGemini, ProtocolGrok:
	default:
		return nil, fmt.Errorf("imagecompat: unsupported protocol %q", p)
	}
	if req.Operation != OperationGenerations && req.Operation != OperationEdits {
		return nil, fmt.Errorf("imagecompat: unsupported operation %q", req.Operation)
	}
	if strings.TrimSpace(req.Model) == "" {
		return nil, invalidParam("model", errorCodeMissingParam, "model is required")
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, invalidParam("prompt", errorCodeMissingParam, "prompt is required")
	}
	switch req.Operation {
	case OperationEdits:
		if len(req.Images) == 0 {
			return nil, invalidParam("image", errorCodeMissingParam, "image is required for image edits")
		}
	case OperationGenerations:
		if len(req.Images) > 0 {
			return nil, invalidParam("image", errorCodeUnsupportedParam,
				"reference images are only supported on /v1/images/edits")
		}
	}
	if p == ProtocolGrok {
		return buildGrokPlan(req)
	}
	if isImagenModel(req.Model) {
		return buildImagenPlan(req)
	}
	return buildGeminiPlan(req)
}

func normalizedN(n int) int {
	if n <= 0 {
		return 1
	}
	return n
}

func isDefaultValue(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "auto":
		return true
	default:
		return false
	}
}

func checkN(label string, n, limit int) *Error {
	if n > limit {
		return invalidParam("n", errorCodeInvalidValue, "%s supports n between 1 and %d, got %d", label, limit, n)
	}
	return nil
}

// rejectUnsupportedCommon rejects the OpenAI-only parameters that none of the
// converted upstream protocols support. Only empty / "auto" (and
// partial_images=0, stream=false) are accepted as equivalent defaults.
func rejectUnsupportedCommon(label string, req Request) *Error {
	if req.Stream {
		return invalidParam("stream", errorCodeUnsupportedParam, "%s does not support streaming image responses; set stream to false", label)
	}
	if req.PartialImages != nil && *req.PartialImages != 0 {
		return invalidParam("partial_images", errorCodeUnsupportedParam, "%s does not support partial_images; omit the parameter", label)
	}
	if req.HasMask {
		return invalidParam("mask", errorCodeUnsupportedParam, "%s does not support mask; omit the parameter", label)
	}
	if req.OutputCompression != nil {
		return invalidParam("output_compression", errorCodeUnsupportedParam, "%s does not support output_compression; omit the parameter", label)
	}
	checks := []struct {
		param string
		value string
	}{
		{"background", req.Background},
		{"output_format", req.OutputFormat},
		{"style", req.Style},
		{"moderation", req.Moderation},
		{"input_fidelity", req.InputFidelity},
	}
	for _, check := range checks {
		if !isDefaultValue(check.value) {
			return unsupportedParam(label, check.param, strings.TrimSpace(check.value))
		}
	}
	return nil
}

// base64OnlyResponseFormat resolves response_format for upstreams that only
// return base64 image data.
func base64OnlyResponseFormat(label, value string) (string, *Error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", responseFormatB64:
		return responseFormatB64, nil
	case responseFormatURL:
		return "", invalidParam("response_format", errorCodeUnsupportedParam,
			"%s only returns base64 image data; use response_format \"b64_json\" (or omit it) instead of \"url\"", label)
	default:
		return "", invalidParam("response_format", errorCodeInvalidValue,
			"response_format must be \"b64_json\" or \"url\", got %q", strings.TrimSpace(value))
	}
}

func checkSizeParsed(req Request) (sizeSpec, *Error) {
	spec, ok := parseSize(req.Size)
	if !ok {
		return spec, invalidParam("size", errorCodeInvalidValue,
			"size %q is invalid; use \"auto\", WIDTHxHEIGHT (e.g. 1024x1024), an aspect ratio (e.g. 16:9) or 1K/2K/4K",
			strings.TrimSpace(req.Size))
	}
	return spec, nil
}

func unsupportedAspectRatio(label string, spec sizeSpec, ratios []aspectRatio) *Error {
	return invalidParam("size", errorCodeUnsupportedParam,
		"size %q (aspect ratio %s) is not supported by %s; supported aspect ratios: %s",
		spec.raw, spec.ratioString(), label, ratioLabels(ratios))
}

// unsupportedTier deliberately omits the upstream model name: it is the
// account-mapped name, which may differ from what the client requested.
func unsupportedTier(label string, spec sizeSpec, supported []string) *Error {
	return invalidParam("size", errorCodeUnsupportedParam,
		"size %q requires %s output, which the selected model on %s does not support (supported: %s); use a smaller size or \"auto\"",
		spec.raw, spec.tier, label, strings.Join(supported, ", "))
}

// normalizeImageMIME resolves the MIME type of an inline reference image and
// checks it against allowed. It falls back to content sniffing when the
// declared type is missing or not an allowed image type.
func normalizeImageMIME(img InputImage, allowed map[string]bool) (string, bool) {
	declared := canonicalMIME(img.MIMEType)
	if allowed[declared] {
		return declared, true
	}
	sniffed := canonicalMIME(http.DetectContentType(img.Data))
	if allowed[sniffed] {
		return sniffed, true
	}
	if declared == "" || declared == "application/octet-stream" {
		return sniffed, false
	}
	return declared, false
}

func canonicalMIME(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if idx := strings.IndexByte(value, ';'); idx >= 0 {
		value = strings.TrimSpace(value[:idx])
	}
	if value == "image/jpg" || value == "image/pjpeg" {
		return "image/jpeg"
	}
	return value
}
