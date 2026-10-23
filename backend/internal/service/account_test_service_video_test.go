//go:build unit

package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestAccountTestServiceVideoUsesNativeProtocols(t *testing.T) {
	for _, tc := range []struct {
		protocol, model, createPath, statusPath, create, status string
		extra                                                   []string
	}{
		{VideoProtocolSeedance, "ep-deployment", "/api/v3/contents/generations/tasks", "/api/v3/contents/generations/tasks/ark-1", `{"id":"ark-1"}`, `{"status":"succeeded","content":{"video_url":"https://cdn.example/video.mp4"}}`, nil},
		{VideoProtocolGemini, "veo-3.0-generate-001", "/v1beta/models/veo-3.0-generate-001:predictLongRunning", "/v1beta/models/veo-3.0-generate-001/operations/one", `{"name":"models/veo-3.0-generate-001/operations/one"}`, `{"done":true,"response":{"generateVideoResponse":{"generatedSamples":[{"video":{"uri":"https://video.example/v1beta/files/one:download?key=should-be-removed"}}]}}}`, nil},
		{VideoProtocolMiniMax, "MiniMax-Hailuo-2.3", "/v1/video_generation", "/v1/query/video_generation", `{"task_id":"123","base_resp":{"status_code":0}}`, `{"task_id":"123","status":"Success","file_id":"456","base_resp":{"status_code":0}}`, []string{`{"file":{"file_id":"456","download_url":"https://cdn.example/video.mp4"},"base_resp":{"status_code":0}}`}},
	} {
		t.Run(tc.protocol, func(t *testing.T) {
			account := &Account{ID: 42, Platform: PlatformVideo, Type: AccountTypeAPIKey, Concurrency: 1, Credentials: map[string]any{
				"api_key": "video-secret", "base_url": "https://video.example", "video_protocol": tc.protocol,
				"model_mapping": map[string]any{"public-video": tc.model}, "hide_video_upstream_url": true,
			}}
			responses := []*http.Response{grokMediaContentStatusResponse(tc.create), grokMediaContentStatusResponse(tc.status)}
			for _, body := range tc.extra {
				responses = append(responses, grokMediaContentStatusResponse(body))
			}
			responses = append(responses, &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"video/mp4"}}, Body: io.NopCloser(strings.NewReader("video-bytes"))})
			svc, upstream := adaptiveCNAccountTestService(account, responses...)
			c, recorder := newTestContext()
			err := svc.TestAccountConnection(c, account.ID, "public-video", "A moving red ball", "video")
			require.NoError(t, err)
			require.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
			require.Contains(t, recorder.Body.String(), `"type":"video"`)
			require.Contains(t, recorder.Body.String(), `"type":"test_complete","success":true`)
			require.NotContains(t, recorder.Body.String(), "video-secret")
			require.NotContains(t, recorder.Body.String(), "should-be-removed")
			require.NotContains(t, recorder.Body.String(), "https://")
			require.Contains(t, recorder.Body.String(), "data:video/mp4;base64,"+base64.StdEncoding.EncodeToString([]byte("video-bytes")))
			require.Equal(t, tc.createPath, upstream.requests[0].URL.Path)
			require.Equal(t, tc.statusPath, upstream.requests[1].URL.Path)
			require.Equal(t, http.MethodPost, upstream.requests[0].Method)
			require.Equal(t, http.MethodGet, upstream.requests[1].Method)
			for _, req := range upstream.requests {
				require.NotContains(t, req.URL.Path, "chat")
				require.NotContains(t, req.URL.Path, "messages")
				if req.URL.Host == "cdn.example" {
					require.Empty(t, req.Header.Get("Authorization"))
					require.Empty(t, req.Header.Get("x-goog-api-key"))
					require.True(t, HTTPUpstreamPublicHostsOnly(req.Context()))
					require.True(t, HTTPUpstreamRedirectsDisabled(req.Context()))
				} else if tc.protocol == VideoProtocolGemini {
					require.Equal(t, "video-secret", req.Header.Get("x-goog-api-key"))
					require.Empty(t, req.Header.Get("Authorization"))
				} else {
					require.Equal(t, "Bearer video-secret", req.Header.Get("Authorization"))
				}
			}
			payload := upstream.bodies[0]
			switch tc.protocol {
			case VideoProtocolSeedance:
				require.Equal(t, tc.model, gjson.GetBytes(payload, "model").String())
				require.Equal(t, "A moving red ball", gjson.GetBytes(payload, "content.0.text").String())
			case VideoProtocolGemini:
				require.False(t, gjson.GetBytes(payload, "model").Exists())
				require.Equal(t, "A moving red ball", gjson.GetBytes(payload, "instances.0.prompt").String())
				require.Equal(t, int64(1), gjson.GetBytes(payload, "parameters.sampleCount").Int())
				require.Empty(t, upstream.lastReq.URL.Query().Get("key"))
				require.True(t, HTTPUpstreamRedirectsDisabled(upstream.lastReq.Context()))
			case VideoProtocolMiniMax:
				require.Equal(t, tc.model, gjson.GetBytes(payload, "model").String())
				require.Equal(t, "768P", gjson.GetBytes(payload, "resolution").String())
				require.Equal(t, int64(6), gjson.GetBytes(payload, "duration").Int())
				require.Equal(t, "123", upstream.requests[1].URL.Query().Get("task_id"))
				require.Equal(t, "/v1/files/retrieve", upstream.requests[2].URL.Path)
				require.Equal(t, "cdn.example", upstream.lastReq.URL.Host)
			}
		})
	}
}

