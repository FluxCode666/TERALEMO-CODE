//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const asyncImagesTestCompleted = `{"id":"imgtask_test","object":"image.generation.task","status":"completed","result":{"created":123,"data":[{"b64_json":"aGVsbG8=","size":"1536x1024"},{"b64_json":"d29ybGQ=","size":"1024x1024"}],"usage":{"input_tokens":12,"output_tokens":34,"total_tokens":46}}}`

type asyncImagesTestUpstream struct {
	HTTPUpstream
	client *http.Client
	check  func(*http.Request, string, int64, int)
	fail   func(*http.Request) bool
}

func (upstream *asyncImagesTestUpstream) Do(request *http.Request, proxyURL string, accountID int64, concurrency int) (*http.Response, error) {
	if upstream.check != nil {
		upstream.check(request, proxyURL, accountID, concurrency)
	}
	if upstream.fail != nil && upstream.fail(request) {
		return nil, errors.New("temporary network error")
	}
	return upstream.client.Do(request)
}

func asyncImagesTestGateway(t *testing.T, serve http.HandlerFunc) (*OpenAIGatewayService, *Account, *asyncImagesTestUpstream) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	server := httptest.NewServer(serve)
	t.Cleanup(server.Close)
	upstream := &asyncImagesTestUpstream{client: server.Client()}
	upstream.client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.ImageAsyncUpstreamPollTimeout = 15
	svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}
	account := &Account{
		ID: 12, Name: "async-images", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Concurrency: 2,
		Credentials: map[string]any{"api_key": "upstream-secret", "base_url": server.URL},
	}
	return svc, account, upstream
}

func asyncImagesTestForward(t *testing.T, svc *OpenAIGatewayService, account *Account, ctx context.Context, endpoint string, stream bool) (*OpenAIForwardResult, error, *httptest.ResponseRecorder, *gin.Context) {
	t.Helper()
	payload := map[string]any{"model": "gpt-image-1", "prompt": "test", "n": 4, "stream": stream}
	if endpoint == openAIImagesEditsEndpoint {
		payload["images"] = []map[string]string{{"image_url": "https://images.example.test/input.png"}}
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ginCtx.Request = httptest.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body)).WithContext(ctx)
	ginCtx.Request.Header.Set("Content-Type", "application/json")
	ginCtx.Request.Header.Set("X-Client-Unrelated", "not-forwarded")
	parsed, err := svc.ParseOpenAIImagesRequest(ginCtx, body)
	require.NoError(t, err)
	result, forwardErr := svc.ForwardImages(ctx, ginCtx, account, body, parsed, "")
	return result, forwardErr, recorder, ginCtx
}

func asyncImagesWriteTask(writer http.ResponseWriter, status int, body string) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Retry-After", "1")
	writer.Header().Set("Location", "https://untrusted.invalid/v1/images/tasks/evil")
	writer.WriteHeader(status)
	_, _ = io.WriteString(writer, body)
}

