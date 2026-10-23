package service

import (
	"errors"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ErrMiniMaxVideoAccountIncompatible allows scheduling to skip an account
// without sending a create request or reserving a contract for the wrong API.
var ErrMiniMaxVideoAccountIncompatible = errors.New("MiniMax request requires a v2 upstream account")

// ParseMiniMaxVideoSchedulingRequest accepts either API's request shape before
// an account (and therefore the upstream version and model mapping) is known.
// Exact model-specific validation belongs to NormalizeMiniMaxSubmission.
func ParseMiniMaxVideoSchedulingRequest(body []byte) (GrokMediaRequestInfo, error) {
	info, err := ParseMiniMaxVideoRequest(body)
	if err == nil || !gjson.ValidBytes(body) || !gjson.ParseBytes(body).IsObject() || info.Model == "" {
		return info, err
	}
	// Any public name, including H3/H3-Max itself, can map to either v2 model.
	// Parse their parameter union without applying the public name's limits.
	// Account overrides may legitimately extend official ranges to 60 seconds
	// and any configured billing tier. Keep those limits out of preselection.
	union := videoModelLimits{Resolutions: videoCapabilityResolutionOrder, MinDuration: 1, MaxDuration: 60}
	if broad, broadErr := parseMiniMaxVideoRequestWithLimits(body, &union); broadErr == nil {
		return broad, nil
	}
	prepared, prepareErr := prepareMiniMaxVideoV2(body)
	if prepareErr != nil {
		return info, err
	}
	v2Info, v2Err := parseMiniMaxVideoV2WithLimits(prepared, GrokMediaRequestInfo{}, &union)
	if v2Err != nil {
		return info, err
	}
	v2Info.Model = info.Model
	return v2Info, nil
}

// NormalizeMiniMaxSubmission resolves the selected account's version and mapped
// model before the caller freezes billing metadata. It retains the public model
// in both results so forwarding maps it exactly once. All JSON mutations copy
// their input, allowing an incompatible account to be skipped safely.
func NormalizeMiniMaxSubmission(body []byte, account *Account, requireV2 bool) ([]byte, GrokMediaRequestInfo, error) {
	var info GrokMediaRequestInfo
	if err := ValidateVideoSubmissionJSON(body); err != nil {
		return nil, info, err
	}
	if err := RejectUnsupportedVideoBillingFields(body, VideoProtocolMiniMax); err != nil {
		return nil, info, err
	}
	info.Model = strings.TrimSpace(gjson.GetBytes(body, "model").String())
	if info.Model == "" {
		return nil, info, fmt.Errorf("model is required")
	}
	if account == nil {
		return nil, info, fmt.Errorf("MiniMax account is required")
	}
	publicModel := info.Model
	mappedModel := account.GetMappedModel(publicModel)
	var err error
	if miniMaxVideoV2(account.VideoBaseURL()) {
		official := videoModelLimits{Resolutions: []string{"480p", "768p", "2k"}, MinDuration: 4, MaxDuration: 15}
		if strings.EqualFold(mappedModel, "MiniMax-H3") {
			official.Resolutions = []string{"768p", "2k"}
		}
		if strings.EqualFold(mappedModel, "MiniMax-H3-Max") {
			official.Resolutions, official.MinDuration = []string{"480p", "768p"}, 5
		}
		limits, limitErr := resolveVideoAccountLimits(account, official)
		if limitErr != nil {
			return nil, info, limitErr
		}
		body, err = prepareMiniMaxVideoV2(body)
		if err == nil {
			info, err = parseMiniMaxVideoV2WithLimits(body, GrokMediaRequestInfo{Model: mappedModel}, &limits)
		}
		info.Model = publicModel
	} else {
		if requireV2 || gjson.GetBytes(body, "content").Exists() || miniMaxH3Model(publicModel) || miniMaxH3Model(mappedModel) {
			return nil, info, ErrMiniMaxVideoAccountIncompatible
		}
		limits, limitErr := resolveVideoAccountLimits(account, videoModelLimits{Resolutions: videoCapabilityResolutionOrder, MinDuration: 6, MaxDuration: 10, Durations: []int{6, 10}})
		if limitErr != nil {
			return nil, info, limitErr
		}
		// Preserve the historical 512P wire alias while permitting configured
		// 480P. Unconfigured v1 still rejects 480P as it did before overrides.
		if !limits.ResolutionOverride && strings.EqualFold(strings.TrimSpace(gjson.GetBytes(body, "resolution").String()), "480P") {
			err = limits.unsupported()
		} else {
			info, err = parseMiniMaxVideoRequestWithLimits(body, &limits)
		}
		if err != nil {
			if prepared, prepareErr := prepareMiniMaxVideoV2(body); prepareErr == nil {
				if _, v2Err := parseMiniMaxVideoV2(prepared, GrokMediaRequestInfo{Model: publicModel}); v2Err == nil {
					return nil, info, ErrMiniMaxVideoAccountIncompatible
				}
			}
		}
	}
	if err != nil {
		return nil, info, err
	}
	body, err = sjson.SetBytes(body, "duration", info.DurationSeconds)
	if err != nil {
		return nil, info, err
	}
	resolution := strings.ToUpper(info.Resolution)
	if !miniMaxVideoV2(account.VideoBaseURL()) && strings.EqualFold(strings.TrimSpace(gjson.GetBytes(body, "resolution").String()), "512P") {
		resolution = "512P"
	}
	body, err = sjson.SetBytes(body, "resolution", resolution)
	return body, info, err
}
