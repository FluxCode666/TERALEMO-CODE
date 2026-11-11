package imagecompat

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

const (
	geminiLabel = "the Gemini generateContent image protocol"
	imagenLabel = "the Gemini Imagen protocol"

	geminiMaxN               = 4
	geminiMaxReferenceImages = 14
	imagenMaxN               = 4
)

var (
	// Gemini 2.5 Flash Image / Gemini 3 Pro Image aspect ratios.
	geminiAspectRatios = []aspectRatio{
		newAspectRatio("1:1"), newAspectRatio("2:3"), newAspectRatio("3:2"),
		newAspectRatio("3:4"), newAspectRatio("4:3"), newAspectRatio("4:5"),
		newAspectRatio("5:4"), newAspectRatio("9:16"), newAspectRatio("16:9"),
		newAspectRatio("21:9"),
	}
	// Gemini 3.1 Flash Image adds the extreme ratios.
	geminiExtendedAspectRatios = append(append([]aspectRatio(nil), geminiAspectRatios...),
		newAspectRatio("1:4"), newAspectRatio("4:1"), newAspectRatio("1:8"), newAspectRatio("8:1"),
	)
	imagenAspectRatios = []aspectRatio{
		newAspectRatio("1:1"), newAspectRatio("3:4"), newAspectRatio("4:3"),
		newAspectRatio("9:16"), newAspectRatio("16:9"),
	}

	geminiInlineImageMIMEs = map[string]bool{
		"image/png": true, "image/jpeg": true, "image/webp": true,
		"image/heic": true, "image/heif": true,
	}

	// Model names end up in the URL path (/v1beta/models/{model}:generateContent),
	// so only common model-name characters are accepted.
	reGeminiModelName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
)

// geminiImageModelCaps describes what a Gemini image model accepts.
type geminiImageModelCaps struct {
	aspectRatios []aspectRatio
	imageSizes   []string // explicit imageSize values; empty → only the 1K default
}

func geminiCapsForModel(model string) geminiImageModelCaps {
	m := strings.ToLower(model)
	switch {
	case strings.HasPrefix(m, "gemini-2.5-flash-image"):
		return geminiImageModelCaps{aspectRatios: geminiAspectRatios}
	case strings.HasPrefix(m, "gemini-3-pro-image"):
		return geminiImageModelCaps{aspectRatios: geminiAspectRatios, imageSizes: []string{tier1K, tier2K, tier4K}}
	default:
		// gemini-3.1-flash-image and newer / unknown image models: accept the
		// extended ratio set and imageSize; the upstream validates the rest and
		// its 400 is converted into invalid_request_error (never silently changed).
		return geminiImageModelCaps{aspectRatios: geminiExtendedAspectRatios, imageSizes: []string{tier1K, tier2K, tier4K}}
	}
}

func imagenImageSizes(model string) []string {
	m := strings.ToLower(model)
	if strings.Contains(m, "fast") || strings.HasPrefix(m, "imagen-3") {
		return nil
	}
	return []string{tier1K, tier2K}
}

func isImagenModel(model string) bool {
	return strings.HasPrefix(strings.ToLower(normalizeGeminiModel(model)), "imagen-")
}

func normalizeGeminiModel(model string) string {
	model = strings.TrimSpace(model)
	return strings.TrimPrefix(model, "models/")
}

func validateGeminiModel(model string) (string, *Error) {
	normalized := normalizeGeminiModel(model)
	if !reGeminiModelName.MatchString(normalized) {
		return "", invalidParam("model", errorCodeInvalidValue,
			"the model name is not a valid Gemini model name (allowed characters: letters, digits, '.', '_' and '-')")
	}
	return normalized, nil
}

type geminiBlob struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type geminiRequestPart struct {
	Text       string      `json:"text,omitempty"`
	InlineData *geminiBlob `json:"inlineData,omitempty"`
}

type geminiGenerateContentRequest struct {
	Contents []struct {
		Role  string              `json:"role"`
		Parts []geminiRequestPart `json:"parts"`
	} `json:"contents"`
	GenerationConfig struct {
		ResponseModalities []string `json:"responseModalities"`
		ImageConfig        *struct {
			AspectRatio string `json:"aspectRatio,omitempty"`
			ImageSize   string `json:"imageSize,omitempty"`
		} `json:"imageConfig,omitempty"`
	} `json:"generationConfig"`
}

