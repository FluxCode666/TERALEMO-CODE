package service

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Native creation endpoints. Only these paths may ever be replayed.
const (
	CreationPathImageGenerations = "/v1/images/generations/async"
	CreationPathImageEdits       = "/v1/images/edits/async"
	CreationPathSeedance         = "/v1/contents/generations/tasks"
	CreationPathMiniMaxV1        = "/v1/video_generation"
	CreationPathMiniMaxV2        = "/v2/video_generation"
	CreationPathGrokVideo        = "/v1/videos/generations"

	// CreationGeminiPathTemplate is the only templated path; the model segment is
	// validated by validateUpstreamPathSegment before it is interpolated.
	CreationGeminiPathTemplate = "/v1beta/models/%s:predictLongRunning"
)

// creationStudioAllowedPaths is the hard-coded replay whitelist (contract §5.4).
var creationStudioAllowedPaths = map[string]struct{}{
	CreationPathImageGenerations: {},
	CreationPathImageEdits:       {},
	CreationPathSeedance:         {},
	CreationPathMiniMaxV1:        {},
	CreationPathMiniMaxV2:        {},
	CreationPathGrokVideo:        {},
}

// creationStudioAllowedPath reports whether path may be replayed. The Gemini path
// is checked structurally (prefix + suffix + safe model segment).
func creationStudioAllowedPath(path string) bool {
	if _, ok := creationStudioAllowedPaths[path]; ok {
		return true
	}
	_, _, err := creationStudioGeminiModel(path)
	return err == nil
}

// creationStudioGeminiModel extracts the model segment of a Gemini path.
func creationStudioGeminiModel(path string) (string, bool, error) {
	const prefix = "/v1beta/models/"
	const suffix = ":predictLongRunning"
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return "", false, errors.New("path is not a Gemini predictLongRunning endpoint")
	}
	model := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix))
	if err := validateUpstreamPathSegment("model", model); err != nil {
		return "", true, err
	}
	return model, true, nil
}

// CreationImageAsset is one uploaded image handed to the request builder: the
// public link for pull-based protocols and the inline bytes for push-based ones.
type CreationImageAsset struct {
	URL         string
	ContentType string
	Data        []byte
}

// NativeCreationRequest is the replay payload.
type NativeCreationRequest struct {
	Path            string
	Body            []byte
	Protocol        string
	RequestIDPrefix string
}

// CreationNativeRequestInput is everything the pure builder needs. Images are
// keyed by role and ordered as the client submitted them.
type CreationNativeRequestInput struct {
	Mode     string
	Protocol string
	Model    string
	Prompt   string
	Params   map[string]any
	Images   map[string][]CreationImageAsset
}

// BuildCreationNativeRequest maps the unified form to the native gateway body.
// It is a pure function: image bytes are supplied by the caller, nothing is read,
// frozen or logged. Every descriptor parameter is written explicitly, including
// defaults, and no cross-protocol quantity field is invented.
func BuildCreationNativeRequest(input CreationNativeRequestInput) (*NativeCreationRequest, error) {
	switch input.Mode {
	case CreationModeImage:
		return buildCreationImageRequest(input)
	case CreationModeVideo:
		return buildCreationVideoRequest(input)
	default:
		return nil, creationInvalidRequest("mode must be image or video")
	}
}

func buildCreationImageRequest(input CreationNativeRequestInput) (*NativeCreationRequest, error) {
	if input.Protocol != CreationProtocolOpenAI && input.Protocol != CreationProtocolGrok {
		return nil, creationInvalidRequest("image mode requires the openai or grok protocol")
	}
	model := strings.TrimSpace(input.Model)
	if model == "" {
		return nil, creationInvalidRequest("model is required")
	}
	source := input.Images[CreationImageRoleSource]
	body := map[string]any{
		"model":  model,
		"prompt": input.Prompt,
	}
	if input.Protocol == CreationProtocolGrok {
		body["aspect_ratio"] = creationStringParam(input.Params, "aspect_ratio", "1:1")
		// The descriptor value (1k/2k) is written to the native size field: Grok
		// derives its billing tier from size, and the gateway converts size back
		// to the upstream resolution and drops it (contract §3.3/§5.4).
		body["size"] = creationStringParam(input.Params, "resolution", "1k")
	} else {
		body["size"] = creationStringParam(input.Params, "size", "1024x1024")
		body["quality"] = creationStringParam(input.Params, "quality", "auto")
	}
	body["n"] = creationIntParam(input.Params, "n", 1)

	path := CreationPathImageGenerations
	if len(source) > 0 {
		asset := source[0]
		dataURL, err := creationImageAssetDataURL(asset)
		if err != nil {
			return nil, err
		}
		if input.Protocol == CreationProtocolGrok {
			body["image"] = map[string]string{"url": dataURL}
		} else {
			body["images"] = []map[string]string{{"image_url": dataURL}}
		}
		path = CreationPathImageEdits
	}
	return newNativeCreationRequest(path, input.Protocol, "", body)
}

