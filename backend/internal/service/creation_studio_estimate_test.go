//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type creationBalanceUserRepo struct {
	UserRepository
	balance float64
}

func (r *creationBalanceUserRepo) GetByID(ctx context.Context, id int64) (*User, error) {
	return &User{ID: id, Balance: r.balance}, nil
}

func newCreationBillingCache(balance float64) *BillingCacheService {
	return &BillingCacheService{userRepo: &creationBalanceUserRepo{balance: balance}}
}

// newCreationEstimateGateway builds a gateway with the dependencies the estimate
// path touches: pricing (billing service), optional channel resolver and the
// balance cache. No upstream, no Redis.
func newCreationEstimateGateway(t *testing.T, group *Group, balance float64) (*OpenAIGatewayService, *APIKey) {
	t.Helper()
	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 1
	cfg.Billing.MinimumBalanceReserve = 0.000001
	gateway := &OpenAIGatewayService{
		cfg:                 cfg,
		billingService:      NewBillingService(cfg, nil),
		billingCacheService: newCreationBillingCache(balance),
	}
	user := &User{ID: 7, Balance: balance}
	key := &APIKey{ID: 9, UserID: 7, Name: "key", Status: StatusActive, Group: group, User: user}
	if group != nil {
		groupID := group.ID
		key.GroupID = &groupID
	}
	return gateway, key
}

func imageEstimateOption() CreationModelOption {
	return creationImageOptionFixture()
}

func TestEstimateCreationImageCostTiers(t *testing.T) {
	group := &Group{ID: 3, Platform: PlatformOpenAI, RateMultiplier: 1, Status: StatusActive,
		ImagePrice1K: creationPtrFloat(0.01), ImagePrice2K: creationPtrFloat(0.02), ImagePrice4K: creationPtrFloat(0.04)}
	gateway, key := newCreationEstimateGateway(t, group, 100)
	ctx := context.Background()

	for _, tc := range []struct {
		size string
		tier string
	}{
		{"1024x1024", ImageBillingSize1K},
		{"1536x1024", ImageBillingSize2K},
		{"auto", ImageBillingSize2K},
		{"", ImageBillingSize2K},
		{"4k", ImageBillingSize4K},
		{"1k", ImageBillingSize1K},
		{"2k", ImageBillingSize2K},
	} {
		t.Run(tc.size, func(t *testing.T) {
			estimate := gateway.EstimateCreationImageCost(ctx, key, key.UserID, "gpt-image-2", tc.size, 1, 100)
			require.True(t, estimate.Priced)
			require.Equal(t, CreationCostBasisImageTier, estimate.Basis)
			require.Equal(t, tc.tier, estimate.Tier)
		})
	}
}

// TestEstimateCreationImageCostMatchesExecutorPricing locks the amount to the
// executor's own image pricing function under the same tier and multiplier.
func TestEstimateCreationImageCostMatchesExecutorPricing(t *testing.T) {
	ctx := context.Background()
	type estimateCase struct {
		name     string
		group    *Group
		resolver *ModelPricingResolver
	}
	cases := []estimateCase{
		{
			name:  "default price",
			group: &Group{ID: 3, Platform: PlatformOpenAI, RateMultiplier: 1, Status: StatusActive},
		},
		{
			name: "group price",
			group: &Group{ID: 3, Platform: PlatformOpenAI, RateMultiplier: 1, Status: StatusActive,
				ImagePrice2K: creationPtrFloat(0.04)},
		},
		{
			name:  "user group multiplier",
			group: &Group{ID: 3, Platform: PlatformOpenAI, RateMultiplier: 1.5, Status: StatusActive},
		},
		{
			name:  "channel per request price",
			group: &Group{ID: 100, Platform: PlatformOpenAI, RateMultiplier: 1, Status: StatusActive},
			resolver: newCreationChannelResolver(t, []ChannelModelPricing{{
				Platform: "openai", Models: []string{"gpt-image-2"}, BillingMode: BillingModePerRequest,
				PerRequestPrice: creationPtrFloat(0.05),
			}}),
		},
		{
			name: "image rate independent",
			group: &Group{ID: 3, Platform: PlatformOpenAI, RateMultiplier: 2, Status: StatusActive,
				ImageRateIndependent: true, ImageRateMultiplier: 0.5, ImagePrice2K: creationPtrFloat(0.02)},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gateway, key := newCreationEstimateGateway(t, tc.group, 10)
			gateway.resolver = tc.resolver
			size, count := "1536x1024", 3

			estimate := gateway.EstimateCreationImageCost(ctx, key, key.UserID, "gpt-image-2", size, count, 128)
			require.True(t, estimate.Priced)

			base := 1.0
			if gateway.cfg != nil {
				base = gateway.cfg.Default.RateMultiplier
			}
			if key.Group != nil && key.GroupID != nil {
				base = gateway.ResolveUserGroupRateMultiplier(ctx, key.UserID, *key.GroupID, key.Group.RateMultiplier)
			}
			_, imageMultiplier := computePeakAwareMultipliers(key, base, time.Now())
			tier := ResolveImageBillingSize(size, nil).BillingSize
			want := gateway.calculateOpenAIImageCost(ctx, "gpt-image-2", key, &OpenAIForwardResult{
				ImageSize: tier, ImageInputSize: size, ImageCount: count,
			}, imageMultiplier)
			require.NotNil(t, want)
			require.InDelta(t, want.ActualCost, estimate.Amount, 1e-12)
		})
	}
}