func buildGeminiPlan(req Request) (*Plan, error) {
	model, errModel := validateGeminiModel(req.Model)
	if errModel != nil {
		return nil, errModel
	}
	n := normalizedN(req.N)
	if e := checkN(geminiLabel, n, geminiMaxN); e != nil {
		return nil, e
	}
	if e := rejectUnsupportedCommon(geminiLabel, req); e != nil {
		return nil, e
	}
	if !isDefaultValue(req.Quality) {
		return nil, unsupportedParam(geminiLabel, "quality", strings.TrimSpace(req.Quality))
	}
	format, e := base64OnlyResponseFormat(geminiLabel, req.ResponseFormat)
	if e != nil {
		return nil, e
	}
	spec, e := checkSizeParsed(req)
	if e != nil {
		return nil, e
	}
	caps := geminiCapsForModel(model)
	var aspect, imageSize string
	if spec.hasRatio {
		label, ok := matchAspectRatio(caps.aspectRatios, spec.ratioW, spec.ratioH)
		if !ok {
			return nil, unsupportedAspectRatio(geminiLabel, spec, caps.aspectRatios)
		}
		aspect = label
	}
	sizeTier, e := resolveOutputTier(geminiLabel, spec, caps.imageSizes)
	if e != nil {
		return nil, e
	}
	if len(caps.imageSizes) > 0 {
		// Always explicit when the model accepts imageSize, so the billed tier
		// never depends on an upstream default; 1K-only models omit it.
		imageSize = sizeTier
	}

	parts := []geminiRequestPart{{Text: strings.TrimSpace(req.Prompt)}}
	if req.Operation == OperationEdits {
		if len(req.Images) > geminiMaxReferenceImages {
			return nil, invalidParam("image", errorCodeInvalidValue,
				"%s supports at most %d reference images, got %d", geminiLabel, geminiMaxReferenceImages, len(req.Images))
		}
		for i, img := range req.Images {
			if len(img.Data) == 0 {
				if strings.TrimSpace(img.URL) != "" {
					return nil, fmt.Errorf("imagecompat: gemini reference image %d must be downloaded into Data before BuildPlan", i)
				}
				return nil, invalidParam("image", errorCodeInvalidValue, "reference image %d is empty", i+1)
			}
			mime, ok := normalizeImageMIME(img, geminiInlineImageMIMEs)
			if !ok {
				return nil, invalidParam("image", errorCodeUnsupportedParam,
					"reference image %d has unsupported type %q; %s accepts PNG, JPEG, WEBP, HEIC and HEIF", i+1, mime, geminiLabel)
			}
			parts = append(parts, geminiRequestPart{InlineData: &geminiBlob{
				MimeType: mime,
				Data:     base64.StdEncoding.EncodeToString(img.Data),
			}})
		}
	}

	var body geminiGenerateContentRequest
	body.Contents = append(body.Contents, struct {
		Role  string              `json:"role"`
		Parts []geminiRequestPart `json:"parts"`
	}{Role: "user", Parts: parts})
	// Gemini 2.0 image preview models reject IMAGE-only output; later models
	// support it and it prevents text-only answers.
	if strings.HasPrefix(strings.ToLower(model), "gemini-2.0") {
		body.GenerationConfig.ResponseModalities = []string{"TEXT", "IMAGE"}
	} else {
		body.GenerationConfig.ResponseModalities = []string{"IMAGE"}
	}
	if aspect != "" || imageSize != "" {
		body.GenerationConfig.ImageConfig = &struct {
			AspectRatio string `json:"aspectRatio,omitempty"`
			ImageSize   string `json:"imageSize,omitempty"`
		}{AspectRatio: aspect, ImageSize: imageSize}
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("imagecompat: marshal gemini request: %w", err)
	}
	call := UpstreamCall{
		Method:       http.MethodPost,
		Path:         "/v1beta/models/" + model + ":generateContent",
		ContentType:  contentTypeJSON,
		Body:         payload,
		ExpectImages: 1,
	}
	calls := make([]UpstreamCall, n)
	for i := range calls {
		calls[i] = call
	}
	return &Plan{
		Protocol:       ProtocolGemini,
		Operation:      req.Operation,
		Model:          model,
		RequestedN:     n,
		ResponseFormat: format,
		SizeTier:       sizeTier,
		Calls:          calls,
	}, nil
}

type imagenPredictRequest struct {
	Instances  []imagenInstance `json:"instances"`
	Parameters imagenParameters `json:"parameters"`
}

