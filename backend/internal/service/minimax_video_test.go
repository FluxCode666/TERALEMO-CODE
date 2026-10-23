//go:build unit

package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func miniMaxVideoTestAccount() *Account {
	return &Account{ID: 91, Platform: PlatformVideo, Type: AccountTypeAPIKey, Credentials: map[string]any{
		"api_key": "minimax-secret", "video_protocol": VideoProtocolMiniMax,
		"model_mapping": map[string]any{"public-video": "MiniMax-Hailuo-2.3"},
	}}
}

func TestMiniMaxVideoCustomVersionedBaseURL(t *testing.T) {
	for _, base := range []string{
		"https://metaso.cn/api/minimax/v2",
		"https://metaso.cn/api/minimax/v2/",
		"https://metaso.cn/api/minimax/v2/video_generation",
		"https://metaso.cn/api/minimax/v2/query/video_generation",
		"https://metaso.cn/api/minimax/v2/files/retrieve",
	} {
		t.Run(base, func(t *testing.T) {
			for _, tc := range []struct {
				endpoint GrokMediaEndpoint
				want     string
			}{
				{MiniMaxVideoEndpointCreate, "https://metaso.cn/api/minimax/v2/video_generation"},
				{MiniMaxVideoEndpointStatus, "https://metaso.cn/api/minimax/v2/query/video_generation/task-1"},
			} {
				got, err := buildMiniMaxVideoURL(base, tc.endpoint, "task-1")
				require.NoError(t, err)
				require.Equal(t, tc.want, got)
			}
			_, err := buildMiniMaxVideoURL(base, MiniMaxVideoEndpointFile, "task-1")
			require.Error(t, err)
		})
	}
	// A full endpoint without a version also remains exact.
	got, err := buildMiniMaxVideoURL("https://proxy.example/minimax/video_generation", MiniMaxVideoEndpointCreate, "")
	require.NoError(t, err)
	require.Equal(t, "https://proxy.example/minimax/video_generation", got)
}

func TestMiniMaxVideoProtocolAndNativeForwarding(t *testing.T) {
	a := miniMaxVideoTestAccount()
	require.NoError(t, ValidateVideoAccount(a))
	require.True(t, a.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityMiniMaxVideo))
	for _, capability := range []OpenAIEndpointCapability{OpenAIEndpointCapabilitySeedance, OpenAIEndpointCapabilityGeminiVideo, OpenAIEndpointCapabilityChatCompletions} {
		require.False(t, a.SupportsOpenAIEndpointCapability(capability))
	}
	for _, base := range []string{"https://api.minimax.io", "https://api.minimax.io/v1/", "https://api.minimax.io/v1/video_generation"} {
		target, err := buildMiniMaxVideoURL(base, MiniMaxVideoEndpointCreate, "")
		require.NoError(t, err)
		require.Equal(t, "https://api.minimax.io/v1/video_generation", target)
	}
	u := &grokMediaContentUpstreamStub{response: grokMediaContentStatusResponse(`{"task_id":"123456789012345678","base_resp":{"status_code":0,"status_msg":"success"}}`)}
	svc := &OpenAIGatewayService{httpUpstream: u}
	c, _ := grokMediaContentTestContext(http.MethodPost, "/v1/video_generation", nil)
	result, err := svc.ForwardMiniMaxVideo(context.Background(), c, a, MiniMaxVideoEndpointCreate, "", []byte(`{"model":"public-video","prompt":"waves","first_frame_image":"https://images.example/a.jpg","duration":10,"prompt_optimizer":false,"future":{"keep":true}}`))
	require.NoError(t, err)
	require.Equal(t, "https://api.minimax.io/v1/video_generation", u.request.URL.String())
	require.Equal(t, "Bearer minimax-secret", u.request.Header.Get("Authorization"))
	require.Empty(t, u.request.Header.Get("x-goog-api-key"))
	require.True(t, HTTPUpstreamRedirectsDisabled(u.request.Context()))
	body, err := io.ReadAll(u.request.Body)
	require.NoError(t, err)
	require.Equal(t, "MiniMax-Hailuo-2.3", gjson.GetBytes(body, "model").String())
	require.Equal(t, "768P", gjson.GetBytes(body, "resolution").String())
	require.Equal(t, "https://images.example/a.jpg", gjson.GetBytes(body, "first_frame_image").String())
	require.True(t, gjson.GetBytes(body, "future.keep").Bool())
	require.False(t, gjson.GetBytes(body, "prompt_optimizer").Bool())
	require.Equal(t, "minimax:123456789012345678", result.ResponseID)
	require.Equal(t, "768p", result.VideoResolution)
	require.Equal(t, 10, result.VideoDurationSeconds)
	require.Zero(t, result.VideoCount)
}