func TestOpenAIImagesAsyncUpstreamCompletesWithActualAccounting(t *testing.T) {
	for _, endpoint := range []string{"/v1/images/generations", "/v1/images/edits"} {
		t.Run(endpoint, func(t *testing.T) {
			var polls atomic.Int32
			svc, account, upstream := asyncImagesTestGateway(t, func(writer http.ResponseWriter, request *http.Request) {
				if request.Method == http.MethodPost {
					require.Equal(t, endpoint, request.URL.Path)
					asyncImagesWriteTask(writer, http.StatusAccepted, `{"task_id":"imgtask_test","object":"image.generation.task","status":"processing","poll_url":"https://untrusted.invalid/evil"}`)
					return
				}
				require.Equal(t, "/v1/images/tasks/imgtask_test", request.URL.Path)
				if polls.Add(1) <= 2 {
					asyncImagesWriteTask(writer, http.StatusOK, `{"id":"imgtask_test","status":"processing"}`)
					return
				}
				asyncImagesWriteTask(writer, http.StatusOK, asyncImagesTestCompleted)
			})
			account.Credentials["base_url"] = account.GetOpenAIBaseURL() + "/v1"
			account.Credentials["user_agent"] = "async-images-test"
			account.Credentials["header_override_enabled"] = true
			account.Credentials["header_overrides"] = map[string]any{"X-Upstream-Custom": "custom-value"}
			proxyID := int64(4)
			account.ProxyID = &proxyID
			account.Proxy = &Proxy{Protocol: "http", Host: "proxy.example.test", Port: 8080}
			upstream.check = func(request *http.Request, proxyURL string, accountID int64, concurrency int) {
				require.Equal(t, "Bearer upstream-secret", request.Header.Get("Authorization"))
				require.Equal(t, "async-images-test", request.Header.Get("User-Agent"))
				require.Equal(t, []string{"custom-value"}, request.Header["x-upstream-custom"])
				require.Equal(t, HTTPUpstreamProfileOpenAI, HTTPUpstreamProfileFromContext(request.Context()))
				require.Equal(t, account.ID, accountID)
				require.Equal(t, account.Concurrency, concurrency)
				require.Equal(t, account.Proxy.URL(), proxyURL)
				if request.Method == http.MethodGet {
					require.True(t, HTTPUpstreamRedirectsDisabled(request.Context()))
					require.Nil(t, request.Body)
					require.Empty(t, request.Header.Get("Content-Type"))
					require.Empty(t, request.Header.Get("X-Client-Unrelated"))
				}
			}
			result, err, recorder, ginCtx := asyncImagesTestForward(t, svc, account, context.Background(), endpoint, true)
			require.NoError(t, err)
			require.Equal(t, int32(3), polls.Load())
			require.Equal(t, http.StatusOK, recorder.Code)
			require.JSONEq(t, gjson.Get(asyncImagesTestCompleted, "result").Raw, recorder.Body.String())
			require.Empty(t, recorder.Header().Get("Location"))
			require.Empty(t, recorder.Header().Get("Retry-After"))
			require.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
			require.Equal(t, 2, result.ImageCount)
			require.Equal(t, 12, result.Usage.InputTokens)
			require.Equal(t, 34, result.Usage.OutputTokens)
			require.Equal(t, []string{"1536x1024", "1024x1024"}, result.ImageOutputSizes)
			require.Empty(t, result.ResponseHeaders.Get("Location"))
			latency, exists := ginCtx.Get(OpsUpstreamLatencyMsKey)
			require.True(t, exists)
			require.GreaterOrEqual(t, latency, int64(3000))
		})
	}
}

