//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type imagePlatformFailoverHTTP struct {
	service.HTTPUpstream
	mu  sync.Mutex
	ids []int64
}

func (u *imagePlatformFailoverHTTP) Do(_ *http.Request, _ string, id int64, _ int) (*http.Response, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.ids = append(u.ids, id)
	status, body := 200, `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"aW1hZ2U="}}]}}]}`
	if id == 1 {
		status, body = 429, `{"error":{"message":"secret https://private.example/stack"}}`
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
}

func TestImagePlatformHandler429SwitchesAccount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(17)
	accounts := []service.Account{}
	for i := range 2 {
		accounts = append(accounts, service.Account{ID: int64(i + 1), Platform: service.PlatformImage, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Priority: i,
			Credentials: map[string]any{"api_key": "secret", "image_protocol": "gemini"}})
	}
	repo := openAIImagesFailoverAccountRepo{accounts: accounts}
	upstream := &imagePlatformFailoverHTTP{}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billing.Stop)
	gateway := service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, nil, cfg, nil, nil, nil, nil, billing, upstream, nil, nil, nil, nil, nil, nil, nil, nil)
	h := NewOpenAIGatewayHandler(gateway, service.NewConcurrencyService(nil), billing, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
	h.maxAccountSwitches = 3
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{"model":"gemini-2.5-flash-image","prompt":"draw","n":3}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: 9, GroupID: &groupID, Group: &service.Group{ID: groupID, Platform: service.PlatformImage, AllowImageGeneration: true}, User: &service.User{ID: 7}})
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 7})
	h.ImagePlatformImages(c)
	require.Equal(t, 200, rec.Code, rec.Body.String())
	require.Equal(t, []int64{1, 1, 1, 2, 2, 2}, upstream.ids)
	require.Contains(t, rec.Body.String(), "aW1hZ2U=")
	require.NotContains(t, rec.Body.String(), "secret")
}

type imagePlatformLifecycleUsage struct {
	service.UsageLogRepository
	mu   sync.Mutex
	logs []*service.UsageLog
}

func (u *imagePlatformLifecycleUsage) Create(_ context.Context, log *service.UsageLog) (bool, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.logs = append(u.logs, log)
	return true, nil
}

type imagePlatformLifecycleHTTP struct {
	service.HTTPUpstream
	fail bool
}

