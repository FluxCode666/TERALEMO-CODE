//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func videoContractTestInput(price *float64) VideoPricingContractInput {
	user := &User{ID: 17}
	key := &APIKey{ID: 18, User: user, UserID: user.ID, GroupID: i64p(19), Group: &Group{
		ID: 19, Platform: PlatformVideo, RateMultiplier: 1,
		VideoPrice768P: price,
	}}
	return VideoPricingContractInput{APIKey: key, User: user, Model: "doubao-seedance", Resolution: "768P", MaxDurationSeconds: 6, MaxVideoCount: 1,
		InboundEndpoint: "/v1/videos", UpstreamEndpoint: "/api/v3/contents/generations/tasks"}
}

func TestVideoPricingContractFrozenTariffAndSerialization(t *testing.T) {
	price := 0.1
	input := videoContractTestInput(&price)
	svc := &OpenAIGatewayService{}
	contract, err := svc.BuildVideoPricingContract(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, .60, contract.ReservedAmount)
	input.APIKey.Group.VideoBillingMode = string(BillingModePerRequest)
	input.APIKey.Group.VideoPricePerRequest = f64p(2)
	input.APIKey.Group.RateMultiplier = 3
	price = 9
	encoded, err := json.Marshal(contract)
	require.NoError(t, err)
	var restarted VideoPricingContract
	require.NoError(t, json.Unmarshal(encoded, &restarted))
	usage, err := restarted.BuildUsageLog("grok-video:frozen", VideoActualUsage{VideoCount: 1, DurationSeconds: 6})
	require.NoError(t, err)
	require.InDelta(t, .60, usage.ActualCost, 1e-12)
	require.Equal(t, "video", *usage.BillingMode)
	require.Equal(t, "768p", *usage.VideoResolution)
	require.Equal(t, "/v1/videos", *usage.InboundEndpoint)
	require.Equal(t, "/api/v3/contents/generations/tasks", *usage.UpstreamEndpoint)
	require.Equal(t, 1.0, usage.RateMultiplier)
}

func TestVideoPricingContractCapsActualButRetainsAccountCost(t *testing.T) {
	price := .1
	input := videoContractTestInput(&price)
	input.APIKey.Group.RateMultiplier = 2
	contract, err := (&OpenAIGatewayService{}).BuildVideoPricingContract(context.Background(), input)
	require.NoError(t, err)
	cost, err := contract.CalculateActualCost(VideoActualUsage{VideoCount: 2, DurationSeconds: 30})
	require.NoError(t, err)
	require.Equal(t, contract.ReservedAmount, cost.ActualCost)
	require.InDelta(t, 6, cost.TotalCost, 1e-12, "upstream cost is actual usage independent of the capped sale")
	cost, err = contract.CalculateActualCost(VideoActualUsage{VideoCount: 1, DurationSeconds: 3})
	require.NoError(t, err)
	require.InDelta(t, .6, cost.ActualCost, 1e-12)
}

func TestVideoPricingContractUpperBoundMoneyPrecision(t *testing.T) {
	for _, tc := range []struct {
		price, want float64
		wantErr     bool
	}{
		{price: .1, want: .6},
		{price: .1000000001, want: .60000001},
		{price: 0, want: 0},
		{price: math.MaxFloat64 / 100, wantErr: true},
	} {
		input := videoContractTestInput(&tc.price)
		contract, err := (&OpenAIGatewayService{}).BuildVideoPricingContract(context.Background(), input)
		if tc.wantErr {
			require.Error(t, err)
			continue
		}
		require.NoError(t, err)
		require.Equal(t, tc.want, contract.ReservedAmount)
	}
}

func TestVideoPricingContractPerRequestIsOneTask(t *testing.T) {
	price := .4
	input := videoContractTestInput(nil)
	input.APIKey.Group.VideoBillingMode = "per_request"
	input.APIKey.Group.VideoPricePerRequest = &price
	input.APIKey.Group.RateMultiplier = 2
	input.MaxDurationSeconds, input.MaxVideoCount = 30, 7
	contract, err := (&OpenAIGatewayService{}).BuildVideoPricingContract(context.Background(), input)
	require.NoError(t, err)
	cost, err := contract.CalculateActualCost(VideoActualUsage{DurationSeconds: 30, VideoCount: 7})
	require.NoError(t, err)
	require.InDelta(t, .8, cost.ActualCost, 1e-12)
}