func TestOpenAIImagesAsyncUpstreamTerminalErrors(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		status int
		error  string
	}{
		{"no images", `{"data":[]}`, http.StatusBadGateway, "Upstream async image task completed without images"},
		{"missing result", "", http.StatusBadGateway, "Upstream async image task completed without images"},
		{"nonstring url", `{"data":[{"url":false}]}`, http.StatusBadGateway, "Upstream async image task completed without images"},
		{"nonstring b64", `{"data":[{"b64_json":123}]}`, http.StatusBadGateway, "Upstream async image task completed without images"},
		{"nonstring output", `{"output":[{"type":"image_generation_call","result":{"invalid":true}}]}`, http.StatusBadGateway, "Upstream async image task completed without images"},
		{"failed error object", `{"type":"invalid_request_error","message":"blocked","code":"moderation_blocked","param":"prompt","details":{"rule":"safety"}}`, http.StatusBadRequest, "blocked"},
		{"failed invalid http status", `null`, http.StatusBadGateway, "Upstream async image task failed"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			body := `{"id":"imgtask_test","object":"image.generation.task","status":"completed"`
			if strings.HasPrefix(testCase.name, "failed") {
				body = `{"id":"imgtask_test","object":"image.generation.task","status":"failed","http_status":400,"error":` + testCase.body
				if testCase.status == http.StatusBadGateway {
					body = strings.Replace(body, `"http_status":400`, `"http_status":200`, 1)
				}
			} else if testCase.body != "" {
				body += `,"result":` + testCase.body
			}
			body += "}"
			svc, account, _ := asyncImagesTestGateway(t, func(writer http.ResponseWriter, request *http.Request) {
				require.Equal(t, http.MethodPost, request.Method)
				asyncImagesWriteTask(writer, http.StatusAccepted, body)
			})
			result, err, recorder, _ := asyncImagesTestForward(t, svc, account, context.Background(), "/v1/images/generations", false)
			require.Nil(t, result)
			var upstreamErr *OpenAIImagesUpstreamError
			require.ErrorAs(t, err, &upstreamErr)
			require.True(t, upstreamErr.AsyncTaskAccepted)
			require.False(t, IsOpenAIImagesRetryableUpstreamError(upstreamErr))
			require.Equal(t, testCase.status, recorder.Code)
			require.Contains(t, recorder.Body.String(), testCase.error)
			require.Empty(t, recorder.Header().Get("Location"))
			if testCase.name == "failed error object" {
				require.JSONEq(t, `{"error":{"type":"invalid_request_error","message":"blocked","code":"moderation_blocked","param":"prompt"}}`, recorder.Body.String())
				require.False(t, gjson.Get(recorder.Body.String(), "error.details").Exists())
			}
		})
	}
}

func TestOpenAIImagesAsyncUpstreamTransientFailuresRecover(t *testing.T) {
	var polls atomic.Int32
	var networkFailures atomic.Int32
	svc, account, upstream := asyncImagesTestGateway(t, func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPost {
			asyncImagesWriteTask(writer, http.StatusAccepted, `{"task_id":"imgtask_test","status":"queued"}`)
			return
		}
		switch polls.Add(1) {
		case 1:
			asyncImagesWriteTask(writer, http.StatusServiceUnavailable, `{"error":"busy"}`)
		case 2:
			asyncImagesWriteTask(writer, http.StatusTooManyRequests, `{"error":"busy"}`)
		default:
			asyncImagesWriteTask(writer, http.StatusOK, asyncImagesTestCompleted)
		}
	})
	upstream.fail = func(request *http.Request) bool {
		return request.Method == http.MethodGet && networkFailures.Add(1) == 1
	}
	result, err, recorder, _ := asyncImagesTestForward(t, svc, account, context.Background(), "/v1/images/generations", false)
	require.NoError(t, err)
	require.Equal(t, 2, result.ImageCount)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, int32(3), polls.Load())
}

func TestOpenAIImagesAsyncUpstreamPollErrors(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusUnauthorized, http.StatusForbidden, http.StatusBadRequest, http.StatusFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var polls atomic.Int32
			svc, account, _ := asyncImagesTestGateway(t, func(writer http.ResponseWriter, request *http.Request) {
				if request.Method == http.MethodPost {
					asyncImagesWriteTask(writer, http.StatusAccepted, `{"id":"imgtask_test","object":"image.generation.task","status":"processing"}`)
					return
				}
				polls.Add(1)
				asyncImagesWriteTask(writer, status, `{"error":"unavailable"}`)
			})
			result, err, recorder, _ := asyncImagesTestForward(t, svc, account, context.Background(), "/v1/images/generations", false)
			require.Nil(t, result)
			var upstreamErr *OpenAIImagesUpstreamError
			require.ErrorAs(t, err, &upstreamErr)
			require.True(t, upstreamErr.AsyncTaskAccepted)
			require.Equal(t, http.StatusBadGateway, recorder.Code)
			wantPolls := int32(1)
			if status == http.StatusNotFound {
				wantPolls = 3
				require.Contains(t, recorder.Body.String(), "Upstream async image task not found or expired")
			}
			require.Equal(t, wantPolls, polls.Load())
		})
	}
}

