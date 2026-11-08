package service

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

// Cost basis values of an estimate (contract §4.2).
const (
	CreationCostBasisImageTier          = "image_tier"
	CreationCostBasisImageTokenEstimate = "image_token_estimate"
	CreationCostBasisVideoReservation   = "video_reservation"

	CreationBalanceScopePersonal     = "personal"
	CreationBalanceScopeEnterprise   = "enterprise"
	CreationBalanceScopeSubscription = "subscription"
)

// CreationCostDetail is one line of the estimate breakdown.
type CreationCostDetail struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// CreationEstimateResponse mirrors the frozen contract §4.2 payload.
type CreationEstimateResponse struct {
	Priced           bool                 `json:"priced"`
	EstimatedCost    float64              `json:"estimated_cost"`
	CostBasis        string               `json:"cost_basis"`
	SelectedTier     string               `json:"selected_tier,omitempty"`
	PricingMode      string               `json:"pricing_mode"`
	PricingSource    string               `json:"pricing_source,omitempty"`
	UnitPrice        *float64             `json:"unit_price"`
	Detail           []CreationCostDetail `json:"detail"`
	BalanceScope     string               `json:"balance_scope"`
	AvailableBalance *float64             `json:"available_balance"`
	MinimumReserve   float64              `json:"minimum_reserve"`
	Sufficient       *bool                `json:"sufficient"`
	Currency         string               `json:"currency"`
}

// CreationImageCostEstimate is the shared result of the image pricing function
// used by both /estimate and the submit precheck (contract §4.2.1).
type CreationImageCostEstimate struct {
	Amount    float64
	Basis     string
	Tier      string
	Priced    bool
	Breakdown *CostBreakdown
}

// EstimateCreationImageCost prices one image submission at the selected size
// tier. It only composes the functions the executor already uses when it records
// usage, so the estimate and the final charge can never disagree on the tier,
// the multiplier or the price source. It has no side effects.
func (s *OpenAIGatewayService) EstimateCreationImageCost(
	ctx context.Context, apiKey *APIKey, userID int64,
	billingModel, size string, n, bodyBytes int,
) CreationImageCostEstimate {
	if n <= 0 {
		n = 1
	}
	estimate := CreationImageCostEstimate{}
	if s == nil || apiKey == nil {
		return estimate
	}
	// 1. Tier: the same resolver ApplyOpenAIImageBillingResolution uses. It maps
	// "" and "auto" to 2K and understands 1k/2k/4k plus pixel dimensions.
	tier := ResolveImageBillingSize(size, nil).BillingSize

	// 2. Multiplier: identical to RecordUsage (user/group/enterprise overrides,
	// then the peak-aware split which keeps image pricing off the peak factor).
	base := 1.0
	if s.cfg != nil {
		base = s.cfg.Default.RateMultiplier
	}
	if apiKey.Group != nil && apiKey.GroupID != nil {
		base = s.ResolveUserGroupRateMultiplier(ctx, userID, *apiKey.GroupID, apiKey.Group.RateMultiplier)
	}
	_, imageMultiplier := computePeakAwareMultipliers(apiKey, base, time.Now())

	// 3. Token-priced channels cannot be known before generation: fall back to
	// the gateway's own in-flight estimate, exactly as the executor does.
	if resolved := s.resolveOpenAIChannelPricing(ctx, billingModel, apiKey); resolved != nil && resolved.Mode == BillingModeToken {
		estimate.Basis = CreationCostBasisImageTokenEstimate
		estimate.Amount, estimate.Priced = s.EstimateInflightReservation(ctx, apiKey, InflightEstimateRequest{
			Model: billingModel, BodyBytes: bodyBytes, Kind: InflightEstimateImage, Units: n,
		})
		return creationNormalizeImageCostEstimate(estimate, billingModel, tier)
	}

	// 4. Per-image pricing at the selected tier, including group, channel,
	// ImagePriceConfig and default price sources.
	cost := s.calculateOpenAIImageCost(ctx, billingModel, apiKey, &OpenAIForwardResult{
		ImageSize: tier, ImageInputSize: size, ImageCount: n,
	}, imageMultiplier)
	estimate.Basis = CreationCostBasisImageTier
	estimate.Tier = tier
	if cost == nil {
		return estimate
	}
	// A resolved cost means the model is priced. An amount of exactly 0 is an
	// explicitly configured free model (group image_price_*, channel per-request
	// price or ImagePriceConfig set to 0) and must stay "priced": calculateOpenAIImageCost
	// never returns 0 for an unconfigured model, it falls back to the built-in
	// default price. Only a negative amount is impossible and is treated as
	// unpriced.
	estimate.Amount = cost.ActualCost
	estimate.Priced = true
	estimate.Breakdown = cost
	return creationNormalizeImageCostEstimate(estimate, billingModel, tier)
}

