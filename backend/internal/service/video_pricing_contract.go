package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"
)

const VideoPricingContractVersion = 1

// VideoPricingContract is an immutable, JSON-persistable tariff. It contains
// values, never live group/channel references. Payer/routing snapshots belong
// to the durable reservation and are persisted in the same transaction.
type VideoPricingContract struct {
	Version                  int           `json:"version"`
	Model                    string        `json:"model"`
	Resolution               string        `json:"resolution"`
	Mode                     BillingMode   `json:"mode"`
	Source                   string        `json:"source"`
	UnitPrice                float64       `json:"unit_price"`
	TokenPricing             *ModelPricing `json:"token_pricing,omitempty"`
	PricingAt                time.Time     `json:"pricing_at"`
	BaseMultiplier           float64       `json:"base_multiplier"`
	PeakMultiplier           float64       `json:"peak_multiplier"`
	PeakApplied              bool          `json:"peak_applied"`
	VideoRateIndependent     bool          `json:"video_rate_independent"`
	ChannelTimeMultiplier    float64       `json:"channel_time_multiplier"`
	ReasoningEffort          string        `json:"reasoning_effort,omitempty"`
	ReasoningMultiplier      float64       `json:"reasoning_multiplier"`
	EffectiveMultiplier      float64       `json:"effective_multiplier"`
	MaxDurationSeconds       int           `json:"max_duration_seconds"`
	RequestedDurationSeconds float64       `json:"requested_duration_seconds,omitempty"`
	MaxVideoCount            int           `json:"max_video_count"`
	MaxCompletionTokens      int           `json:"max_completion_tokens,omitempty"`
	UpperBoundBasis          string        `json:"upper_bound_basis,omitempty"`
	ReservedAmount           float64       `json:"reserved_amount"`
	InboundEndpoint          string        `json:"inbound_endpoint,omitempty"`
	UpstreamEndpoint         string        `json:"upstream_endpoint,omitempty"`
}

type VideoPricingContractInput struct {
	APIKey                    *APIKey
	User                      *User
	Model                     string
	Resolution                string
	MaxDurationSeconds        int
	RequestedDurationSeconds  float64
	MaxVideoCount             int
	MaxCompletionTokens       int
	AllowTokenFallback        bool
	RequireExplicitVideoPrice bool
	PricingAt                 time.Time
	ReasoningEffort           string
	UpperBoundBasis           string
	InboundEndpoint           string
	UpstreamEndpoint          string
}

type VideoActualUsage struct {
	VideoCount           int
	DurationSeconds      int
	DurationSecondsExact float64
	CompletionTokens     int
}

// ConservativeVideoCompletionTokenBound is a deliberately conservative Seedance
// fallback: a 4096×4096 raster at 60 frames/s, one token per 1024 pixels/frame.
// The protocol must supply its maximum allowed duration and output count. An
// unknown bound is an error, never a free/fail-open reservation.
func ConservativeVideoCompletionTokenBound(maxDurationSeconds, maxVideoCount int) (int, error) {
	if maxDurationSeconds <= 0 || maxVideoCount <= 0 {
		return 0, fmt.Errorf("video token upper bound requires positive duration and count")
	}
	bound := math.Ceil((4096.0 * 4096.0 / 1024.0) * 60 * float64(maxDurationSeconds) * float64(maxVideoCount))
	if math.IsInf(bound, 0) || bound >= float64(math.MaxInt) {
		return 0, fmt.Errorf("video token upper bound overflows")
	}
	return int(bound), nil
}

