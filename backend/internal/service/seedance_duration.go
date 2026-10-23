package service

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Official Ark create-task reference, retrieved 2026-10-22:
// https://www.volcengine.com/docs/82379/1520757
// 1.0 pro/pro-fast: 2..12s, frames=25+4n in [29,289] at 24fps.
// 1.5 pro: 4..12s or -1. 2.0: 4..15s or -1. 2.5: 4..30s or -1.
const SeedanceDefaultDurationSeconds = 5

// Recognize the flag name before parsing its value so malformed expressions
// cannot disappear from the match and silently receive a different default.
var seedanceTimingFlag = regexp.MustCompile(`(?i)^--(duration_seconds|durationseconds|duration|framespersecond|frames|frame|fps|dur)(?:$|[^a-z0-9_-])`)

func seedanceModelTiming(model string) (minDuration, maxDuration int, smart, frames bool) {
	name := strings.ToLower(model)
	switch {
	case strings.Contains(name, "seedance-2-5") || strings.Contains(name, "seedance-2.5"):
		return 4, 30, true, false
	case strings.Contains(name, "seedance-2-0") || strings.Contains(name, "seedance-2.0"):
		return 4, 15, true, false
	case strings.Contains(name, "seedance-1-5-pro") || strings.Contains(name, "seedance-1.5-pro"):
		return 4, 12, true, false
	case strings.Contains(name, "seedance-1-0-pro") || strings.Contains(name, "seedance-1.0-pro"):
		return 2, 12, false, true
	default:
		// Fixed requests sent to aliases/deployment IDs are strongly specified in
		// the body; the conservative supported family ceiling is thirty seconds.
		// Smart/frames need an identified model, never an invented five-second cap.
		return 1, 30, false, false
	}
}

func seedanceTiming(body []byte, prompt string) (duration, frames int, specified bool, err error) {
	readInt := func(value gjson.Result, name string) (int, error) {
		if value.Type != gjson.Number || math.IsNaN(value.Float()) || math.IsInf(value.Float(), 0) || value.Float() != math.Trunc(value.Float()) {
			return 0, fmt.Errorf("Seedance %s must be an integer", name)
		}
		return int(value.Int()), nil
	}
	if value := gjson.GetBytes(body, "duration"); value.Exists() {
		duration, err = readInt(value, "duration")
		specified = true
		if err != nil {
			return
		}
	}
	if value := gjson.GetBytes(body, "frames"); value.Exists() {
		frames, err = readInt(value, "frames")
		if err != nil {
			return
		}
	}
	for _, name := range []string{"fps", "frame", "framespersecond", "duration_seconds", "durationSeconds"} {
		if gjson.GetBytes(body, name).Exists() {
			err = fmt.Errorf("unsupported Seedance timing field %s", name)
			return
		}
	}
	words := strings.Fields(prompt)
	for i, word := range words {
		flag := seedanceTimingFlag.FindStringSubmatch(word)
		if flag == nil {
			continue
		}
		name := flag[1]
		if word != "--"+name || i+1 >= len(words) {
			err = fmt.Errorf("Seedance timing flags require a separate integer value")
			return
		}
		if name != "duration" && name != "dur" && name != "frames" {
			err = fmt.Errorf("unsupported Seedance timing flag %s", name)
			return
		}
		value, parseErr := strconv.Atoi(words[i+1])
		if parseErr != nil {
			err = fmt.Errorf("invalid Seedance %s flag", name)
			return
		}
		switch name {
		case "duration", "dur":
			if specified && duration != value {
				err = fmt.Errorf("conflicting Seedance duration expressions")
				return
			}
			duration = value
			specified = true
		case "frames":
			if frames != 0 && frames != value {
				err = fmt.Errorf("conflicting Seedance frames expressions")
				return
			}
			frames = value
		default:
			err = fmt.Errorf("unsupported Seedance timing flag %s", name)
			return
		}
	}
	if frames != 0 && (frames < 29 || frames > 289 || (frames-25)%4 != 0) {
		err = fmt.Errorf("Seedance frames must be 25+4n in [29,289]")
		return
	}
	if gjson.GetBytes(body, "frames").Exists() && frames == 0 {
		err = fmt.Errorf("invalid Seedance frames")
		return
	}
	if specified && duration != -1 && duration <= 0 {
		err = fmt.Errorf("Seedance duration must be positive or supported smart duration -1")
	}
	return
}

// NormalizeSeedanceSubmission freezes the exact timing sent to Ark. Strong body
// fields prevent weak prompt flags or provider defaults silently changing the
// reservation estimate. Model-specific forms are checked after account mapping.
func NormalizeSeedanceSubmission(body []byte, mappedModel string) ([]byte, GrokMediaRequestInfo, error) {
	return normalizeSeedanceSubmission(body, mappedModel, nil)
}

// NormalizeSeedanceSubmissionWithAccount applies an account's explicit proxy
// capabilities after model mapping. Absent overrides retain the native rules.
func NormalizeSeedanceSubmissionWithAccount(body []byte, account *Account) ([]byte, GrokMediaRequestInfo, error) {
	if account == nil {
		return nil, GrokMediaRequestInfo{}, fmt.Errorf("Seedance account is required")
	}
	model := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	return normalizeSeedanceSubmission(body, account.GetMappedModel(model), account)
}

