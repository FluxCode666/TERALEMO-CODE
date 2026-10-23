//go:build unit

package service

import (
	"encoding/json"
	"math"
	"net/http"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func videoCapabilitiesTestAccount(fields map[string]any) *Account {
	credentials := map[string]any{"api_key": "test-upstream-key"}
	for key, value := range fields {
		credentials[key] = value
	}
	return &Account{Platform: PlatformVideo, Type: AccountTypeAPIKey, Credentials: credentials}
}

func TestValidateVideoAccountCapabilitiesNormalizesOnSave(t *testing.T) {
	for _, resolutions := range []any{
		[]string{"2K", " 720P ", "480p", "1080P", "768P", "720p", "2k"},
		[]any{"2K", " 720P ", "480p", "1080P", "768P", "720p", "2k"},
	} {
		account := videoCapabilitiesTestAccount(map[string]any{
			"video_resolutions":          resolutions,
			"video_min_duration_seconds": json.Number("1"),
			"video_max_duration_seconds": float64(60),
			"opaque":                     "preserve",
		})
		require.NoError(t, ValidateVideoAccount(account))
		require.Equal(t, []string{"480p", "720p", "768p", "1080p", "2k"}, account.Credentials["video_resolutions"])
		require.Equal(t, 1, account.Credentials["video_min_duration_seconds"])
		require.Equal(t, 60, account.Credentials["video_max_duration_seconds"])
		require.Equal(t, "preserve", account.Credentials["opaque"])
		require.NoError(t, ValidateVideoAccount(account), "saving an already normalized account is idempotent")
	}
	account := videoCapabilitiesTestAccount(map[string]any{
		"video_min_duration_seconds": 8,
		"video_max_duration_seconds": 8,
	})
	require.NoError(t, ValidateVideoAccount(account), "equal duration bounds are allowed")
}

func TestVideoAccountCapabilitiesAbsentPreservesExistingBehavior(t *testing.T) {
	account := videoCapabilitiesTestAccount(nil)
	require.NoError(t, ValidateVideoAccount(account))
	require.Equal(t, map[string]any{"api_key": "test-upstream-key"}, account.Credentials)
	for _, candidate := range []*Account{account, nil} {
		caps, err := readVideoAccountCapabilities(candidate)
		require.NoError(t, err)
		require.Empty(t, caps.resolutions)
		require.Zero(t, caps.minDuration)
		require.Zero(t, caps.maxDuration)
		official := videoModelLimits{Resolutions: []string{"720p"}, MinDuration: 4, MaxDuration: 8, Durations: []int{4, 6, 8}}
		resolved, err := resolveVideoAccountLimits(candidate, official)
		require.NoError(t, err)
		require.Equal(t, official, resolved)
	}
}

func TestVideoAccountCapabilitiesRuntimeReadDoesNotMutateCredentials(t *testing.T) {
	account := videoCapabilitiesTestAccount(map[string]any{
		"video_resolutions":          []any{"2K", "720P", "720p"},
		"video_min_duration_seconds": json.Number("5"),
		"video_max_duration_seconds": float64(20),
		"opaque":                     map[string]any{"keep": true},
	})
	expected := map[string]any{
		"api_key":                    "test-upstream-key",
		"video_resolutions":          []any{"2K", "720P", "720p"},
		"video_min_duration_seconds": json.Number("5"),
		"video_max_duration_seconds": float64(20),
		"opaque":                     map[string]any{"keep": true},
	}
	caps, err := readVideoAccountCapabilities(account)
	require.NoError(t, err)
	require.Equal(t, []string{"720p", "2k"}, caps.resolutions)
	require.Equal(t, 5, caps.minDuration)
	require.Equal(t, 20, caps.maxDuration)
	limits, err := resolveVideoAccountLimits(account, videoModelLimits{Resolutions: []string{"720p"}, MinDuration: 4, MaxDuration: 8})
	require.NoError(t, err)
	_, _, err = limits.normalize("720p", 8, false, false)
	require.NoError(t, err)
	caps.resolutions[0], limits.Resolutions[0] = "480p", "1080p"
	require.Equal(t, expected, account.Credentials, "scheduler account credentials must remain unchanged during request-time reads")
}

func TestValidateVideoAccountCapabilitiesRejectsInvalidResolutionFields(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
	}{
		{"null", nil}, {"nil strings", []string(nil)}, {"nil any", []any(nil)},
		{"empty strings", []string{}}, {"empty any", []any{}},
		{"scalar string", "720p"}, {"boolean", true}, {"number", 720},
		{"map", map[string]any{"720p": true}}, {"integer slice", []int{720}},
		{"mixed number", []any{"720p", 1080}}, {"mixed null", []any{"720p", nil}},
		{"blank", []string{" "}}, {"unknown", []string{"4k"}},
		{"billing alias is not capability tier", []string{"hd"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := videoCapabilitiesTestAccount(map[string]any{"video_resolutions": tc.value})
			err := ValidateVideoAccount(account)
			require.Error(t, err)
			require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
			require.Equal(t, "INVALID_VIDEO_ACCOUNT", infraerrors.Reason(err))
			require.ErrorContains(t, err, "video_resolutions")
		})
	}
}

