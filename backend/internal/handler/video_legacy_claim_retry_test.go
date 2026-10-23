//go:build unit

package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type vaClaimReleaseObservation struct {
	err       error
	claimKept bool
}

// vaClaimContextCache models Redis SET/GET/SETNX/DEL, including the important
// rule that DEL with a cancelled context performs no mutation. Its virtual
// clock checks the real service's pending/claim TTLs without waiting two days.
type vaClaimContextCache struct {
	service.GatewayCache
	mu              sync.Mutex
	now             time.Time
	pending         []byte
	pendingUntil    time.Time
	pendingTTL      time.Duration
	claimKey        string
	claimUntil      time.Time
	claimTTL        time.Duration
	releaseObserved chan vaClaimReleaseObservation
}

func (c *vaClaimContextCache) SetGrokVideoPendingBilling(ctx context.Context, _ string, payload []byte, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pending = append([]byte(nil), payload...)
	c.pendingTTL = ttl
	c.pendingUntil = c.now.Add(ttl)
	return nil
}

func (c *vaClaimContextCache) GetGrokVideoPendingBilling(ctx context.Context, _ string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.now.Before(c.pendingUntil) {
		return nil, nil
	}
	return append([]byte(nil), c.pending...), nil
}

func (c *vaClaimContextCache) ClaimGrokVideoBilled(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.claimKey == key && c.now.Before(c.claimUntil) {
		return false, nil
	}
	c.claimKey = key
	c.claimTTL = ttl
	c.claimUntil = c.now.Add(ttl)
	return true, nil
}

func (c *vaClaimContextCache) ReleaseGrokVideoBilled(ctx context.Context, key string) error {
	err := ctx.Err()
	c.mu.Lock()
	if err == nil && c.claimKey == key {
		c.claimKey = ""
	}
	kept := c.claimKey == key && c.now.Before(c.claimUntil)
	c.mu.Unlock()
	c.releaseObserved <- vaClaimReleaseObservation{err: err, claimKept: kept}
	return err
}

func (c *vaClaimContextCache) advance(elapsed time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(elapsed)
	c.mu.Unlock()
}

type vaClaimDelayedBillingRepo struct {
	service.UsageBillingRepository
	delay            time.Duration
	calls            int
	lastCmd          *service.UsageBillingCommand
	ctxErrAfterDelay error
}

func (r *vaClaimDelayedBillingRepo) Apply(ctx context.Context, cmd *service.UsageBillingCommand) (*service.UsageBillingApplyResult, error) {
	r.calls++
	copy := *cmd
	r.lastCmd = &copy
	time.Sleep(r.delay)
	r.ctxErrAfterDelay = ctx.Err()
	return nil, errors.New("mock billing transaction rolled back after worker deadline")
}

type vaClaimUsageLogRepo struct {
	service.UsageLogRepository
	lastLog *service.UsageLog
}

func (r *vaClaimUsageLogRepo) Create(_ context.Context, log *service.UsageLog) (bool, error) {
	copy := *log
	r.lastLog = &copy
	return true, nil
}

func vaClaimGatewayService(cache service.GatewayCache, billing service.UsageBillingRepository, logs service.UsageLogRepository) *service.OpenAIGatewayService {
	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 1
	return service.NewOpenAIGatewayService(
		nil,     // accountRepo: ordinary account needs no lookup
		logs,    // usageLogRepo
		billing, // usageBillingRepo
		nil,     // userRepo: unified billing mock owns the debit
		nil,     // userSubRepo
		nil,     // userGroupRateRepo: use the group's rate
		cache,   // GatewayCache
		cfg,     // config
		nil,     // schedulerSnapshot
		nil,     // concurrencyService
		service.NewBillingService(cfg, nil),
		nil,                            // rateLimitService
		&service.BillingCacheService{}, // billingCacheService
		nil,                            // HTTPUpstream: no request can reach an upstream
		&service.DeferredService{},     // deferredService
		nil,                            // OpenAI token provider
		nil,                            // Grok token provider
		nil,                            // pricing resolver: use explicit group prices
		nil,                            // channelService
		nil,                            // balanceNotifyService
		nil,                            // settingService
		nil,                            // userPlatformQuotaRepo
	)
}

