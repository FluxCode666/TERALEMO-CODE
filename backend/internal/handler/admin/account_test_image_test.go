//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type imageAccountTestAdminRepo struct {
	service.AccountRepository
	account *service.Account
	cleared int
}

func (r *imageAccountTestAdminRepo) GetByID(context.Context, int64) (*service.Account, error) {
	return r.account, nil
}

func (r *imageAccountTestAdminRepo) ClearError(context.Context, int64) error {
	r.cleared++
	return nil
}

type imageAccountTestAdminHTTP struct {
	service.HTTPUpstream
	client *http.Client
}

func (u imageAccountTestAdminHTTP) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return u.client.Do(req)
}

func TestAccountHandlerImageConnectionSSEAndRecovery(t *testing.T) {
	for _, protocol := range []string{"openai", "gemini", "grok"} {
		for _, success := range []bool{true, false} {
			name := protocol + "/failure"
			if success {
				name = protocol + "/success"
			}
			t.Run(name, func(t *testing.T) {
				calls := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					calls++
					w.Header().Set("Content-Type", "application/json")
					if !success {
						w.WriteHeader(401)
						_, _ = io.WriteString(w, `{"error":{"message":"handler-image-secret https://hidden.example /srv/stack"}}`)
						return
					}
					if protocol == "gemini" {
						_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"aW1hZ2U="}}]}}]}`)
					} else {
						_, _ = io.WriteString(w, `{"data":[{"b64_json":"aW1hZ2U="}]}`)
					}
				}))
				defer server.Close()
				repo := &imageAccountTestAdminRepo{account: &service.Account{
					ID: 42, Platform: service.PlatformImage, Type: service.AccountTypeAPIKey, Status: service.StatusError,
					Credentials: map[string]any{"api_key": "handler-image-secret", "base_url": server.URL, "image_protocol": protocol},
				}}
				cfg := &config.Config{}
				cfg.Security.URLAllowlist.AllowInsecureHTTP = true
				testService := service.NewAccountTestService(repo, nil, nil, nil, nil, imageAccountTestAdminHTTP{client: server.Client()}, cfg, nil)
				handler := NewAccountHandler(nil, nil, nil, nil, nil, nil, nil, nil, testService, nil, nil, nil, nil, nil)
				handler.rateLimitService = service.NewRateLimitService(repo, nil, cfg, nil, nil)
				gin.SetMode(gin.TestMode)
				router := gin.New()
				router.POST("/accounts/:id/test", handler.Test)
				req := httptest.NewRequest(http.MethodPost, "/accounts/42/test", strings.NewReader(`{"prompt":"draw a cat"}`))
				req.Header.Set("Content-Type", "application/json")
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, req)
				require.Equal(t, 200, recorder.Code)
				require.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
				var image, complete bool
				for _, line := range strings.Split(recorder.Body.String(), "\n") {
					if !strings.HasPrefix(line, "data:") {
						continue
					}
					var event service.TestEvent
					require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &event))
					if event.Type == "image" {
						image = true
						require.Equal(t, "data:image/png;base64,aW1hZ2U=", event.ImageURL)
					}
					if event.Type == "test_complete" {
						complete = event.Success
					}
				}
				require.Equal(t, success, image)
				require.Equal(t, success, complete)
				require.Equal(t, 1, calls)
				if success {
					require.Equal(t, 1, repo.cleared)
				} else {
					require.Zero(t, repo.cleared)
					require.Contains(t, recorder.Body.String(), `"type":"error"`)
				}
				for _, secret := range []string{"handler-image-secret", server.URL, "hidden.example", "/srv/stack"} {
					require.NotContains(t, recorder.Body.String(), secret)
				}
			})
		}
	}
}