type imagenInstance struct {
	Prompt string `json:"prompt"`
}

type imagenParameters struct {
	SampleCount     int    `json:"sampleCount"`
	AspectRatio     string `json:"aspectRatio,omitempty"`
	SampleImageSize string `json:"sampleImageSize,omitempty"`
}

func buildImagenPlan(req Request) (*Plan, error) {
	model, errModel := validateGeminiModel(req.Model)
	if errModel != nil {
		return nil, errModel
	}
	if req.Operation == OperationEdits {
		return nil, invalidParam("model", errorCodeUnsupportedParam,
			"Imagen models do not support image edits; use a Gemini image model (e.g. gemini-2.5-flash-image) for /v1/images/edits")
	}
	n := normalizedN(req.N)
	if e := checkN(imagenLabel, n, imagenMaxN); e != nil {
		return nil, e
	}
	if e := rejectUnsupportedCommon(imagenLabel, req); e != nil {
		return nil, e
	}
	if !isDefaultValue(req.Quality) {
		return nil, unsupportedParam(imagenLabel, "quality", strings.TrimSpace(req.Quality))
	}
	format, e := base64OnlyResponseFormat(imagenLabel, req.ResponseFormat)
	if e != nil {
		return nil, e
	}
	spec, e := checkSizeParsed(req)
	if e != nil {
		return nil, e
	}
	params := imagenParameters{SampleCount: n}
	if spec.hasRatio {
		label, ok := matchAspectRatio(imagenAspectRatios, spec.ratioW, spec.ratioH)
		if !ok {
			return nil, unsupportedAspectRatio(imagenLabel, spec, imagenAspectRatios)
		}
		params.AspectRatio = label
	}
	sizes := imagenImageSizes(model)
	sizeTier, e := resolveOutputTier(imagenLabel, spec, sizes)
	if e != nil {
		return nil, e
	}
	if len(sizes) > 0 {
		params.SampleImageSize = sizeTier
	}
	payload, err := json.Marshal(imagenPredictRequest{
		Instances:  []imagenInstance{{Prompt: strings.TrimSpace(req.Prompt)}},
		Parameters: params,
	})
	if err != nil {
		return nil, fmt.Errorf("imagecompat: marshal imagen request: %w", err)
	}
	return &Plan{
		Protocol:       ProtocolGemini,
		Operation:      req.Operation,
		Model:          model,
		RequestedN:     n,
		ResponseFormat: format,
		SizeTier:       sizeTier,
		Calls: []UpstreamCall{{
			Method:       http.MethodPost,
			Path:         "/v1beta/models/" + model + ":predict",
			ContentType:  contentTypeJSON,
			Body:         payload,
			ExpectImages: n,
		}},
	}, nil
}

// ---- responses ----

type geminiResponseBlob struct {
	MimeType      string `json:"mimeType"`
	MimeTypeSnake string `json:"mime_type"`
	Data          string `json:"data"`
}

type geminiResponsePart struct {
	Text            string              `json:"text"`
	Thought         bool                `json:"thought"`
	InlineData      *geminiResponseBlob `json:"inlineData"`
	InlineDataSnake *geminiResponseBlob `json:"inline_data"`
}

type geminiSafetyRating struct {
	Category string `json:"category"`
	Blocked  bool   `json:"blocked"`
}