func TestMiniMaxVideoCompletionAndFailures(t *testing.T) {
	a := miniMaxVideoTestAccount()
	u := &grokMediaContentUpstreamStub{responses: []*http.Response{
		grokMediaContentStatusResponse(`{"task_id":"task-1","status":"Success","file_id":"123456789012345678","base_resp":{"status_code":0}}`),
		grokMediaContentStatusResponse(`{"file":{"file_id":123456789012345678,"download_url":"https://cdn.minimax.io/video.mp4?signature=one"},"base_resp":{"status_code":0}}`),
	}}
	svc := &OpenAIGatewayService{httpUpstream: u}
	c, w := grokMediaContentTestContext(http.MethodGet, "/v1/query/video_generation?task_id=task-1", nil)
	result, err := svc.ForwardMiniMaxVideo(context.Background(), c, a, MiniMaxVideoEndpointStatus, "minimax:task-1", nil)
	require.NoError(t, err)
	require.Equal(t, "Success", gjson.Get(w.Body.String(), "status").String(), "native response must remain unchanged")
	require.Equal(t, "123456789012345678", result.VideoFileID, "int64 file IDs must not lose precision")
	require.Equal(t, "completed", result.AsyncTaskStatus)
	require.Equal(t, 1, result.VideoCount)
	require.Equal(t, "https://cdn.minimax.io/video.mp4?signature=one", result.AsyncTaskURL)
	require.Equal(t, "task-1", u.requests[0].URL.Query().Get("task_id"))
	require.Equal(t, "123456789012345678", u.requests[1].URL.Query().Get("file_id"))
	for _, state := range []string{"Preparing", "Queueing", "Processing", "Fail"} {
		u.response = grokMediaContentStatusResponse(`{"task_id":"task-1","status":"` + state + `","base_resp":{"status_code":0,"status_msg":"generation blocked"}}`)
		c, _ = grokMediaContentTestContext(http.MethodGet, "/v1/query/video_generation", nil)
		result, err = svc.ForwardMiniMaxVideo(context.Background(), c, a, MiniMaxVideoEndpointStatus, "minimax:task-1", nil)
		require.NoError(t, err)
		require.Zero(t, result.VideoCount)
		if state == "Fail" {
			require.Equal(t, "failed", result.AsyncTaskStatus)
			require.Equal(t, "generation blocked", result.AsyncTaskError)
		} else {
			require.Equal(t, "processing", result.AsyncTaskStatus)
		}
	}
	for _, response := range []string{
		`{"task_id":"wrong-task","status":"Success","file_id":"a","base_resp":{"status_code":0}}`,
		`{"task_id":"task-1","status":"Success","base_resp":{"status_code":0}}`,
		`{"task_id":"task-1","status":"Success","file_id":"a","base_resp":{"status_code":0}}`,
	} {
		u.response = grokMediaContentStatusResponse(response)
		c, _ = grokMediaContentTestContext(http.MethodGet, "/v1/query/video_generation", nil)
		_, err = svc.ForwardMiniMaxVideo(context.Background(), c, a, MiniMaxVideoEndpointStatus, "minimax:task-1", nil)
		require.Error(t, err)
	}
}

