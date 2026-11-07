package service

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// Creation studio modes and protocols. The protocol values of the video
// protocols are the gateway's own constants so the two sides can never drift.
const (
	CreationModeImage = "image"
	CreationModeVideo = "video"

	CreationProtocolOpenAI   = "openai"
	CreationProtocolGrok     = "grok"
	CreationProtocolSeedance = VideoProtocolSeedance
	CreationProtocolGemini   = VideoProtocolGemini
	CreationProtocolMiniMax  = VideoProtocolMiniMax

	// CreationMaxPromptRunes bounds the prompt length in runes (contract §5.2 V3).
	CreationMaxPromptRunes = 8000

	// CreationImageRoleSource is the only image role of the image modes.
	CreationImageRoleSource = "source"
)

var (
	// ErrCreationKeyNotFound covers "does not exist" and "belongs to somebody
	// else" with one reason so the panel never leaks key ownership.
	ErrCreationKeyNotFound = infraerrors.New(http.StatusNotFound, "CREATION_KEY_NOT_FOUND", "creation key not found")
	// ErrCreationModelNotAvailable means the (mode, protocol, model) triple is not
	// in the options result of this key.
	ErrCreationModelNotAvailable = infraerrors.New(http.StatusBadRequest, "CREATION_MODEL_NOT_AVAILABLE", "creation model is not available for this key")
	// ErrCreationAsyncImageDisabled means the async image switch (object storage)
	// is off, so image generation cannot be submitted.
	ErrCreationAsyncImageDisabled = infraerrors.New(http.StatusNotFound, "ASYNC_IMAGE_DISABLED", "async image tasks are not enabled")
	// ErrCreationBodyTooLarge is the 1 MiB submit body limit.
	ErrCreationBodyTooLarge = infraerrors.New(http.StatusRequestEntityTooLarge, "CREATION_BODY_TOO_LARGE", "creation request body is too large")
	// ErrCreationVideoPricingNotConfigured means a video model has no usable price.
	ErrCreationVideoPricingNotConfigured = infraerrors.New(http.StatusBadRequest, "video_pricing_not_configured", "video pricing is not configured for this model")
	// ErrCreationEstimateUnavailable means pricing could not be computed at all.
	ErrCreationEstimateUnavailable = infraerrors.New(http.StatusServiceUnavailable, "CREATION_ESTIMATE_UNAVAILABLE", "creation estimate is temporarily unavailable")
	// ErrCreationDispatcherUnavailable means the in-process dispatcher was never
	// injected (assembly error).
	ErrCreationDispatcherUnavailable = infraerrors.New(http.StatusServiceUnavailable, "CREATION_DISPATCHER_UNAVAILABLE", "creation dispatcher is not available")
	// ErrCreationInternal is the catch-all for unexpected internal failures.
	ErrCreationInternal = infraerrors.New(http.StatusInternalServerError, "CREATION_INTERNAL_ERROR", "creation request failed")
)

// creationInvalidRequest builds a 400 INVALID_CREATION_REQUEST with a specific,
// user-displayable message.
func creationInvalidRequest(format string, args ...any) error {
	return infraerrors.Newf(http.StatusBadRequest, "INVALID_CREATION_REQUEST", format, args...)
}

// CreationSubmitRequest is the unified submit body (contract §5.1).
type CreationSubmitRequest struct {
	APIKeyID int64              `json:"api_key_id"`
	Mode     string             `json:"mode"`
	Model    string             `json:"model"`
	Protocol string             `json:"protocol"`
	Prompt   string             `json:"prompt"`
	Params   map[string]any     `json:"params"`
	Images   []CreationImageRef `json:"images,omitempty"`
}

// CreationImageRef points at an uploaded image by role and upload id.
type CreationImageRef struct {
	Role     string `json:"role"`
	UploadID string `json:"upload_id"`
}

// CreationEstimateRequest is the estimate body (contract §4.1). It shares the
// shape of the submit body because both sides price the same form.
type CreationEstimateRequest struct {
	APIKeyID int64              `json:"api_key_id"`
	Mode     string             `json:"mode"`
	Model    string             `json:"model"`
	Protocol string             `json:"protocol"`
	Params   map[string]any     `json:"params"`
	Images   []CreationImageRef `json:"images,omitempty"`
}

// creationStudioReplayKey is the unexported context key of the in-process replay
// marker. Only the panel handler can set it, so external HTTP callers can never
// forge it.
type creationStudioReplayKey struct{}

// WithCreationStudioReplay marks a replayed native request. It disables recursion
// in the panel handler and enables the image submit precheck.
func WithCreationStudioReplay(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, creationStudioReplayKey{}, true)
}