func TestEstimateCreationImageCostUsesInflightEstimateForTokenChannels(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 1
	group := &Group{ID: 100, Platform: PlatformOpenAI, RateMultiplier: 1, Status: StatusActive}
	resolver := newCreationChannelResolver(t, []ChannelModelPricing{{
		Platform: "openai", Models: []string{"gpt-image-2"}, BillingMode: BillingModeToken,
		InputPrice: creationPtrFloat(1e-6), OutputPrice: creationPtrFloat(2e-6),
	}})
	gateway := &OpenAIGatewayService{
		cfg: cfg, billingService: NewBillingService(cfg, nil), resolver: resolver,
	}
	user := &User{ID: 7}
	key := &APIKey{ID: 9, UserID: 7, Group: group, User: user, GroupID: creationPtrInt64(100)}

	estimate := gateway.EstimateCreationImageCost(ctx, key, 7, "gpt-image-2", "auto", 2, 256)
	require.Equal(t, CreationCostBasisImageTokenEstimate, estimate.Basis)

	amount, priced := gateway.EstimateInflightReservation(ctx, key, InflightEstimateRequest{
		Model: "gpt-image-2", BodyBytes: 256, Kind: InflightEstimateImage, Units: 2,
	})
	require.Equal(t, priced, estimate.Priced)
	require.InDelta(t, amount, estimate.Amount, 1e-12)
}

func TestEstimateCreationImageResponseShape(t *testing.T) {
	group := &Group{ID: 3, Platform: PlatformOpenAI, RateMultiplier: 1, Status: StatusActive,
		ImagePrice1K: creationPtrFloat(0.01), ImagePrice2K: creationPtrFloat(0.02), ImagePrice4K: creationPtrFloat(0.04)}
	gateway, key := newCreationEstimateGateway(t, group, 100)
	option := imageEstimateOption()
	estimate, err := gateway.EstimateCreation(context.Background(), key, CreationEstimateRequest{
		APIKeyID: key.ID, Mode: CreationModeImage, Model: option.Model, Protocol: option.Protocol,
		Params: map[string]any{"size": "1024x1024", "n": 2},
	}, option, 0)
	require.NoError(t, err)
	require.True(t, estimate.Priced)
	require.Equal(t, CreationCostBasisImageTier, estimate.CostBasis)
	require.Equal(t, ImageBillingSize1K, estimate.SelectedTier)
	require.Equal(t, "image", estimate.PricingMode)
	require.NotNil(t, estimate.UnitPrice)
	require.InDelta(t, 0.01, *estimate.UnitPrice, 1e-12)
	require.InDelta(t, 0.02, estimate.EstimatedCost, 1e-12)
	require.Equal(t, CreationBalanceScopePersonal, estimate.BalanceScope)
	require.NotNil(t, estimate.AvailableBalance)
	require.InDelta(t, 100, *estimate.AvailableBalance, 1e-12)
	require.NotNil(t, estimate.Sufficient)
	require.True(t, *estimate.Sufficient)
	require.Equal(t, "USD", estimate.Currency)

	encoded, err := json.Marshal(estimate)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "selected_tier_cost", "v1.1 removed the extra amount field")
	require.Contains(t, string(encoded), `"estimated_cost"`)
}