func (u imagePlatformLifecycleHTTP) Do(*http.Request, string, int64, int) (*http.Response, error) {
	status, body := 200, `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"aW1hZ2U="}}]}}]}`
	if u.fail {
		status, body = 400, `{"error":{"message":"private upstream secret stack"}}`
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
}

func TestImagePlatformAsyncFullLifecycleBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[fail], func(t *testing.T) {
			gid, price := int64(17), .17
			repo := openAIImagesFailoverAccountRepo{accounts: []service.Account{{ID: 1, Platform: service.PlatformImage, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "secret", "image_protocol": "gemini"}}}}
			cfg := &config.Config{RunMode: config.RunModeSimple}
			billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
			t.Cleanup(billing.Stop)
			usage := &imagePlatformLifecycleUsage{}
			gateway := service.NewOpenAIGatewayService(repo, usage, nil, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, billing, imagePlatformLifecycleHTTP{fail: fail}, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil)
			images := NewOpenAIGatewayHandler(gateway, service.NewConcurrencyService(nil), billing, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
			tasks := service.NewImageTaskServiceWithUploader(&asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}, nil, time.Hour, time.Minute)
			h := NewAsyncImageHandler(tasks, images)
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: 9, UserID: 7, GroupID: &gid, Group: &service.Group{ID: gid, Platform: service.PlatformImage, AllowImageGeneration: true, RateMultiplier: 1, ImagePrice1K: &price}, User: &service.User{ID: 7}})
				c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 7})
				c.Next()
			})
			router.POST("/v1/images/generations/async", h.Submit)
			router.GET("/v1/images/tasks/:task_id", h.Get)
			req := httptest.NewRequest(http.MethodPost, "/v1/images/generations/async", strings.NewReader(`{"model":"gemini-2.5-flash-image","prompt":"draw","n":2,"size":"1536x1024"}`))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			require.Equal(t, 202, rec.Code, rec.Body.String())
			pollURL := gjson.GetBytes(rec.Body.Bytes(), "poll_url").String()
			var final []byte
			require.Eventually(t, func() bool {
				poll := httptest.NewRecorder()
				router.ServeHTTP(poll, httptest.NewRequest(http.MethodGet, pollURL, nil))
				final = append([]byte(nil), poll.Body.Bytes()...)
				status := gjson.GetBytes(final, "status").String()
				return status == service.ImageTaskStatusCompleted || status == service.ImageTaskStatusFailed
			}, 3*time.Second, 10*time.Millisecond)
			usage.mu.Lock()
			defer usage.mu.Unlock()
			if fail {
				require.Equal(t, service.ImageTaskStatusFailed, gjson.GetBytes(final, "status").String())
				require.Empty(t, usage.logs)
				var response struct {
					Error imageRegressionError `json:"error"`
				}
				require.NoError(t, json.Unmarshal(final, &response))
				require.Equal(t, "invalid_request_error", response.Error.Type)
				require.NotNil(t, response.Error.Code)
				require.Equal(t, "upstream_invalid_request", *response.Error.Code)
				require.Nil(t, response.Error.Param)
				require.NotContains(t, response.Error.Message, "secret")
			} else {
				require.Equal(t, service.ImageTaskStatusCompleted, gjson.GetBytes(final, "status").String())
				require.Len(t, gjson.GetBytes(final, "result.data").Array(), 2)
				require.Len(t, usage.logs, 1)
				require.InDelta(t, .34, usage.logs[0].ActualCost, 1e-9)
				require.Equal(t, "1K", *usage.logs[0].ImageSize)
			}
		})
	}
}

func TestImagePlatformModelsAndDefaults(t *testing.T) {
	for _, protocol := range []string{"openai", "gemini", "grok", "empty"} {
		accounts := []service.Account{}
		if protocol != "empty" {
			accounts = append(accounts, service.Account{Platform: service.PlatformImage, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"image_protocol": protocol}})
		}
		repo := &gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{17: accounts}}
		h := newGatewayModelsHandlerForTest(repo)
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest("GET", "/v1/models", nil)
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{Group: &service.Group{ID: 17, Platform: service.PlatformImage}})
		h.Models(c)
		require.Equal(t, 200, rec.Code)
		var output gatewayModelsResponseForTest
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &output))
		var models []string
		for _, item := range output.Data {
			models = append(models, item.ID)
		}
		if protocol == "empty" {
			require.ElementsMatch(t, defaultModelIDsForPlatform(service.PlatformImage), models)
		} else {
			require.ElementsMatch(t, service.DefaultImageModelIDs(protocol), models)
		}
	}
}

func TestImagePlatformCreationInsufficientBalance(t *testing.T) {
	for _, balance := range []float64{0, .001} {
		fixture := newCreationPrecheckFixture(t, balance)
		group := &service.Group{ID: 3, Platform: service.PlatformImage, AllowImageGeneration: true, RateMultiplier: 1, Status: service.StatusActive}
		c, rec := creationPrecheckRequestWithSize(t, true, "1024x1024", group)
		fixture.handler.Submit(c)
		require.Contains(t, []int{402, 403}, rec.Code)
		require.Zero(t, creationPrecheckTaskCount(t, fixture.rdb))
	}
}

func TestImagePlatformAsyncRequestAndDispatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gateway := &service.OpenAIGatewayService{}
	h := NewAsyncImageHandler(nil, &OpenAIGatewayHandler{gatewayService: gateway})
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations/async", nil)
	c.Request.Header.Set("Content-Type", "application/json")
	require.NoError(t, h.validateRequest(c, service.PlatformImage, []byte(`{"model":"imagen-4.0-generate-001","prompt":"draw"}`)))
	platform, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context())
	require.True(t, ok)
	require.Equal(t, service.PlatformImage, platform)
}
