//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type effectiveGroupRateTestRepo struct {
	UserGroupRateRepository
	rate  *float64
	rates map[int64]float64
	err   error
	calls int
}

func (r *effectiveGroupRateTestRepo) GetByUserAndGroup(context.Context, int64, int64) (*float64, error) {
	panic("billing must use effective rates")
}

func (r *effectiveGroupRateTestRepo) GetByUserID(context.Context, int64) (map[int64]float64, error) {
	panic("displays must use effective rates")
}

func (r *effectiveGroupRateTestRepo) GetEffectiveByUserAndGroup(context.Context, int64, int64) (*float64, error) {
	r.calls++
	return r.rate, r.err
}

func (r *effectiveGroupRateTestRepo) GetEffectiveByUserID(context.Context, int64) (map[int64]float64, error) {
	return r.rates, r.err
}

func TestUserGroupRateResolverEffectiveRatesAndInvalidation(t *testing.T) {
	ctx := context.Background()
	enterpriseRate, personalRate := 0.8, 0.6
	repo := &effectiveGroupRateTestRepo{rate: &enterpriseRate}
	gateways := []*userGroupRateResolver{
		newUserGroupRateResolver(repo, nil, time.Minute, nil, "service.gateway"),
		newUserGroupRateResolver(repo, nil, time.Minute, nil, "service.openai_gateway"),
	}
	for _, resolver := range gateways {
		require.Equal(t, 30*time.Second, resolver.cacheTTL)
		require.Equal(t, enterpriseRate, resolver.Resolve(ctx, 1, 2, 1.5))
	}
	require.Equal(t, 2, repo.calls)
	repo.rate = &personalRate
	for _, resolver := range gateways {
		require.Equal(t, enterpriseRate, resolver.Resolve(ctx, 1, 2, 1.5))
	}
	flushUserGroupRateCaches()
	for _, resolver := range gateways {
		require.Equal(t, personalRate, resolver.Resolve(ctx, 1, 2, 1.5))
	}
	require.Equal(t, 4, repo.calls)
	repo.rate = nil
	flushUserGroupRateCaches()
	require.Equal(t, 1.5, gateways[0].Resolve(ctx, 1, 2, 1.5))
	zero := 0.0
	repo.rate = &zero
	flushUserGroupRateCaches()
	require.Zero(t, gateways[0].Resolve(ctx, 1, 2, 1.5))
}