func TestValidateVideoAccountCapabilitiesAcceptsIntegerDurationTypes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
		want  int
	}{
		{"int minimum", int(1), 1}, {"int maximum", int(60), 60},
		{"int8", int8(8), 8}, {"int16", int16(8), 8}, {"int32", int32(8), 8}, {"int64", int64(8), 8},
		{"uint", uint(8), 8}, {"uint8", uint8(8), 8}, {"uint16", uint16(8), 8}, {"uint32", uint32(8), 8}, {"uint64", uint64(8), 8},
		{"float32", float32(8), 8}, {"float64", float64(8), 8},
		{"json minimum", json.Number("1"), 1}, {"json maximum", json.Number("60"), 60},
		{"json integral decimal", json.Number("8.0"), 8}, {"json integral exponent", json.Number("1e1"), 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, field := range []string{"video_min_duration_seconds", "video_max_duration_seconds"} {
				account := videoCapabilitiesTestAccount(map[string]any{field: tc.value})
				require.NoError(t, ValidateVideoAccount(account))
				require.Equal(t, tc.want, account.Credentials[field])
				require.Len(t, account.Credentials, 2, "saving one bound must not persist the inherited model bound")
			}
		})
	}
}

func TestValidateVideoAccountCapabilitiesRejectsInvalidDurationFields(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
	}{
		{"null", nil}, {"zero", 0}, {"negative", -1}, {"above maximum", 61},
		{"unsigned above maximum", uint64(61)}, {"fraction", 8.5}, {"float32 fraction", float32(8.5)},
		{"string", "8"}, {"blank string", ""}, {"boolean", true},
		{"array", []any{8}}, {"object", map[string]any{"value": 8}},
		{"NaN", math.NaN()}, {"infinity", math.Inf(1)}, {"negative infinity", math.Inf(-1)},
		{"json fraction", json.Number("8.5")}, {"json overflow", json.Number("1e400")},
		{"json invalid", json.Number("invalid")}, {"json zero", json.Number("0")}, {"json above maximum", json.Number("61")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, field := range []string{"video_min_duration_seconds", "video_max_duration_seconds"} {
				account := videoCapabilitiesTestAccount(map[string]any{field: tc.value})
				err := ValidateVideoAccount(account)
				require.Error(t, err)
				require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
				require.Equal(t, "INVALID_VIDEO_ACCOUNT", infraerrors.Reason(err))
				require.ErrorContains(t, err, field)
			}
		})
	}
	account := videoCapabilitiesTestAccount(map[string]any{
		"video_resolutions":          []string{"2K", "720P"},
		"video_min_duration_seconds": 10,
		"video_max_duration_seconds": 9,
	})
	err := ValidateVideoAccount(account)
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
	require.Equal(t, "INVALID_VIDEO_ACCOUNT", infraerrors.Reason(err))
	require.ErrorContains(t, err, "must not exceed")
	require.Equal(t, []string{"2K", "720P"}, account.Credentials["video_resolutions"], "invalid saves must not partially normalize credentials")
}

func TestResolveVideoAccountLimitsReplacesOnlyConfiguredDimensions(t *testing.T) {
	official := videoModelLimits{Resolutions: []string{"720p", "1080p"}, MinDuration: 4, MaxDuration: 8, Durations: []int{4, 6, 8}}
	for _, tc := range []struct {
		name   string
		fields map[string]any
		want   videoModelLimits
	}{
		{"resolutions replace rather than intersect", map[string]any{"video_resolutions": []string{"2K", "480P"}}, videoModelLimits{Resolutions: []string{"480p", "2k"}, MinDuration: 4, MaxDuration: 8, Durations: []int{4, 6, 8}, ResolutionOverride: true}},
		{"minimum inherits maximum", map[string]any{"video_min_duration_seconds": 5}, videoModelLimits{Resolutions: []string{"720p", "1080p"}, MinDuration: 5, MaxDuration: 8, DurationOverride: true}},
		{"maximum inherits minimum", map[string]any{"video_max_duration_seconds": 20}, videoModelLimits{Resolutions: []string{"720p", "1080p"}, MinDuration: 4, MaxDuration: 20, DurationOverride: true}},
		{"bounds replace discrete durations", map[string]any{"video_min_duration_seconds": 1, "video_max_duration_seconds": 60}, videoModelLimits{Resolutions: []string{"720p", "1080p"}, MinDuration: 1, MaxDuration: 60, DurationOverride: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limits, err := resolveVideoAccountLimits(videoCapabilitiesTestAccount(tc.fields), official)
			require.NoError(t, err)
			require.Equal(t, tc.want, limits)
			require.Equal(t, []string{"720p", "1080p"}, official.Resolutions)
			require.Equal(t, []int{4, 6, 8}, official.Durations)
		})
	}
}