// IsCreationStudioReplay reports whether the request is an in-process replay of
// a creation studio submission.
func IsCreationStudioReplay(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	marked, ok := ctx.Value(creationStudioReplayKey{}).(bool)
	return ok && marked
}

// ValidCreationMode reports whether mode is one of the two supported modes.
func ValidCreationMode(mode string) bool {
	return mode == CreationModeImage || mode == CreationModeVideo
}

// creationStudioIsMiniMaxV2 reports whether a public MiniMax model name uses the
// v2 content shape (contract §3.3/§5.4).
func creationStudioIsMiniMaxV2(model string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "minimax-h3")
}

// ValidateCreationSubmit applies the pre-replay rules of contract §5.2 that do
// not depend on storage or the gateway: V3 (mode/prompt), V5 (params), V6
// (images) and V9 (protocol-level image exclusivity). V4 belongs to the caller,
// which owns the options result, and V7/V8 need storage access.
func ValidateCreationSubmit(req *CreationSubmitRequest, option CreationModelOption) error {
	if req == nil {
		return creationInvalidRequest("request body is required")
	}
	if !ValidCreationMode(req.Mode) {
		return creationInvalidRequest("mode must be image or video")
	}
	if strings.TrimSpace(req.Model) == "" {
		return creationInvalidRequest("model is required")
	}
	if !ValidCreationProtocol(req.Protocol) {
		return creationInvalidRequest("protocol is invalid")
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return creationInvalidRequest("prompt is required")
	}
	if utf8.RuneCountInString(prompt) > CreationMaxPromptRunes {
		return creationInvalidRequest("prompt must be at most %d characters", CreationMaxPromptRunes)
	}
	if err := validateCreationParams(req.Params, option); err != nil {
		return err
	}
	if err := validateCreationImages(req, option); err != nil {
		return err
	}
	return validateCreationImageExclusivity(req)
}

// ValidCreationProtocol reports whether protocol is one of the five protocols.
func ValidCreationProtocol(protocol string) bool {
	switch protocol {
	case CreationProtocolOpenAI, CreationProtocolGrok, CreationProtocolSeedance, CreationProtocolGemini, CreationProtocolMiniMax:
		return true
	default:
		return false
	}
}

func validateCreationParams(params map[string]any, option CreationModelOption) error {
	descriptors := make(map[string]CreationParamDescriptor, len(option.Params))
	for _, descriptor := range option.Params {
		descriptors[descriptor.Key] = descriptor
	}
	for key, raw := range params {
		descriptor, ok := descriptors[key]
		if !ok || descriptor.Kind == CreationParamKindImage {
			return creationInvalidRequest("unknown parameter %q", key)
		}
		if err := validateCreationParamValue(descriptor, raw); err != nil {
			return err
		}
	}
	return nil
}

func validateCreationParamValue(descriptor CreationParamDescriptor, raw any) error {
	switch descriptor.Kind {
	case CreationParamKindSelect, CreationParamKindRatio:
		value, ok := creationParamString(raw)
		if !ok {
			return creationInvalidRequest("%s must be a string", descriptor.Key)
		}
		for _, candidate := range creationParamChoiceValues(descriptor) {
			if candidate == value {
				return nil
			}
		}
		return creationInvalidRequest("%s must be one of the offered values", descriptor.Key)
	case CreationParamKindInteger:
		value, ok := creationParamInt(raw)
		if !ok {
			return creationInvalidRequest("%s must be an integer", descriptor.Key)
		}
		if value < descriptor.Min || value > descriptor.Max {
			return creationInvalidRequest("%s must be between %d and %d", descriptor.Key, descriptor.Min, descriptor.Max)
		}
		step := descriptor.Step
		if step <= 0 {
			step = 1
		}
		if (value-descriptor.Min)%step != 0 {
			return creationInvalidRequest("%s must be a multiple of %d above %d", descriptor.Key, step, descriptor.Min)
		}
		return nil
	default:
		return creationInvalidRequest("parameter %s is not submittable", descriptor.Key)
	}
}