func TestOpenAIImagesAsyncUpstreamTimeout(t *testing.T) {
	svc, account, _ := asyncImagesTestGateway(t, func(writer http.ResponseWriter, _ *http.Request) {
		asyncImagesWriteTask(writer, http.StatusAccepted, `{"id":"imgtask_test","object":"image.generation.task","status":"processing"}`)
	})
	svc.cfg.Gateway.ImageAsyncUpstreamPollTimeout = 1
	result, err, recorder, _ := asyncImagesTestForward(t, svc, account, context.Background(), "/v1/images/generations", false)
	require.Nil(t, result)
	var upstreamErr *OpenAIImagesUpstreamError
	require.ErrorAs(t, err, &upstreamErr)
	require.True(t, upstreamErr.AsyncTaskAccepted)
	require.Equal(t, http.StatusGatewayTimeout, recorder.Code)
	require.Equal(t, "timeout_error", gjson.Get(recorder.Body.String(), "error.type").String())
}

func TestOpenAIImagesAsyncUpstreamPollBodyLimit(t *testing.T) {
	svc, account, _ := asyncImagesTestGateway(t, func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPost {
			asyncImagesWriteTask(writer, http.StatusAccepted, `{"task_id":"imgtask_test","status":"processing"}`)
			return
		}
		asyncImagesWriteTask(writer, http.StatusOK, asyncImagesTestCompleted)
	})
	svc.cfg.Gateway.UpstreamResponseReadMaxBytes = 128
	result, err, recorder, _ := asyncImagesTestForward(t, svc, account, context.Background(), "/v1/images/generations", false)
	require.Nil(t, result)
	var upstreamErr *OpenAIImagesUpstreamError
	require.ErrorAs(t, err, &upstreamErr)
	require.True(t, upstreamErr.AsyncTaskAccepted)
	require.Equal(t, http.StatusBadGateway, recorder.Code)
	require.Contains(t, recorder.Body.String(), "Upstream async image task response too large")
}

func TestOpenAIImagesAsyncUpstreamSynchronousRegression(t *testing.T) {
	for _, body := range []string{`{"created":123,"data":[{"b64_json":"aGVsbG8="}]}`, `{"accepted":true}`, `{"object":"image.generation.task","status":"processing","data":null}`} {
		t.Run(body, func(t *testing.T) {
			svc, account, _ := asyncImagesTestGateway(t, func(writer http.ResponseWriter, request *http.Request) {
				require.Equal(t, http.MethodPost, request.Method)
				asyncImagesWriteTask(writer, http.StatusOK, body)
			})
			result, err, recorder, _ := asyncImagesTestForward(t, svc, account, context.Background(), "/v1/images/generations", false)
			require.NoError(t, err)
			require.JSONEq(t, body, recorder.Body.String())
			require.Equal(t, http.StatusOK, recorder.Code)
			wantCount := 4
			if gjson.Get(body, "data").IsArray() {
				wantCount = 1
			}
			require.Equal(t, wantCount, result.ImageCount)
		})
	}
}

func TestOpenAIImagesAsyncUpstreamInvalidTaskIDs(t *testing.T) {
	for _, taskID := range []string{"", "../escape", "a/b", "a?b", "a b", "a%2fb", "a\nb", "..", ".", strings.Repeat("a", 129)} {
		t.Run(taskID, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{"id": taskID, "object": "image.generation.task", "status": "processing"})
			require.NoError(t, err)
			svc, account, _ := asyncImagesTestGateway(t, func(writer http.ResponseWriter, request *http.Request) {
				require.Equal(t, http.MethodPost, request.Method)
				asyncImagesWriteTask(writer, http.StatusAccepted, string(body))
			})
			result, err, recorder, _ := asyncImagesTestForward(t, svc, account, context.Background(), "/v1/images/generations", false)
			require.Nil(t, result)
			var upstreamErr *OpenAIImagesUpstreamError
			require.ErrorAs(t, err, &upstreamErr)
			require.True(t, upstreamErr.AsyncTaskAccepted)
			require.Equal(t, http.StatusBadGateway, recorder.Code)
		})
	}
}