func TestAccountTestServiceVideoFailuresDoNotRetryOrSucceed(t *testing.T) {
	for _, tc := range []struct {
		name, protocol, create, status, want string
	}{
		{"business error", VideoProtocolMiniMax, `{"base_resp":{"status_code":1008,"status_msg":"insufficient balance"}}`, "", "insufficient balance"},
		{"failed generation", VideoProtocolMiniMax, `{"task_id":"123","base_resp":{"status_code":0}}`, `{"task_id":"123","status":"Fail","base_resp":{"status_code":1000,"status_msg":"generation failed"}}`, "generation failed"},
		{"missing task ID", VideoProtocolSeedance, `{}`, "", "missing task ID"},
		{"missing output", VideoProtocolGemini, `{"name":"models/veo-3.0-generate-001/operations/one"}`, `{"done":true,"response":{}}`, "no output"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := &Account{ID: 42, Platform: PlatformVideo, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "secret", "video_protocol": tc.protocol}}
			responses := []*http.Response{grokMediaContentStatusResponse(tc.create)}
			if tc.status != "" {
				responses = append(responses, grokMediaContentStatusResponse(tc.status))
			}
			svc, upstream := adaptiveCNAccountTestService(account, responses...)
			c, recorder := newTestContext()
			err := svc.TestAccountConnection(c, account.ID, "", "", "video")
			require.ErrorContains(t, err, tc.want)
			require.Len(t, upstream.requests, len(responses))
			require.NotContains(t, recorder.Body.String(), `"success":true`)
		})
	}
}

func TestAccountTestServiceVideoCancellationDoesNotResubmit(t *testing.T) {
	account := &Account{ID: 42, Platform: PlatformVideo, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "secret"}}
	svc, upstream := adaptiveCNAccountTestService(account)
	c, recorder := newTestContext()
	ctx, cancel := context.WithCancel(c.Request.Context())
	cancel()
	c.Request = c.Request.WithContext(ctx)
	// A canceled request must never create a potentially billable task.
	err := svc.TestAccountConnection(c, account.ID, "", "", "video")
	require.Error(t, err)
	require.Empty(t, upstream.requests)
	require.NotContains(t, recorder.Body.String(), `"success":true`)
}

func TestAccountTestServiceVideoPollingStopsOnCancellation(t *testing.T) {
	account := &Account{ID: 42, Platform: PlatformVideo, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "secret"}}
	svc, upstream := adaptiveCNAccountTestService(account,
		grokMediaContentStatusResponse(`{"id":"ark-1"}`),
		grokMediaContentStatusResponse(`{"status":"running"}`),
	)
	c, recorder := newTestContext()
	ctx, cancel := context.WithTimeout(c.Request.Context(), 50*time.Millisecond)
	defer cancel()
	c.Request = c.Request.WithContext(ctx)
	err := svc.TestAccountConnection(c, account.ID, "", "", "video")
	require.ErrorContains(t, err, "task_id=seedance:ark-1")
	require.Len(t, upstream.requests, 2)
	require.Contains(t, recorder.Body.String(), "processing")
	require.NotContains(t, recorder.Body.String(), `"success":true`)
}