func TestEstimateCreationBalanceSufficiency(t *testing.T) {
	group := &Group{ID: 3, Platform: PlatformOpenAI, RateMultiplier: 1, Status: StatusActive,
		ImagePrice2K: creationPtrFloat(1)}
	option := imageEstimateOption()
	request := CreationEstimateRequest{Mode: CreationModeImage, Model: option.Model, Protocol: option.Protocol,
		Params: map[string]any{"size": "1536x1024", "n": 1}}
	ctx := context.Background()

	t.Run("personal with enough balance", func(t *testing.T) {
		gateway, key := newCreationEstimateGateway(t, group, 5)
		estimate, err := gateway.EstimateCreation(ctx, key, request, option, 0)
		require.NoError(t, err)
		require.Equal(t, CreationBalanceScopePersonal, estimate.BalanceScope)
		require.NotNil(t, estimate.Sufficient)
		require.True(t, *estimate.Sufficient)
	})

	t.Run("personal below the estimate", func(t *testing.T) {
		gateway, key := newCreationEstimateGateway(t, group, 0.5)
		estimate, err := gateway.EstimateCreation(ctx, key, request, option, 0)
		require.NoError(t, err)
		require.NotNil(t, estimate.Sufficient)
		require.False(t, *estimate.Sufficient)
	})

	t.Run("personal below the minimum reserve", func(t *testing.T) {
		gateway, key := newCreationEstimateGateway(t, group, 0.0000005)
		estimate, err := gateway.EstimateCreation(ctx, key, request, option, 0)
		require.NoError(t, err)
		require.NotNil(t, estimate.Sufficient)
		require.False(t, *estimate.Sufficient)
	})

	t.Run("enterprise is decided on submit", func(t *testing.T) {
		gateway, key := newCreationEstimateGateway(t, group, 5)
		key.User.Enterprise = &EnterpriseMemberContext{}
		estimate, err := gateway.EstimateCreation(ctx, key, request, option, 0)
		require.NoError(t, err)
		require.Equal(t, CreationBalanceScopeEnterprise, estimate.BalanceScope)
		require.Nil(t, estimate.AvailableBalance)
		require.Nil(t, estimate.Sufficient)
	})

	t.Run("subscription is decided on submit", func(t *testing.T) {
		subscriptionGroup := &Group{ID: 3, Platform: PlatformOpenAI, RateMultiplier: 1, Status: StatusActive,
			SubscriptionType: SubscriptionTypeSubscription, ImagePrice2K: creationPtrFloat(1)}
		gateway, key := newCreationEstimateGateway(t, subscriptionGroup, 5)
		estimate, err := gateway.EstimateCreation(ctx, key, request, option, 0)
		require.NoError(t, err)
		require.Equal(t, CreationBalanceScopeSubscription, estimate.BalanceScope)
		require.Nil(t, estimate.AvailableBalance)
		require.Nil(t, estimate.Sufficient)
	})
}

func TestEstimateCreationVideoMatchesFrozenContract(t *testing.T) {
	group := &Group{ID: 4, Platform: PlatformVideo, RateMultiplier: 1, Status: StatusActive,
		VideoPrice720P: creationPtrFloat(0.02)}
	gateway, key := newCreationEstimateGateway(t, group, 100)
	option := creationSeedanceOptionFixture(t)
	request := CreationEstimateRequest{
		Mode: CreationModeVideo, Model: option.Model, Protocol: option.Protocol,
		Params: map[string]any{"resolution": "720p", "duration": 5},
	}
	estimate, err := gateway.EstimateCreation(context.Background(), key, request, option, 0)
	require.NoError(t, err)
	require.True(t, estimate.Priced)
	require.Equal(t, CreationCostBasisVideoReservation, estimate.CostBasis)
	require.Equal(t, string(BillingModeVideo), estimate.PricingMode)

	maxTokens, err := ConservativeVideoCompletionTokenBound(5, 1)
	require.NoError(t, err)
	contract, err := gateway.BuildVideoPricingContract(context.Background(), VideoPricingContractInput{
		APIKey: key, User: key.User, Model: option.Model, Resolution: "720p",
		MaxDurationSeconds: 5, MaxVideoCount: 1, MaxCompletionTokens: maxTokens,
		RequestedDurationSeconds: 5,
		AllowTokenFallback:       true,
		UpperBoundBasis:          "protocol duration/count maximum; token fallback <=4096x4096 pixels at 60fps /1024",
	})
	require.NoError(t, err)
	require.InDelta(t, contract.ReservedAmount, estimate.EstimatedCost, 1e-12)
}

