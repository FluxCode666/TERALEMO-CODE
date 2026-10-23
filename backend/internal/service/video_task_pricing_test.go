//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestVideoGroupFallbackRequiredOnCreateAndUpdate(t *testing.T) {
	ctx := context.Background()
	repo := &groupRepoStubForAdmin{}
	svc := &adminServiceImpl{groupRepo: repo}
	input := &CreateGroupInput{Name: "video", Platform: PlatformVideo, RateMultiplier: 1}
	_, err := svc.CreateGroup(ctx, input)
	require.Equal(t, "VIDEO_FALLBACK_PRICE_REQUIRED", infraerrors.Reason(err))
	require.Contains(t, err.Error(), "768p")
	require.Nil(t, repo.created)

	price, zero, clear := 0.3, 0.0, -1.0
	input.VideoBillingMode = string(BillingModePerRequest)
	input.VideoPricePerRequest = &price
	group, err := svc.CreateGroup(ctx, input)
	require.NoError(t, err)
	require.Equal(t, string(BillingModePerRequest), group.VideoBillingMode)
	require.Equal(t, price, *group.VideoPricePerRequest)

	repo.getByID = group
	_, err = svc.UpdateGroup(ctx, 1, &UpdateGroupInput{VideoPricePerRequest: &clear})
	require.Equal(t, "VIDEO_FALLBACK_PRICE_REQUIRED", infraerrors.Reason(err))
	require.Nil(t, repo.updated)
	group.VideoPricePerRequest = &price
	_, err = svc.UpdateGroup(ctx, 1, &UpdateGroupInput{VideoBillingMode: string(BillingModeVideo)})
	require.Equal(t, "VIDEO_FALLBACK_PRICE_REQUIRED", infraerrors.Reason(err))
	require.Nil(t, repo.updated)
	group.VideoBillingMode = string(BillingModePerRequest)
	_, err = svc.UpdateGroup(ctx, 1, &UpdateGroupInput{VideoPricePerRequest: &zero})
	require.NoError(t, err)
	require.Zero(t, *repo.updated.VideoPricePerRequest)

	input.VideoBillingMode = string(BillingModeVideo)
	input.VideoPrice480P, input.VideoPrice720P, input.VideoPrice768P = &price, &price, &zero
	input.VideoPrice1080P, input.VideoPrice2K = &price, &price
	_, err = svc.CreateGroup(ctx, input)
	require.NoError(t, err)
	input.VideoPrice2K = nil
	_, err = svc.CreateGroup(ctx, input)
	require.Equal(t, "VIDEO_FALLBACK_PRICE_REQUIRED", infraerrors.Reason(err))
	require.Contains(t, err.Error(), "2K")
	input.VideoBillingMode = "unknown"
	_, err = svc.CreateGroup(ctx, input)
	require.Equal(t, "INVALID_VIDEO_BILLING_MODE", infraerrors.Reason(err))
}