// creationNormalizeImageCostEstimate rejects impossible negative amounts, which
// would otherwise be handed to the reservation logic as a negative charge.
func creationNormalizeImageCostEstimate(estimate CreationImageCostEstimate, billingModel, tier string) CreationImageCostEstimate {
	if !estimate.Priced {
		return estimate
	}
	if estimate.Amount < 0 || math.IsNaN(estimate.Amount) || math.IsInf(estimate.Amount, 0) {
		logger.LegacyPrintf("service.creation_studio", "Warning: invalid image cost estimate for model %s tier %s: %v; treating as unpriced", billingModel, tier, estimate.Amount)
		return CreationImageCostEstimate{Basis: estimate.Basis, Priced: false}
	}
	return estimate
}

// creationOptionDefault returns the descriptor default of one parameter.
func creationOptionDefault(option CreationModelOption, key string) any {
	for _, descriptor := range option.Params {
		if descriptor.Key == key {
			return descriptor.Default
		}
	}
	return nil
}

// creationOptionParamValue prefers the submitted value and falls back to the
// descriptor default, so the estimate prices exactly what a submit would.
func creationOptionParamValue(option CreationModelOption, params map[string]any, key string) any {
	if params != nil {
		if raw, ok := params[key]; ok {
			return raw
		}
	}
	return creationOptionDefault(option, key)
}

// creationStudioImageSizeParam is the descriptor key that drives billing: the
// OpenAI protocol submits "size", the Grok protocol submits "resolution".
func creationStudioImageSizeParam(option CreationModelOption) string {
	if option.Protocol == CreationProtocolGrok {
		return "resolution"
	}
	return "size"
}

// creationStudioBodyBytes mirrors the weight of the replayed body for token
// priced channels: the serialized native body plus the uploaded image sizes.
// Inline image bytes cannot be known before they are read, so they are excluded;
// token pricing stays an estimate by definition (contract §11.3).
func creationStudioBodyBytes(option CreationModelOption, params map[string]any, prompt string, uploadBytes int64) int {
	probe := CreationNativeRequestInput{
		Mode: option.Mode, Protocol: option.Protocol, Model: option.Model, Prompt: prompt, Params: params,
	}
	native, err := BuildCreationNativeRequest(probe)
	if err != nil {
		return 0
	}
	return len(native.Body) + int(uploadBytes)
}

// EstimateCreation prices one submission without any side effect: no hold, no
// in-flight reservation, no Redis write, no upstream call, no object read.
func (s *OpenAIGatewayService) EstimateCreation(
	ctx context.Context, key *APIKey, request CreationEstimateRequest, option CreationModelOption, uploadBytes int64,
) (*CreationEstimateResponse, error) {
	if s == nil || key == nil {
		return nil, ErrCreationKeyNotFound
	}
	if !ValidCreationMode(request.Mode) {
		return nil, creationInvalidRequest("mode must be image or video")
	}
	if strings.TrimSpace(request.Model) == "" || request.Model != option.Model || request.Protocol != option.Protocol {
		return nil, ErrCreationModelNotAvailable
	}
	params, err := ResolveCreationParams(option, request.Params)
	if err != nil {
		return nil, err
	}
	minimumReserve := s.minimumBalanceReserve()
	response := &CreationEstimateResponse{
		CostBasis:      CreationCostBasisImageTier,
		PricingMode:    "image",
		Detail:         []CreationCostDetail{},
		BalanceScope:   creationBalanceScope(key),
		MinimumReserve: minimumReserve,
		Currency:       "USD",
	}
	if request.Mode == CreationModeImage {
		s.fillCreationImageEstimate(ctx, response, key, option, params, request, uploadBytes)
	} else if err := s.fillCreationVideoEstimate(ctx, response, key, option, params, request); err != nil {
		return nil, err
	}
	s.fillCreationBalance(ctx, response, key)
	return response, nil
}

// fillCreationImageEstimate prices the image branch with the shared function.
func (s *OpenAIGatewayService) fillCreationImageEstimate(
	ctx context.Context, response *CreationEstimateResponse, key *APIKey, option CreationModelOption,
	params map[string]any, request CreationEstimateRequest, uploadBytes int64,
) {
	sizeKey := creationStudioImageSizeParam(option)
	size := creationStringParam(map[string]any{sizeKey: creationOptionParamValue(option, params, sizeKey)}, sizeKey, "")
	n := creationIntParam(params, "n", 1)
	bodyBytes := creationStudioBodyBytes(option, params, "", uploadBytes)
	estimate := s.EstimateCreationImageCost(ctx, key, key.UserID, option.Model, size, n, bodyBytes)
	if !estimate.Priced {
		response.Priced = false
		response.CostBasis = estimate.Basis
		response.SelectedTier = estimate.Tier
		if response.CostBasis == "" {
			response.CostBasis = CreationCostBasisImageTier
		}
		return
	}
	response.Priced = true
	response.CostBasis = estimate.Basis
	response.SelectedTier = estimate.Tier
	response.EstimatedCost = estimate.Amount
	if estimate.Basis == CreationCostBasisImageTokenEstimate {
		response.PricingMode = string(BillingModeToken)
	} else {
		response.PricingMode = "image"
	}
	unit := estimate.Amount / float64(n)
	response.UnitPrice = &unit
	response.Detail = []CreationCostDetail{
		creationStudioDetail("count", strconv.Itoa(n)),
		creationStudioDetail("unit_price", creationStudioFormatFloat(unit)),
	}
	if estimate.Tier != "" {
		response.Detail = append(response.Detail, creationStudioDetail("resolution", estimate.Tier))
	}
	_ = request
}