func TestAccountTestServiceVideoFirstFrameUsesNativeFields(t *testing.T) {
	for _, tc := range []struct{ protocol, field string }{
		{VideoProtocolSeedance, "content.1.image_url.url"},
		{VideoProtocolGemini, "instances.0.image.bytesBase64Encoded"},
		{VideoProtocolMiniMax, "first_frame_image"},
	} {
		t.Run(tc.protocol, func(t *testing.T) {
			account := &Account{ID: 42, Platform: PlatformVideo, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "secret", "video_protocol": tc.protocol}}
			response := grokMediaContentStatusResponse(`{"error":{"message":"test rejection"}}`)
			response.StatusCode = http.StatusBadRequest
			svc, upstream := adaptiveCNAccountTestService(account, response)
			c, _ := newTestContext()
			image := minimalAccountTestPNGDataURL(8, 8)
			err := svc.TestAccountConnection(c, account.ID, "", "", "video", AccountTestOptions{ImageDataURL: image})
			require.Error(t, err)
			require.Len(t, upstream.requests, 1)
			field := gjson.GetBytes(upstream.bodies[0], tc.field).String()
			if tc.protocol == VideoProtocolGemini {
				_, encoded, _ := strings.Cut(image, ",")
				require.Equal(t, encoded, field)
				require.Equal(t, "image/png", gjson.GetBytes(upstream.bodies[0], "instances.0.image.mimeType").String())
			} else {
				require.Equal(t, image, field)
			}
		})
	}
}

func TestAccountTestServiceVideoPreviewRejectsCrossOriginGeminiDownload(t *testing.T) {
	account := &Account{ID: 42, Platform: PlatformVideo, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "secret", "video_protocol": "gemini", "base_url": "https://google.example"}}
	svc, upstream := adaptiveCNAccountTestService(account)
	c, recorder := newTestContext()
	err := svc.emitNativeVideoTestResult(c, c.Request.Context(), account, "https://evil.example/video.mp4")
	require.ErrorContains(t, err, "configured upstream origin")
	require.Empty(t, upstream.requests)
	require.Empty(t, recorder.Body.String())
}

func TestVideoTestModelsPreserveAliasesAndFilterMixedLegacyWhitelist(t *testing.T) {
	a := &Account{Platform: PlatformVideo, Type: AccountTypeAPIKey, Credentials: map[string]any{"video_protocol": "minimax", "model_mapping": map[string]any{
		"doubao-seedance-1-0-pro": "doubao-seedance-1-0-pro", "veo-3.0-generate-001": "veo-3.0-generate-001",
		"MiniMax-Hailuo-02": "MiniMax-Hailuo-02", "MiniMax-Hailuo-2.3": "MiniMax-Hailuo-2.3", "my-video": "MiniMax-Hailuo-2.3",
	}}}
	require.Equal(t, []string{"MiniMax-Hailuo-2.3", "MiniMax-Hailuo-02", "my-video"}, a.VideoTestModelIDs())
	a.Credentials["video_protocol"] = "seedance"
	require.Equal(t, []string{"doubao-seedance-1-0-pro"}, a.VideoTestModelIDs())
	a.Credentials["model_mapping"] = map[string]any{"public-ark": "ep-private-deployment"}
	require.Equal(t, []string{"public-ark"}, a.VideoTestModelIDs())
	a.Credentials["model_mapping"] = map[string]any{"veo-*": "veo-*"}
	a.Credentials["video_protocol"] = "gemini"
	require.Equal(t, []string{"veo-3.0-fast-generate-001", "veo-3.0-generate-001"}, a.VideoTestModelIDs())
}