func TestEstimateCreationUnpricedReturnsNoError(t *testing.T) {
	// A Gemini video model without any configured price must estimate as unpriced,
	// not as an error (contract §4.3).
	group := &Group{ID: 4, Platform: PlatformVideo, RateMultiplier: 1, Status: StatusActive}
	gateway, key := newCreationEstimateGateway(t, group, 100)
	option := creationGeminiOptionFixture()
	estimate, err := gateway.EstimateCreation(context.Background(), key, CreationEstimateRequest{
		Mode: CreationModeVideo, Model: option.Model, Protocol: option.Protocol,
		Params: map[string]any{"resolution": "720p", "duration": 8},
	}, option, 0)
	require.NoError(t, err)
	require.False(t, estimate.Priced)
	require.Zero(t, estimate.EstimatedCost)
	require.Equal(t, CreationCostBasisVideoReservation, estimate.CostBasis)
}

func TestEstimateCreationRejectsUnknownModel(t *testing.T) {
	group := &Group{ID: 3, Platform: PlatformOpenAI, RateMultiplier: 1, Status: StatusActive}
	gateway, key := newCreationEstimateGateway(t, group, 10)
	option := imageEstimateOption()
	_, err := gateway.EstimateCreation(context.Background(), key, CreationEstimateRequest{
		Mode: CreationModeImage, Model: "gpt-image-9", Protocol: CreationProtocolOpenAI,
	}, option, 0)
	require.ErrorIs(t, err, ErrCreationModelNotAvailable)
}

// newCreationChannelResolver builds a resolver over an in-memory channel so the
// estimate can be exercised against channel cards (per-request and token modes).
func newCreationChannelResolver(t *testing.T, pricing []ChannelModelPricing) *ModelPricingResolver {
	t.Helper()
	const groupID = 100
	repo := &mockChannelRepository{
		listAllFn: func(context.Context) ([]Channel, error) {
			return []Channel{{
				ID: 1, Name: "creation-channel", Status: StatusActive,
				GroupIDs: []int64{groupID}, ModelPricing: pricing,
			}}, nil
		},
		getGroupPlatformsFn: func(context.Context, []int64) (map[int64]string, error) {
			return map[int64]string{groupID: PlatformOpenAI}, nil
		},
	}
	return NewModelPricingResolver(NewChannelService(repo, nil, nil, nil, nil), NewBillingService(&config.Config{}, nil))
}

func creationPtrFloat(value float64) *float64 { return &value }

func creationPtrInt64(value int64) *int64 { return &value }

// TestEstimateCreationFreeImageModel locks the v1.2 rule: a resolved price of 0
// is "priced and free", not "unpriced".
func TestEstimateCreationFreeImageModel(t *testing.T) {
	free := 0.0
	group := &Group{ID: 3, Platform: PlatformOpenAI, RateMultiplier: 1, Status: StatusActive,
		ImagePrice2K: &free}
	gateway, key := newCreationEstimateGateway(t, group, 100)
	option := imageEstimateOption()
	estimate, err := gateway.EstimateCreation(context.Background(), key, CreationEstimateRequest{
		Mode: CreationModeImage, Model: option.Model, Protocol: option.Protocol,
		Params: map[string]any{"size": "1536x1024", "n": 2},
	}, option, 0)
	require.NoError(t, err)
	require.True(t, estimate.Priced, "an explicit 0 price is priced, not unpriced")
	require.Zero(t, estimate.EstimatedCost)
	require.Equal(t, CreationCostBasisImageTier, estimate.CostBasis)
	require.Equal(t, ImageBillingSize2K, estimate.SelectedTier)
	require.NotNil(t, estimate.UnitPrice)
	require.Zero(t, *estimate.UnitPrice)
	require.NotNil(t, estimate.Sufficient)
	require.True(t, *estimate.Sufficient)
}

// TestEstimateCreationImageUnconfiguredUsesDefaultPrice documents the pricing
// conclusion: an image model without any configured price falls back to the
// built-in default, so it is never reported as free.
func TestEstimateCreationImageUnconfiguredUsesDefaultPrice(t *testing.T) {
	group := &Group{ID: 3, Platform: PlatformOpenAI, RateMultiplier: 1, Status: StatusActive}
	gateway, key := newCreationEstimateGateway(t, group, 100)
	option := imageEstimateOption()
	estimate, err := gateway.EstimateCreation(context.Background(), key, CreationEstimateRequest{
		Mode: CreationModeImage, Model: option.Model, Protocol: option.Protocol,
		Params: map[string]any{"size": "1024x1024", "n": 1},
	}, option, 0)
	require.NoError(t, err)
	require.True(t, estimate.Priced)
	require.Positive(t, estimate.EstimatedCost, "unconfigured image models fall back to the default price")
}