func (s *OpenAIGatewayService) BuildVideoPricingContract(ctx context.Context, input VideoPricingContractInput) (*VideoPricingContract, error) {
	if s == nil || input.APIKey == nil || input.User == nil || strings.TrimSpace(input.Model) == "" {
		return nil, fmt.Errorf("video pricing requires key, payer and model")
	}
	if input.MaxDurationSeconds <= 0 || input.MaxVideoCount <= 0 {
		return nil, fmt.Errorf("video pricing requires a known positive usage upper bound")
	}
	if !validVideoContractNumber(input.RequestedDurationSeconds) || input.RequestedDurationSeconds > float64(input.MaxDurationSeconds) {
		return nil, fmt.Errorf("video requested duration must be within its upper bound")
	}
	at := input.PricingAt
	if at.IsZero() {
		at = time.Now()
	}
	resolution := strings.ToLower(strings.TrimSpace(input.Resolution))
	if normalized, known := LookupVideoBillingResolution(resolution); known {
		resolution = normalized
	}
	key := s.apiKeyWithFreshGroupMediaPricing(ctx, input.APIKey)
	base := 1.0
	if s.cfg != nil {
		base = s.cfg.Default.RateMultiplier
	}
	if key.Group != nil && key.GroupID != nil {
		// The effective repository already resolves enterprise overrides before
		// personal overrides; do not substitute group.RateMultiplier here.
		base = s.ResolveUserGroupRateMultiplier(ctx, input.User.ID, *key.GroupID, key.Group.RateMultiplier)
	}
	peak := 1.0
	if key.Group != nil {
		peak = key.Group.PeakMultiplierAt(at)
	}
	contract := &VideoPricingContract{
		Version: VideoPricingContractVersion, Model: strings.TrimSpace(input.Model), Resolution: resolution,
		PricingAt: at, BaseMultiplier: base, PeakMultiplier: peak,
		ChannelTimeMultiplier: 1, ReasoningEffort: input.ReasoningEffort, ReasoningMultiplier: 1,
		MaxDurationSeconds: input.MaxDurationSeconds, MaxVideoCount: input.MaxVideoCount,
		RequestedDurationSeconds: input.RequestedDurationSeconds,
		MaxCompletionTokens:      input.MaxCompletionTokens, UpperBoundBasis: input.UpperBoundBasis,
		InboundEndpoint: input.InboundEndpoint, UpstreamEndpoint: input.UpstreamEndpoint,
	}
	if key.Group != nil {
		contract.VideoRateIndependent = key.Group.VideoRateIndependent
	}
	if price := resolveVideoTaskPrice(ctx, s.resolver, key, input.Model, resolution); price != nil {
		contract.Mode, contract.UnitPrice, contract.Source = price.mode, price.unitPrice, price.source
		contract.ReasoningMultiplier = reasoningEffortBillingMultiplier(input.ReasoningEffort, price.reasoningEffortMultipliers)
		contract.EffectiveMultiplier = resolveVideoRateMultiplier(key, base) * contract.ReasoningMultiplier
	} else {
		if input.RequireExplicitVideoPrice {
			return nil, fmt.Errorf("%w: explicit video tariff required", ErrModelPricingUnavailable)
		}
		// A missing/unknown resolution may only use an explicit default video
		// price. It must not silently borrow another tier or token pricing.
		if _, known := LookupVideoBillingResolution(resolution); !known {
			return nil, fmt.Errorf("%w: no explicit video default for resolution %q", ErrModelPricingUnavailable, resolution)
		}
		legacyTokenOverride := key.Group != nil && key.Group.Platform != PlatformVideo && legacyGroupTokenOverride(key, input.Model)
		if !legacyTokenOverride {
			if unit, known := getDefaultGrokImagineVideoPrice(input.Model, resolution); known && resolution != VideoBillingResolution768P && resolution != VideoBillingResolution2K {
				contract.Mode, contract.UnitPrice, contract.Source = BillingModeVideo, unit, PricingSourceFallback
				contract.EffectiveMultiplier = resolveVideoRateMultiplier(key, base)
			}
		}
		if contract.Mode == "" {
			if !input.AllowTokenFallback || input.MaxCompletionTokens <= 0 {
				return nil, fmt.Errorf("%w: video tariff or bounded token fallback required", ErrModelPricingUnavailable)
			}
			if err := s.freezeVideoTokenPricing(ctx, contract, key); err != nil {
				return nil, err
			}
		}
	}
	if !validVideoContractNumber(contract.UnitPrice) || !validVideoContractNumber(contract.EffectiveMultiplier) {
		return nil, fmt.Errorf("invalid video contract tariff or multiplier")
	}
	upper, err := contract.uncappedActualCost(VideoActualUsage{VideoCount: input.MaxVideoCount, DurationSeconds: input.MaxDurationSeconds, CompletionTokens: input.MaxCompletionTokens})
	if err != nil || upper == nil || !validVideoContractNumber(upper.ActualCost) {
		return nil, fmt.Errorf("invalid video cost upper bound: %w", err)
	}
	// Reserve in the database's 8-decimal money precision, rounding upwards.
	// Remove one binary arithmetic ULP before ceiling: 0.1*6 must reserve
	// $0.60 rather than $0.60000001 and reject an exactly funded wallet.
	scaled := upper.ActualCost * 1e8
	if !validVideoContractNumber(scaled) {
		return nil, fmt.Errorf("video reserved amount overflows")
	}
	if scaled > 0 {
		scaled = math.Nextafter(scaled, math.Inf(-1))
	}
	contract.ReservedAmount = math.Ceil(scaled) / 1e8
	return contract, nil
}