func TestAccountTestServiceVideoPreviewPublicDownloadSafety(t *testing.T) {
	for _, protocol := range []string{VideoProtocolSeedance, VideoProtocolMiniMax} {
		for _, target := range []string{
			"https://127.0.0.1/video.mp4", "https://[::1]/video.mp4", "https://localhost/video.mp4",
			"http://cdn.example/video.mp4", "https://user:pass@cdn.example/video.mp4", "https://cdn.example/video.mp4#secret",
		} {
			t.Run(protocol+"/"+target, func(t *testing.T) {
				account := &Account{ID: 42, Platform: PlatformVideo, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "preview-secret", "video_protocol": protocol}}
				svc, upstream := adaptiveCNAccountTestService(account)
				c, recorder := newTestContext()
				require.Error(t, svc.emitNativeVideoTestResult(c, c.Request.Context(), account, target))
				require.Empty(t, upstream.requests)
				require.Empty(t, recorder.Body.String())
			})
		}
		t.Run(protocol+"/redirect", func(t *testing.T) {
			account := &Account{ID: 42, Platform: PlatformVideo, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "preview-secret", "video_protocol": protocol}}
			svc, upstream := adaptiveCNAccountTestService(account, &http.Response{
				StatusCode: http.StatusTemporaryRedirect, Header: http.Header{"Location": {"https://other.example/secret?signature=private"}}, Body: io.NopCloser(strings.NewReader("")),
			})
			c, recorder := newTestContext()
			require.ErrorContains(t, svc.emitNativeVideoTestResult(c, c.Request.Context(), account, "https://cdn.example/video.mp4?signature=private"), "upstream status 307")
			require.Len(t, upstream.requests, 1)
			require.True(t, HTTPUpstreamPublicHostsOnly(upstream.lastReq.Context()))
			require.True(t, HTTPUpstreamRedirectsDisabled(upstream.lastReq.Context()))
			require.Empty(t, upstream.lastReq.Header.Get("Authorization"))
			require.Empty(t, upstream.lastReq.Header.Get("x-goog-api-key"))
			require.NotContains(t, recorder.Body.String(), `"type":"video"`)
			require.NotContains(t, recorder.Body.String(), "private")
		})
	}
}

// Generate an oversized body without allocating it before the bounded reader.
type nativeVideoTestPreviewReader struct{}

func (nativeVideoTestPreviewReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'v'
	}
	return len(p), nil
}

func TestAccountTestServiceVideoPreviewRejectsUnsafeResponse(t *testing.T) {
	for _, tc := range []struct {
		name, contentType string
		length            int64
		reader            io.Reader
		want              string
	}{
		{"non-video", "text/html", 4, strings.NewReader("oops"), "non-video"},
		{"declared too large", "video/mp4", (32 << 20) + 1, strings.NewReader("video"), "too large"},
		{"stream too large", "video/mp4", -1, nativeVideoTestPreviewReader{}, "too large"},
		{"empty", "video/mp4", 0, strings.NewReader(""), "empty"},
		{"read error", "video/mp4", -1, passthroughErrReadCloser{err: errors.New("https://private.example/?signature=secret")}, "could not be read"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := &Account{ID: 42, Platform: PlatformVideo, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "preview-secret", "video_protocol": VideoProtocolSeedance}}
			body := &passthroughCloseTrackingReadCloser{Reader: tc.reader}
			svc, _ := adaptiveCNAccountTestService(account, &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {tc.contentType}}, Body: body, ContentLength: tc.length})
			c, recorder := newTestContext()
			require.ErrorContains(t, svc.emitNativeVideoTestResult(c, c.Request.Context(), account, "https://cdn.example/video.mp4"), tc.want)
			require.True(t, body.closed)
			require.NotContains(t, recorder.Body.String(), `"type":"video"`)
			require.NotContains(t, recorder.Body.String(), "private.example")
		})
	}
}

func TestAccountTestServiceMiniMaxOversizedResponseEndsWithFailureEvent(t *testing.T) {
	for _, poll := range []bool{false, true} {
		t.Run(map[bool]string{false: "submit", true: "poll"}[poll], func(t *testing.T) {
			account := &Account{ID: 42, Platform: PlatformVideo, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "response-secret", "video_protocol": VideoProtocolMiniMax}}
			responses := []*http.Response{}
			if poll {
				responses = append(responses, grokMediaContentStatusResponse(`{"task_id":"123","base_resp":{"status_code":0}}`))
			}
			responses = append(responses, grokMediaContentStatusResponse(`{"diagnostic":"`+strings.Repeat("x", 100)+`"}`))
			svc, upstream := adaptiveCNAccountTestService(account, responses...)
			svc.cfg.Gateway.UpstreamResponseReadMaxBytes = 64
			c, recorder := newTestContext()
			var err error
			require.NotPanics(t, func() { err = svc.TestAccountConnection(c, account.ID, "", "", "video") })
			require.ErrorContains(t, err, "upstream response body too large")
			require.Len(t, upstream.requests, len(responses))
			require.NotContains(t, recorder.Body.String(), `"success":true`)
			lines := strings.Split(strings.TrimSpace(recorder.Body.String()), "\n")
			last := strings.TrimPrefix(lines[len(lines)-1], "data: ")
			require.Equal(t, "error", gjson.Get(last, "type").String(), "SSE must end in an explicit failure event")
		})
	}
}

