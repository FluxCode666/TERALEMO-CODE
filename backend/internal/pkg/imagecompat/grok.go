package imagecompat

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/tidwall/gjson"
)

const (
	grokLabel = "the xAI Grok image protocol"

	grokMaxN               = 10
	grokMaxReferenceImages = 5
)

var (
	// Official xAI Imagine image aspect ratios (same table as
	// service/grok_media_image_geometry.go; copied, not imported).
	grokAspectRatios = []aspectRatio{
		newAspectRatio("1:1"), newAspectRatio("16:9"), newAspectRatio("9:16"),
		newAspectRatio("4:3"), newAspectRatio("3:4"), newAspectRatio("3:2"),
		newAspectRatio("2:3"), newAspectRatio("2:1"), newAspectRatio("1:2"),
		newAspectRatio("19.5:9"), newAspectRatio("9:19.5"), newAspectRatio("20:9"),
		newAspectRatio("9:20"),
	}
	grokTiers       = []string{tier1K, tier2K}
	grokResolutions = map[string]string{tier1K: "1k", tier2K: "2k"}

	grokInlineImageMIMEs = map[string]bool{"image/png": true, "image/jpeg": true, "image/webp": true}
)

type grokImageRef struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

type grokImageRequest struct {
	Model          string         `json:"model"`
	Prompt         string         `json:"prompt"`
	N              int            `json:"n"`
	ResponseFormat string         `json:"response_format"`
	AspectRatio    string         `json:"aspect_ratio,omitempty"`
	Resolution     string         `json:"resolution,omitempty"`
	Quality        string         `json:"quality,omitempty"`
	Image          *grokImageRef  `json:"image,omitempty"`
	Images         []grokImageRef `json:"images,omitempty"`
}

func buildGrokPlan(req Request) (*Plan, error) {
	model := strings.TrimSpace(req.Model)
	n := normalizedN(req.N)
	if e := checkN(grokLabel, n, grokMaxN); e != nil {
		return nil, e
	}
	if e := rejectUnsupportedCommon(grokLabel, req); e != nil {
		return nil, e
	}
	payload := grokImageRequest{
		Model:  model,
		Prompt: strings.TrimSpace(req.Prompt),
		N:      n,
	}

	switch quality := strings.ToLower(strings.TrimSpace(req.Quality)); quality {
	case "", "auto":
		// xAI defaults to auto; omitting keeps models without quality support working.
	case "low", "medium":
		payload.Quality = quality
	default:
		return nil, invalidParam("quality", errorCodeUnsupportedParam,
			"%s does not support quality %q; use \"low\", \"medium\" or \"auto\"", grokLabel, strings.TrimSpace(req.Quality))
	}

	switch format := strings.ToLower(strings.TrimSpace(req.ResponseFormat)); format {
	case "", responseFormatB64:
		payload.ResponseFormat = responseFormatB64
	case responseFormatURL:
		payload.ResponseFormat = responseFormatURL
	default:
		return nil, invalidParam("response_format", errorCodeInvalidValue,
			"response_format must be \"b64_json\" or \"url\", got %q", strings.TrimSpace(req.ResponseFormat))
	}

	spec, e := checkSizeParsed(req)
	if e != nil {
		return nil, e
	}
	if spec.hasRatio {
		label, ok := matchAspectRatio(grokAspectRatios, spec.ratioW, spec.ratioH)
		if !ok {
			return nil, unsupportedAspectRatio(grokLabel, spec, grokAspectRatios)
		}
		payload.AspectRatio = label
	}
	sizeTier, e := resolveOutputTier(grokLabel, spec, grokTiers)
	if e != nil {
		return nil, e
	}
	// Always explicit (also for ratio-only sizes) so the billed tier never
	// depends on the upstream default resolution.
	payload.Resolution = grokResolutions[sizeTier]

	path := "/v1/images/generations"
	if req.Operation == OperationEdits {
		path = "/v1/images/edits"
		if len(req.Images) > grokMaxReferenceImages {
			return nil, invalidParam("image", errorCodeInvalidValue,
				"%s supports at most %d reference images, got %d", grokLabel, grokMaxReferenceImages, len(req.Images))
		}
		refs := make([]grokImageRef, 0, len(req.Images))
		for i, img := range req.Images {
			ref, e := grokImageReference(i, img)
			if e != nil {
				return nil, e
			}
			refs = append(refs, ref)
		}
		// xAI: "image" for a single source image, "images" for multi-image
		// edits; the two fields are mutually exclusive.
		if len(refs) == 1 {
			payload.Image = &refs[0]
		} else {
			payload.Images = refs
		}
	}

	body, err := marshalNoEscape(payload)
	if err != nil {
		return nil, fmt.Errorf("imagecompat: marshal grok request: %w", err)
	}
	return &Plan{
		Protocol:       ProtocolGrok,
		Operation:      req.Operation,
		Model:          model,
		RequestedN:     n,
		ResponseFormat: payload.ResponseFormat,
		SizeTier:       sizeTier,
		Calls: []UpstreamCall{{
			Method:       http.MethodPost,
			Path:         path,
			ContentType:  contentTypeJSON,
			Body:         body,
			ExpectImages: n,
		}},
	}, nil
}

