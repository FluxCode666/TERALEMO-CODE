//go:build unit

package handler

import (
	"context"
	"encoding/json"
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

type creationHandlerAccountRepo struct {
	service.AccountRepository
	accounts []service.Account
}

func (r *creationHandlerAccountRepo) ListSchedulableByGroupID(ctx context.Context, groupID int64) ([]service.Account, error) {
	return r.accounts, nil
}

type creationHandlerKeyRepo struct {
	service.APIKeyRepository
	keys map[int64]*service.APIKey
}

func (r *creationHandlerKeyRepo) GetByID(ctx context.Context, id int64) (*service.APIKey, error) {
	key, ok := r.keys[id]
	if !ok {
		return nil, service.ErrAPIKeyNotFound
	}
	return key, nil
}

func (r *creationHandlerKeyRepo) GetByKeyForAuth(ctx context.Context, value string) (*service.APIKey, error) {
	return r.GetByKey(ctx, value)
}

func (r *creationHandlerKeyRepo) GetByKey(ctx context.Context, value string) (*service.APIKey, error) {
	for _, key := range r.keys {
		if key.Key == value {
			return key, nil
		}
	}
	return nil, service.ErrAPIKeyNotFound
}

// creationFakeDispatcher records the replayed request and returns a canned reply.
type creationFakeDispatcher struct {
	requests []*http.Request
	bodies   [][]byte
	status   int
	body     string
}

func (d *creationFakeDispatcher) Dispatch(w http.ResponseWriter, r *http.Request) {
	payload := make([]byte, 4096)
	n, _ := r.Body.Read(payload)
	d.requests = append(d.requests, r)
	d.bodies = append(d.bodies, payload[:n])
	if d.status == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(d.status)
	_, _ = w.Write([]byte(d.body))
}

type creationHandlerFixture struct {
	handler    *CreationStudioHandler
	dispatcher *creationFakeDispatcher
	key        *service.APIKey
	imageTasks *service.ImageTaskService
}

func newCreationHandlerFixture(t *testing.T, groupPlatform string, accounts ...service.Account) *creationHandlerFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 1
	gateway := service.NewOpenAIGatewayService(&creationHandlerAccountRepo{accounts: accounts}, nil, nil, nil, nil, nil,
		nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	groupID := int64(3)
	group := &service.Group{ID: groupID, Name: "group", Platform: groupPlatform, AllowImageGeneration: true,
		RateMultiplier: 1, Status: service.StatusActive}
	user := &service.User{ID: 7, Balance: 100}
	key := &service.APIKey{ID: 9, UserID: 7, Key: "sk-canary-creation", Name: "canary", Status: service.StatusActive,
		GroupID: &groupID, Group: group, User: user}
	other := &service.APIKey{ID: 10, UserID: 8, Key: "sk-other-user", Name: "other", Status: service.StatusActive,
		GroupID: &groupID, Group: group, User: &service.User{ID: 8}}
	apiKeys := service.NewAPIKeyService(&creationHandlerKeyRepo{keys: map[int64]*service.APIKey{9: key, 10: other}}, nil, nil, nil, nil, nil, cfg)

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	imageTasks := service.NewImageTaskServiceWithUploader(repository.NewImageTaskStore(rdb), nil, time.Hour, time.Minute)

	h := NewCreationStudioHandler(apiKeys, gateway, imageTasks)
	dispatcher := &creationFakeDispatcher{}
	h.SetDispatcher(dispatcher)
	return &creationHandlerFixture{handler: h, dispatcher: dispatcher, key: key, imageTasks: imageTasks}
}

func creationSubmitContext(t *testing.T, body string, replay bool) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/user/creation/tasks", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Cookie", "session=secret-cookie")
	request.Header.Set("Authorization", "Bearer outer-jwt")
	request.Header.Set("User-Agent", "creation-test")
	request.Header.Set("X-Forwarded-For", "203.0.113.9")
	request.RemoteAddr = "203.0.113.9:54321"
	ctx := request.Context()
	if replay {
		ctx = service.WithCreationStudioReplay(ctx)
	}
	c.Request = request.WithContext(ctx)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
	return c, recorder
}