func TestOpenAIImagesAsyncUpstreamDetachedAndBackfilled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	svc, account, _ := asyncImagesTestGateway(t, func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPost {
			cancel()
			asyncImagesWriteTask(writer, http.StatusAccepted, `{"id":"imgtask_test","object":"image.generation.task","status":"processing"}`)
			return
		}
		asyncImagesWriteTask(writer, http.StatusOK, `{"status":"completed","result":{"data":[{"url":"data:image/png;base64,YWJj"}]}}`)
	})
	account.Extra = map[string]any{AccountExtraImagesURLToB64JSON: true}
	result, err, recorder, _ := asyncImagesTestForward(t, svc, account, ctx, "/v1/images/generations", false)
	require.NoError(t, err)
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, "YWJj", gjson.Get(recorder.Body.String(), "data.0.b64_json").String())
}

func TestOpenAIImagesAsyncUpstreamImmediateCompletion(t *testing.T) {
	svc, account, _ := asyncImagesTestGateway(t, func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		asyncImagesWriteTask(writer, http.StatusOK, asyncImagesTestCompleted)
	})
	result, err, recorder, _ := asyncImagesTestForward(t, svc, account, context.Background(), "/v1/images/generations", false)
	require.NoError(t, err)
	require.Equal(t, 2, result.ImageCount)
	require.JSONEq(t, gjson.Get(asyncImagesTestCompleted, "result").Raw, recorder.Body.String())
}

func TestOpenAIImagesAsyncUpstreamDetection(t *testing.T) {
	for _, testCase := range []struct {
		status int
		body   string
		want   bool
	}{
		{http.StatusAccepted, `{"task_id":"task","status":"processing"}`, true},
		{http.StatusAccepted, `{"id":"task","status":"in_progress"}`, true},
		{http.StatusAccepted, `{"id":"task","status":"pending"}`, true},
		{http.StatusAccepted, `{"id":"task","status":"completed"}`, false},
		{http.StatusOK, `{"task_id":"task","status":"processing"}`, false},
		{http.StatusOK, `{"object":"image.generation.task","status":"completed"}`, true},
		{http.StatusBadRequest, `{"object":"image.generation.task"}`, false},
		{http.StatusAccepted, `{"object":"image.generation.task","data":null}`, false},
		{http.StatusAccepted, `{"object":"image.generation.task","data":[]}`, false},
		{http.StatusAccepted, `[{"object":"image.generation.task"}]`, false},
		{http.StatusAccepted, `{`, false},
	} {
		require.Equal(t, testCase.want, detectOpenAIImagesAsyncUpstreamTask(testCase.status, []byte(testCase.body)) != nil, testCase.body)
	}
}