func TestVideoPricingContractFrameDurationIsExact(t *testing.T) {
	price := .1
	input := videoContractTestInput(&price)
	input.MaxDurationSeconds = 13
	input.RequestedDurationSeconds = 289.0 / 24.0
	contract, err := (&OpenAIGatewayService{}).BuildVideoPricingContract(context.Background(), input)
	require.NoError(t, err)
	for _, actual := range []VideoActualUsage{
		{VideoCount: 1}, // provider omits duration: frozen requested frames apply.
		{VideoCount: 1, DurationSeconds: 12, DurationSecondsExact: 289.0 / 24.0},
	} {
		usage, err := contract.BuildUsageLog("grok-video:frames", actual)
		require.NoError(t, err)
		require.InDelta(t, .1*289/24, usage.ActualCost, 1e-12)
		require.Equal(t, 13, *usage.VideoDurationSeconds)
		require.Contains(t, *usage.BillingTier, "duration_seconds=12.0416")
	}
}

func TestVideoPricingContractFreeAndUnknownResolution(t *testing.T) {
	zero := 0.0
	input := videoContractTestInput(&zero)
	contract, err := (&OpenAIGatewayService{}).BuildVideoPricingContract(context.Background(), input)
	require.NoError(t, err)
	require.Zero(t, contract.ReservedAmount)
	input.Resolution = "4k"
	_, err = (&OpenAIGatewayService{}).BuildVideoPricingContract(context.Background(), input)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	input.APIKey.Group.ModelPricing = []ChannelModelPricing{{Models: []string{input.Model}, BillingMode: BillingModeVideo, PerRequestPrice: f64p(.9)}}
	contract, err = (&OpenAIGatewayService{}).BuildVideoPricingContract(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, .9, contract.UnitPrice)
	require.Equal(t, "4k", contract.Resolution)
}

func TestVideoPricingRejectsImageAndTokenTariffsAsExplicitVideoPrices(t *testing.T) {
	for _, mode := range []BillingMode{BillingModeImage, BillingModeToken} {
		t.Run(string(mode), func(t *testing.T) {
			svc, input := &OpenAIGatewayService{}, videoContractTestInput(nil)
			input.RequireExplicitVideoPrice = true
			input.APIKey.Group.VideoPrice768P = nil
			input.APIKey.Group.ModelPricing = []ChannelModelPricing{{Models: []string{input.Model}, BillingMode: mode, PerRequestPrice: f64p(.1)}}
			contract, err := svc.BuildVideoPricingContract(context.Background(), input)
			require.ErrorIs(t, err, ErrModelPricingUnavailable)
			require.Nil(t, contract)
		})
	}
}

func TestVideoPricingContractEnterpriseIndependentAndPeakPolicies(t *testing.T) {
	for _, independent := range []bool{false, true} {
		t.Run(map[bool]string{false: "enterprise effective rate", true: "independent video rate"}[independent], func(t *testing.T) {
			price, rate := .5, .8
			input := videoContractTestInput(&price)
			input.PricingAt = at(15, 0)
			input.APIKey.Group.SubscriptionType = SubscriptionTypeSubscription
			input.APIKey.Group.PeakRateEnabled, input.APIKey.Group.PeakStart, input.APIKey.Group.PeakEnd, input.APIKey.Group.PeakRateMultiplier = true, "14:00", "18:00", 3
			input.APIKey.Group.VideoRateIndependent, input.APIKey.Group.VideoRateMultiplier = independent, 1.2
			rates := &effectiveGroupRateTestRepo{rate: &rate}
			svc := &OpenAIGatewayService{userGroupRateResolver: newUserGroupRateResolver(rates, nil, time.Second, nil, "test")}
			contract, err := svc.BuildVideoPricingContract(context.Background(), input)
			require.NoError(t, err)
			require.Equal(t, 3.0, contract.PeakMultiplier)
			require.False(t, contract.PeakApplied)
			want := rate
			if independent {
				want = 1.2
			}
			require.Equal(t, want, contract.EffectiveMultiplier)
			cost, err := contract.CalculateActualCost(VideoActualUsage{VideoCount: 1, DurationSeconds: 6})
			require.NoError(t, err)
			require.InDelta(t, 3*want, cost.ActualCost, 1e-12)
		})
	}
}

