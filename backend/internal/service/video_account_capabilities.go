package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strings"
)

// A local rejection may exclude this account without sending a request or
// freezing money. It is separate from malformed protocol input.
var ErrVideoAccountCapabilityIncompatible = errors.New("video request exceeds account capabilities")

var videoCapabilityResolutionOrder = []string{"480p", "720p", "768p", "1080p", "2k"}

type videoAccountCapabilities struct {
	resolutions []string
	minDuration int
	maxDuration int
}

type videoModelLimits struct {
	Resolutions        []string
	MinDuration        int
	MaxDuration        int
	Durations          []int
	ResolutionOverride bool
	DurationOverride   bool
}

func readVideoAccountCapabilities(account *Account) (videoAccountCapabilities, error) {
	var caps videoAccountCapabilities
	if account == nil || account.Platform != PlatformVideo {
		return caps, nil
	}
	if raw, exists := account.Credentials["video_resolutions"]; exists {
		var values []any
		switch value := raw.(type) {
		case []string:
			for _, item := range value {
				values = append(values, item)
			}
		case []any:
			values = value
		default:
			return caps, fmt.Errorf("video_resolutions must be a non-empty string array")
		}
		if len(values) == 0 {
			return caps, fmt.Errorf("video_resolutions must be a non-empty string array")
		}
		seen := make(map[string]bool)
		for _, item := range values {
			value, ok := item.(string)
			value = strings.ToLower(strings.TrimSpace(value))
			if !ok || !slices.Contains(videoCapabilityResolutionOrder, value) {
				return caps, fmt.Errorf("video_resolutions must contain only 480p, 720p, 768p, 1080p or 2k")
			}
			seen[value] = true
		}
		for _, value := range videoCapabilityResolutionOrder {
			if seen[value] {
				caps.resolutions = append(caps.resolutions, value)
			}
		}
	}
	for _, field := range []struct {
		name   string
		target *int
	}{
		{"video_min_duration_seconds", &caps.minDuration},
		{"video_max_duration_seconds", &caps.maxDuration},
	} {
		if raw, exists := account.Credentials[field.name]; exists {
			value, ok := videoCapabilityPositiveInteger(raw)
			if !ok {
				return caps, fmt.Errorf("%s must be an integer between 1 and 60", field.name)
			}
			*field.target = value
		}
	}
	if caps.minDuration != 0 && caps.maxDuration != 0 && caps.minDuration > caps.maxDuration {
		return caps, fmt.Errorf("video_min_duration_seconds must not exceed video_max_duration_seconds")
	}
	return caps, nil
}

func videoCapabilityPositiveInteger(raw any) (int, bool) {
	if number, ok := raw.(json.Number); ok {
		value, err := number.Float64()
		return int(value), err == nil && value >= 1 && value <= 60 && math.Trunc(value) == value
	}
	value := reflect.ValueOf(raw)
	if !value.IsValid() {
		return 0, false
	}
	switch value.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n := value.Int()
		return int(n), n >= 1 && n <= 60
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n := value.Uint()
		return int(n), n >= 1 && n <= 60
	case reflect.Float32, reflect.Float64:
		n := value.Float()
		return int(n), n >= 1 && n <= 60 && math.Trunc(n) == n
	default:
		return 0, false
	}
}

// Normalize only when saving; request-time reads never mutate the shared
// scheduler account or its credentials map.
func normalizeVideoAccountCapabilities(account *Account) error {
	caps, err := readVideoAccountCapabilities(account)
	if err != nil {
		return err
	}
	if len(caps.resolutions) > 0 {
		account.Credentials["video_resolutions"] = caps.resolutions
	}
	if caps.minDuration > 0 {
		account.Credentials["video_min_duration_seconds"] = caps.minDuration
	}
	if caps.maxDuration > 0 {
		account.Credentials["video_max_duration_seconds"] = caps.maxDuration
	}
	return nil
}

func resolveVideoAccountLimits(account *Account, official videoModelLimits) (videoModelLimits, error) {
	caps, err := readVideoAccountCapabilities(account)
	if err != nil {
		return official, fmt.Errorf("%w: %v", ErrVideoAccountCapabilityIncompatible, err)
	}
	if len(caps.resolutions) > 0 {
		official.Resolutions, official.ResolutionOverride = caps.resolutions, true
	}
	if caps.minDuration > 0 {
		official.MinDuration = caps.minDuration
	}
	if caps.maxDuration > 0 {
		official.MaxDuration = caps.maxDuration
	}
	if caps.minDuration > 0 || caps.maxDuration > 0 {
		official.DurationOverride, official.Durations = true, nil
	}
	if official.MinDuration > official.MaxDuration {
		return official, fmt.Errorf("%w: configured and model duration bounds conflict (%d–%d seconds)", ErrVideoAccountCapabilityIncompatible, official.MinDuration, official.MaxDuration)
	}
	return official, nil
}

func (limits videoModelLimits) normalize(resolution string, duration int, resolutionExplicit, durationExplicit bool) (string, int, error) {
	if canonical, ok := LookupVideoBillingResolution(resolution); ok {
		resolution = canonical
	}
	if !slices.Contains(limits.Resolutions, resolution) {
		if resolutionExplicit || len(limits.Resolutions) == 0 {
			return "", 0, limits.unsupported()
		}
		for _, candidate := range videoCapabilityResolutionOrder {
			if slices.Contains(limits.Resolutions, candidate) {
				resolution = candidate
				break
			}
		}
	}
	if !durationExplicit {
		duration = max(limits.MinDuration, min(duration, limits.MaxDuration))
	}
	if duration < limits.MinDuration || duration > limits.MaxDuration || len(limits.Durations) > 0 && !slices.Contains(limits.Durations, duration) {
		return "", 0, limits.unsupported()
	}
	return resolution, duration, nil
}

func (limits videoModelLimits) unsupported() error {
	if len(limits.Durations) > 0 {
		return fmt.Errorf("%w: supported resolutions: %s; supported durations: %v seconds", ErrVideoAccountCapabilityIncompatible, strings.Join(limits.Resolutions, ", "), limits.Durations)
	}
	return fmt.Errorf("%w: supported resolutions: %s; duration must be an integer from %d to %d seconds", ErrVideoAccountCapabilityIncompatible, strings.Join(limits.Resolutions, ", "), limits.MinDuration, limits.MaxDuration)
}