func TestMiniMaxVideoValidationAndBusinessErrorsNeverRetry(t *testing.T) {
	for _, body := range []string{`[]`, `{}`, `{"model":"m"}`, `{"model":"m","prompt":"p","duration":6.5}`, `{"model":"m","prompt":"p","duration":0}`, `{"model":"m","prompt":"p","duration":"6"}`, `{"model":"m","prompt":"p","resolution":"4k"}`} {
		_, err := ParseMiniMaxVideoRequest([]byte(body))
		require.Error(t, err, body)
	}
	for resolution, tier := range map[string]string{"512P": "480p", "720P": "720p", "768P": "768p", "1080P": "1080p", "2K": "2k"} {
		info, err := ParseMiniMaxVideoRequest([]byte(`{"model":"m","first_frame_image":"data:image/png;base64,a","resolution":"` + resolution + `"}`))
		require.NoError(t, err)
		require.Equal(t, tier, info.Resolution)
		require.Equal(t, 6, info.DurationSeconds)
	}
	for _, id := range []string{"", "../a", "one?key=secret", "https://evil.example", "one/two", "%2f"} {
		require.Error(t, ValidateMiniMaxVideoID(id))
	}
	for _, response := range []string{`{"base_resp":{"status_code":1002,"status_msg":"rate limited"}}`, `{"task_id":"unaccepted","base_resp":{"status_code":1004}}`, `{"task_id":"invalid-response"}`} {
		u := &grokMediaContentUpstreamStub{response: grokMediaContentStatusResponse(response)}
		svc := &OpenAIGatewayService{httpUpstream: u}
		c, w := grokMediaContentTestContext(http.MethodPost, "/v1/video_generation", nil)
		_, err := svc.ForwardMiniMaxVideo(context.Background(), c, miniMaxVideoTestAccount(), MiniMaxVideoEndpointCreate, "", []byte(`{"model":"m","prompt":"waves"}`))
		require.Error(t, err)
		require.GreaterOrEqual(t, w.Code, 400)
		require.Len(t, u.requests, 1)
	}
}

func TestMiniMaxVideoDownloadRefreshesSignedURLWithoutCredentials(t *testing.T) {
	a := miniMaxVideoTestAccount()
	u := &grokMediaContentUpstreamStub{}
	svc := &OpenAIGatewayService{cache: &schedulerTestGatewayCache{}, accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{*a}}, httpUpstream: u}
	groupID := int64(12)
	task := &AsyncTaskRecord{AsyncTask: AsyncTask{GroupID: &groupID, APIKeyID: 2, ResultURL: "https://old.example/expired"}, UserID: 1, RequestID: "minimax:task-1"}
	require.NoError(t, svc.BindGrokMediaVideoRequestAccount(context.Background(), &groupID, task.RequestID, task.UserID, task.APIKeyID, a.ID))
	for _, link := range []string{"https://cdn.example/fresh.mp4?signature=new", "https://127.0.0.1/a", "https://user@cdn.example/a", "http://cdn.example/a", "https://cdn.example/a#fragment"} {
		u.requests = nil
		u.responses = []*http.Response{
			grokMediaContentStatusResponse(`{"task_id":"task-1","status":"Success","file_id":"file-1","base_resp":{"status_code":0}}`),
			grokMediaContentStatusResponse(`{"file":{"file_id":"file-1","download_url":"` + link + `"},"base_resp":{"status_code":0}}`),
			{StatusCode: 200, Header: http.Header{"Content-Type": []string{"video/mp4"}}, Body: io.NopCloser(strings.NewReader("video-data")), ContentLength: 10},
		}
		c, w := grokMediaContentTestContext(http.MethodGet, "/user/async-tasks/one/content", nil)
		err := svc.ForwardMiniMaxVideoContent(context.Background(), c, task)
		if strings.Contains(link, "fresh.mp4") {
			require.NoError(t, err)
			require.Equal(t, "video-data", w.Body.String())
			require.Len(t, u.requests, 3)
			require.Equal(t, link, u.requests[2].URL.String())
			require.Empty(t, u.requests[2].Header.Get("Authorization"))
			require.Empty(t, u.requests[2].Header.Get("x-goog-api-key"))
			require.True(t, HTTPUpstreamRedirectsDisabled(u.requests[2].Context()))
			require.True(t, HTTPUpstreamPublicHostsOnly(u.requests[2].Context()))
		} else {
			require.Error(t, err, link)
			require.Len(t, u.requests, 2)
		}
	}
	task.UserID++
	before := len(u.requests)
	c, _ := grokMediaContentTestContext(http.MethodGet, "/user/async-tasks/one/content", nil)
	require.Error(t, svc.ForwardMiniMaxVideoContent(context.Background(), c, task))
	require.Len(t, u.requests, before)
}

