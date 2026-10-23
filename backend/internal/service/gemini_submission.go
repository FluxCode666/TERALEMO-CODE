package service

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// parseGeminiVideoSchedulingRequest validates the union of supported Veo
// parameters. An account can map any public model name to another family, so
// family-specific limits are applied only by NormalizeGeminiVideoSubmission.
func parseGeminiVideoSchedulingRequest(body []byte) (GrokMediaRequestInfo, error) {
	var info GrokMediaRequestInfo
	if err := ValidateVideoSubmissionJSON(body); err != nil {
		return info, err
	}
	root := gjson.ParseBytes(body)
	model := root.Get("model")
	if model.Type != gjson.String || strings.TrimSpace(model.String()) == "" {
		return info, fmt.Errorf("model is required and must be a string")
	}
	info.Model = strings.TrimSpace(model.String())
	instances := root.Get("instances")
	if !instances.IsArray() || len(instances.Array()) != 1 || !instances.Array()[0].IsObject() {
		return info, fmt.Errorf("instances must contain exactly one object")
	}
	parameters := root.Get("parameters")
	if parameters.Exists() && !parameters.IsObject() {
		return info, fmt.Errorf("parameters must be an object")
	}
	if err := rejectGeminiVideoBillingAliases(root); err != nil {
		return info, err
	}
	instance := instances.Array()[0]
	info.Prompt = instance.Get("prompt").String()
	if uri := instance.Get("image.gcsUri").String(); uri != "" {
		info.InputImageURLs = append(info.InputImageURLs, uri)
	}
	if b64 := instance.Get("image.bytesBase64Encoded").String(); b64 != "" {
		info.InputImageURLs = append(info.InputImageURLs, "data:"+instance.Get("image.mimeType").String()+";base64,"+b64)
	}
	var err error
	info.DurationSeconds, err = geminiVideoInteger(parameters.Get("durationSeconds"), "parameters.durationSeconds", 8, 1, 60)
	if err != nil {
		return info, err
	}
	info.DurationSecondsExact = float64(info.DurationSeconds)
	info.N, err = geminiVideoInteger(parameters.Get("sampleCount"), "parameters.sampleCount", 1, 1, 4)
	if err != nil {
		return info, err
	}
	info.Resolution = VideoBillingResolution720P
	if value := parameters.Get("resolution"); value.Exists() {
		if value.Type != gjson.String {
			return info, fmt.Errorf("parameters.resolution must be a string")
		}
		info.Resolution = strings.ToLower(strings.TrimSpace(value.String()))
		switch info.Resolution {
		case VideoBillingResolution480P, VideoBillingResolution720P, VideoBillingResolution768P, VideoBillingResolution1080P, VideoBillingResolution2K:
		default:
			return info, fmt.Errorf("unsupported Gemini video resolution; use a billable 480p, 720p, 768p, 1080p or 2k tier")
		}
	}
	info.AspectRatio = "16:9"
	if value := parameters.Get("aspectRatio"); value.Exists() {
		if value.Type != gjson.String || value.String() != "16:9" && value.String() != "9:16" {
			return info, fmt.Errorf("parameters.aspectRatio must be 16:9 or 9:16")
		}
		info.AspectRatio = value.String()
	}
	return info, nil
}

func geminiVideoInteger(value gjson.Result, field string, fallback, minValue, maxValue int) (int, error) {
	if !value.Exists() {
		return fallback, nil
	}
	// Parse exactly: allow mathematical integers such as 8.0 and 8e0, without
	// rounding a long fractional decimal into a different billable quantity.
	// Bound exact-number work too: a tiny JSON token such as 1e999999999 must
	// never make the arbitrary-precision parser allocate an enormous integer.
	if value.Type != gjson.Number || len(value.Raw) > 128 {
		return 0, fmt.Errorf("%s must be an integer between %d and %d", field, minValue, maxValue)
	}
	if index := strings.IndexAny(value.Raw, "eE"); index >= 0 {
		exponent, err := strconv.Atoi(value.Raw[index+1:])
		if err != nil || exponent < -128 || exponent > 128 {
			return 0, fmt.Errorf("%s must be an integer between %d and %d", field, minValue, maxValue)
		}
	}
	number, ok := new(big.Rat).SetString(value.Raw)
	if !ok || !number.IsInt() || !number.Num().IsInt64() ||
		number.Num().Int64() < int64(minValue) || number.Num().Int64() > int64(maxValue) {
		return 0, fmt.Errorf("%s must be an integer between %d and %d", field, minValue, maxValue)
	}
	return int(number.Num().Int64()), nil
}

