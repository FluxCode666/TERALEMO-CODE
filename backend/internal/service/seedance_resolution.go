package service

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/tidwall/gjson"
)

var seedanceResolutionFlag = regexp.MustCompile(`(?i)^--(resolution|rs)(?:$|[^a-z0-9_-])`)
var seedanceQuantityFlag = regexp.MustCompile(`(?i)^--(n|num|count|samplecount|sample_count|num_videos|number_of_videos|video_count|batch_size)(?:$|[^a-z0-9_-])`)

// seedanceResolution makes weak text controls and the strong body field agree.
// Ark documents --resolution/--rs, but no precedence for conflicting expressions;
// rejecting conflicts avoids relying on an upstream's choice or prompt order.
func seedanceResolution(body []byte, prompt string) (string, error) {
	normalize := func(value string) (string, error) {
		resolution, ok := LookupVideoBillingResolution(value)
		if !ok {
			return "", fmt.Errorf("Seedance gateway supports billable resolutions 480p, 720p, 768p, 1080p or 2K")
		}
		return resolution, nil
	}
	resolution := ""
	if value := gjson.GetBytes(body, "resolution"); value.Exists() {
		if value.Type != gjson.String {
			return "", fmt.Errorf("Seedance resolution must be a string")
		}
		var err error
		resolution, err = normalize(value.String())
		if err != nil {
			return "", err
		}
	}
	words := strings.Fields(prompt)
	for index, word := range words {
		if seedanceQuantityFlag.MatchString(word) {
			return "", fmt.Errorf("Seedance does not support output quantity flags")
		}
		flag := seedanceResolutionFlag.FindStringSubmatch(word)
		if flag == nil {
			continue
		}
		if word != "--"+flag[1] || index+1 >= len(words) || flag[1] != "resolution" && flag[1] != "rs" {
			return "", fmt.Errorf("Seedance resolution flags require a separate resolution value")
		}
		value, err := normalize(words[index+1])
		if err != nil {
			return "", err
		}
		if resolution != "" && resolution != value {
			return "", fmt.Errorf("conflicting Seedance resolution expressions")
		}
		resolution = value
	}
	if resolution == "" {
		resolution = VideoBillingResolution720P
	}
	return resolution, nil
}

func validateSeedanceModelResolution(model, resolution string) error {
	model = strings.ToLower(model)
	isV2 := strings.Contains(model, "seedance-2-0") || strings.Contains(model, "seedance-2.0")
	if isV2 && (strings.Contains(model, "fast") || strings.Contains(model, "mini")) && resolution == VideoBillingResolution1080P {
		return fmt.Errorf("Seedance 2.0 fast/mini support 480p or 720p")
	}
	return nil
}

func seedanceResolutionSpecified(body []byte, prompt string) bool {
	if gjson.GetBytes(body, "resolution").Exists() {
		return true
	}
	for _, word := range strings.Fields(prompt) {
		if seedanceResolutionFlag.MatchString(word) {
			return true
		}
	}
	return false
}

func seedanceModelLimits(model string) videoModelLimits {
	minimum, maximum, _, _ := seedanceModelTiming(model)
	resolutions := []string{VideoBillingResolution480P, VideoBillingResolution720P, VideoBillingResolution768P, VideoBillingResolution1080P, VideoBillingResolution2K}
	if validateSeedanceModelResolution(model, VideoBillingResolution1080P) != nil {
		resolutions = []string{VideoBillingResolution480P, VideoBillingResolution720P, VideoBillingResolution768P, VideoBillingResolution2K}
	}
	return videoModelLimits{Resolutions: resolutions, MinDuration: minimum, MaxDuration: maximum}
}