func TestMiniMaxVideoNonJSONErrorsPreserveStatusAndRetryAfter(t *testing.T) {
	for _, version := range []string{"v1", "v2"} {
		for _, hide := range []bool{false, true} {
			for _, code := range []int{http.StatusBadRequest, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusServiceUnavailable} {
				for _, body := range []string{"", "upstream failed: https://diagnostic.example/video?signature=private", `<html>https://diagnostic.example/video?signature=private</html>`} {
					t.Run(fmt.Sprintf("%s/hide=%t/%d/body=%d", version, hide, code, len(body)), func(t *testing.T) {
						a := miniMaxVideoTestAccount()
						a.Credentials["base_url"] = "https://proxy.example/" + version
						a.Credentials["hide_video_upstream_url"] = hide
						u := &grokMediaContentUpstreamStub{response: &http.Response{
							StatusCode: code,
							Header: http.Header{
								"Content-Type":   {"text/html"},
								"Content-Length": {"999"},
								"Retry-After":    {"23"},
								"Location":       {"https://diagnostic.example/video?signature=private"},
							},
							Body: io.NopCloser(strings.NewReader(body)),
						}}
						svc := &OpenAIGatewayService{httpUpstream: u}
						c, w := grokMediaContentTestContext(http.MethodPost, "/"+version+"/video_generation", nil)
						result, err := svc.ForwardMiniMaxVideo(context.Background(), c, a, MiniMaxVideoEndpointCreate, "", []byte(`{"model":"m","prompt":"waves"}`))
						require.Error(t, err)
						require.Nil(t, result, "errors must not register an accepted task")
						require.Equal(t, code, w.Code)
						require.Equal(t, "23", w.Header().Get("Retry-After"))
						require.Contains(t, w.Header().Get("Content-Type"), "application/json")
						require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
						require.JSONEq(t, `{"error":{"type":"upstream_error","message":"MiniMax upstream request failed"}}`, w.Body.String())
						require.NotContains(t, w.Body.String(), "diagnostic.example")
						require.NotContains(t, w.Body.String(), "signature")
						require.Empty(t, w.Header().Get("Location"))
						require.NotEqual(t, "999", w.Header().Get("Content-Length"))
						require.Len(t, u.requests, 1, "failed submission must not retry")
					})
				}
			}
		}
	}
	for _, retryAfter := range []string{"0", "Wed, 21 Oct 2037 07:28:00 GMT", "Wed, 21 Oct 2015 07:28:00 GMT", "https://diagnostic.example/?signature=private", "-1", "1.5", "not-a-date"} {
		t.Run("retry="+retryAfter, func(t *testing.T) {
			u := &grokMediaContentUpstreamStub{response: &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     http.Header{"Retry-After": {retryAfter}},
				Body:       io.NopCloser(strings.NewReader("upstream failed")),
			}}
			svc := &OpenAIGatewayService{httpUpstream: u}
			c, w := grokMediaContentTestContext(http.MethodGet, "/v1/query/video_generation?task_id=task-1", nil)
			_, err := svc.ForwardMiniMaxVideo(context.Background(), c, miniMaxVideoTestAccount(), MiniMaxVideoEndpointStatus, "minimax:task-1", nil)
			require.Error(t, err)
			require.Equal(t, http.StatusTooManyRequests, w.Code)
			if retryAfter == "0" || strings.HasPrefix(retryAfter, "Wed,") {
				require.Equal(t, retryAfter, w.Header().Get("Retry-After"))
			} else {
				require.Empty(t, w.Header().Get("Retry-After"), "diagnostic URLs and malformed values are not retry intervals")
			}
		})
	}
}

func TestMiniMaxVideoOversizedResponseReturnsErrorWithoutPanic(t *testing.T) {
	for _, endpoint := range []GrokMediaEndpoint{MiniMaxVideoEndpointCreate, MiniMaxVideoEndpointStatus, MiniMaxVideoEndpointFile} {
		t.Run(string(endpoint), func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Gateway.UpstreamResponseReadMaxBytes = 8
			u := &grokMediaContentUpstreamStub{response: grokMediaContentStatusResponse(`{"task_id":"task-1","base_resp":{"status_code":0}}`)}
			svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: u}
			c, w := grokMediaContentTestContext(http.MethodPost, "/v1/video_generation", nil)
			require.NotPanics(t, func() {
				result, err := svc.ForwardMiniMaxVideo(context.Background(), c, miniMaxVideoTestAccount(), endpoint, "minimax:task-1", []byte(`{"model":"m","prompt":"waves"}`))
				require.ErrorIs(t, err, ErrUpstreamResponseBodyTooLarge)
				require.Nil(t, result)
			})
			require.Empty(t, w.Body.String(), "the caller must choose the HTTP or SSE error format")
			require.Len(t, u.requests, 1)
		})
	}
}