func TestUserGroupRateResolverCacheHitsShareReadLock(t *testing.T) {
	rate := 0.8
	repo := &effectiveGroupRateTestRepo{rate: &rate}
	resolver := newUserGroupRateResolver(repo, nil, 30*time.Second, nil, "service.test")
	require.Equal(t, rate, resolver.Resolve(context.Background(), 1, 2, 1.5))

	// Hold a reader while concurrent hits finish: a write lock on the hit path
	// would block every worker until this reader is released.
	resolver.cacheMu.RLock()
	defer resolver.cacheMu.RUnlock()
	const workers = 32
	done := make(chan float64, workers)
	for i := 0; i < workers; i++ {
		go func() { done <- resolver.Resolve(context.Background(), 1, 2, 1.5) }()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for i := 0; i < workers; i++ {
		select {
		case got := <-done:
			require.Equal(t, rate, got)
		case <-ctx.Done():
			t.Fatal("cached requests attempted to acquire an exclusive lock")
		}
	}
	require.Equal(t, 1, repo.calls)
}

type blockedEffectiveGroupRateTestRepo struct {
	effectiveGroupRateTestRepo
	started chan struct{}
	release chan struct{}
}

func (r *blockedEffectiveGroupRateTestRepo) GetEffectiveByUserAndGroup(ctx context.Context, _, _ int64) (*float64, error) {
	close(r.started)
	select {
	case <-r.release:
		return r.rate, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestUserGroupRateResolverInvalidationFencesInflightLoad(t *testing.T) {
	oldRate := 0.8
	repo := &blockedEffectiveGroupRateTestRepo{
		effectiveGroupRateTestRepo: effectiveGroupRateTestRepo{rate: &oldRate},
		started:                    make(chan struct{}), release: make(chan struct{}),
	}
	resolver := newUserGroupRateResolver(repo, nil, time.Second, nil, "service.test")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan float64, 1)
	go func() { done <- resolver.Resolve(ctx, 1, 2, 1.5) }()
	select {
	case <-repo.started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	flushUserGroupRateCaches()
	close(repo.release)
	require.Equal(t, oldRate, <-done)
	_, cached := resolver.cache.Get("1:2")
	require.False(t, cached, "an invalidated in-flight load cannot repopulate the cache")
}

func TestAPIKeyServiceGetUserGroupRatesUsesEffectiveMap(t *testing.T) {
	rates := map[int64]float64{1: 0.8, 2: 0, 3: 0.6}
	repo := &effectiveGroupRateTestRepo{rates: rates}
	svc := &APIKeyService{userGroupRateRepo: repo}
	got, err := svc.GetUserGroupRates(context.Background(), 10)
	require.NoError(t, err)
	require.Equal(t, rates, got)
	repo.err = errors.New("database unavailable")
	_, err = svc.GetUserGroupRates(context.Background(), 10)
	require.ErrorContains(t, err, "database unavailable")
}

func TestOpenAIUsageRecordsEffectiveEnterpriseRate(t *testing.T) {
	enterpriseRate := 0.8
	usage := OpenAIUsage{InputTokens: 15, OutputTokens: 4, CacheReadInputTokens: 3}
	logs := &openAIRecordUsageLogRepoStub{inserted: true}
	users := &openAIRecordUsageUserRepoStub{}
	rates := &effectiveGroupRateTestRepo{rate: &enterpriseRate}
	svc := newOpenAIRecordUsageServiceForTest(logs, users, &openAIRecordUsageSubRepoStub{}, rates)
	err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{RequestID: "enterprise-rate", Usage: usage, Model: "gpt-5.1", Duration: time.Second},
		APIKey: &APIKey{ID: 1001, GroupID: i64p(11), Group: &Group{ID: 11, RateMultiplier: 1.4}},
		User:   &User{ID: 2001}, Account: &Account{ID: 3001},
	})
	require.NoError(t, err)
	require.NotNil(t, logs.lastLog)
	require.Equal(t, enterpriseRate, logs.lastLog.RateMultiplier)
	expected := expectedOpenAICost(t, svc, "gpt-5.1", usage, enterpriseRate)
	require.InDelta(t, expected.ActualCost, logs.lastLog.ActualCost, 1e-12)
	require.InDelta(t, expected.ActualCost, users.lastAmount, 1e-12)
}

func TestGatewayUsageUsesEffectiveEnterpriseRate(t *testing.T) {
	enterpriseRate := 0.8
	rates := &effectiveGroupRateTestRepo{rate: &enterpriseRate}
	svc := &GatewayService{userGroupRateResolver: newUserGroupRateResolver(rates, nil, time.Second, nil, "service.test")}
	key := &APIKey{GroupID: i64p(11), Group: &Group{ID: 11, RateMultiplier: 1.4}}
	text, image, _ := svc.recordUsageMultipliers(context.Background(), key, &User{ID: 2001}, time.Now())
	require.Equal(t, enterpriseRate, text)
	require.Equal(t, enterpriseRate, image)
	require.Equal(t, 1, rates.calls)
}

func TestBatchImageEffectiveGroupRateSnapshot(t *testing.T) {
	for _, independent := range []bool{false, true} {
		t.Run(map[bool]string{false: "enterprise rate", true: "independent image rate"}[independent], func(t *testing.T) {
			svc, jobs, _, _, _ := newTestBatchImagePublicService(true)
			groupID := int64(7)
			enterpriseRate := 0.8
			rates := &effectiveGroupRateTestRepo{rate: &enterpriseRate}
			svc.UserGroupRateRepo = rates
			svc.GroupRepo = &publicBatchImageGroupRepo{groups: map[int64]*Group{groupID: {
				ID: groupID, Platform: PlatformGemini, RateMultiplier: 2,
				AllowBatchImageGeneration: true, ImageRateIndependent: independent, ImageRateMultiplier: 1.2,
				BatchImageDiscountMultiplier: 0.5, BatchImageHoldMultiplier: 0.6,
			}}}
			batch, err := svc.Submit(context.Background(), BatchImageOwner{UserID: 11, APIKeyID: 22, GroupID: &groupID}, validBatchImageSubmitRequest(), "")
			require.NoError(t, err)
			want := enterpriseRate
			if independent {
				want = 1.2
			}
			job := jobs.jobs[batch.ID]
			require.Equal(t, want, job.GroupRateMultiplier)
			require.InDelta(t, job.BaseUnitPrice*want*0.5, job.BillableUnitPrice, 1e-12)
			require.Equal(t, 1, rates.calls)
			rates.err = errors.New("database unavailable")
			_, err = svc.resolvePricingSnapshot(context.Background(), BatchImageOwner{UserID: 11, GroupID: &groupID}, validBatchImageSubmitRequest(), BatchImageProviderGeminiAPI, nil)
			require.ErrorIs(t, err, ErrBatchImageSettlementPricingMissing)
		})
	}
}
