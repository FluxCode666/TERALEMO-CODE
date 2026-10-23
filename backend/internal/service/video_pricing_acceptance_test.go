//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Regression assertions converted from the W2 acceptance probes.
func TestAcceptancePricingLegacyGroupTokenOverrideVideoChannel(t *testing.T) {
	ctx := context.Background()
	const groupID = int64(9901)
	const model = "doubao-seedance"
	outputPrice, channelPrice := 0.000002, 0.4
	repo := &openAIRecordUsageLogRepoStub{inserted: true}
	svc := newOpenAIRecordUsageServiceForTest(repo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
	svc.resolver = newOpenAIImageChannelPricingResolverForTest(t, groupID, model, channelPrice)
	channelPricing := svc.resolver.channelService.cache.Load().(*channelCache).pricingByGroupModel[channelModelKey{groupID: groupID, model: model}]
	require.NotNil(t, channelPricing)
	channelPricing.BillingMode = BillingModeVideo
	key := &APIKey{ID: 9901, GroupID: i64p(groupID), Group: &Group{
		ID: groupID, Platform: PlatformOpenAI, RateMultiplier: 1,
		ModelPricing: []ChannelModelPricing{{Models: []string{model}, BillingMode: BillingModeToken, OutputPrice: &outputPrice}},
	}}
	// Establish that the configured legacy group override really is the selected
	// token card, before checking the subsequent RecordUsage branch.
	resolved := svc.resolveOpenAIChannelPricing(ctx, model, key)
	require.NotNil(t, resolved)
	require.Equal(t, PricingSourceGroup, resolved.Source)
	require.Equal(t, BillingModeToken, resolved.Mode)
	err := svc.RecordUsage(ctx, &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{
			RequestID: "accept-legacy-token", Model: model, BillingModel: model,
			VideoCount: 1, VideoResolution: "720p", VideoDurationSeconds: 6,
			Usage: OpenAIUsage{OutputTokens: 1000}, Duration: time.Second,
		},
		APIKey: key, User: &User{ID: 9901}, Account: &Account{ID: 9901, Platform: PlatformOpenAI},
	})
	require.NoError(t, err)
	require.NotNil(t, repo.lastLog)
	require.NotNil(t, repo.lastLog.BillingMode)
	correctCost, defectCost := outputPrice*1000, channelPrice*6
	t.Logf("resolved=%s/%s observed_cost=%.12f observed_mode=%s correct_cost=%.12f defect_cost=%.12f", resolved.Source, resolved.Mode, repo.lastLog.ActualCost, *repo.lastLog.BillingMode, correctCost, defectCost)
	require.InDelta(t, correctCost, repo.lastLog.ActualCost, 1e-12, "legacy group token override must retain priority")
	require.Equal(t, string(BillingModeToken), *repo.lastLog.BillingMode)
}

func TestAcceptancePricingAlias2KDoesNotBecome480P(t *testing.T) {
	ctx := context.Background()
	const groupID = int64(9902)
	const model = "cinema-public"
	// Control: the canonical model prefix preserves the explicitly requested 2K.
	canonical := ParseGrokMediaRequest("application/json", []byte(`{"model":"grok-imagine-video-1.5","resolution":"2K","duration":6,"prompt":"waves"}`))
	require.Equal(t, VideoBillingResolution2K, canonical.Resolution)
	info := ParseGrokMediaRequest("application/json", []byte(`{"model":"cinema-public","resolution":"2K","duration":6,"prompt":"waves"}`))
	low, high := 0.1, 0.9
	repo := &openAIRecordUsageLogRepoStub{inserted: true}
	svc := newOpenAIRecordUsageServiceForTest(repo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
	key := &APIKey{ID: 9902, GroupID: i64p(groupID), Group: &Group{
		ID: groupID, Platform: PlatformGrok, RateMultiplier: 1,
		VideoPrice480P: &low, VideoPrice2K: &high,
	}}
	// The handler persists info.Resolution as the create-time resolution and
	// restores it for completion billing. Feed that same parsed value through
	// the real RecordUsage path; no upstream request or database is needed.
	err := svc.RecordUsage(ctx, &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{
			RequestID: "accept-alias-2k", Model: model, BillingModel: model,
			VideoCount: 1, VideoResolution: info.Resolution, VideoDurationSeconds: info.DurationSeconds,
			Duration: time.Second,
		},
		APIKey: key, User: &User{ID: 9902}, Account: &Account{ID: 9902, Platform: PlatformGrok},
	})
	require.NoError(t, err)
	require.NotNil(t, repo.lastLog)
	require.NotNil(t, repo.lastLog.VideoResolution)
	correctCost, defectCost := high*6, low*6
	t.Logf("model=%s native_resolution=2K canonical_parsed_resolution=%s alias_parsed_resolution=%s usage_resolution=%s observed_cost=%.12f correct_cost=%.12f defect_cost=%.12f", model, canonical.Resolution, info.Resolution, *repo.lastLog.VideoResolution, repo.lastLog.ActualCost, correctCost, defectCost)
	require.Equal(t, VideoBillingResolution2K, info.Resolution, "video endpoint aliases must preserve explicit 2K")
	require.Equal(t, VideoBillingResolution2K, *repo.lastLog.VideoResolution)
	require.InDelta(t, correctCost, repo.lastLog.ActualCost, 1e-12)
}

func TestAcceptancePricingSeedanceUnknownResolutionDoesNotBorrow480P(t *testing.T) {
	info, err := ParseSeedanceRequest([]byte(`{"model":"doubao-seedance","content":[{"type":"text","text":"waves"}],"resolution":"4k","duration":6}`))
	require.Error(t, err, "an unsupported upstream tier must be rejected instead of falling back to a cheaper tier")
	// Keep the pricing-layer regression independent of request validation:
	// internal callers must still not borrow a different tier for an unknown one.
	info.Resolution, info.DurationSeconds = "4k", 6
	low, fallback := 0.1, 0.9
	config := &ChannelModelPricing{
		BillingMode: BillingModeVideo, PerRequestPrice: &fallback,
		Intervals: []PricingInterval{{TierLabel: "480p", PerRequestPrice: &low}},
	}
	price := configuredVideoTaskPrice(config, info.Resolution, PricingSourceChannel)
	require.NotNil(t, price)
	cost := price.cost(1, info.DurationSeconds, 1)
	correctCost, defectCost := fallback*6, low*6
	t.Logf("parsed_resolution=%s observed_unit_price=%.12f explicit_default=%.12f observed_cost=%.12f correct_cost=%.12f defect_cost=%.12f", info.Resolution, price.unitPrice, fallback, cost.ActualCost, correctCost, defectCost)
	require.Equal(t, fallback, price.unitPrice, "unmatched resolution must use only the explicit default, not 480p")
	require.InDelta(t, correctCost, cost.ActualCost, 1e-12)
}