func TestMiniMaxVideoCompletionFileErrorsPreserveStatusAndRetryAfter(t *testing.T) {
	for _, hide := range []bool{false, true} {
		for _, code := range []int{http.StatusBadRequest, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusServiceUnavailable} {
			for _, body := range []string{"", "file lookup failed: https://diagnostic.example/video?signature=private", `<html>https://diagnostic.example/video?signature=private</html>`} {
				t.Run(fmt.Sprintf("hide=%t/%d/body=%d", hide, code, len(body)), func(t *testing.T) {
					a := miniMaxVideoTestAccount()
					a.Credentials["hide_video_upstream_url"] = hide
					u := &grokMediaContentUpstreamStub{responses: []*http.Response{
						grokMediaContentStatusResponse(`{"task_id":"task-1","status":"Success","file_id":"file-1","base_resp":{"status_code":0}}`),
						{
							StatusCode: code,
							Header: http.Header{
								"Content-Type": {"text/html"},
								"Retry-After":  {"23"},
								"Location":     {"https://diagnostic.example/video?signature=private"},
							},
							Body: io.NopCloser(strings.NewReader(body)),
						},
					}}
					svc := &OpenAIGatewayService{httpUpstream: u}
					c, w := grokMediaContentTestContext(http.MethodGet, "/v1/query/video_generation?task_id=task-1", nil)
					result, err := svc.ForwardMiniMaxVideo(context.Background(), c, a, MiniMaxVideoEndpointStatus, "minimax:task-1", nil)
					require.Error(t, err)
					require.Nil(t, result, "the initial Success response must not mask the failed file lookup")
					require.Equal(t, code, w.Code)
					require.Equal(t, "23", w.Header().Get("Retry-After"))
					require.Contains(t, w.Header().Get("Content-Type"), "application/json")
					require.JSONEq(t, `{"error":{"type":"upstream_error","message":"MiniMax upstream request failed"}}`, w.Body.String())
					require.NotContains(t, w.Body.String(), "diagnostic.example")
					require.NotContains(t, w.Body.String(), "signature")
					require.Empty(t, w.Header().Get("Location"))
					require.Len(t, u.requests, 2, "status and file lookup each execute exactly once")
					require.Equal(t, "file-1", u.requests[1].URL.Query().Get("file_id"))
					require.Equal(t, "Bearer minimax-secret", u.requests[1].Header.Get("Authorization"))
				})
			}
		}
	}
}

func TestMiniMaxVideoCompletionFileReadErrorsAllowNilContext(t *testing.T) {
	for _, oversized := range []bool{false, true} {
		t.Run(fmt.Sprintf("oversized=%t", oversized), func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Gateway.UpstreamResponseReadMaxBytes = 128
			response := &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     http.Header{"Retry-After": {"23"}},
				Body:       io.NopCloser(strings.NewReader("try later")),
			}
			if oversized {
				response.StatusCode = http.StatusOK
				response.Body = io.NopCloser(strings.NewReader(strings.Repeat("x", 129)))
			}
			u := &grokMediaContentUpstreamStub{responses: []*http.Response{
				grokMediaContentStatusResponse(`{"task_id":"task-1","status":"Success","file_id":"file-1","base_resp":{"status_code":0}}`),
				response,
			}}
			a := miniMaxVideoTestAccount()
			a.Credentials["hide_video_upstream_url"] = true
			svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: u}
			require.NotPanics(t, func() {
				result, err := svc.ForwardMiniMaxVideo(context.Background(), nil, a, MiniMaxVideoEndpointStatus, "minimax:task-1", nil)
				require.Nil(t, result)
				require.Error(t, err)
				var responseErr *miniMaxVideoResponseError
				require.ErrorAs(t, err, &responseErr)
				require.Equal(t, response, responseErr.response)
				if oversized {
					require.ErrorIs(t, err, ErrUpstreamResponseBodyTooLarge, "nested file errors must retain their cause")
				} else {
					require.Equal(t, "23", responseErr.response.Header.Get("Retry-After"))
					require.Equal(t, "try later", string(responseErr.body))
				}
			})
			require.Len(t, u.requests, 2)
		})
	}
}