// Regression B2 (acceptance B05): run the real prepare -> recordGrokMediaUsage -> worker -> RecordUsage ->
// Apply(error) -> ReleaseGrokVideoBilling path. Only repositories/cache are mocks.
// A 20ms worker deadline and a 100ms failed transaction reproduce the production
// 5s-worker/15s-detached-billing mismatch without an eight-second test.
func TestLegacyVideoClaimReleaseSurvivesExpiredWorkerContext(t *testing.T) {
	cache := &vaClaimContextCache{
		now:             time.Now(),
		releaseObserved: make(chan vaClaimReleaseObservation, 1),
	}
	billing := &vaClaimDelayedBillingRepo{delay: 100 * time.Millisecond}
	logs := &vaClaimUsageLogRepo{}
	svc := vaClaimGatewayService(cache, billing, logs)
	pool := service.NewUsageRecordWorkerPoolWithOptions(service.UsageRecordWorkerPoolOptions{
		WorkerCount: 1,
		QueueSize:   1,
		TaskTimeout: 20 * time.Millisecond,
	})
	defer pool.Stop()
	h := &OpenAIGatewayHandler{gatewayService: svc, usageRecordWorkerPool: pool}

	price := 0.1
	groupID := int64(126)
	user := &service.User{ID: 20126}
	key := &service.APIKey{
		ID: 10126, UserID: user.ID, User: user, GroupID: &groupID,
		Group: &service.Group{
			ID: groupID, Platform: service.PlatformVideo, RateMultiplier: 1,
			VideoBillingMode: string(service.BillingModeVideo),
			VideoPrice480P:   &price, VideoPrice720P: &price, VideoPrice768P: &price,
			VideoPrice1080P: &price, VideoPrice2K: &price,
		},
	}
	subject := middleware.AuthSubject{UserID: user.ID}
	taskID := service.SeedanceTaskKey("accept-expired-worker")
	model := "doubao-seedance-1-0-pro-250528"
	ctx := context.Background()
	require.NoError(t, svc.StoreGrokVideoPendingBilling(ctx, taskID, user.ID, key.ID, service.GrokVideoPendingBilling{
		Model: model, BillingModel: model, VideoResolution: "720p", VideoDurationSeconds: 10,
		CreatedAt: service.GrokVideoPendingCreatedAtNow(), BillingIdentity: newGrokVideoBillingIdentity(key, nil),
	}))
	status := &service.OpenAIForwardResult{
		ResponseID: taskID, AsyncTaskStatus: "completed", VideoResolution: "720p", VideoDurationSeconds: 10,
	}
	billResult := prepareSeedanceCompletionBilling(ctx, h, key, subject, taskID, status)
	require.NotNil(t, billResult, "a successful task must first acquire its billing claim")

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v3/contents/generations/tasks/accept-expired-worker", nil)
	account := &service.Account{ID: 30126, Platform: service.PlatformVideo, Type: service.AccountTypeAPIKey}
	recordGrokMediaUsage(c, h, zap.NewNop(), key, subject, nil, account, billResult, model, nil, taskID)

	var observed vaClaimReleaseObservation
	select {
	case observed = <-cache.releaseObserved:
	case <-time.After(3 * time.Second):
		t.Fatal("real handler path did not reach claim release after failed billing")
	}
	pool.Stop() // Join the real worker before reading the repositories' fields.
	require.Equal(t, 1, billing.calls)
	require.InDelta(t, 1.0, billing.lastCmd.BalanceCost, 1e-12)
	require.NoError(t, billing.ctxErrAfterDelay, "the debit uses a detached, still-live context")
	require.NoError(t, observed.err, "claim release must use a live context after the worker deadline")
	require.False(t, observed.claimKept, "failed settlement must release its claim for the next observer")
	require.Nil(t, logs.lastLog, "a rolled-back payment must not leave a zero-cost placeholder")
	require.NotNil(t, prepareSeedanceCompletionBilling(ctx, h, key, subject, taskID, status), "the next poll can retry immediately")
	require.Equal(t, 1, billing.calls)
	t.Logf("failed bill=%.2f, billing context=%v, release context=%v, claim kept=%t; next poll can retry", billing.lastCmd.BalanceCost, billing.ctxErrAfterDelay, observed.err, observed.claimKept)
}
