package service

import (
	"context"
	"fmt"
	"math"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

func NormalizeVideoGroupBillingMode(mode string) string {
	if mode == "" {
		return string(BillingModeVideo)
	}
	return mode
}

// ValidateVideoGroupFallbackPricing requires a complete fallback independently
// of the models currently covered by a channel. Zero is an explicit free price.
func ValidateVideoGroupFallbackPricing(group *Group) error {
	if group == nil || group.Platform != PlatformVideo {
		return nil
	}
	mode := NormalizeVideoGroupBillingMode(group.VideoBillingMode)
	if mode != string(BillingModeVideo) && mode != string(BillingModePerRequest) {
		return infraerrors.BadRequest("INVALID_VIDEO_BILLING_MODE", "video_billing_mode must be video or per_request")
	}
	prices := []struct {
		label string
		price *float64
	}{{"per_request", group.VideoPricePerRequest}}
	if mode == string(BillingModeVideo) {
		prices = []struct {
			label string
			price *float64
		}{
			{"480p", group.VideoPrice480P}, {"720p", group.VideoPrice720P},
			{"768p", group.VideoPrice768P}, {"1080p", group.VideoPrice1080P}, {"2K", group.VideoPrice2K},
		}
	}
	var missing []string
	for _, item := range prices {
		if item.price == nil || *item.price < 0 || math.IsNaN(*item.price) || math.IsInf(*item.price, 0) {
			missing = append(missing, item.label)
		}
	}
	if len(missing) > 0 {
		return infraerrors.BadRequest("VIDEO_FALLBACK_PRICE_REQUIRED", fmt.Sprintf("Video group fallback prices are required (%s); enter a non-negative USD price for each required field", strings.Join(missing, ", ")))
	}
	return nil
}

type videoTaskPrice struct {
	mode                       BillingMode
	unitPrice                  float64
	source                     string
	reasoningEffortMultipliers map[string]float64
}

func (p *videoTaskPrice) cost(videoCount, duration int, multiplier float64, reasoningEffort ...string) *CostBreakdown {
	units := 1 // per_request bills one successfully completed task, regardless of duration or output count.
	if p.mode != BillingModePerRequest {
		units = max(1, videoCount)
	}
	if p.mode == BillingModeVideo {
		units *= NormalizeVideoBillingDurationSecondsOrDefault(duration)
	}
	total := p.unitPrice * float64(units)
	cost := &CostBreakdown{TotalCost: total, ActualCost: total * math.Max(0, multiplier), BillingMode: string(p.mode)}
	if len(reasoningEffort) > 0 {
		applyCostBreakdownMultiplier(cost, reasoningEffortBillingMultiplier(reasoningEffort[0], p.reasoningEffortMultipliers))
	}
	return cost
}

func configuredVideoTaskPrice(config *ChannelModelPricing, resolution, source string) *videoTaskPrice {
	if config == nil {
		return nil
	}
	mode := config.BillingMode
	if mode != BillingModeVideo && mode != BillingModePerRequest {
		return nil
	}
	// Resolve only this resolution or an explicitly configured default. A zero
	// tier must remain free even when another tier/default has a positive price.
	var price *float64
	requestedTier, requestedKnown := LookupVideoBillingResolution(resolution)
	for _, tier := range config.Intervals {
		label, known := LookupVideoBillingResolution(tier.TierLabel)
		if requestedKnown && known && label == requestedTier && tier.PerRequestPrice != nil {
			price = tier.PerRequestPrice
			break
		}
	}
	if price == nil {
		price = config.PerRequestPrice
	}
	if price == nil && mode != BillingModeVideo {
		for _, tier := range config.Intervals {
			if strings.TrimSpace(tier.TierLabel) == "" && tier.PerRequestPrice != nil {
				price = tier.PerRequestPrice
				break
			}
		}
	}
	if price == nil || *price < 0 || math.IsNaN(*price) || math.IsInf(*price, 0) {
		return nil
	}
	return &videoTaskPrice{mode: mode, unitPrice: *price, source: source, reasoningEffortMultipliers: config.ReasoningEffortMultipliers}
}

// resolveVideoTaskPrice is shared by submission, completion and reservation.
// Explicit channel prices take precedence; group model prices and group fallback
// prices cover models/resolutions that the channel does not price.
func resolveVideoTaskPrice(ctx context.Context, resolver *ModelPricingResolver, key *APIKey, model, resolution string) *videoTaskPrice {
	if key == nil || key.Group == nil {
		return nil
	}
	group := key.Group
	// Existing Grok/OpenAI groups retain their group override policy. The video
	// platform uses channel prices first and requires group fallback prices.
	if group.Platform != PlatformVideo {
		// A legacy group token card is an explicit override of the entire
		// billing mode, not merely a missing video price.
		if card := matchGroupModelPricing(group, model); card != nil && (card.BillingMode == "" || card.BillingMode == BillingModeToken) {
			return nil
		}
		if price := groupVideoTaskPrice(group, model, resolution); price != nil {
			return price
		}
	}
	if resolver != nil {
		if price := configuredVideoTaskPrice(resolver.lookupChannelPricingNormalized(ctx, group.ID, model), resolution, PricingSourceChannel); price != nil {
			return price
		}
	}
	return groupVideoTaskPrice(group, model, resolution)
}

func groupVideoTaskPrice(group *Group, model, resolution string) *videoTaskPrice {
	if group == nil {
		return nil
	}
	if price := configuredVideoTaskPrice(matchGroupModelPricing(group, model), resolution, PricingSourceGroup); price != nil {
		return price
	}
	if group.Platform == PlatformVideo && NormalizeVideoGroupBillingMode(group.VideoBillingMode) == string(BillingModePerRequest) {
		if price := group.VideoPricePerRequest; price != nil && *price >= 0 {
			return &videoTaskPrice{mode: BillingModePerRequest, unitPrice: *price, source: PricingSourceGroup}
		}
		return nil
	}
	if price := group.GetVideoPriceForModel(model, resolution); price != nil && *price >= 0 {
		return &videoTaskPrice{mode: BillingModeVideo, unitPrice: *price, source: PricingSourceGroup}
	}
	return nil
}

func (s *OpenAIGatewayService) VideoTaskBillingMode(ctx context.Context, key *APIKey, model, resolution string) BillingMode {
	if price := resolveVideoTaskPrice(ctx, s.resolver, key, model, resolution); price != nil {
		return price.mode
	}
	return ""
}