type geminiGenerateContentResponse struct {
	Candidates []struct {
		Content *struct {
			Parts []geminiResponsePart `json:"parts"`
		} `json:"content"`
		FinishReason  string               `json:"finishReason"`
		FinishMessage string               `json:"finishMessage"`
		SafetyRatings []geminiSafetyRating `json:"safetyRatings"`
	} `json:"candidates"`
	PromptFeedback *struct {
		BlockReason        string `json:"blockReason"`
		BlockReasonMessage string `json:"blockReasonMessage"`
	} `json:"promptFeedback"`
	UsageMetadata *struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		ThoughtsTokenCount   int `json:"thoughtsTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
}

func geminiCandidateBlockReason(finishReason string, ratings []geminiSafetyRating) string {
	if reason := strings.ToUpper(strings.TrimSpace(finishReason)); geminiBlockedFinishReasons[reason] {
		return reason
	}
	for _, rating := range ratings {
		if rating.Blocked {
			return "SAFETY"
		}
	}
	return ""
}

func geminiImagePart(part geminiResponsePart) (imageItem, bool) {
	if part.Thought {
		return imageItem{}, false // interim "thinking" images are not final output
	}
	blob := part.InlineData
	if blob == nil {
		blob = part.InlineDataSnake
	}
	if blob == nil || strings.TrimSpace(blob.Data) == "" {
		return imageItem{}, false
	}
	mime := blob.MimeType
	if mime == "" {
		mime = blob.MimeTypeSnake
	}
	if mime != "" && !strings.HasPrefix(strings.ToLower(mime), "image/") {
		return imageItem{}, false
	}
	return imageItem{b64: strings.TrimSpace(blob.Data)}, true
}

// geminiBlockedFinishReasons are finish reasons that mean the output was
// withheld by safety / policy filters.
var geminiBlockedFinishReasons = map[string]bool{
	"SAFETY":                   true,
	"RECITATION":               true,
	"BLOCKLIST":                true,
	"PROHIBITED_CONTENT":       true,
	"SPII":                     true,
	"IMAGE_SAFETY":             true,
	"IMAGE_PROHIBITED_CONTENT": true,
	"IMAGE_RECITATION":         true,
}

func parseGeminiGenerateContent(statusCode int, body []byte) ([]imageItem, *Usage, *Error) {
	var resp geminiGenerateContentResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, nil, invalidUpstreamResponse(ProtocolGemini, statusCode)
	}
	var usage *Usage
	if u := resp.UsageMetadata; u != nil {
		output := u.CandidatesTokenCount + u.ThoughtsTokenCount
		total := u.TotalTokenCount
		if total == 0 {
			total = u.PromptTokenCount + output
		}
		if u.PromptTokenCount > 0 || output > 0 || total > 0 {
			usage = &Usage{InputTokens: u.PromptTokenCount, OutputTokens: output, TotalTokens: total}
		}
	}
	// 1. A prompt-level block withholds the whole call, even if image data
	//    is present.
	if resp.PromptFeedback != nil {
		if reason := strings.TrimSpace(resp.PromptFeedback.BlockReason); reason != "" {
			return nil, usage, contentPolicyError(statusCode, "reason: "+strings.ToUpper(reason))
		}
	}
	// 2. Candidates flagged by safety (blocked finish reason or a blocked
	//    safety rating) contribute no images; only unflagged images count.
	var images []imageItem
	blockReason := ""
	for _, cand := range resp.Candidates {
		if reason := geminiCandidateBlockReason(cand.FinishReason, cand.SafetyRatings); reason != "" {
			if blockReason == "" {
				blockReason = reason
			}
			continue
		}
		if cand.Content == nil {
			continue
		}
		for _, part := range cand.Content.Parts {
			if item, ok := geminiImagePart(part); ok {
				images = append(images, item)
			}
		}
	}
	if len(images) > 0 {
		return images, usage, nil
	}
	if blockReason != "" {
		return nil, usage, contentPolicyError(statusCode, "reason: "+blockReason)
	}
	return nil, usage, noImageError(ProtocolGemini, statusCode)
}

type imagenPredictResponse struct {
	Predictions []struct {
		BytesBase64Encoded string `json:"bytesBase64Encoded"`
		MimeType           string `json:"mimeType"`
		RaiFilteredReason  string `json:"raiFilteredReason"`
	} `json:"predictions"`
}

func parseImagenPredict(statusCode int, body []byte) ([]imageItem, *Error) {
	var resp imagenPredictResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, invalidUpstreamResponse(ProtocolGemini, statusCode)
	}
	var images []imageItem
	filteredReason := ""
	for _, pred := range resp.Predictions {
		// A prediction carrying raiFilteredReason was filtered: its bytes (if
		// any) are never returned or billed.
		if reason := strings.TrimSpace(pred.RaiFilteredReason); reason != "" {
			if filteredReason == "" {
				filteredReason = reason
			}
			continue
		}
		if data := strings.TrimSpace(pred.BytesBase64Encoded); data != "" {
			images = append(images, imageItem{b64: data})
		}
	}
	if len(images) > 0 {
		return images, nil
	}
	if filteredReason != "" || len(resp.Predictions) == 0 {
		// Imagen drops filtered images; an empty predictions list means every
		// sample was filtered by the Responsible AI filters.
		return nil, contentPolicyError(statusCode, filteredReason)
	}
	return nil, noImageError(ProtocolGemini, statusCode)
}

func containsString(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}