func creationImageAccountFixture() service.Account {
	return service.Account{
		ID: 1, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true,
		Credentials: map[string]any{"model_mapping": map[string]any{"gpt-image-2": "gpt-image-2"}},
	}
}

func creationVideoAccountFixture() service.Account {
	return service.Account{
		ID: 2, Platform: service.PlatformVideo, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true,
		Credentials: map[string]any{
			"video_protocol": service.VideoProtocolSeedance,
			"model_mapping":  map[string]any{"doubao-seedance-1-0-pro": "doubao-seedance-1-0-pro"},
		},
	}
}

func TestCreationStudioSubmitReplaysAsTheSelectedKey(t *testing.T) {
	fixture := newCreationHandlerFixture(t, service.PlatformOpenAI, creationImageAccountFixture())
	fixture.dispatcher.status = http.StatusAccepted
	fixture.dispatcher.body = `{"id":"imgtask_abc","status":"processing"}`

	body := `{"api_key_id":9,"mode":"image","model":"gpt-image-2","protocol":"openai","prompt":"a cat",
		"params":{"size":"1024x1024","quality":"auto","n":1}}`
	c, recorder := creationSubmitContext(t, body, false)
	fixture.handler.Submit(c)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	require.Len(t, fixture.dispatcher.requests, 1)
	inner := fixture.dispatcher.requests[0]
	require.Equal(t, service.CreationPathImageGenerations, inner.URL.Path)
	require.Equal(t, "Bearer "+fixture.key.Key, inner.Header.Get("Authorization"))
	require.Empty(t, inner.Header.Get("Cookie"), "outer cookies must not be replayed")
	require.Equal(t, "creation-test", inner.Header.Get("User-Agent"))
	require.Equal(t, "203.0.113.9", inner.Header.Get("X-Forwarded-For"))
	require.Equal(t, "203.0.113.9:54321", inner.RemoteAddr)
	require.True(t, service.IsCreationStudioReplay(inner.Context()), "the replay marker must be set")

	payload := recorder.Body.Bytes()
	require.Equal(t, "imgtask_abc", gjson.GetBytes(payload, "data.task_id").String())
	require.Equal(t, "imgtask_abc", gjson.GetBytes(payload, "data.catalog_id").String())
	require.Equal(t, "processing", gjson.GetBytes(payload, "data.status").String())
	require.Equal(t, "image", gjson.GetBytes(payload, "data.mode").String())
	require.Equal(t, "openai", gjson.GetBytes(payload, "data.protocol").String())
	require.Positive(t, gjson.GetBytes(payload, "data.estimated_cost").Float())
	require.NotContains(t, recorder.Body.String(), fixture.key.Key, "the key plaintext must never leave the backend")
}

func TestCreationStudioSubmitVideoCatalogID(t *testing.T) {
	fixture := newCreationHandlerFixture(t, service.PlatformVideo, creationVideoAccountFixture())
	fixture.dispatcher.status = http.StatusOK
	fixture.dispatcher.body = `{"id":"cgt-123"}`
	fixture.dispatcher.status = http.StatusAccepted

	body := `{"api_key_id":9,"mode":"video","model":"doubao-seedance-1-0-pro","protocol":"seedance","prompt":"waves",
		"params":{"resolution":"720p","duration":5,"ratio":"16:9","watermark":"false"}}`
	c, recorder := creationSubmitContext(t, body, false)
	fixture.handler.Submit(c)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, service.CreationPathSeedance, fixture.dispatcher.requests[0].URL.Path)
	require.Equal(t, "cgt-123", gjson.GetBytes(recorder.Body.Bytes(), "data.task_id").String())
	// No catalog record exists yet, so the panel falls back to a search lookup.
	require.Empty(t, gjson.GetBytes(recorder.Body.Bytes(), "data.catalog_id").String())
}