func grokImageReference(index int, img InputImage) (grokImageRef, *Error) {
	if len(img.Data) > 0 {
		mime, ok := normalizeImageMIME(img, grokInlineImageMIMEs)
		if !ok {
			return grokImageRef{}, invalidParam("image", errorCodeUnsupportedParam,
				"reference image %d has unsupported type %q; %s accepts PNG, JPEG and WEBP", index+1, mime, grokLabel)
		}
		return grokImageRef{
			Type: "image_url",
			URL:  "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(img.Data),
		}, nil
	}
	raw := strings.TrimSpace(img.URL)
	if raw == "" {
		return grokImageRef{}, invalidParam("image", errorCodeInvalidValue, "reference image %d is empty", index+1)
	}
	if strings.HasPrefix(strings.ToLower(raw), "data:") {
		if !strings.Contains(raw, ";base64,") {
			return grokImageRef{}, invalidParam("image", errorCodeInvalidValue,
				"reference image %d must be a base64 data URL (data:<mime>;base64,...)", index+1)
		}
		return grokImageRef{Type: "image_url", URL: raw}, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return grokImageRef{}, invalidParam("image", errorCodeInvalidValue,
			"reference image %d must be an http(s) URL or a base64 data URL", index+1)
	}
	return grokImageRef{Type: "image_url", URL: raw}, nil
}

type grokImageResponse struct {
	Data []struct {
		B64JSON       string `json:"b64_json"`
		URL           string `json:"url"`
		RevisedPrompt string `json:"revised_prompt"`
	} `json:"data"`
}

func parseGrokImages(statusCode int, body []byte, format string) ([]imageItem, *Usage, *Error) {
	var resp grokImageResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, nil, invalidUpstreamResponse(ProtocolGrok, statusCode)
	}
	usage := parseOpenAIStyleUsage(body)
	var images []imageItem
	for _, item := range resp.Data {
		b64 := strings.TrimSpace(item.B64JSON)
		link := strings.TrimSpace(item.URL)
		switch {
		case format == responseFormatURL && link != "":
			images = append(images, imageItem{url: link, revisedPrompt: item.RevisedPrompt})
		case b64 != "":
			images = append(images, imageItem{b64: b64, revisedPrompt: item.RevisedPrompt})
		case link != "":
			images = append(images, imageItem{url: link, revisedPrompt: item.RevisedPrompt})
		}
	}
	if len(images) > 0 {
		return images, usage, nil
	}
	if info := parseUpstreamErrorBody(body); isContentPolicyText(info.searchText()) {
		return nil, usage, contentPolicyError(statusCode, info.message)
	}
	return nil, usage, noImageError(ProtocolGrok, statusCode)
}

// parseOpenAIStyleUsage reads {"usage":{"input_tokens","output_tokens","total_tokens"}}
// (or prompt_tokens / completion_tokens). Returns nil when no token counts exist.
func parseOpenAIStyleUsage(body []byte) *Usage {
	usage := gjson.GetBytes(body, "usage")
	if !usage.IsObject() {
		return nil
	}
	pick := func(keys ...string) int {
		for _, key := range keys {
			if v := usage.Get(key); v.Exists() && v.Type == gjson.Number && v.Int() > 0 {
				return int(v.Int())
			}
		}
		return 0
	}
	input := pick("input_tokens", "prompt_tokens")
	output := pick("output_tokens", "completion_tokens")
	total := pick("total_tokens")
	if total == 0 {
		total = input + output
	}
	if input == 0 && output == 0 && total == 0 {
		return nil
	}
	return &Usage{InputTokens: input, OutputTokens: output, TotalTokens: total}
}
