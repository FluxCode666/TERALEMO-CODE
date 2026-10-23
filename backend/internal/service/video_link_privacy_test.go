//go:build unit

package service

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestVideoURLPrivacyPreservesNativeShapeAndHidesLinks(t *testing.T) {
	for _, tc := range []struct {
		protocol, id, body, field string
	}{
		{VideoProtocolMiniMax, "minimax:task-1", `{"task":{"id":"task-1","status":"succeeded","content":{"url":"https://files.metaso.cn/a.mp4?signature=secret"}},"future":{"url":"https://upstream.example/debug"}}`, "task.content.url"},
		{VideoProtocolMiniMax, "minimax-file:file-1", `{"file":{"file_id":2113125869779501056,"download_url":"https://files.metaso.cn/a.mp4?signature=secret"},"base_resp":{"status_code":0}}`, "file.download_url"},
		{VideoProtocolSeedance, "seedance:task-1", `{"id":"task-1","status":"succeeded","content":{"video_url":"https://files.metaso.cn/a.mp4?signature=secret","last_frame_url":"https://files.metaso.cn/frame.png"}}`, "content.video_url"},
		{VideoProtocolGemini, "gemini:models/veo/operations/one", `{"name":"models/veo/operations/one","done":true,"response":{"generatedVideos":[{"video":{"uri":"https://files.metaso.cn/a.mp4?signature=secret"}},{"video":{"uri":"https://google.example/b.mp4"}}]}}`, "response.generatedVideos.0.video.uri"},
		{VideoProtocolGemini, "gemini:models/veo/operations/one", `{"done":true,"response":{"generateVideoResponse":{"generatedSamples":[{"video":{"uri":"https://files.metaso.cn/a.mp4?signature=secret"}}]}}}`, "response.generateVideoResponse.generatedSamples.0.video.uri"},
	} {
		t.Run(tc.id+tc.field, func(t *testing.T) {
			for _, hide := range []bool{false, true} {
				a := &Account{Credentials: map[string]any{"video_protocol": tc.protocol, "hide_video_upstream_url": hide}}
				svc := &OpenAIGatewayService{}
				c, w := grokMediaContentTestContext(http.MethodGet, "/query", nil)
				resp := grokMediaContentStatusResponse(tc.body)
				resp.Header.Set("Location", "https://files.metaso.cn/leak")
				svc.writeNativeVideoResponse(c, a, resp, []byte(tc.body), tc.id)
				require.Equal(t, 200, w.Code)
				if !hide {
					require.JSONEq(t, tc.body, w.Body.String())
					continue
				}
				require.Equal(t, VideoContentProxyURL(tc.id, 0), gjson.Get(w.Body.String(), tc.field).String())
				require.NotContains(t, w.Body.String(), "https://")
				require.NotContains(t, w.Body.String(), "files.metaso.cn")
				require.NotContains(t, w.Body.String(), "signature=secret")
				require.Empty(t, w.Header().Get("Location"))
				require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
				if strings.HasPrefix(tc.id, "minimax-file:") {
					require.Equal(t, "2113125869779501056", gjson.Get(w.Body.String(), "file.file_id").String())
				}
				if strings.Contains(tc.body, "b.mp4") {
					require.Equal(t, VideoContentProxyURL(tc.id, 1), gjson.Get(w.Body.String(), "response.generatedVideos.1.video.uri").String())
				}
			}
		})
	}
}

func TestVideoPrivacyErrorResponsesAndCallbacks(t *testing.T) {
	a := miniMaxV2TestAccount()
	a.Credentials["hide_video_upstream_url"] = true
	u := &grokMediaContentUpstreamStub{}
	svc := &OpenAIGatewayService{httpUpstream: u}
	c, w := grokMediaContentTestContext(http.MethodPost, "/v2/video_generation", nil)
	_, err := svc.ForwardMiniMaxVideo(context.Background(), c, a, MiniMaxVideoEndpointCreate, "", []byte(`{"model":"MiniMax-H3","prompt":"waves","callback_url":"https://client.example/callback"}`))
	require.ErrorContains(t, err, "callback_url")
	require.Equal(t, 400, w.Code)
	require.Empty(t, u.requests)
	resp := grokMediaContentStatusResponse(`{"error":{"message":"Download https://files.metaso.cn/a?signature=secret failed"}}`)
	resp.StatusCode = 400
	c, w = grokMediaContentTestContext(http.MethodGet, "/query", nil)
	svc.writeNativeVideoResponse(c, a, resp, []byte(`{"error":{"message":"Download https://files.metaso.cn/a?signature=secret failed"}}`), "minimax:one")
	require.Equal(t, 400, w.Code)
	require.NotContains(t, w.Body.String(), "metaso")
	a.Credentials["hide_video_upstream_url"] = "true"
	require.ErrorContains(t, ValidateVideoAccount(a), "boolean")
}