func buildCreationVideoRequest(input CreationNativeRequestInput) (*NativeCreationRequest, error) {
	model := strings.TrimSpace(input.Model)
	if model == "" {
		return nil, creationInvalidRequest("model is required")
	}
	switch input.Protocol {
	case CreationProtocolSeedance:
		content := []any{map[string]any{"type": "text", "text": input.Prompt}}
		for _, role := range []string{"first_frame", "last_frame", "reference"} {
			nativeRole := creationNativeImageRole(input.Protocol, role)
			for _, asset := range input.Images[role] {
				if strings.TrimSpace(asset.URL) == "" {
					return nil, creationInvalidRequest("image %s is missing its link", role)
				}
				content = append(content, map[string]any{
					"type": "image_url", "image_url": map[string]string{"url": asset.URL}, "role": nativeRole,
				})
			}
		}
		body := map[string]any{
			"model":      model,
			"content":    content,
			"resolution": strings.ToLower(creationStringParam(input.Params, "resolution", VideoBillingResolution720P)),
			"duration":   creationIntParam(input.Params, "duration", 5),
			"ratio":      creationStringParam(input.Params, "ratio", "16:9"),
			"watermark":  creationBoolParam(input.Params, "watermark", false),
		}
		return newNativeCreationRequest(CreationPathSeedance, input.Protocol, "seedance:", body)
	case CreationProtocolGemini:
		if err := validateUpstreamPathSegment("model", model); err != nil {
			return nil, creationInvalidRequest("gemini model name is not a valid upstream path segment")
		}
		instance := map[string]any{"prompt": input.Prompt}
		if frames := input.Images["first_frame"]; len(frames) > 0 {
			image, err := creationGeminiInlineImage(frames[0])
			if err != nil {
				return nil, err
			}
			instance["image"] = image
		}
		references := make([]any, 0, len(input.Images["reference"]))
		for _, asset := range input.Images["reference"] {
			image, err := creationGeminiInlineImage(asset)
			if err != nil {
				return nil, err
			}
			references = append(references, map[string]any{"image": image, "referenceType": "asset"})
		}
		if len(references) > 0 {
			instance["referenceImages"] = references
		}
		body := map[string]any{
			"instances": []any{instance},
			"parameters": map[string]any{
				"durationSeconds": creationIntParam(input.Params, "duration", 8),
				"resolution":      strings.ToLower(creationStringParam(input.Params, "resolution", VideoBillingResolution720P)),
				"aspectRatio":     creationStringParam(input.Params, "aspect_ratio", "16:9"),
				"sampleCount":     1,
			},
		}
		path := fmt.Sprintf(CreationGeminiPathTemplate, model)
		return newNativeCreationRequest(path, input.Protocol, "gemini:", body)
	case CreationProtocolMiniMax:
		resolution := strings.ToUpper(creationStringParam(input.Params, "resolution", "768p"))
		if creationStudioIsMiniMaxV2(model) {
			content := []any{map[string]any{"type": "text", "text": input.Prompt}}
			for _, role := range []string{"first_frame", "last_frame", "reference"} {
				nativeRole := creationNativeImageRole(input.Protocol, role)
				for _, asset := range input.Images[role] {
					if strings.TrimSpace(asset.URL) == "" {
						return nil, creationInvalidRequest("image %s is missing its link", role)
					}
					content = append(content, map[string]any{
						"type": "image_url", "image_url": map[string]string{"url": asset.URL}, "role": nativeRole,
					})
				}
			}
			body := map[string]any{
				"model":      model,
				"content":    content,
				"duration":   creationIntParam(input.Params, "duration", 6),
				"resolution": resolution,
				"ratio":      creationStringParam(input.Params, "ratio", "16:9"),
			}
			return newNativeCreationRequest(CreationPathMiniMaxV2, input.Protocol, "minimax:", body)
		}
		body := map[string]any{
			"model":      model,
			"prompt":     input.Prompt,
			"duration":   creationIntParam(input.Params, "duration", 6),
			"resolution": resolution,
		}
		if frames := input.Images["first_frame"]; len(frames) > 0 {
			body["first_frame_image"] = frames[0].URL
		}
		if frames := input.Images["last_frame"]; len(frames) > 0 {
			body["last_frame_image"] = frames[0].URL
		}
		return newNativeCreationRequest(CreationPathMiniMaxV1, input.Protocol, "minimax:", body)
	case CreationProtocolGrok:
		body := map[string]any{
			"model":      model,
			"prompt":     input.Prompt,
			"duration":   creationIntParam(input.Params, "duration", 8),
			"resolution": strings.ToLower(creationStringParam(input.Params, "resolution", VideoBillingResolution480P)),
		}
		if frames := input.Images["first_frame"]; len(frames) > 0 {
			if strings.TrimSpace(frames[0].URL) == "" {
				return nil, creationInvalidRequest("image first_frame is missing its link")
			}
			body["image"] = map[string]string{"url": frames[0].URL}
		}
		return newNativeCreationRequest(CreationPathGrokVideo, input.Protocol, "", body)
	default:
		return nil, creationInvalidRequest("video mode requires seedance, gemini, minimax or grok")
	}
}

