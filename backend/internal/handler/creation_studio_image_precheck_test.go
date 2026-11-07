//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// creationBillingCacheFake provides the balance and in-flight reservation parts
// of BillingCache that the image precheck touches.
type creationBillingCacheFake struct {
	service.BillingCache
	balance      float64
	balanceReads int
	reserved     []string
	released     []string
}

func (f *creationBillingCacheFake) GetUserBalance(ctx context.Context, userID int64) (float64, error) {
	f.balanceReads++
	return f.balance, nil
}

func (f *creationBillingCacheFake) ReserveInflightBalance(ctx context.Context, userID int64, requestID string, amount, balance float64, ttl time.Duration) (bool, float64, error) {
	f.reserved = append(f.reserved, requestID)
	return true, 0, nil
}

func (f *creationBillingCacheFake) ReleaseInflightBalance(ctx context.Context, userID int64, requestID string) error {
	f.released = append(f.released, requestID)
	return nil
}

type creationPrecheckFixture struct {
	handler *AsyncImageHandler
	cache   *creationBillingCacheFake
	rdb     *redis.Client
}

func newCreationPrecheckFixture(t *testing.T, balance float64) *creationPrecheckFixture {
	t.Helper()
	return newCreationPrecheckFixtureWithPrice(t, balance, nil)
}

func newCreationPrecheckFixtureWithPrice(t *testing.T, balance float64, imagePrice *float64) *creationPrecheckFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	store := repository.NewImageTaskStore(rdb)
	tasks := service.NewImageTaskServiceWithUploader(store, nil, time.Hour, time.Minute)

	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 1
	cfg.Billing.MinimumBalanceReserve = 0.000001
	cfg.Billing.InflightReservation.Enabled = true
	cache := &creationBillingCacheFake{balance: balance}
	billing := service.NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billing.Stop)
	gateway := service.NewOpenAIGatewayService(nil, nil, nil, nil, nil, nil, nil, cfg, nil, nil,
		service.NewBillingService(cfg, nil), nil, billing, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	openAI := &OpenAIGatewayHandler{billingCacheService: billing, gatewayService: gateway}
	h := NewAsyncImageHandler(tasks, openAI)
	h.execute = func(string, *gin.Context) {}
	return &creationPrecheckFixture{handler: h, cache: cache, rdb: rdb}
}