func legacyGroupTokenOverride(key *APIKey, model string) bool {
	if key == nil || key.Group == nil || key.Group.Platform == PlatformVideo {
		return false
	}
	card := matchGroupModelPricing(key.Group, model)
	return card != nil && (card.BillingMode == "" || card.BillingMode == BillingModeToken)
}

func (s *OpenAIGatewayService) freezeVideoTokenPricing(ctx context.Context, contract *VideoPricingContract, key *APIKey) error {
	if s.billingService == nil {
		return fmt.Errorf("%w: token pricing service unavailable", ErrModelPricingUnavailable)
	}
	resolver := s.resolver
	if resolver == nil {
		resolver = NewModelPricingResolver(nil, s.billingService)
	}
	resolved := resolver.Resolve(ctx, PricingInput{Model: contract.Model, GroupID: key.GroupID, Group: key.Group})
	if resolved == nil || (resolved.Mode != "" && resolved.Mode != BillingModeToken) {
		return fmt.Errorf("%w: video token fallback card unavailable", ErrModelPricingUnavailable)
	}
	// Seedance usage is completion_tokens only, so input/context tokens are
	// always zero. Freeze the same context interval the live token path uses.
	pricingContext := 0
	if !resolved.longContextPricingEnabled {
		pricingContext = 1
	}
	pricing := resolver.GetIntervalPricing(resolved, pricingContext)
	if pricing == nil {
		return fmt.Errorf("%w: video token fallback card unavailable", ErrModelPricingUnavailable)
	}
	pricing = s.billingService.applyModelSpecificPricingPolicyEx(contract.Model, pricing, resolved.Source == PricingSourceLiteLLM, contract.PricingAt)
	// Deep-copy every pointer/map. Updating a cached price card after acceptance
	// must not mutate the persisted or in-memory accepted contract.
	encoded, err := json.Marshal(pricing)
	if err != nil {
		return fmt.Errorf("invalid token fallback card: %w", err)
	}
	var frozen ModelPricing
	if err := json.Unmarshal(encoded, &frozen); err != nil {
		return err
	}
	if resolved.Source == PricingSourceLiteLLM && isDeepSeekModel(contract.Model) {
		factor := deepseekPeakMultiplierAt(contract.PricingAt)
		frozen.InputPricePerToken *= factor
		frozen.OutputPricePerToken *= factor
		frozen.CacheReadPricePerToken *= factor
	}
	if !validVideoContractNumber(frozen.OutputPricePerToken) {
		return fmt.Errorf("invalid video output token price")
	}
	contract.Mode, contract.Source, contract.TokenPricing = BillingModeToken, resolved.Source, &frozen
	contract.UnitPrice = frozen.OutputPricePerToken
	contract.PeakApplied = true
	contract.ChannelTimeMultiplier = resolvedChannelTimeMultiplier(resolved, contract.PricingAt)
	contract.ReasoningMultiplier = reasoningEffortBillingMultiplier(contract.ReasoningEffort, frozen.ReasoningEffortMultipliers)
	contract.EffectiveMultiplier = contract.BaseMultiplier * contract.PeakMultiplier * contract.ChannelTimeMultiplier * contract.ReasoningMultiplier
	return nil
}

func validVideoContractNumber(v float64) bool { return v >= 0 && !math.IsNaN(v) && !math.IsInf(v, 0) }