func TestVideoContentResourceContainsOnlyValidatedTaskIdentity(t *testing.T) {
	for _, id := range []string{"minimax:task-1", "minimax-file:file-1", "seedance:task-1", "gemini:models/veo/operations/one"} {
		resource := base64.RawURLEncoding.EncodeToString([]byte(id))
		parsed, err := ParseVideoContentResource(resource)
		require.NoError(t, err)
		require.Equal(t, id, parsed)
	}
	for _, id := range []string{"", "https://files.metaso.cn/a", "minimax:../a", "seedance:", "gemini:models/../operations/one", "minimax:one?url=https://evil.example"} {
		_, err := ParseVideoContentResource(base64.RawURLEncoding.EncodeToString([]byte(id)))
		require.Error(t, err, id)
	}
}

func TestVideoPrivacyAppliesCurrentAccountSettingToHistoricalPanelTasks(t *testing.T) {
	a := miniMaxV2TestAccount()
	svc := &OpenAIGatewayService{cache: &schedulerTestGatewayCache{}, accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{*a}}}
	groupID := int64(12)
	task := AsyncTask{ID: "vidtask_one", Kind: "video", Protocol: "minimax", TaskID: "task-1", GroupID: &groupID, APIKeyID: 2, Status: "completed", ResultURL: "https://files.metaso.cn/original.mp4"}
	require.NoError(t, svc.BindGrokMediaVideoRequestAccount(context.Background(), &groupID, "minimax:task-1", 1, 2, a.ID))
	require.Equal(t, task.ResultURL, svc.PublicVideoTask(context.Background(), 1, task).ResultURL)
	a.Credentials["hide_video_upstream_url"] = true
	require.Equal(t, "/api/v1/user/async-tasks/vidtask_one/content", svc.PublicVideoTask(context.Background(), 1, task).ResultURL)
	require.Equal(t, "https://files.metaso.cn/original.mp4", task.ResultURL, "stored URL stays internal for downloads")
	a.Credentials["hide_video_upstream_url"] = false
	require.Equal(t, task.ResultURL, svc.PublicVideoTask(context.Background(), 1, task).ResultURL)
	task.APIKeyID++
	require.NotContains(t, svc.PublicVideoTask(context.Background(), 1, task).ResultURL, "metaso")
}

func TestPrivateVideoDownloadRefreshesTaskAndAuthenticatesOnlyGemini(t *testing.T) {
	for _, protocol := range []string{VideoProtocolMiniMax, VideoProtocolGemini, VideoProtocolSeedance} {
		t.Run(protocol, func(t *testing.T) {
			a := &Account{ID: 91, Platform: PlatformVideo, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "secret", "base_url": "https://google.example/v1beta", "video_protocol": protocol, "hide_video_upstream_url": true}}
			id, status := "gemini:models/veo/operations/one", `{"name":"models/veo/operations/one","done":true,"response":{"generatedVideos":[{"video":{"uri":"https://google.example/first.mp4"}},{"video":{"uri":"https://google.example/second.mp4?key=old"}}]}}`
			index := 1
			if protocol == VideoProtocolMiniMax {
				a.Credentials["base_url"] = "https://metaso.cn/api/minimax/v2"
				id, status, index = "minimax:task-1", `{"task":{"id":"task-1","status":"succeeded","content":{"url":"https://files.metaso.cn/fresh.mp4"}}}`, 0
			} else if protocol == VideoProtocolSeedance {
				a.Credentials["base_url"] = "https://ark.example/api/v3"
				id, status, index = "seedance:task-1", `{"id":"task-1","status":"succeeded","content":{"video_url":"https://cdn.example/fresh.mp4"}}`, 0
			}
			u := &grokMediaContentUpstreamStub{responses: []*http.Response{grokMediaContentStatusResponse(status), {StatusCode: 200, Header: http.Header{"Content-Type": {"video/mp4"}, "Location": {"https://upstream.example/secret"}}, Body: io.NopCloser(strings.NewReader("video-bytes")), ContentLength: 11}}}
			svc := &OpenAIGatewayService{cache: &schedulerTestGatewayCache{}, accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{*a}}, httpUpstream: u}
			groupID := int64(12)
			task := &AsyncTaskRecord{AsyncTask: AsyncTask{Protocol: protocol, GroupID: &groupID, APIKeyID: 2}, UserID: 1, RequestID: id}
			require.NoError(t, svc.BindGrokMediaVideoRequestAccount(context.Background(), &groupID, id, 1, 2, a.ID))
			c, w := grokMediaContentTestContext(http.MethodGet, VideoContentProxyURL(id, index), nil)
			require.NoError(t, svc.ForwardNativeVideoContent(context.Background(), c, task, index))
			require.Equal(t, "video-bytes", w.Body.String())
			require.Empty(t, w.Header().Get("Location"))
			require.Len(t, u.requests, 2)
			download := u.requests[1]
			require.True(t, HTTPUpstreamRedirectsDisabled(download.Context()))
			require.Empty(t, download.Header.Get("Authorization"))
			if protocol == VideoProtocolGemini {
				require.Equal(t, "secret", download.Header.Get("x-goog-api-key"))
				require.Equal(t, "/second.mp4", download.URL.Path)
				require.Empty(t, download.URL.Query().Get("key"))
			} else {
				require.Empty(t, download.Header.Get("x-goog-api-key"))
				require.True(t, HTTPUpstreamPublicHostsOnly(download.Context()))
			}
			task.UserID++
			require.Error(t, svc.ForwardNativeVideoContent(context.Background(), c, task, index))
			require.Len(t, u.requests, 2)
		})
	}
}