func TestCreationStudioSubmitKeyOwnership(t *testing.T) {
	fixture := newCreationHandlerFixture(t, service.PlatformOpenAI, creationImageAccountFixture())
	body := `{"api_key_id":10,"mode":"image","model":"gpt-image-2","protocol":"openai","prompt":"a cat","params":{}}`
	c, recorder := creationSubmitContext(t, body, false)
	fixture.handler.Submit(c)
	require.Equal(t, http.StatusNotFound, recorder.Code)
	require.Equal(t, "CREATION_KEY_NOT_FOUND", gjson.GetBytes(recorder.Body.Bytes(), "reason").String())
	require.Empty(t, fixture.dispatcher.requests, "a foreign key must never reach the dispatcher")
}

func TestCreationStudioSubmitRejectsUnknownModel(t *testing.T) {
	fixture := newCreationHandlerFixture(t, service.PlatformOpenAI, creationImageAccountFixture())
	body := `{"api_key_id":9,"mode":"image","model":"gpt-image-9","protocol":"openai","prompt":"a cat","params":{}}`
	c, recorder := creationSubmitContext(t, body, false)
	fixture.handler.Submit(c)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, "CREATION_MODEL_NOT_AVAILABLE", gjson.GetBytes(recorder.Body.Bytes(), "reason").String())
}

func TestCreationStudioSubmitRequiresDispatcher(t *testing.T) {
	fixture := newCreationHandlerFixture(t, service.PlatformOpenAI, creationImageAccountFixture())
	fixture.handler.SetDispatcher(nil)
	body := `{"api_key_id":9,"mode":"image","model":"gpt-image-2","protocol":"openai","prompt":"a cat","params":{}}`
	c, recorder := creationSubmitContext(t, body, false)
	fixture.handler.Submit(c)
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Equal(t, "CREATION_DISPATCHER_UNAVAILABLE", gjson.GetBytes(recorder.Body.Bytes(), "reason").String())
}

func TestCreationStudioSubmitNormalizesInnerErrors(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		wantStatus int
		wantReason string
	}{
		{
			name: "insufficient balance", status: http.StatusForbidden,
			body:       `{"code":"billing_error","message":"insufficient balance"}`,
			wantStatus: http.StatusForbidden, wantReason: "INSUFFICIENT_BALANCE",
		},
		{
			name: "reservation rejected", status: http.StatusPaymentRequired,
			body:       `{"code":"insufficient_balance","message":"Available balance does not cover the reservation"}`,
			wantStatus: http.StatusPaymentRequired, wantReason: "insufficient_balance",
		},
		{
			name: "quota", status: http.StatusTooManyRequests,
			body:       `{"code":"USAGE_LIMIT_EXCEEDED","message":"daily limit"}`,
			wantStatus: http.StatusTooManyRequests, wantReason: "USAGE_LIMIT_EXCEEDED",
		},
		{
			name: "no capacity", status: http.StatusServiceUnavailable,
			body:       `{"code":"seedance_no_eligible_account","message":"no account"}`,
			wantStatus: http.StatusServiceUnavailable, wantReason: "seedance_no_eligible_account",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newCreationHandlerFixture(t, service.PlatformOpenAI, creationImageAccountFixture())
			fixture.dispatcher.status = tc.status
			fixture.dispatcher.body = tc.body
			body := `{"api_key_id":9,"mode":"image","model":"gpt-image-2","protocol":"openai","prompt":"a cat","params":{}}`
			c, recorder := creationSubmitContext(t, body, false)
			fixture.handler.Submit(c)
			require.Equal(t, tc.wantStatus, recorder.Code)
			require.Equal(t, tc.wantReason, gjson.GetBytes(recorder.Body.Bytes(), "reason").String())
		})
	}
}

// TestCreationStudioSubmitRejectsRecursiveReplay covers the recursion guard.
func TestCreationStudioSubmitRejectsRecursiveReplay(t *testing.T) {
	fixture := newCreationHandlerFixture(t, service.PlatformOpenAI, creationImageAccountFixture())
	body := `{"api_key_id":9,"mode":"image","model":"gpt-image-2","protocol":"openai","prompt":"a cat","params":{}}`
	c, recorder := creationSubmitContext(t, body, true)
	fixture.handler.Submit(c)
	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.Equal(t, "CREATION_INTERNAL_ERROR", gjson.GetBytes(recorder.Body.Bytes(), "reason").String())
}