func normalizeSeedanceSubmission(body []byte, mappedModel string, account *Account) ([]byte, GrokMediaRequestInfo, error) {
	originalBody := body
	info, err := ParseSeedanceRequest(body)
	if err != nil {
		return nil, info, err
	}
	if id, _ := seedanceDraftTaskID(body); id != "" {
		return nil, info, ErrSeedanceDraftTaskUnavailable
	}
	limits, err := resolveVideoAccountLimits(account, seedanceModelLimits(mappedModel))
	if err != nil {
		return nil, info, err
	}
	if !limits.ResolutionOverride {
		if err := validateSeedanceModelResolution(mappedModel, info.Resolution); err != nil {
			return nil, info, seedanceAccountLimitError(account, err)
		}
	}
	caps, _ := readVideoAccountCapabilities(account) // resolve already validated.
	draftModel := mappedModel
	if caps.maxDuration > 0 && !strings.Contains(strings.ToLower(mappedModel), "seedance") {
		// An explicit administrator ceiling also bounds smart tasks sent through
		// opaque deployments. Draft's boolean and 480p restrictions still apply.
		draftModel = ""
	}
	duration, frames, specified, err := seedanceTiming(body, info.Prompt)
	if err != nil {
		return nil, info, err
	}
	minDuration, maxDuration, smart, framesSupported := seedanceModelTiming(mappedModel)
	if frames > 0 {
		if !framesSupported {
			return nil, info, seedanceAccountLimitError(account, fmt.Errorf("frames is supported only for identified Seedance 1.0 pro/pro-fast models"))
		}
		if limits.ResolutionOverride || limits.DurationOverride {
			resolution, _, capabilityErr := limits.normalize(info.Resolution, limits.MinDuration, seedanceResolutionSpecified(body, info.Prompt), true)
			if capabilityErr != nil {
				return nil, info, capabilityErr
			}
			info.Resolution = resolution
			exact := float64(frames) / 24
			// Native frame endpoints are 29/24 and 289/24 seconds, not the
			// integer-duration endpoints 2 and 12. Override only declared ends.
			if caps.minDuration > 0 && exact < float64(caps.minDuration) || caps.maxDuration > 0 && exact > float64(caps.maxDuration) {
				return nil, info, limits.unsupported()
			}
		}
		body, err = sjson.SetBytes(body, "frames", frames)
		if err == nil {
			body, err = sjson.DeleteBytes(body, "duration")
		}
		info.DurationSecondsExact = float64(frames) / 24
		info.DurationSeconds = int(math.Ceil(info.DurationSecondsExact))
		info.Frames = frames
	} else {
		if !specified {
			duration = SeedanceDefaultDurationSeconds
			if strings.Contains(strings.ToLower(mappedModel), "seedance-2-5") || strings.Contains(strings.ToLower(mappedModel), "seedance-2.5") {
				duration = -1
			}
		}
		if duration == -1 && !smart && caps.maxDuration == 0 {
			return nil, info, seedanceAccountLimitError(account, fmt.Errorf("smart duration -1 requires an identified Seedance 1.5/2.0/2.5 model"))
		}
		if limits.ResolutionOverride || limits.DurationOverride {
			normalizeDuration := duration
			if duration == -1 {
				// Smart remains native -1; only its finite reservation ceiling
				// changes. The helper receives a valid placeholder for resolution.
				normalizeDuration = limits.MinDuration
			}
			resolution, normalizedDuration, capabilityErr := limits.normalize(info.Resolution, normalizeDuration, seedanceResolutionSpecified(body, info.Prompt), specified && duration != -1)
			if capabilityErr != nil {
				return nil, info, capabilityErr
			}
			info.Resolution = resolution
			if duration != -1 {
				duration = normalizedDuration
			}
		} else if duration != -1 && (duration < minDuration || duration > maxDuration) {
			return nil, info, seedanceAccountLimitError(account, fmt.Errorf("Seedance duration must be in [%d,%d] for this model", minDuration, maxDuration))
		}
		body, err = sjson.SetBytes(body, "duration", duration)
		info.DurationSeconds = duration
		info.DurationSecondsExact = float64(duration)
	}
	if err != nil {
		return nil, info, err
	}
	// Preserve the original timing syntax for opaque deployments: a duration
	// inserted above must not masquerade as an explicitly supplied fixed value.
	if err := validateSeedanceDraftMode(originalBody, draftModel, info.Resolution); err != nil {
		return nil, info, seedanceAccountLimitError(account, err)
	}
	// Ark 1.0 defaults to 1080p, while the gateway's existing default is 720p.
	// Pin the selected contract tier even when the request used only weak flags.
	body, err = sjson.SetBytes(body, "resolution", info.Resolution)
	return body, info, err
}

func seedanceAccountLimitError(account *Account, err error) error {
	if account != nil {
		return fmt.Errorf("%w: %v", ErrVideoAccountCapabilityIncompatible, err)
	}
	return err
}

func SeedanceReservationDuration(info GrokMediaRequestInfo, mappedModel string) int {
	if info.DurationSeconds > 0 {
		return info.DurationSeconds
	}
	_, maximum, _, _ := seedanceModelTiming(mappedModel)
	return maximum
}

// SeedanceReservationDurationForAccount uses the same resolved ceiling as the
// submission normalizer. Fixed/native frame timing is already frozen in info.
func SeedanceReservationDurationForAccount(info GrokMediaRequestInfo, account *Account) int {
	if info.DurationSeconds > 0 {
		return info.DurationSeconds
	}
	if account == nil {
		return SeedanceReservationDuration(info, info.Model)
	}
	limits, err := resolveVideoAccountLimits(account, seedanceModelLimits(account.GetMappedModel(info.Model)))
	if err != nil {
		return 0 // Admission must reject invalid capabilities before reserving.
	}
	return limits.MaxDuration
}
