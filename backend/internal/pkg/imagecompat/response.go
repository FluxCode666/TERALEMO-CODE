package imagecompat

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// UpstreamResult is the raw outcome of one UpstreamCall.
type UpstreamResult struct {
	StatusCode int
	Body       []byte
}

// Usage is the upstream token usage, summed across the calls of a plan.
type Usage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

// Response is the converted OpenAI image response.
type Response struct {
	Body   []byte // {"created":..,"data":[{"b64_json"|"url","revised_prompt"?}],"usage"?}
	Images int    // images actually returned to the client → billing
	Usage  *Usage // nil when the upstream reported no token usage
}

type imageItem struct {
	b64           string
	url           string
	revisedPrompt string
}

type openAIImageData struct {
	B64JSON       string `json:"b64_json,omitempty"`
	URL           string `json:"url,omitempty"`
	RevisedPrompt string `json:"revised_prompt,omitempty"`
}

type openAIImageUsage struct {
	TotalTokens  int `json:"total_tokens"`
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type openAIImageResponse struct {
	Created int64             `json:"created"`
	Data    []openAIImageData `json:"data"`
	Usage   *openAIImageUsage `json:"usage,omitempty"`
}

// BuildResponse converts the results (one per plan.Calls entry, same order)
// into an OpenAI image response. At least one image → success containing only
// the images actually produced (each call is capped at its ExpectImages so the
// client is never billed above the requested n). No image at all → *Error:
// the converted upstream error, a content policy violation, or "no image";
// its CallIndex names the call it came from (-1 when no call applies).
//
// Response format: when plan.ResponseFormat is "url" and an upstream entry
// only carries base64 data, the entry is returned as
// {"url":"data:<mime>;base64,<data>"} (mime sniffed: png/jpeg/webp/gif,
// otherwise image/png); entries whose base64 cannot be decoded are dropped.
// The reverse (b64_json requested, upstream returned only a URL) is left as a
// url entry for the caller to backfill.
func BuildResponse(plan *Plan, results []UpstreamResult, now time.Time) (*Response, error) {
	if plan == nil {
		return nil, errors.New("imagecompat: nil plan")
	}
	if len(results) != len(plan.Calls) {
		return nil, fmt.Errorf("imagecompat: got %d upstream results for %d planned calls", len(results), len(plan.Calls))
	}
	switch plan.Protocol {
	case ProtocolGemini, ProtocolGrok:
	default:
		return nil, fmt.Errorf("imagecompat: unsupported protocol %q", plan.Protocol)
	}

	var (
		images   []imageItem
		usage    *Usage
		failures []*Error
	)
	for i, result := range results {
		callImages, callUsage, callErr := parseUpstreamResult(plan, result)
		usage = addUsage(usage, callUsage)
		if callErr == nil && plan.ResponseFormat == responseFormatURL {
			callImages = base64ItemsToDataURLs(callImages)
			if len(callImages) == 0 {
				callErr = noImageError(plan.Protocol, result.StatusCode)
			}
		}
		if callErr != nil {
			callErr.CallIndex = i
			failures = append(failures, callErr)
			continue
		}
		if limit := plan.Calls[i].ExpectImages; limit > 0 && len(callImages) > limit {
			callImages = callImages[:limit]
		}
		images = append(images, callImages...)
	}
	if len(images) == 0 {
		return nil, pickFailure(plan.Protocol, failures)
	}

	payload := openAIImageResponse{Created: now.Unix(), Data: make([]openAIImageData, 0, len(images))}
	for _, img := range images {
		payload.Data = append(payload.Data, openAIImageData{B64JSON: img.b64, URL: img.url, RevisedPrompt: img.revisedPrompt})
	}
	if usage != nil {
		payload.Usage = &openAIImageUsage{TotalTokens: usage.TotalTokens, InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens}
	}
	body, err := marshalNoEscape(payload)
	if err != nil {
		return nil, fmt.Errorf("imagecompat: marshal image response: %w", err)
	}
	return &Response{Body: body, Images: len(images), Usage: usage}, nil
}

func parseUpstreamResult(plan *Plan, result UpstreamResult) ([]imageItem, *Usage, *Error) {
	if result.StatusCode < 200 || result.StatusCode >= 300 {
		return nil, nil, ConvertUpstreamError(plan.Protocol, result.StatusCode, result.Body)
	}
	switch {
	case plan.Protocol == ProtocolGrok:
		return parseGrokImages(result.StatusCode, result.Body, plan.ResponseFormat)
	case isImagenModel(plan.Model):
		images, err := parseImagenPredict(result.StatusCode, result.Body)
		return images, nil, err
	default:
		return parseGeminiGenerateContent(result.StatusCode, result.Body)
	}
}

// pickFailure chooses the error for a plan that produced no image: if every
// call failed transiently the error stays Retryable so the caller can fail
// over; otherwise the first non-retryable error wins (retrying the same
// request elsewhere would not help).
func pickFailure(p Protocol, failures []*Error) *Error {
	if len(failures) == 0 {
		return noImageError(p, 0)
	}
	for _, failure := range failures {
		if !failure.Retryable {
			return failure
		}
	}
	return failures[0]
}

// base64ItemsToDataURLs turns base64-only items into data: URL items for
// plans that must answer with response_format "url". Items whose base64
// cannot be decoded are dropped (they are not images and are not billed).
func base64ItemsToDataURLs(items []imageItem) []imageItem {
	out := items[:0]
	for _, item := range items {
		if item.url != "" {
			out = append(out, imageItem{url: item.url, revisedPrompt: item.revisedPrompt})
			continue
		}
		dataURL, ok := base64ToDataURL(item.b64)
		if !ok {
			continue
		}
		out = append(out, imageItem{url: dataURL, revisedPrompt: item.revisedPrompt})
	}
	return out
}

// base64ToDataURL validates base64 image data (standard or URL-safe, padded
// or not, optionally already a data URL) and returns a data: URL using
// standard padded base64 and the sniffed image MIME type.
func base64ToDataURL(value string) (string, bool) {
	value = strings.Join(strings.Fields(value), "")
	if lower := strings.ToLower(value); strings.HasPrefix(lower, "data:") {
		idx := strings.Index(lower, ";base64,")
		if len(lower) != len(value) {
			idx = -1 // non-ASCII: not a valid base64 data URL
		}
		if idx < 0 {
			return "", false
		}
		value = value[idx+len(";base64,"):]
	}
	if value == "" {
		return "", false
	}
	decoded, err := base64.StdEncoding.DecodeString(value)
	encoded := value
	if err != nil {
		decoded = nil
		for _, enc := range []*base64.Encoding{base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
			if d, e := enc.DecodeString(value); e == nil {
				decoded = d
				break
			}
		}
		if decoded == nil {
			return "", false
		}
		encoded = base64.StdEncoding.EncodeToString(decoded)
	}
	if len(decoded) == 0 {
		return "", false
	}
	return "data:" + sniffImageMIME(decoded) + ";base64," + encoded, true
}

func sniffImageMIME(data []byte) string {
	switch mime := http.DetectContentType(data); mime {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
		return mime
	default:
		return "image/png"
	}
}

func addUsage(total, add *Usage) *Usage {
	if add == nil {
		return total
	}
	if total == nil {
		copied := *add
		return &copied
	}
	total.InputTokens += add.InputTokens
	total.OutputTokens += add.OutputTokens
	total.TotalTokens += add.TotalTokens
	return total
}

func invalidUpstreamResponse(p Protocol, upstreamStatus int) *Error {
	return upstreamError(http.StatusBadGateway, "upstream_invalid_response",
		protocolLabel(p)+" upstream returned an invalid response", false, upstreamStatus)
}

func noImageError(p Protocol, upstreamStatus int) *Error {
	return upstreamError(http.StatusBadGateway, "upstream_no_image",
		protocolLabel(p)+" upstream returned no image", false, upstreamStatus)
}
