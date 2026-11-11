//go:build unit

package handler

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type imageErrorHTTP struct {
	service.HTTPUpstream
	mu                          sync.Mutex
	status                      int
	sse, partial, recoverSecond bool
	code                        string
	ids                         []int64
}

func (u *imageErrorHTTP) Do(_ *http.Request, _ string, id int64, _ int) (*http.Response, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.ids = append(u.ids, id)
	header := http.Header{"Content-Type": {"application/json"}, "Retry-After": {"13"}}
	status := u.status
	message := "relay-secret-42 https://relay.example/v1 relay.example\n at render (/srv/image.js:42)"
	body := fmt.Sprintf(`{"error":{"message":%q,"code":%q,"param":"prompt"}}`, message, u.code)
	if u.sse {
		status = 200
		header.Set("Content-Type", "text/event-stream")
		body = "data: " + strings.Replace(body, `{"error":`, `{"type":"error","error":`, 1) + "\n\n"
		if u.partial || (u.recoverSecond && id == 2) {
			body = "data: {\"type\":\"image_generation.completed\",\"b64_json\":\"aW1hZ2U=\"}\n\n" + body
		}
		if u.recoverSecond && id == 2 {
			body = "data: {\"type\":\"image_generation.completed\",\"b64_json\":\"aW1hZ2U=\"}\n\ndata: [DONE]\n\n"
		}
	}
	return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body))}, nil
}

func imageErrorHandlerRequest(t *testing.T, u *imageErrorHTTP, protocol string) (*httptest.ResponseRecorder, *imagePlatformLifecycleUsage) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	accounts := []service.Account{}
	for i := range 2 {
		accounts = append(accounts, service.Account{ID: int64(i + 1), Platform: service.PlatformImage, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Priority: i, Credentials: map[string]any{"api_key": "relay-secret-42", "base_url": "https://relay.example/v1", "image_protocol": protocol}})
	}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Default.RateMultiplier = 1
	billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billing.Stop)
	usage := &imagePlatformLifecycleUsage{}
	gateway := service.NewOpenAIGatewayService(openAIImagesFailoverAccountRepo{accounts: accounts}, usage, nil, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, billing, u, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil)
	h := NewOpenAIGatewayHandler(gateway, service.NewConcurrencyService(nil), billing, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
	h.maxAccountSwitches = 3
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	model := "gemini-2.5-flash-image"
	if protocol == "openai" {
		model = "gpt-image-2"
	}
	c.Request = httptest.NewRequest("POST", "/v1/images/generations", strings.NewReader(fmt.Sprintf(`{"model":%q,"prompt":"draw","stream":%t}`, model, u.sse)))
	c.Request.Header.Set("Content-Type", "application/json")
	gid, price := int64(17), .17
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: 9, UserID: 7, GroupID: &gid, Group: &service.Group{ID: gid, Platform: service.PlatformImage, AllowImageGeneration: true, RateMultiplier: 1, ImagePrice2K: &price}, User: &service.User{ID: 7}})
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 7})
	h.ImagePlatformImages(c)
	return rec, usage
}

func TestImagePlatformExhaustionKeepsMappedError(t *testing.T) {
	for _, status := range []int{404, 503, 504, 429} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			u := &imageErrorHTTP{status: status, code: "model_not_found"}
			rec, usage := imageErrorHandlerRequest(t, u, "gemini")
			require.Equal(t, status, rec.Code, rec.Body.String())
			require.Equal(t, []int64{1, 2}, u.ids)
			require.Empty(t, usage.logs)
			if status == 404 {
				require.Equal(t, "model_not_found", gjson.GetBytes(rec.Body.Bytes(), "error.code").String())
			}
			if status == 429 {
				require.Equal(t, "13", rec.Header().Get("Retry-After"))
			}
			for _, secret := range []string{"relay-secret-42", "relay.example", "/srv/image"} {
				require.NotContains(t, rec.Body.String(), secret)
			}
		})
	}
}

func TestImagePlatformSSEFirstErrorRetries(t *testing.T) {
	for _, code := range []string{"invalid_api_key", "rate_limit_exceeded", "model_not_found"} {
		t.Run(code, func(t *testing.T) {
			u := &imageErrorHTTP{sse: true, recoverSecond: true, code: code}
			rec, usage := imageErrorHandlerRequest(t, u, "openai")
			require.Equal(t, 200, rec.Code, rec.Body.String())
			require.Equal(t, []int64{1, 2}, u.ids)
			require.Contains(t, rec.Body.String(), "aW1hZ2U=")
			require.NotContains(t, rec.Body.String(), "relay-secret-42")
			require.Len(t, usage.logs, 1)
		})
	}
}

func TestImagePlatformSSEPolicyAndPartial(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(fmt.Sprint(partial), func(t *testing.T) {
			u := &imageErrorHTTP{sse: true, partial: partial, code: "content_policy_violation"}
			rec, usage := imageErrorHandlerRequest(t, u, "openai")
			require.Equal(t, []int64{1}, u.ids)
			require.Contains(t, rec.Body.String(), "content_policy_violation")
			require.Contains(t, rec.Body.String(), "prompt")
			require.NotContains(t, rec.Body.String(), "relay-secret-42")
			require.NotContains(t, rec.Body.String(), "/srv/image")
			if partial {
				require.Len(t, usage.logs, 1)
				require.Equal(t, 1, usage.logs[0].ImageCount)
				require.InDelta(t, .17, usage.logs[0].ActualCost, 1e-9)
			} else {
				require.Empty(t, usage.logs)
			}
		})
	}
}