// Reject controls that compatible proxies could consume outside the canonical
// REST parameters object. Arbitrary prompt/media metadata remains untouched.
func rejectGeminiVideoBillingAliases(root gjson.Result) error {
	quantity := map[string]bool{
		"n": true, "num": true, "count": true, "sampleCount": true, "sample_count": true,
		"num_videos": true, "numVideos": true, "number_of_videos": true, "numberOfVideos": true,
		"video_count": true, "videoCount": true, "batch_size": true, "batchSize": true,
	}
	timing := map[string]bool{
		"duration": true, "duration_seconds": true, "durationSeconds": true,
		"resolution": true, "video_resolution": true, "videoResolution": true,
		"fps": true, "frames": true, "num_frames": true, "numFrames": true,
		"size": true, "width": true, "height": true,
	}
	containers := map[string]bool{
		"parameters": true, "generation_config": true, "generationConfig": true,
		"video_config": true, "videoConfig": true, "extra": true,
		"extra_body": true, "extraBody": true, "options": true,
	}
	var inspect func(gjson.Result, string) error
	inspect = func(object gjson.Result, path string) error {
		var fieldErr error
		object.ForEach(func(key, value gjson.Result) bool {
			name := key.String()
			canonical := path == "parameters" && (name == "sampleCount" || name == "durationSeconds" || name == "resolution")
			field := strings.TrimPrefix(path+"."+name, ".")
			if !canonical && (quantity[name] || timing[name]) {
				fieldErr = fmt.Errorf("unsupported Gemini video billing field %s; use parameters.sampleCount, durationSeconds and resolution", field)
				return false
			}
			if containers[name] && (value.IsObject() || value.IsArray()) {
				if value.IsArray() {
					for _, item := range value.Array() {
						if fieldErr = inspect(item, field); fieldErr != nil {
							return false
						}
					}
				} else {
					fieldErr = inspect(value, field)
				}
			}
			return fieldErr == nil
		})
		return fieldErr
	}
	if err := inspect(root, ""); err != nil {
		return err
	}
	for _, instance := range root.Get("instances").Array() {
		if err := inspect(instance, "instances.0"); err != nil {
			return err
		}
	}
	return nil
}

// NormalizeGeminiVideoSubmission makes the exact REST quantity, duration and
// resolution explicit before a price contract is frozen. Keep the public model
// in both outputs: reusing the normalized body must not chain account mappings.
// Limits follow Gemini Developer API, not Vertex's distinct 1-4 video limit.
func NormalizeGeminiVideoSubmission(body []byte, account *Account) ([]byte, GrokMediaRequestInfo, error) {
	prepared, info, _, err := normalizeGeminiVideoSubmission(body, account)
	return prepared, info, err
}

