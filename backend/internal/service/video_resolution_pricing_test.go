//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestVideoResolutionPricingPreservesIndependentTiers(t *testing.T) {
	for _, resolution := range []string{"768p", "2k"} {
		t.Run(resolution, func(t *testing.T) {
			normalized, ok := LookupVideoBillingResolution(resolution)
			require.True(t, ok)
			require.Equal(t, resolution, normalized)
		})
	}
	info, err := ParseMiniMaxVideoRequest([]byte(`{"model":"MiniMax-H3","prompt":"waves","resolution":"768P"}`))
	require.NoError(t, err)
	require.Equal(t, "768p", info.Resolution)
	info, err = ParseMiniMaxVideoRequest([]byte(`{"model":"MiniMax-H3","prompt":"waves","resolution":"2K"}`))
	require.NoError(t, err)
	require.Equal(t, "2k", info.Resolution)
}

func TestVideoResolutionPricingAuthCacheRoundtrip(t *testing.T) {
	price768, price2K := 0.12, 0.3
	key := profitAuthTestAPIKey()
	key.Group.VideoPrice768P, key.Group.VideoPrice2K = &price768, &price2K
	key.Group.VideoBillingMode = string(BillingModePerRequest)
	key.Group.VideoPricePerRequest = &price768
	svc := &APIKeyService{}
	snapshot := svc.snapshotFromAPIKey(context.Background(), key)
	data, err := json.Marshal(&APIKeyAuthCacheEntry{Snapshot: snapshot})
	require.NoError(t, err)
	var cached APIKeyAuthCacheEntry
	require.NoError(t, json.Unmarshal(data, &cached))
	restored, used, err := svc.applyAuthCacheEntry(key.Key, &cached)
	require.NoError(t, err)
	require.True(t, used)
	require.Equal(t, price768, *restored.Group.VideoPrice768P)
	require.Equal(t, price2K, *restored.Group.VideoPrice2K)
	require.Equal(t, string(BillingModePerRequest), restored.Group.VideoBillingMode)
	require.Equal(t, price768, *restored.Group.VideoPricePerRequest)
}

func TestVideoResolutionPricingFlatAndModelOverridesForAllModels(t *testing.T) {
	price720, price768, price2K := 0.07, 0.12, 0.30
	for _, model := range []string{"MiniMax-H3", "MiniMax-Hailuo-2.3", "veo-3.1", "doubao-seedance", "grok-imagine-video-1.5", "custom-video"} {
		group := &Group{VideoPrice720P: &price720, VideoPrice768P: &price768, VideoPrice2K: &price2K}
		for _, tc := range []struct {
			resolution string
			price      float64
		}{{"768P", price768}, {"2K", price2K}} {
			cost := (&BillingService{}).CalculateVideoCost(model, tc.resolution, 2, 6, group.VideoPriceConfig(), 1.5)
			require.InDelta(t, tc.price*2*6*1.5, cost.ActualCost, 1e-12, model+tc.resolution)
			group.VideoModelPrices = NormalizeVideoModelPrices(map[string]map[string]float64{model: {tc.resolution: 0.8}})
			cost = (&BillingService{}).CalculateVideoCost(model, tc.resolution, 2, 6, group.VideoPriceConfig(), 1.5)
			require.InDelta(t, 0.8*2*6*1.5, cost.ActualCost, 1e-12, model+tc.resolution)
		}
	}
	group := &Group{VideoPrice720P: &price720}
	require.Nil(t, group.GetVideoPrice("768p"))
	require.Nil(t, group.GetVideoPrice("2K"))
}

func TestVideoResolutionPricingZeroAndMissingTier(t *testing.T) {
	zero, price720 := 0.0, 0.07
	resolved := &ResolvedPricing{Mode: BillingModeVideo, RequestTiers: []PricingInterval{
		{TierLabel: "720p", PerRequestPrice: &price720},
		{TierLabel: "768P", PerRequestPrice: &zero},
	}}
	price, ok := resolved.VideoUnitPrice("768p")
	require.True(t, ok)
	require.Zero(t, price)
	_, ok = resolved.VideoUnitPrice("2k")
	require.False(t, ok)
	_, err := (&BillingService{}).CalculateCostUnified(CostInput{
		Resolved: resolved, Resolver: &ModelPricingResolver{}, SizeTier: "2K", UsageUnits: 6, RateMultiplier: 1,
	})
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	resolved.channelPricing = &ChannelModelPricing{PerRequestPrice: &zero}
	price, ok = resolved.VideoUnitPrice("2k")
	require.True(t, ok)
	require.Zero(t, price)
}

func TestVideoResolutionPricingRecordUsageFromChannel(t *testing.T) {
	for _, resolution := range []string{"768p", "2k"} {
		t.Run(resolution, func(t *testing.T) {
			const groupID = int64(148)
			price720, price768, price2K := 0.07, 0.12, 0.30
			repo := &openAIRecordUsageLogRepoStub{inserted: true}
			svc := newOpenAIRecordUsageServiceForTest(repo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
			resolver := newOpenAIImageChannelPricingResolverForTest(t, groupID, "custom-video", 0.5)
			pricing := resolver.channelService.cache.Load().(*channelCache).pricingByGroupModel[channelModelKey{groupID: groupID, model: "custom-video"}]
			pricing.BillingMode = BillingModeVideo
			pricing.Intervals = []PricingInterval{
				{TierLabel: "720p", PerRequestPrice: &price720},
				{TierLabel: "768P", PerRequestPrice: &price768},
				{TierLabel: "2K", PerRequestPrice: &price2K},
			}
			svc.resolver = resolver
			key := &APIKey{ID: 1, GroupID: i64p(groupID), Group: &Group{ID: groupID, Platform: PlatformVideo, RateMultiplier: 2}}
			require.True(t, svc.HasVideoTaskPricing(context.Background(), key, "custom-video", resolution))
			err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
				Result: &OpenAIForwardResult{RequestID: "task_" + resolution, Model: "custom-video", BillingModel: "custom-video", VideoCount: 1, VideoResolution: resolution, VideoDurationSeconds: 6, Duration: time.Second},
				APIKey: key, User: &User{ID: 2}, Account: &Account{ID: 3, Platform: PlatformVideo},
			})
			require.NoError(t, err)
			require.NotNil(t, repo.lastLog)
			require.Equal(t, resolution, *repo.lastLog.VideoResolution)
			price := price768
			if resolution == "2k" {
				price = price2K
			}
			require.InDelta(t, price*6*2, repo.lastLog.ActualCost, 1e-12)
		})
	}
}

func TestVideoResolutionPricingDoesNotBorrowOtherTier(t *testing.T) {
	price720, price768, price2K := 0.07, 0.12, 0.30
	resolved := &ResolvedPricing{Mode: BillingModeVideo, DefaultPerRequestPrice: 0.5, RequestTiers: []PricingInterval{
		{TierLabel: "720p", PerRequestPrice: &price720},
		{TierLabel: "768P", PerRequestPrice: &price768},
		{TierLabel: "2K", PerRequestPrice: &price2K},
	}}
	for _, tc := range []struct {
		resolution string
		price      float64
	}{{"768p", price768}, {"2k", price2K}, {"1080p", 0.5}} {
		cost, err := (&BillingService{}).CalculateCostUnified(CostInput{
			Resolved: resolved, Resolver: &ModelPricingResolver{}, SizeTier: tc.resolution,
			UsageUnits: 6, RateMultiplier: 2,
		})
		require.NoError(t, err)
		require.InDelta(t, tc.price*6*2, cost.ActualCost, 1e-12, tc.resolution)
	}
}
