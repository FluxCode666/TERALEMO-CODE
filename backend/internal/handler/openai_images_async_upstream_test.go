//go:build unit

package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type openAIImagesAsyncHTTPUpstream struct {
	service.HTTPUpstream
	client *http.Client
}

func (u openAIImagesAsyncHTTPUpstream) Do(request *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return u.client.Do(request)
}

type openAIImagesAsyncSettingRepo struct {
	service.SettingRepository
	values map[string]string
}

func (r *openAIImagesAsyncSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	value, ok := r.values[key]
	if !ok {
		return "", service.ErrSettingNotFound
	}
	return value, nil
}

func (r *openAIImagesAsyncSettingRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			values[key] = value
		}
	}
	return values, nil
}

func (r *openAIImagesAsyncSettingRepo) SetMultiple(_ context.Context, values map[string]string) error {
	for key, value := range values {
		r.values[key] = value
	}
	return nil
}

func newOpenAIImagesAsyncTestGateway(accounts []service.Account, upstream service.HTTPUpstream, settings *service.SettingService) (*service.OpenAIGatewayService, *config.Config) {
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	var rateLimit *service.RateLimitService
	if settings != nil {
		rateLimit = service.NewRateLimitService(openAIImagesFailoverAccountRepo{accounts: accounts}, nil, cfg, nil, nil)
		rateLimit.SetSettingService(settings)
	}
	gateway := service.NewOpenAIGatewayService(
		openAIImagesFailoverAccountRepo{accounts: accounts},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		cfg,
		nil,
		nil,
		nil,
		rateLimit,
		nil,
		upstream,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		settings,
		nil,
	)
	return gateway, cfg
}