func TestOpenAIImagesAsyncUpstreamKeepaliveError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ginCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	stop := StartOpenAIImagesJSONKeepalive(ginCtx, 5*time.Millisecond)
	defer stop()
	waitForOpenAIImagesJSONKeepalive(t, ginCtx)
	upstreamErr := &OpenAIImagesUpstreamError{
		StatusCode: http.StatusBadRequest, AsyncTaskAccepted: true,
		ErrorType: "invalid_request_error", Message: "blocked",
	}
	require.True(t, writeOpenAIImagesUpstreamErrorResponse(ginCtx, upstreamErr))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"error":{"type":"invalid_request_error","message":"blocked"}}`, recorder.Body.String())
}

func TestOpenAIImagesAsyncUpstreamFailedErrorSanitization(t *testing.T) {
	for _, withRule := range []bool{false, true} {
		t.Run(map[bool]string{false: "sanitized", true: "admin rule"}[withRule], func(t *testing.T) {
			svc, account, _ := asyncImagesTestGateway(t, func(writer http.ResponseWriter, _ *http.Request) {
				asyncImagesWriteTask(writer, http.StatusAccepted, `{"id":"imgtask_test","object":"image.generation.task","status":"failed","http_status":400,"error":{"type":"invalid_request_error","code":"blocked","param":"prompt","message":"denied https://internal.example/error?key=secret-value&access_token=secret-token","internal":"private-debug"}}`)
			})
			body := []byte(`{"model":"gpt-image-1","prompt":"test"}`)
			recorder := httptest.NewRecorder()
			ginCtx, _ := gin.CreateTestContext(recorder)
			ginCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
			ginCtx.Request.Header.Set("Content-Type", "application/json")
			if withRule {
				responseCode := http.StatusUnprocessableEntity
				message := "Custom safe failure"
				passthrough := &ErrorPassthroughService{}
				passthrough.setLocalCache([]*model.ErrorPassthroughRule{{
					ID: 1, Name: "async failure", Enabled: true, Platforms: []string{PlatformOpenAI},
					ErrorCodes: []int{http.StatusBadRequest}, Keywords: []string{"denied"}, MatchMode: model.MatchModeAll,
					ResponseCode: &responseCode, CustomMessage: &message,
				}})
				BindErrorPassthroughService(ginCtx, passthrough)
			}
			parsed, err := svc.ParseOpenAIImagesRequest(ginCtx, body)
			require.NoError(t, err)
			result, err := svc.ForwardImages(context.Background(), ginCtx, account, body, parsed, "")
			require.Nil(t, result)
			var upstreamErr *OpenAIImagesUpstreamError
			require.ErrorAs(t, err, &upstreamErr)
			require.True(t, upstreamErr.AsyncTaskAccepted)
			require.NotContains(t, recorder.Body.String(), "secret-value")
			require.NotContains(t, recorder.Body.String(), "secret-token")
			require.NotContains(t, recorder.Body.String(), "private-debug")
			require.False(t, gjson.Get(recorder.Body.String(), "error.internal").Exists())
			if withRule {
				require.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
				require.JSONEq(t, `{"error":{"type":"upstream_error","message":"Custom safe failure"}}`, recorder.Body.String())
			} else {
				require.Equal(t, http.StatusBadRequest, recorder.Code)
				require.Equal(t, "denied https://internal.example/error?key=***&access_token=***", gjson.Get(recorder.Body.String(), "error.message").String())
				require.Equal(t, "blocked", gjson.Get(recorder.Body.String(), "error.code").String())
				require.Equal(t, "prompt", gjson.Get(recorder.Body.String(), "error.param").String())
			}
		})
	}
}

func TestOpenAIImagesAsyncPollPolicy(t *testing.T) {
	for _, testCase := range []struct {
		header string
		want   time.Duration
	}{{"", 3 * time.Second}, {"not-seconds", 3 * time.Second}, {"0", time.Second}, {"-2", time.Second}, {"1", time.Second}, {"5", 5 * time.Second}, {"99", 10 * time.Second}} {
		require.Equal(t, testCase.want, openAIImagesAsyncPollInterval(http.Header{"Retry-After": {testCase.header}}))
	}
	svc := &OpenAIGatewayService{}
	require.Equal(t, 30*time.Minute, svc.openAIImagesAsyncPollTimeout())
	svc.cfg = &config.Config{}
	for _, seconds := range []int{0, -1, 12} {
		svc.cfg.Gateway.ImageAsyncUpstreamPollTimeout = seconds
		want := 30 * time.Minute
		if seconds > 0 {
			want = time.Duration(seconds) * time.Second
		}
		require.Equal(t, want, svc.openAIImagesAsyncPollTimeout())
	}
}