func TestVideoPricingContractTokenFallbackFreezeAcrossPeakAndCardChanges(t *testing.T) {
	input := videoContractTestInput(nil)
	input.APIKey.Group.Platform = PlatformOpenAI
	input.APIKey.Group.SubscriptionType = SubscriptionTypeSubscription
	input.APIKey.Group.PeakRateEnabled, input.APIKey.Group.PeakStart, input.APIKey.Group.PeakEnd, input.APIKey.Group.PeakRateMultiplier = true, "14:00", "18:00", 3
	input.APIKey.Group.ModelPricing = []ChannelModelPricing{{Models: []string{input.Model}, BillingMode: BillingModeToken, OutputPrice: f64p(.000002)}}
	input.PricingAt, input.AllowTokenFallback, input.MaxCompletionTokens = at(15, 0), true, 10000
	logs := &openAIRecordUsageLogRepoStub{inserted: true}
	svc := newOpenAIRecordUsageServiceForTest(logs, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
	svc.resolver = NewModelPricingResolver(nil, svc.billingService)
	contract, err := svc.BuildVideoPricingContract(context.Background(), input)
	require.NoError(t, err)
	require.True(t, contract.PeakApplied)
	require.Equal(t, 3.0, contract.EffectiveMultiplier)
	input.APIKey.Group.ModelPricing[0].OutputPrice = f64p(.1)
	input.APIKey.Group.PeakRateMultiplier = 99
	contract.PricingAt = at(20, 0) // settlement time does not re-evaluate peak.
	encoded, err := json.Marshal(contract)
	require.NoError(t, err)
	var restarted VideoPricingContract
	require.NoError(t, json.Unmarshal(encoded, &restarted))
	cost, err := restarted.CalculateActualCost(VideoActualUsage{CompletionTokens: 1000})
	require.NoError(t, err)
	require.InDelta(t, .006, cost.ActualCost, 1e-12)
	require.InDelta(t, .002, cost.TotalCost, 1e-12)
	require.Equal(t, "token", cost.BillingMode)
}

func TestVideoPricingContractRejectsUnknownBoundsAndPrices(t *testing.T) {
	price := .1
	input := videoContractTestInput(&price)
	input.MaxDurationSeconds = 0
	_, err := (&OpenAIGatewayService{}).BuildVideoPricingContract(context.Background(), input)
	require.Error(t, err)
	input.MaxDurationSeconds = 30
	input.APIKey.Group.VideoPrice768P = nil
	input.AllowTokenFallback = true
	_, err = (&OpenAIGatewayService{}).BuildVideoPricingContract(context.Background(), input)
	require.Error(t, err, "token upper bound must be provided")
	input.Model = "grok-imagine-video"
	input.AllowTokenFallback = false
	_, err = (&OpenAIGatewayService{}).BuildVideoPricingContract(context.Background(), input)
	require.ErrorIs(t, err, ErrModelPricingUnavailable, "768p may not borrow the bundled 480p tariff")
	bound, err := ConservativeVideoCompletionTokenBound(30, 1)
	require.NoError(t, err)
	require.Equal(t, 29491200, bound)
	_, err = ConservativeVideoCompletionTokenBound(0, 1)
	require.Error(t, err)
	_, err = ConservativeVideoCompletionTokenBound(math.MaxInt, math.MaxInt)
	require.Error(t, err)
	input.Resolution, input.Model = "480p", "grok-imagine-video"
	input.RequireExplicitVideoPrice = true
	_, err = (&OpenAIGatewayService{}).BuildVideoPricingContract(context.Background(), input)
	require.ErrorIs(t, err, ErrModelPricingUnavailable, "Gemini/MiniMax must require configured tariffs even with a Grok-looking public alias")
}

func TestVideoPricingContractTokenChannelTimeAndReasoningAreFrozen(t *testing.T) {
	input := videoContractTestInput(nil)
	input.AllowTokenFallback, input.MaxCompletionTokens, input.ReasoningEffort = true, 10000, "high"
	input.PricingAt = at(15, 0)
	input.APIKey.Group.SubscriptionType = SubscriptionTypeSubscription
	input.APIKey.Group.PeakRateEnabled, input.APIKey.Group.PeakStart, input.APIKey.Group.PeakEnd, input.APIKey.Group.PeakRateMultiplier = true, "14:00", "18:00", 3
	svc := newOpenAIRecordUsageServiceForTest(&openAIRecordUsageLogRepoStub{inserted: true}, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
	svc.resolver = newOpenAIImageChannelPricingResolverForTest(t, input.APIKey.Group.ID, input.Model, .4)
	card := svc.resolver.channelService.cache.Load().(*channelCache).pricingByGroupModel[channelModelKey{groupID: input.APIKey.Group.ID, model: input.Model}]
	card.BillingMode, card.OutputPrice, card.PerRequestPrice = BillingModeToken, f64p(.000002), nil
	card.Intervals = nil
	card.ReasoningEffortMultipliers = map[string]float64{"high": 2}
	card.TimePricing = &ChannelTimePricing{Timezone: "UTC", Periods: []ChannelTimePricingPeriod{{StartTime: "14:00", EndTime: "18:00", Multiplier: 4}}}
	contract, err := svc.BuildVideoPricingContract(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, PricingSourceChannel, contract.Source)
	require.Equal(t, 24.0, contract.EffectiveMultiplier)
	card.ReasoningEffortMultipliers["high"], *card.OutputPrice = 9, .1
	card.TimePricing.Periods[0].Multiplier = 19
	cost, err := contract.CalculateActualCost(VideoActualUsage{CompletionTokens: 1000})
	require.NoError(t, err)
	require.InDelta(t, .048, cost.ActualCost, 1e-12)
	require.InDelta(t, .016, cost.TotalCost, 1e-12)
	require.Equal(t, 2.0, contract.TokenPricing.ReasoningEffortMultipliers["high"])
}

func TestVideoSettledLogErrorsAreRetryableAndFailedBillsHaveNoPlaceholder(t *testing.T) {
	logs := &openAIRecordUsageLogRepoStub{inserted: true}
	billing := &openAIRecordUsageBillingRepoStub{err: errors.New("billing unavailable")}
	svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, billing, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
	price := .1
	input := videoContractTestInput(&price)
	usage := &OpenAIRecordUsageInput{Result: &OpenAIForwardResult{RequestID: "grok-video:retry", Model: input.Model, VideoCount: 1, VideoResolution: "768p", VideoDurationSeconds: 6}, APIKey: input.APIKey, User: input.User, Account: &Account{ID: 20}}
	require.Error(t, svc.RecordUsage(context.Background(), usage))
	require.Zero(t, logs.calls, "failed debit must not occupy the stable usage key")
	billing.err = nil
	logs.err = errors.New("usage log unavailable")
	require.ErrorContains(t, svc.RecordUsage(context.Background(), usage), "persist settled video usage")
	logs.err = nil
	billing.result = &UsageBillingApplyResult{Applied: false} // already committed: retry repairs only the log.
	require.NoError(t, svc.RecordUsage(context.Background(), usage))
	require.InDelta(t, .6, logs.lastLog.ActualCost, 1e-12)
}

func TestVideoPricingUnknownResolutionOnlyUsesExplicitDefault(t *testing.T) {
	low, fallback := .1, .9
	resolver := NewModelPricingResolver(nil, NewBillingService(nil, nil))
	config := &ChannelModelPricing{BillingMode: BillingModeVideo, PerRequestPrice: &fallback,
		Intervals: []PricingInterval{{TierLabel: "480p", PerRequestPrice: &low}}}
	resolved := resolver.resolveConfiguredPricing(config, "custom-video", PricingSourceGroup)
	price, ok := resolved.VideoUnitPrice("4k")
	require.True(t, ok)
	require.Equal(t, fallback, price)
	config.PerRequestPrice = nil
	resolved = resolver.resolveConfiguredPricing(config, "custom-video", PricingSourceGroup)
	_, ok = resolved.VideoUnitPrice("4k")
	require.False(t, ok)
	group := &Group{VideoPrice480P: &low}
	require.Nil(t, group.GetVideoPrice("4k"))
	for _, tier := range []string{"4k", "768P", "2K"} {
		require.Zero(t, resolver.billingService.getDefaultVideoPrice("grok-imagine-video", tier))
	}
}