func TestAsyncImageHandlerSubmitAndPoll_AsyncAPIKeyUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const upstreamTaskID = "imgtask_upstream"
	const imageResult = `{"created":123,"data":[{"url":"https://images.example.test/one.png"},{"url":"https://images.example.test/two.png"}],"usage":{"input_tokens":10,"output_tokens":20,"total_tokens":30}}`
	var submissions atomic.Int32
	var polls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assert.Equal(t, "Bearer upstream-key", request.Header.Get("Authorization"))
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("Retry-After", "1")
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/v1/images/generations":
			submissions.Add(1)
			writer.Header().Set("Location", "/v1/images/tasks/"+upstreamTaskID)
			writer.WriteHeader(http.StatusAccepted)
			_, _ = fmt.Fprintf(writer, `{"id":%q,"task_id":%q,"object":"image.generation.task","status":"processing"}`, upstreamTaskID, upstreamTaskID)
		case request.Method == http.MethodGet && request.URL.Path == "/v1/images/tasks/"+upstreamTaskID:
			if polls.Add(1) == 1 {
				_, _ = fmt.Fprintf(writer, `{"id":%q,"object":"image.generation.task","status":"processing"}`, upstreamTaskID)
				return
			}
			_, _ = fmt.Fprintf(writer, `{"id":%q,"object":"image.generation.task","status":"completed","http_status":200,"result":%s}`, upstreamTaskID, imageResult)
		default:
			t.Errorf("unexpected upstream request: %s %s", request.Method, request.URL.Path)
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(upstream.Close)

	gateway, _ := newOpenAIImagesAsyncTestGateway(nil, openAIImagesAsyncHTTPUpstream{client: upstream.Client()}, nil)
	account := &service.Account{
		ID:       1,
		Platform: service.PlatformOpenAI,
		Type:     service.AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "upstream-key",
			"base_url": upstream.URL,
		},
	}
	store := &asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}
	tasks := service.NewImageTaskServiceWithUploader(store, nil, time.Hour, time.Minute)
	handler := &AsyncImageHandler{tasks: tasks}
	type forwardOutcome struct {
		result *service.OpenAIForwardResult
		err    error
		header http.Header
	}
	forwarded := make(chan forwardOutcome, 1)
	handler.execute = func(_ string, taskCtx *gin.Context) {
		body, err := io.ReadAll(taskCtx.Request.Body)
		if err != nil {
			forwarded <- forwardOutcome{err: err}
			taskCtx.Status(http.StatusInternalServerError)
			return
		}
		parsed, err := gateway.ParseOpenAIImagesRequest(taskCtx, body)
		if err != nil {
			forwarded <- forwardOutcome{err: err}
			taskCtx.Status(http.StatusBadRequest)
			return
		}
		result, err := gateway.ForwardImages(taskCtx.Request.Context(), taskCtx, account, body, parsed, "")
		forwarded <- forwardOutcome{result: result, err: err, header: taskCtx.Writer.Header().Clone()}
	}

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
			ID:     9,
			UserID: 7,
			Group:  &service.Group{ID: 3, Platform: service.PlatformOpenAI, AllowImageGeneration: true},
		})
		c.Next()
	})
	router.POST("/v1/images/generations/async", handler.Submit)
	router.GET("/v1/images/tasks/:task_id", handler.Get)

	requestCtx, cancelRequest := context.WithCancel(context.Background())
	t.Cleanup(cancelRequest)
	request := httptest.NewRequest(http.MethodPost, "/v1/images/generations/async", strings.NewReader(`{"model":"gpt-image-1","prompt":"cat","n":4}`)).WithContext(requestCtx)
	request.Header.Set("Content-Type", "application/json")
	accepted := httptest.NewRecorder()
	router.ServeHTTP(accepted, request)
	require.Equal(t, http.StatusAccepted, accepted.Code)
	taskID := gjson.GetBytes(accepted.Body.Bytes(), "task_id").String()
	pollURL := gjson.GetBytes(accepted.Body.Bytes(), "poll_url").String()
	require.NotEmpty(t, taskID)
	require.NotEqual(t, upstreamTaskID, taskID)
	require.Equal(t, "/v1/images/tasks/"+taskID, pollURL)
	require.Equal(t, pollURL, accepted.Header().Get("Location"))
	cancelRequest()

	select {
	case outcome := <-forwarded:
		require.NoError(t, outcome.err)
		require.NotNil(t, outcome.result)
		require.Equal(t, 2, outcome.result.ImageCount)
		require.Empty(t, outcome.header.Get("Location"))
		require.Empty(t, outcome.header.Get("Retry-After"))
	case <-time.After(5 * time.Second):
		t.Fatal("image gateway did not finish polling the upstream task")
	}
	require.Eventually(t, func() bool {
		task, err := tasks.Get(context.Background(), service.ImageTaskOwner{UserID: 7, APIKeyID: 9}, taskID)
		return err == nil && task.Status == service.ImageTaskStatusCompleted
	}, time.Second, 10*time.Millisecond)

	poll := httptest.NewRecorder()
	router.ServeHTTP(poll, httptest.NewRequest(http.MethodGet, pollURL, nil))
	require.Equal(t, http.StatusOK, poll.Code)
	require.Empty(t, poll.Header().Get("Retry-After"))
	require.Equal(t, service.ImageTaskStatusCompleted, gjson.GetBytes(poll.Body.Bytes(), "status").String())
	require.Equal(t, int64(http.StatusOK), gjson.GetBytes(poll.Body.Bytes(), "http_status").Int())
	require.JSONEq(t, imageResult, gjson.GetBytes(poll.Body.Bytes(), "result").Raw)
	require.Len(t, gjson.GetBytes(poll.Body.Bytes(), "result.data").Array(), 2)
	require.NotContains(t, poll.Body.String(), upstreamTaskID)
	require.Equal(t, int32(1), submissions.Load())
	require.Equal(t, int32(2), polls.Load())
}