func normalizeGeminiVideoSubmission(body []byte, account *Account) ([]byte, GrokMediaRequestInfo, string, error) {
	info, err := parseGeminiVideoSchedulingRequest(body)
	if err != nil {
		return nil, info, "", err
	}
	if account == nil {
		return nil, info, "", fmt.Errorf("Gemini video account is required")
	}
	upstreamModel := strings.TrimPrefix(strings.TrimSpace(account.GetMappedModel(info.Model)), "models/")
	if upstreamModel == "" {
		return nil, info, "", fmt.Errorf("video model is required")
	}
	if err := validateUpstreamPathSegment("video model", upstreamModel); err != nil {
		return nil, info, "", err
	}
	instance := gjson.GetBytes(body, "instances.0")
	if instance.Get("video").Exists() {
		// Extension appends seven seconds and returns a video containing the
		// input. Its billing semantics differ from ordinary video generation.
		return nil, info, "", fmt.Errorf("Gemini video extension is not supported by video generation billing")
	}
	references := instance.Get("referenceImages")
	if references.Exists() && (!references.IsArray() || len(references.Array()) > 3) {
		return nil, info, "", fmt.Errorf("referenceImages must be an array of at most three images")
	}
	family := "unknown"
	switch {
	case strings.HasPrefix(upstreamModel, "veo-2.0-"):
		family = "2"
	case strings.HasPrefix(upstreamModel, "veo-3.0-"):
		family = "3"
	case strings.HasPrefix(upstreamModel, "veo-3.1-"):
		family = "3.1"
	}
	official := videoModelLimits{Resolutions: []string{VideoBillingResolution720P}, MinDuration: 8, MaxDuration: 8}
	switch family {
	case "2":
		// Google's Veo 2 cookbook prose documents 1-2 (some code comments say
		// 1-4). Use the smaller documented Gemini API limit, never Vertex's 4.
		if info.N > 2 {
			return nil, info, "", fmt.Errorf("Gemini Veo 2 requires 1-2 videos")
		}
		official.MinDuration = 5
		if len(references.Array()) > 0 {
			return nil, info, "", fmt.Errorf("referenceImages requires Gemini Veo 3.1")
		}
	case "3":
		if info.N != 1 {
			return nil, info, "", fmt.Errorf("Gemini Veo 3 requires one video")
		}
		official.Resolutions = []string{VideoBillingResolution720P, VideoBillingResolution1080P}
		if len(references.Array()) > 0 {
			return nil, info, "", fmt.Errorf("referenceImages requires Gemini Veo 3.1")
		}
	case "3.1":
		if info.N != 1 {
			return nil, info, "", fmt.Errorf("Gemini Veo 3.1 requires one video")
		}
		official.Resolutions = []string{VideoBillingResolution720P, VideoBillingResolution1080P}
		official.MinDuration, official.Durations = 4, []int{4, 6, 8}
	default:
		// Private names retain conservative defaults unless the administrator
		// declares the compatible relay's duration and resolution capabilities.
		if info.N != 1 || len(references.Array()) > 0 {
			return nil, info, "", fmt.Errorf("unknown Gemini video model requires one video without referenceImages")
		}
	}
	limits, err := resolveVideoAccountLimits(account, official)
	if err != nil {
		return nil, info, "", err
	}
	info.Resolution, info.DurationSeconds, err = limits.normalize(info.Resolution, info.DurationSeconds,
		gjson.GetBytes(body, "parameters.resolution").Exists(), gjson.GetBytes(body, "parameters.durationSeconds").Exists())
	if err != nil {
		return nil, info, "", err
	}
	info.DurationSecondsExact = float64(info.DurationSeconds)
	if family == "3" && info.Resolution == VideoBillingResolution1080P && info.AspectRatio != "16:9" && !limits.ResolutionOverride {
		return nil, info, "", fmt.Errorf("%w: Gemini Veo 3 1080p requires aspectRatio 16:9", ErrVideoAccountCapabilityIncompatible)
	}
	if family == "3.1" && info.DurationSeconds != 8 {
		if info.Resolution == VideoBillingResolution1080P && !limits.ResolutionOverride && !limits.DurationOverride {
			return nil, info, "", fmt.Errorf("%w: Gemini Veo 3.1 1080p requires 8 seconds", ErrVideoAccountCapabilityIncompatible)
		}
		if len(references.Array()) > 0 && !limits.DurationOverride {
			return nil, info, "", fmt.Errorf("%w: Gemini Veo 3.1 referenceImages requires 8 seconds", ErrVideoAccountCapabilityIncompatible)
		}
	}
	prepared := append([]byte(nil), body...)
	for _, field := range []struct {
		path  string
		value any
	}{
		{"parameters.sampleCount", info.N},
		{"parameters.durationSeconds", info.DurationSeconds},
		{"parameters.resolution", info.Resolution},
		{"parameters.aspectRatio", info.AspectRatio},
	} {
		prepared, err = sjson.SetBytes(prepared, field.path, field.value)
		if err != nil {
			return nil, info, "", err
		}
	}
	return prepared, info, upstreamModel, nil
}