func TestAccountTestServiceVideoErrorsRedactBrowserAndLogs(t *testing.T) {
	diagnostic := `denied https://signed.example/output.mp4?X-Tos-Signature=url-signature&Expires=url-expiry gs://private-bucket/object /download?token=query-secret&expires=query-expiry signature=standalone-signature X-Amz-Signature=aws-signature api_key=upstream-api-secret`
	for _, tc := range []struct {
		protocol, create, failed string
	}{
		{VideoProtocolSeedance, `{"id":"ark-1"}`, `{"status":"failed","error":{"message":%s}}`},
		{VideoProtocolGemini, `{"name":"models/veo-3.0-generate-001/operations/one"}`, `{"done":true,"error":{"message":%s}}`},
		{VideoProtocolMiniMax, `{"task_id":"123","base_resp":{"status_code":0}}`, `{"task_id":"123","status":"Fail","base_resp":{"status_code":1000,"status_msg":%s}}`},
	} {
		for _, phase := range []string{"submit", "poll", "generation"} {
			t.Run(tc.protocol+"/"+phase, func(t *testing.T) {
				account := &Account{ID: 42, Platform: PlatformVideo, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "upstream-api-secret", "video_protocol": tc.protocol}}
				encoded, err := json.Marshal(diagnostic)
				require.NoError(t, err)
				responses := []*http.Response{}
				if phase != "submit" {
					responses = append(responses, grokMediaContentStatusResponse(tc.create))
				}
				if phase == "generation" {
					responses = append(responses, grokMediaContentStatusResponse(fmt.Sprintf(tc.failed, encoded)))
				} else {
					failure := grokMediaContentStatusResponse(`{"error":{"message":` + string(encoded) + `}}`)
					failure.StatusCode = http.StatusBadRequest
					responses = append(responses, failure)
				}
				svc, upstream := adaptiveCNAccountTestService(account, responses...)
				core, logs := observer.New(zap.WarnLevel)
				c, recorder := newTestContext()
				c.Request = c.Request.WithContext(logger.IntoContext(c.Request.Context(), zap.New(core)))
				err = svc.TestAccountConnection(c, account.ID, "", "", "video")
				require.Error(t, err)
				require.Len(t, upstream.requests, len(responses))
				require.Contains(t, recorder.Body.String(), `"type":"error"`)
				require.NotContains(t, recorder.Body.String(), `"success":true`)
				entries := logs.FilterMessage("account_test.failed").All()
				require.Len(t, entries, 1)
				logged, ok := entries[0].ContextMap()["error"].(string)
				require.True(t, ok)
				for _, output := range []string{err.Error(), recorder.Body.String(), logged} {
					for _, secret := range []string{"signed.example", "url-signature", "url-expiry", "private-bucket", "query-secret", "query-expiry", "standalone-signature", "aws-signature", "upstream-api-secret"} {
						require.NotContains(t, output, secret)
					}
					require.Contains(t, output, "denied")
				}
			})
		}
	}
}

func TestNativeVideoTestErrorRedactionHandlesEscapesAndEmptyKey(t *testing.T) {
	for _, message := range []string{
		`diagnostic https:\/\/signed.example\/video?signature=secret-signature`,
		`diagnostic {"signature":"secret-signature","api_key":"secret-key"}`,
		`diagnostic /video?opaque=secret-query&expires=secret-expiry`,
	} {
		safe := sanitizeNativeVideoTestError(message, &Account{Credentials: map[string]any{}})
		require.Contains(t, safe, "diagnostic")
		for _, secret := range []string{"signed.example", "secret-signature", "secret-key", "secret-query", "secret-expiry"} {
			require.NotContains(t, safe, secret)
		}
	}
	key := strings.Repeat("private-key", 100)
	safe := sanitizeNativeVideoTestError("denied api_key="+key, &Account{Credentials: map[string]any{"api_key": key}})
	require.NotContains(t, safe, "private-key")
	require.Contains(t, safe, "denied")
}