func TestVideoTaskPricingChannelPriorityAndGroupFallback(t *testing.T) {
	ctx := context.Background()
	const groupID = int64(249)
	channel, fallback, zero := 0.4, 2.0, 0.0
	resolver := newOpenAIImageChannelPricingResolverForTest(t, groupID, "minimax-h3", channel)
	pricing := resolver.channelService.cache.Load().(*channelCache).pricingByGroupModel[channelModelKey{groupID: groupID, model: "minimax-h3"}]
	pricing.BillingMode = BillingModePerRequest
	key := &APIKey{ID: 1, GroupID: i64p(groupID), User: &User{ID: 2}, Group: &Group{
		ID: groupID, Platform: PlatformVideo, RateMultiplier: 2,
		VideoPrice768P: &fallback, VideoPrice2K: &fallback,
		ModelPricing: []ChannelModelPricing{{Models: []string{"MiniMax-*"}, BillingMode: BillingModeVideo, PerRequestPrice: &fallback}},
	}}
	svc := &OpenAIGatewayService{resolver: resolver}
	require.True(t, svc.HasVideoTaskPricing(ctx, key, "MiniMax-H3", "768P"))
	price := resolveVideoTaskPrice(ctx, resolver, key, "MiniMax-H3", "768P")
	require.Equal(t, PricingSourceChannel, price.source)
	require.Equal(t, BillingModePerRequest, price.mode)
	require.InDelta(t, channel*2, price.cost(3, 12, 2).ActualCost, 1e-12)
	pricing.PerRequestPrice = &zero
	require.Zero(t, resolveVideoTaskPrice(ctx, resolver, key, "MiniMax-H3", "768P").cost(3, 12, 2).ActualCost)

	pricing.PerRequestPrice = nil
	pricing.BillingMode = BillingModeVideo
	pricing.Intervals = []PricingInterval{{TierLabel: "768P", PerRequestPrice: &channel}}
	price = resolveVideoTaskPrice(ctx, resolver, key, "MiniMax-H3", "768p")
	require.Equal(t, PricingSourceChannel, price.source)
	require.InDelta(t, channel*3*12*2, price.cost(3, 12, 2).ActualCost, 1e-12)
	price = resolveVideoTaskPrice(ctx, resolver, key, "MiniMax-H3", "2K")
	require.Equal(t, PricingSourceGroup, price.source)
	require.Equal(t, fallback, price.unitPrice)
	key.Group.ModelPricing = nil
	key.Group.VideoBillingMode, key.Group.VideoPricePerRequest = string(BillingModePerRequest), &fallback
	price = resolveVideoTaskPrice(ctx, resolver, key, "MiniMax-H3", "2K")
	require.Equal(t, BillingModePerRequest, price.mode)
	require.InDelta(t, fallback*2, price.cost(3, 12, 2).ActualCost, 1e-12)
	deps := inflightEstimateDeps{resolver: resolver}
	cost := deps.estimateOne(ctx, key, "MiniMax-H3", InflightEstimateRequest{Kind: InflightEstimateVideo, Units: 3, VideoResolution: "2K", VideoDurationSeconds: 12}, 2, 2)
	require.InDelta(t, fallback*2, cost, 1e-12)
}

func TestVideoTaskPerRequestRecordUsage(t *testing.T) {
	for _, source := range []string{PricingSourceGroup, PricingSourceChannel} {
		t.Run(source, func(t *testing.T) {
			const groupID = int64(250)
			channelPrice, fallback := 0.4, 2.0
			repo := &openAIRecordUsageLogRepoStub{inserted: true}
			svc := newOpenAIRecordUsageServiceForTest(repo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
			key := &APIKey{ID: 1, GroupID: i64p(groupID), Group: &Group{ID: groupID, Platform: PlatformVideo, RateMultiplier: 2,
				VideoBillingMode: string(BillingModePerRequest), VideoPricePerRequest: &fallback, VideoPrice768P: &fallback}}
			expected := fallback
			if source == PricingSourceChannel {
				svc.resolver = newOpenAIImageChannelPricingResolverForTest(t, groupID, "minimax-h3", channelPrice)
				pricing := svc.resolver.channelService.cache.Load().(*channelCache).pricingByGroupModel[channelModelKey{groupID: groupID, model: "minimax-h3"}]
				pricing.BillingMode = BillingModePerRequest
				expected = channelPrice
			}
			err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
				Result: &OpenAIForwardResult{RequestID: "task_per_request", Model: "MiniMax-H3", BillingModel: "MiniMax-H3", VideoCount: 3, VideoResolution: "768p", VideoDurationSeconds: 12, Duration: time.Second},
				APIKey: key, User: &User{ID: 2}, Account: &Account{ID: 3, Platform: PlatformVideo},
			})
			require.NoError(t, err)
			require.NotNil(t, repo.lastLog)
			require.InDelta(t, expected*2, repo.lastLog.ActualCost, 1e-12)
			require.Equal(t, string(BillingModePerRequest), *repo.lastLog.BillingMode)
			require.Equal(t, 3, repo.lastLog.VideoCount)
		})
	}
}