// fillCreationVideoEstimate prices the video branch with the same contract the
// submit path freezes (VideoPricingContractInput of ReserveVideoTask).
func (s *OpenAIGatewayService) fillCreationVideoEstimate(
	ctx context.Context, response *CreationEstimateResponse, key *APIKey, option CreationModelOption,
	params map[string]any, request CreationEstimateRequest,
) error {
	model := option.Model
	if strings.TrimSpace(model) == "" {
		return creationInvalidRequest("model is required")
	}
	resolution := creationStringParam(params, "resolution", creationCandidateDefaultResolution(
		creationCandidate{mode: option.Mode, protocol: option.Protocol, model: option.Model}, videoModelLimits{}))
	duration := creationIntParam(params, "duration", 8)
	if duration <= 0 {
		return creationInvalidRequest("duration must be a positive integer")
	}
	maxTokens, err := ConservativeVideoCompletionTokenBound(duration, 1)
	if err != nil {
		return ErrCreationEstimateUnavailable.WithCause(err)
	}
	contract, err := s.BuildVideoPricingContract(ctx, VideoPricingContractInput{
		APIKey: key, User: key.User, Model: model, Resolution: resolution,
		MaxDurationSeconds: duration, MaxVideoCount: 1, MaxCompletionTokens: maxTokens,
		RequestedDurationSeconds:  float64(duration),
		RequireExplicitVideoPrice: option.Protocol == CreationProtocolGemini || option.Protocol == CreationProtocolMiniMax,
		AllowTokenFallback:        option.Protocol == CreationProtocolSeedance || legacyGroupTokenOverride(key, model),
		PricingAt:                 time.Now(),
		UpperBoundBasis:           "protocol duration/count maximum; token fallback <=4096x4096 pixels at 60fps /1024",
	})
	if err != nil {
		if errors.Is(err, ErrModelPricingUnavailable) {
			response.Priced = false
			response.CostBasis = CreationCostBasisVideoReservation
			response.PricingMode = string(BillingModeVideo)
			_ = request
			return nil
		}
		return ErrCreationEstimateUnavailable.WithCause(err)
	}
	unit := contract.UnitPrice
	response.Priced = true
	response.CostBasis = CreationCostBasisVideoReservation
	response.PricingMode = string(contract.Mode)
	response.PricingSource = contract.Source
	response.EstimatedCost = contract.ReservedAmount
	response.UnitPrice = &unit
	response.Detail = []CreationCostDetail{
		creationStudioDetail("duration", strconv.Itoa(duration)),
		creationStudioDetail("resolution", contract.Resolution),
		creationStudioDetail("unit_price", creationStudioFormatFloat(unit)),
		creationStudioDetail("multiplier", creationStudioFormatFloat(contract.EffectiveMultiplier)),
	}
	_ = request
	return nil
}

// creationBalanceScope classifies the payer the same way the gateway does.
func creationBalanceScope(key *APIKey) string {
	if key == nil {
		return CreationBalanceScopePersonal
	}
	if key.Group != nil && key.Group.IsSubscriptionType() {
		return CreationBalanceScopeSubscription
	}
	if key.User != nil && key.User.Enterprise != nil {
		return CreationBalanceScopeEnterprise
	}
	return CreationBalanceScopePersonal
}

// minimumBalanceReserve is the conservative preflight floor of the config.
func (s *OpenAIGatewayService) minimumBalanceReserve() float64 {
	if s == nil || s.cfg == nil {
		return 0
	}
	return s.cfg.Billing.MinimumBalanceReserve
}

// fillCreationBalance adds the personal balance verdict. Enterprises and
// subscriptions always report nil: their admission is decided on submit.
func (s *OpenAIGatewayService) fillCreationBalance(ctx context.Context, response *CreationEstimateResponse, key *APIKey) {
	response.MinimumReserve = s.minimumBalanceReserve()
	if response.BalanceScope != CreationBalanceScopePersonal {
		response.AvailableBalance = nil
		response.Sufficient = nil
		return
	}
	balance := 0.0
	if s.billingCacheService != nil && key != nil && key.UserID > 0 {
		if fetched, err := s.billingCacheService.GetUserBalance(ctx, key.UserID); err == nil {
			balance = fetched
		}
	}
	response.AvailableBalance = &balance
	sufficient := balance > 0 && balance >= response.MinimumReserve && balance >= response.EstimatedCost
	response.Sufficient = &sufficient
}

// creationStudioDetail builds one cost detail entry.
func creationStudioDetail(label, value string) CreationCostDetail {
	return CreationCostDetail{Label: label, Value: value}
}