func TestResolveVideoAccountLimitsRejectsInheritedBoundConflictsLocally(t *testing.T) {
	official := videoModelLimits{Resolutions: []string{"720p"}, MinDuration: 4, MaxDuration: 15}
	for _, fields := range []map[string]any{
		{"video_min_duration_seconds": 16},
		{"video_max_duration_seconds": 3},
	} {
		account := videoCapabilitiesTestAccount(fields)
		require.NoError(t, ValidateVideoAccount(account), "a single saved bound has no mapped-model context")
		_, err := resolveVideoAccountLimits(account, official)
		require.ErrorIs(t, err, ErrVideoAccountCapabilityIncompatible)
		require.ErrorContains(t, err, "bounds conflict")
	}
	_, err := resolveVideoAccountLimits(videoCapabilitiesTestAccount(map[string]any{"video_resolutions": "720p"}), official)
	require.ErrorIs(t, err, ErrVideoAccountCapabilityIncompatible, "invalid persisted legacy credentials must exclude the account at runtime")
}

func TestVideoAccountCapabilityLimitsNormalizeDefaultsAndExplicitRequests(t *testing.T) {
	official := videoModelLimits{Resolutions: []string{"720p"}, MinDuration: 4, MaxDuration: 15}
	for _, tc := range []struct {
		name           string
		fields         map[string]any
		wantResolution string
		wantDuration   int
	}{
		{"keep supported protocol default", map[string]any{"video_resolutions": []string{"480p", "720p"}}, "720p", 8},
		{"fallback lowest configured tier", map[string]any{"video_resolutions": []string{"2K", "1080P", "480P"}}, "480p", 8},
		{"clamp default up", map[string]any{"video_min_duration_seconds": 10}, "720p", 10},
		{"clamp default down", map[string]any{"video_max_duration_seconds": 6}, "720p", 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limits, err := resolveVideoAccountLimits(videoCapabilitiesTestAccount(tc.fields), official)
			require.NoError(t, err)
			resolution, duration, err := limits.normalize("720p", 8, false, false)
			require.NoError(t, err)
			require.Equal(t, tc.wantResolution, resolution)
			require.Equal(t, tc.wantDuration, duration)
		})
	}
	limits, err := resolveVideoAccountLimits(videoCapabilitiesTestAccount(map[string]any{
		"video_resolutions": []string{"480p", "2K"}, "video_min_duration_seconds": 5, "video_max_duration_seconds": 10,
	}), official)
	require.NoError(t, err)
	for _, tc := range []struct {
		resolution string
		duration   int
	}{{"720p", 8}, {"480p", 4}, {"480p", 11}} {
		_, _, err := limits.normalize(tc.resolution, tc.duration, true, true)
		require.ErrorIs(t, err, ErrVideoAccountCapabilityIncompatible, "explicit client values must not be silently replaced or clamped")
	}
	resolution, duration, err := limits.normalize("2K", 10, true, true)
	require.NoError(t, err)
	require.Equal(t, "2k", resolution)
	require.Equal(t, 10, duration)
}

func TestVideoAccountCapabilitiesDoNotApplyToGrokOrLegacyOpenAI(t *testing.T) {
	official := videoModelLimits{Resolutions: []string{"720p"}, MinDuration: 4, MaxDuration: 8, Durations: []int{4, 6, 8}}
	for _, platform := range []string{PlatformGrok, PlatformOpenAI} {
		for _, fields := range []map[string]any{
			{"video_resolutions": []string{"2K", "480P"}, "video_min_duration_seconds": float64(1), "video_max_duration_seconds": json.Number("60")},
			{"video_resolutions": "invalid", "video_min_duration_seconds": -1, "video_max_duration_seconds": "60"},
		} {
			account := videoCapabilitiesTestAccount(fields)
			account.Platform = platform
			account.Credentials["video_protocol"] = VideoProtocolSeedance
			require.NoError(t, ValidateVideoAccount(account))
			caps, err := readVideoAccountCapabilities(account)
			require.NoError(t, err)
			require.Empty(t, caps.resolutions)
			require.Zero(t, caps.minDuration)
			require.Zero(t, caps.maxDuration)
			limits, err := resolveVideoAccountLimits(account, official)
			require.NoError(t, err)
			require.Equal(t, official, limits)
			for key, value := range fields {
				require.Equal(t, value, account.Credentials[key], "non-Video credentials must not be normalized")
			}
		}
	}
}