// creationNativeImageRole maps a form role to the protocol's native role name.
func creationNativeImageRole(protocol, role string) string {
	if role == "reference" {
		return "reference_image"
	}
	return role
}

// newNativeCreationRequest validates the path against the whitelist and encodes
// the body once.
func newNativeCreationRequest(path, protocol, requestIDPrefix string, body map[string]any) (*NativeCreationRequest, error) {
	if !creationStudioAllowedPath(path) {
		return nil, creationInvalidRequest("native creation path is not allowed")
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, ErrCreationInternal.WithCause(err)
	}
	return &NativeCreationRequest{Path: path, Body: encoded, Protocol: protocol, RequestIDPrefix: requestIDPrefix}, nil
}

// creationGeminiInlineImage inlines the bytes: Gemini accepts inline data or a
// GCS URI, never an arbitrary https link.
func creationGeminiInlineImage(asset CreationImageAsset) (map[string]string, error) {
	if len(asset.Data) == 0 {
		return nil, creationInvalidRequest("gemini image bytes are unavailable")
	}
	contentType := strings.TrimSpace(asset.ContentType)
	if contentType == "" {
		contentType = creationUploadContentTypePNG
	}
	return map[string]string{
		"bytesBase64Encoded": base64.StdEncoding.EncodeToString(asset.Data),
		"mimeType":           contentType,
	}, nil
}

// creationImageAssetDataURL builds a data URL for the JSON image-edit paths.
func creationImageAssetDataURL(asset CreationImageAsset) (string, error) {
	if len(asset.Data) == 0 {
		return "", creationInvalidRequest("image edit requires the source bytes")
	}
	contentType := strings.TrimSpace(asset.ContentType)
	if contentType == "" {
		contentType = creationUploadContentTypePNG
	}
	return "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(asset.Data), nil
}

func creationStringParam(params map[string]any, key, fallback string) string {
	if params == nil {
		return fallback
	}
	raw, ok := params[key]
	if !ok {
		return fallback
	}
	value, ok := creationParamString(raw)
	if !ok || value == "" {
		return fallback
	}
	return value
}

func creationIntParam(params map[string]any, key string, fallback int) int {
	if params == nil {
		return fallback
	}
	raw, ok := params[key]
	if !ok {
		return fallback
	}
	value, ok := creationParamInt(raw)
	if !ok {
		return fallback
	}
	return value
}

func creationBoolParam(params map[string]any, key string, fallback bool) bool {
	if params == nil {
		return fallback
	}
	raw, ok := params[key]
	if !ok {
		return fallback
	}
	value, ok := creationParamBool(raw)
	if !ok {
		return fallback
	}
	return value
}