func (c *VideoPricingContract) uncappedActualCost(usage VideoActualUsage) (*CostBreakdown, error) {
	if c == nil || c.Version != VideoPricingContractVersion || !validVideoContractNumber(c.UnitPrice) || !validVideoContractNumber(c.EffectiveMultiplier) {
		return nil, fmt.Errorf("invalid video pricing contract")
	}
	if usage.VideoCount < 0 || usage.DurationSeconds < 0 || usage.CompletionTokens < 0 || !validVideoContractNumber(usage.DurationSecondsExact) {
		return nil, fmt.Errorf("invalid negative video usage")
	}
	var total float64
	switch c.Mode {
	case BillingModePerRequest:
		total = c.UnitPrice
	case BillingModeVideo, BillingModeImage:
		count := usage.VideoCount
		if count <= 0 {
			count = c.MaxVideoCount
		}
		duration := float64(usage.DurationSeconds)
		if usage.DurationSecondsExact > 0 {
			duration = usage.DurationSecondsExact
		}
		if duration <= 0 {
			duration = c.RequestedDurationSeconds
			if duration <= 0 {
				duration = float64(c.MaxDurationSeconds)
			}
		}
		total = c.UnitPrice * float64(count)
		if c.Mode == BillingModeVideo {
			total *= duration
		}
	case BillingModeToken:
		if c.TokenPricing == nil {
			return nil, fmt.Errorf("video token contract card missing")
		}
		total = c.TokenPricing.OutputPricePerToken * float64(usage.CompletionTokens)
	default:
		return nil, fmt.Errorf("invalid video pricing mode %q", c.Mode)
	}
	actual := total * c.EffectiveMultiplier
	// These tariff modifiers affect raw account cost as well as the customer
	// charge. The effective customer multiplier already includes them.
	total *= c.ChannelTimeMultiplier * c.ReasoningMultiplier
	if !validVideoContractNumber(total) || !validVideoContractNumber(actual) {
		return nil, fmt.Errorf("video cost overflow")
	}
	cost := &CostBreakdown{TotalCost: total, ActualCost: actual, BillingMode: string(c.Mode)}
	if c.Mode == BillingModeToken {
		cost.OutputCost = total
	}
	return cost, nil
}

// CalculateActualCost evaluates only frozen values and caps the customer charge
// at the durable hold. TotalCost remains actual upstream/accounting cost.
func (c *VideoPricingContract) CalculateActualCost(usage VideoActualUsage) (*CostBreakdown, error) {
	cost, err := c.uncappedActualCost(usage)
	if err != nil {
		return nil, err
	}
	if !validVideoContractNumber(c.ReservedAmount) {
		return nil, fmt.Errorf("invalid video reserved amount")
	}
	if cost.ActualCost > c.ReservedAmount {
		if cost.ActualCost > math.Nextafter(c.ReservedAmount, math.Inf(1)) {
			slog.Warn("video_billing_actual_cost_exceeds_reservation", "model", c.Model, "resolution", c.Resolution, "actual_cost", cost.ActualCost, "reserved_amount", c.ReservedAmount)
		}
		cost.ActualCost = c.ReservedAmount
	}
	return cost, nil
}

// BuildUsageLog provides the immutable economic fields for a durable settlement.
// The caller adds payer/key/account IDs, endpoints and enterprise identity from
// the reservation snapshot, then commits the log and funds atomically.
func (c *VideoPricingContract) BuildUsageLog(requestID string, usage VideoActualUsage) (*UsageLog, error) {
	cost, err := c.CalculateActualCost(usage)
	if err != nil {
		return nil, err
	}
	mode, resolution := string(c.Mode), c.Resolution
	duration := usage.DurationSeconds
	if usage.DurationSecondsExact > 0 {
		duration = int(math.Ceil(usage.DurationSecondsExact))
	}
	if duration <= 0 {
		duration = int(math.Ceil(c.RequestedDurationSeconds))
		if duration <= 0 {
			duration = c.MaxDurationSeconds
		}
	}
	count := usage.VideoCount
	if count <= 0 {
		count = c.MaxVideoCount
	}
	log := &UsageLog{
		RequestID: requestID, Model: c.Model, RequestedModel: c.Model,
		VideoCount: count, VideoResolution: &resolution, VideoDurationSeconds: &duration,
		OutputTokens: usage.CompletionTokens, OutputCost: cost.OutputCost,
		TotalCost: cost.TotalCost, ActualCost: cost.ActualCost,
		RateMultiplier: c.EffectiveMultiplier, BillingMode: &mode, CreatedAt: time.Now(),
		InboundEndpoint: optionalTrimmedStringPtr(c.InboundEndpoint), UpstreamEndpoint: optionalTrimmedStringPtr(c.UpstreamEndpoint),
	}
	if exact := usage.DurationSecondsExact; exact > 0 {
		tier := "duration_seconds=" + strconv.FormatFloat(exact, 'g', -1, 64)
		log.BillingTier = &tier
	} else if c.RequestedDurationSeconds > 0 && c.RequestedDurationSeconds != math.Trunc(c.RequestedDurationSeconds) && usage.DurationSeconds <= 0 {
		tier := "duration_seconds=" + strconv.FormatFloat(c.RequestedDurationSeconds, 'g', -1, 64)
		log.BillingTier = &tier
	}
	return log, nil
}