func TestOpenAIGatewayHandlerImages_AsyncTaskErrorSkipsScheduleFeedback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, testCase := range []struct {
		name              string
		accepted          bool
		status            int
		errorType         string
		wantStatsAccounts int
	}{
		{name: "accepted user error", accepted: true, status: http.StatusBadRequest, errorType: "invalid_request_error"},
		{name: "accepted authentication error", accepted: true, status: http.StatusUnauthorized, errorType: "authentication_error"},
		{name: "accepted rate limit error", accepted: true, status: http.StatusTooManyRequests, errorType: "rate_limit_error"},
		{name: "accepted server error", accepted: true, status: http.StatusBadGateway, errorType: "server_error"},
		{name: "accepted timeout error", accepted: true, status: http.StatusGatewayTimeout, errorType: "timeout_error"},
		{name: "unaccepted user error retains feedback", status: http.StatusBadRequest, errorType: "invalid_request_error", wantStatsAccounts: 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			settings := service.NewSettingService(&openAIImagesAsyncSettingRepo{values: make(map[string]string)}, &config.Config{})
			t.Cleanup(func() {
				require.NoError(t, settings.UpdateSettings(context.Background(), &service.SystemSettings{}))
			})
			require.NoError(t, settings.UpdateSettings(context.Background(), &service.SystemSettings{OpenAIAdvancedSchedulerEnabled: true}))
			errorObject := fmt.Sprintf(`{"type":%q,"code":"image_generation_failed","message":"upstream image generation failed"}`, testCase.errorType)
			var submissions atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				submissions.Add(1)
				assert.Equal(t, http.MethodPost, request.Method)
				assert.Equal(t, "/v1/images/generations", request.URL.Path)
				writer.Header().Set("Content-Type", "application/json")
				if testCase.accepted {
					writer.WriteHeader(http.StatusAccepted)
					_, _ = fmt.Fprintf(writer, `{"id":"imgtask_failed","object":"image.generation.task","status":"failed","http_status":%d,"error":%s}`, testCase.status, errorObject)
					return
				}
				writer.WriteHeader(testCase.status)
				_, _ = fmt.Fprintf(writer, `{"error":%s}`, errorObject)
			}))
			t.Cleanup(upstream.Close)

			accounts := []service.Account{
				{
					ID:          1,
					Name:        "async-image-account",
					Platform:    service.PlatformOpenAI,
					Type:        service.AccountTypeAPIKey,
					Status:      service.StatusActive,
					Schedulable: true,
					Credentials: map[string]any{"api_key": "upstream-key", "base_url": upstream.URL},
				},
				{
					ID:          2,
					Name:        "fallback-image-account",
					Platform:    service.PlatformOpenAI,
					Type:        service.AccountTypeAPIKey,
					Status:      service.StatusActive,
					Schedulable: true,
					Priority:    1,
					Credentials: map[string]any{"api_key": "fallback-key", "base_url": upstream.URL},
				},
			}
			gateway, cfg := newOpenAIImagesAsyncTestGateway(accounts, openAIImagesAsyncHTTPUpstream{client: upstream.Client()}, settings)
			require.Zero(t, gateway.SnapshotOpenAIAccountSchedulerMetrics().RuntimeStatsAccountCount)
			billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
			t.Cleanup(billing.Stop)
			handler := NewOpenAIGatewayHandler(
				gateway,
				service.NewConcurrencyService(nil),
				billing,
				service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg),
				nil,
				nil,
				nil,
				nil,
				cfg,
			)
			handler.maxAccountSwitches = 10
			groupID := int64(3)
			recorder := httptest.NewRecorder()
			requestCtx, _ := gin.CreateTestContext(recorder)
			requestCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{"model":"gpt-image-1","prompt":"cat"}`))
			requestCtx.Request.Header.Set("Content-Type", "application/json")
			requestCtx.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
				ID:      9,
				UserID:  7,
				GroupID: &groupID,
				Group:   &service.Group{ID: groupID, Platform: service.PlatformOpenAI, AllowImageGeneration: true},
				User:    &service.User{ID: 7},
			})
			requestCtx.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 7})

			handler.Images(requestCtx)

			require.Equal(t, testCase.status, recorder.Code)
			require.JSONEq(t, `{"error":`+errorObject+`}`, recorder.Body.String())
			require.Equal(t, int32(1), submissions.Load())
			metrics := gateway.SnapshotOpenAIAccountSchedulerMetrics()
			require.Equal(t, int64(1), metrics.SelectTotal, "the advanced scheduler must actually handle the request")
			require.Equal(t, testCase.wantStatsAccounts, metrics.RuntimeStatsAccountCount)
			require.Zero(t, metrics.AccountSwitchTotal)
			gateway.ReportOpenAIAccountScheduleResult(&service.Account{ID: 99, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey}, "gpt-image-1", true, nil)
			require.Equal(t, testCase.wantStatsAccounts+1, gateway.SnapshotOpenAIAccountSchedulerMetrics().RuntimeStatsAccountCount,
				"successful schedule feedback must remain observable on the same gateway")
		})
	}
}

type openAIImagesAsyncFlushWriter struct {
	gin.ResponseWriter
	flushed chan struct{}
}

func (w openAIImagesAsyncFlushWriter) Flush() {
	w.ResponseWriter.Flush()
	select {
	case w.flushed <- struct{}{}:
	default:
	}
}

func TestAsyncImageHandlerRun_AsyncAPIKeyFailureAfterJSONKeepalive(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const errorObject = `{"type":"invalid_request_error","code":"image_generation_failed","message":"upstream image generation failed"}`
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assert.Equal(t, http.MethodPost, request.Method)
		assert.Equal(t, "/v1/images/generations", request.URL.Path)
		assert.Equal(t, "Bearer upstream-key", request.Header.Get("Authorization"))
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusAccepted)
		_, _ = fmt.Fprintf(writer, `{"id":"imgtask_failed","object":"image.generation.task","status":"failed","http_status":400,"error":%s}`, errorObject)
	}))
	t.Cleanup(upstream.Close)
	gateway, _ := newOpenAIImagesAsyncTestGateway(nil, openAIImagesAsyncHTTPUpstream{client: upstream.Client()}, nil)
	account := &service.Account{
		ID:          1,
		Platform:    service.PlatformOpenAI,
		Type:        service.AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "upstream-key", "base_url": upstream.URL},
	}
	store := &asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}
	tasks := service.NewImageTaskServiceWithUploader(store, nil, time.Hour, time.Minute)
	owner := service.ImageTaskOwner{UserID: 7, APIKeyID: 9}
	task, err := tasks.Create(context.Background(), owner)
	require.NoError(t, err)
	const requestBody = `{"model":"gpt-image-1","prompt":"cat"}`
	recorder := httptest.NewRecorder()
	taskCtx, _ := gin.CreateTestContext(recorder)
	taskCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(requestBody))
	taskCtx.Request.Header.Set("Content-Type", "application/json")
	parsed, err := gateway.ParseOpenAIImagesRequest(taskCtx, []byte(requestBody))
	require.NoError(t, err)
	var forwardErr error
	var forwardResult *service.OpenAIForwardResult
	var committed bool
	handler := &AsyncImageHandler{tasks: tasks}
	handler.execute = func(_ string, c *gin.Context) {
		flushed := make(chan struct{}, 1)
		c.Writer = openAIImagesAsyncFlushWriter{ResponseWriter: c.Writer, flushed: flushed}
		stopKeepalive := service.StartOpenAIImagesJSONKeepalive(c, time.Millisecond)
		defer stopKeepalive()
		select {
		case <-flushed:
		case <-time.After(time.Second):
			forwardErr = errors.New("image JSON keepalive did not flush a heartbeat")
			return
		}
		committed = service.StopOpenAIImagesJSONKeepaliveCommitted(c)
		forwardResult, forwardErr = gateway.ForwardImages(c.Request.Context(), c, account, []byte(requestBody), parsed, "")
	}

	handler.run(task.ID, service.PlatformOpenAI, taskCtx, recorder, func() {})

	require.True(t, committed)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"error":`+errorObject+`}`, recorder.Body.String())
	require.Nil(t, forwardResult)
	var upstreamErr *service.OpenAIImagesUpstreamError
	require.ErrorAs(t, forwardErr, &upstreamErr)
	require.True(t, upstreamErr.AsyncTaskAccepted)
	signaledErr := service.OpenAIImagesAsyncUpstreamErrorFromContext(taskCtx)
	require.NotNil(t, signaledErr)
	require.Same(t, upstreamErr, signaledErr)
	require.True(t, signaledErr.AsyncTaskAccepted)
	require.Empty(t, taskCtx.Errors)
	failed, err := tasks.Get(context.Background(), owner, task.ID)
	require.NoError(t, err)
	require.Equal(t, service.ImageTaskStatusFailed, failed.Status)
	require.Equal(t, http.StatusBadRequest, failed.HTTPStatus)
	require.JSONEq(t, errorObject, string(failed.Error))
	require.Empty(t, failed.Result)
}