func creationPrecheckRequestWithSize(t *testing.T, replay bool, size string, group *service.Group) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := `{"model":"gpt-image-2","prompt":"a cat","size":"` + size + `","n":1}`
	request := httptest.NewRequest(http.MethodPost, "/v1/images/generations/async", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	ctx := request.Context()
	if replay {
		ctx = service.WithCreationStudioReplay(ctx)
	}
	c.Request = request.WithContext(ctx)
	groupID := int64(3)
	if group == nil {
		group = &service.Group{ID: groupID, Platform: service.PlatformOpenAI, AllowImageGeneration: true, RateMultiplier: 1, Status: service.StatusActive}
	}
	apiKey := &service.APIKey{
		ID: 9, UserID: 7, Key: "sk-creation", Name: "key", Status: service.StatusActive,
		GroupID: &groupID, Group: group,
	}
	apiKey.User = &service.User{ID: 7, Balance: 10}
	c.Set(string(middleware.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
	return c, recorder
}

func creationPrecheckRequest(t *testing.T, replay bool) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	return creationPrecheckRequestWithSize(t, replay, "1024x1024", nil)
}

func creationPrecheckTaskCount(t *testing.T, rdb *redis.Client) int {
	t.Helper()
	keys, err := rdb.Keys(context.Background(), "image_task:*").Result()
	require.NoError(t, err)
	return len(keys)
}

// TestCreationPrecheckSkippedWithoutReplayMarker proves direct API users keep
// their previous behaviour: no eligibility check, no hold, task created.
func TestCreationPrecheckSkippedWithoutReplayMarker(t *testing.T) {
	fixture := newCreationPrecheckFixture(t, 0)
	c, recorder := creationPrecheckRequest(t, false)
	fixture.handler.Submit(c)
	require.Equal(t, http.StatusAccepted, recorder.Code)
	require.Zero(t, fixture.cache.balanceReads, "the precheck must not run without the replay marker")
	require.Empty(t, fixture.cache.reserved)
	require.Equal(t, 1, creationPrecheckTaskCount(t, fixture.rdb))
}

// TestCreationPrecheckInsufficientBalance covers table A (403).
func TestCreationPrecheckInsufficientBalance(t *testing.T) {
	fixture := newCreationPrecheckFixture(t, 0)
	c, recorder := creationPrecheckRequest(t, true)
	fixture.handler.Submit(c)
	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Equal(t, "INSUFFICIENT_BALANCE", gjson.GetBytes(recorder.Body.Bytes(), "error.code").String())
	require.Zero(t, creationPrecheckTaskCount(t, fixture.rdb))
}

// TestCreationPrecheckBalanceBelowEstimate covers table B (402) and proves no
// task was created.
func TestCreationPrecheckBalanceBelowEstimate(t *testing.T) {
	fixture := newCreationPrecheckFixture(t, 0.001)
	c, recorder := creationPrecheckRequest(t, true)
	fixture.handler.Submit(c)
	require.Equal(t, http.StatusPaymentRequired, recorder.Code)
	require.Equal(t, "insufficient_balance", gjson.GetBytes(recorder.Body.Bytes(), "error.code").String())
	require.Zero(t, creationPrecheckTaskCount(t, fixture.rdb), "a rejected precheck must not create a task")
}

// TestCreationPrecheckPassReleasesReservation proves the hold does not outlive
// the admission check.
func TestCreationPrecheckPassReleasesReservation(t *testing.T) {
	fixture := newCreationPrecheckFixture(t, 100)
	c, recorder := creationPrecheckRequest(t, true)
	fixture.handler.Submit(c)
	require.Equal(t, http.StatusAccepted, recorder.Code)
	require.Len(t, fixture.cache.reserved, 1, "the precheck must reserve the estimated amount")
	require.Equal(t, fixture.cache.reserved, fixture.cache.released, "the reservation must be released immediately")
	require.Equal(t, 1, creationPrecheckTaskCount(t, fixture.rdb))
}

// TestCreationPrecheckFreeImageModelPasses proves an explicitly free image model
// is admitted without a reservation instead of being read as "unpriced".
func TestCreationPrecheckFreeImageModelPasses(t *testing.T) {
	free := 0.0
	fixture := newCreationPrecheckFixtureWithPrice(t, 100, &free)
	freeGroup := &service.Group{ID: 3, Platform: service.PlatformOpenAI, AllowImageGeneration: true, RateMultiplier: 1, Status: service.StatusActive, ImagePrice2K: &free}
	c, recorder := creationPrecheckRequestWithSize(t, true, "1536x1024", freeGroup)
	fixture.handler.Submit(c)
	require.Equal(t, http.StatusAccepted, recorder.Code, recorder.Body.String())
	require.Empty(t, fixture.cache.reserved, "a free model has nothing to reserve")
	require.Empty(t, fixture.cache.released)
	require.Equal(t, 1, creationPrecheckTaskCount(t, fixture.rdb))
}

// TestCreationPrecheckAPIKeyQuota covers the key quota branch (429).
func TestCreationPrecheckAPIKeyQuota(t *testing.T) {
	fixture := newCreationPrecheckFixture(t, 100)
	c, recorder := creationPrecheckRequest(t, true)
	apiKey, ok := middleware.GetAPIKeyFromContext(c)
	require.True(t, ok)
	apiKey.Quota = 1
	apiKey.QuotaUsed = 1
	fixture.handler.Submit(c)
	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
	require.Equal(t, "API_KEY_QUOTA_EXHAUSTED", gjson.GetBytes(recorder.Body.Bytes(), "error.code").String())
	require.Zero(t, creationPrecheckTaskCount(t, fixture.rdb))
}