func validateCreationImages(req *CreationSubmitRequest, option CreationModelOption) error {
	if len(req.Images) == 0 {
		return nil
	}
	descriptors := make(map[string]CreationParamDescriptor, len(option.Params))
	for _, descriptor := range option.Params {
		if descriptor.Kind == CreationParamKindImage {
			descriptors[descriptor.Key] = descriptor
		}
	}
	counts := make(map[string]int, len(req.Images))
	seen := make(map[string]struct{}, len(req.Images))
	for _, image := range req.Images {
		role := strings.TrimSpace(image.Role)
		descriptor, ok := descriptors[role]
		if !ok {
			return creationInvalidRequest("image role %q is not supported by this model", role)
		}
		if descriptor.Disabled {
			return creationInvalidRequest("image role %q is disabled", role)
		}
		uploadID := strings.TrimSpace(image.UploadID)
		if uploadID == "" {
			return creationInvalidRequest("image upload_id is required")
		}
		if _, duplicate := seen[uploadID]; duplicate {
			return creationInvalidRequest("the same upload cannot be referenced twice")
		}
		seen[uploadID] = struct{}{}
		counts[role]++
		maxCount := descriptor.MaxCount
		if maxCount <= 0 {
			maxCount = 1
		}
		if counts[role] > maxCount {
			return creationInvalidRequest("image role %q accepts at most %d image(s)", role, maxCount)
		}
	}
	return nil
}

// validateCreationImageExclusivity implements V9: on Seedance and MiniMax v2 a
// reference image cannot be combined with a first/last frame.
func validateCreationImageExclusivity(req *CreationSubmitRequest) error {
	exclusive := req.Protocol == CreationProtocolSeedance ||
		(req.Protocol == CreationProtocolMiniMax && creationStudioIsMiniMaxV2(req.Model))
	if !exclusive {
		return nil
	}
	var hasReference, hasFrame bool
	for _, image := range req.Images {
		switch strings.TrimSpace(image.Role) {
		case "reference":
			hasReference = true
		case "first_frame", "last_frame":
			hasFrame = true
		}
	}
	if hasReference && hasFrame {
		return creationInvalidRequest("reference images cannot be combined with first_frame or last_frame")
	}
	return nil
}

// ResolveCreationParams validates the submitted params against the model
// descriptors and fills every missing non-image parameter with its default, so
// the native request always carries explicit values (contract §5.4).
func ResolveCreationParams(option CreationModelOption, params map[string]any) (map[string]any, error) {
	resolved := make(map[string]any, len(option.Params))
	for _, descriptor := range option.Params {
		if descriptor.Kind == CreationParamKindImage {
			continue
		}
		if raw, ok := params[descriptor.Key]; ok {
			resolved[descriptor.Key] = raw
			continue
		}
		resolved[descriptor.Key] = descriptor.Default
	}
	if err := validateCreationParams(params, option); err != nil {
		return nil, err
	}
	return resolved, nil
}

// creationParamString normalizes a select/ratio value to a string.
func creationParamString(raw any) (string, bool) {
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value), true
	case json.Number:
		return value.String(), true
	case float64:
		if math.Trunc(value) == value {
			return strconv.FormatInt(int64(value), 10), true
		}
		return strconv.FormatFloat(value, 'f', -1, 64), true
	case int:
		return strconv.Itoa(value), true
	case int64:
		return strconv.FormatInt(value, 10), true
	case bool:
		return strconv.FormatBool(value), true
	default:
		return "", false
	}
}

// creationParamInt normalizes an integer value; plain numeric strings are
// accepted because HTML inputs submit strings.
func creationParamInt(raw any) (int, bool) {
	switch value := raw.(type) {
	case int:
		return value, true
	case int64:
		return int(value), true
	case float64:
		if math.Trunc(value) != value || math.IsInf(value, 0) || math.IsNaN(value) {
			return 0, false
		}
		return int(value), true
	case json.Number:
		parsed, err := strconv.Atoi(value.String())
		if err != nil {
			if asFloat, floatErr := value.Float64(); floatErr == nil && math.Trunc(asFloat) == asFloat {
				return int(asFloat), true
			}
			return 0, false
		}
		return parsed, true
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

// creationParamBool normalizes a boolean select ("true"/"false").
func creationParamBool(raw any) (bool, bool) {
	switch value := raw.(type) {
	case bool:
		return value, true
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return false, false
		}
		return parsed, true
	default:
		text, ok := creationParamString(raw)
		if !ok {
			return false, false
		}
		parsed, err := strconv.ParseBool(text)
		if err != nil {
			return false, false
		}
		return parsed, true
	}
}

// creationParamChoiceValues lists the selectable values of a descriptor.
// Both select and ratio descriptors publish their choices in Options.
func creationParamChoiceValues(descriptor CreationParamDescriptor) []string {
	values := make([]string, 0, len(descriptor.Options))
	for _, option := range descriptor.Options {
		values = append(values, option.Value)
	}
	return values
}

// creationStudioFormatFloat renders a money-free numeric detail value.
func creationStudioFormatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