// TestCreationStudioSubmitBodyTooLarge covers the V1 limit and the envelope.
func TestCreationStudioSubmitBodyTooLarge(t *testing.T) {
	fixture := newCreationHandlerFixture(t, service.PlatformOpenAI, creationImageAccountFixture())
	payload, err := json.Marshal(map[string]any{
		"api_key_id": 9, "mode": "image", "model": "gpt-image-2", "protocol": "openai",
		"prompt": strings.Repeat("a", (1<<20)+16),
	})
	require.NoError(t, err)
	c, recorder := creationSubmitContext(t, string(payload), false)
	fixture.handler.Submit(c)
	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
	require.Equal(t, "CREATION_BODY_TOO_LARGE", gjson.GetBytes(recorder.Body.Bytes(), "reason").String())
}

func TestCreationStudioOptionsReturnsCatalog(t *testing.T) {
	fixture := newCreationHandlerFixture(t, service.PlatformOpenAI, creationImageAccountFixture())
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/user/creation/options?api_key_id=9&timezone=Asia%2FShanghai", nil)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
	fixture.handler.Options(c)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	body := recorder.Body.Bytes()
	require.True(t, gjson.GetBytes(body, "data.image.available").Bool())
	require.Equal(t, "image:openai:gpt-image-2", gjson.GetBytes(body, "data.models.0.id").String())
	require.NotContains(t, recorder.Body.String(), fixture.key.Key)
}

// TestCreationStudioQuotaExhaustedOutranksModelAvailability: the model is fine,
// the key cannot pay, so the answer is 429 rather than 400.
func TestCreationStudioQuotaExhaustedOutranksModelAvailability(t *testing.T) {
	fixture := newCreationHandlerFixture(t, service.PlatformOpenAI, creationImageAccountFixture())
	fixture.key.Quota, fixture.key.QuotaUsed = 0.1, 3.2

	body := `{"api_key_id":9,"mode":"image","model":"gpt-image-2","protocol":"openai","prompt":"a cat","params":{}}`
	c, recorder := creationSubmitContext(t, body, false)
	fixture.handler.Submit(c)
	require.Equal(t, http.StatusTooManyRequests, recorder.Code, recorder.Body.String())
	require.Equal(t, "API_KEY_QUOTA_EXHAUSTED", gjson.GetBytes(recorder.Body.Bytes(), "reason").String())
	require.Empty(t, fixture.dispatcher.requests, "an exhausted key must not reach the dispatcher")
}

func TestCreationStudioEstimateQuotaExhausted(t *testing.T) {
	fixture := newCreationHandlerFixture(t, service.PlatformOpenAI, creationImageAccountFixture())
	fixture.key.Quota, fixture.key.QuotaUsed = 0.1, 3.2

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := `{"api_key_id":9,"mode":"image","model":"gpt-image-2","protocol":"openai","params":{"size":"1024x1024"}}`
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/user/creation/estimate", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
	fixture.handler.Estimate(c)
	require.Equal(t, http.StatusTooManyRequests, recorder.Code, recorder.Body.String())
	require.Equal(t, "API_KEY_QUOTA_EXHAUSTED", gjson.GetBytes(recorder.Body.Bytes(), "reason").String())
}

// TestCreationStudioDisabledKeyKeepsModelUnavailableBehaviour: a genuinely
// unusable key still reports the model as unavailable, as before.
func TestCreationStudioDisabledKeyKeepsModelUnavailableBehaviour(t *testing.T) {
	fixture := newCreationHandlerFixture(t, service.PlatformOpenAI, creationImageAccountFixture())
	fixture.key.Status = service.StatusDisabled

	body := `{"api_key_id":9,"mode":"image","model":"gpt-image-2","protocol":"openai","prompt":"a cat","params":{}}`
	c, recorder := creationSubmitContext(t, body, false)
	fixture.handler.Submit(c)
	require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
	require.Equal(t, "CREATION_MODEL_NOT_AVAILABLE", gjson.GetBytes(recorder.Body.Bytes(), "reason").String())
}
