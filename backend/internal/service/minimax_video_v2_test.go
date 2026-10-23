//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func miniMaxV2TestAccount() *Account {
	a := miniMaxVideoTestAccount()
	a.Credentials["base_url"] = "https://metaso.cn/api/minimax/v2/video_generation"
	a.Credentials["model_mapping"] = map[string]any{"public-video": "MiniMax-H3"}
	return a
}

func TestMiniMaxV2NativeAndLegacyCreate(t *testing.T) {
	for _, body := range []string{
		`{"model":"public-video","content":[{"type":"text","text":"A red ball"}],"duration":7,"ratio":"16:9","extra":{"keep":true}}`,
		`{"model":"public-video","prompt":"A red ball","duration":6,"first_frame_image":"https://images.example/a.png","extra":{"keep":true}}`,
	} {
		a := miniMaxV2TestAccount()
		u := &grokMediaContentUpstreamStub{response: grokMediaContentStatusResponse(`{"task_id":"2113125869779501056"}`)}
		svc := &OpenAIGatewayService{httpUpstream: u}
		c, w := grokMediaContentTestContext(http.MethodPost, "/v2/video_generation", nil)
		result, err := svc.ForwardMiniMaxVideo(context.Background(), c, a, MiniMaxVideoEndpointCreate, "", []byte(body))
		require.NoError(t, err)
		require.Equal(t, "minimax:2113125869779501056", result.ResponseID)
		require.Equal(t, `{"task_id":"2113125869779501056"}`, w.Body.String())
		require.Equal(t, "https://metaso.cn/api/minimax/v2/video_generation", u.request.URL.String())
		payload, err := io.ReadAll(u.request.Body)
		require.NoError(t, err)
		require.Equal(t, "MiniMax-H3", gjson.GetBytes(payload, "model").String())
		require.Equal(t, "A red ball", gjson.GetBytes(payload, "content.0.text").String())
		require.Equal(t, "768P", gjson.GetBytes(payload, "resolution").String())
		require.True(t, gjson.GetBytes(payload, "extra.keep").Bool())
		require.False(t, gjson.GetBytes(payload, "prompt").Exists())
		if strings.Contains(body, "first_frame_image") {
			require.Equal(t, "adaptive", gjson.GetBytes(payload, "ratio").String())
			require.Equal(t, "first_frame", gjson.GetBytes(payload, "content.1.role").String())
		} else {
			require.Equal(t, "16:9", gjson.GetBytes(payload, "ratio").String())
			require.Equal(t, 7, result.VideoDurationSeconds)
		}
		require.Len(t, u.requests, 1)
		require.Zero(t, result.VideoCount)
	}
}

func TestMiniMaxV2StatusUsesOwnedTaskAndDirectResult(t *testing.T) {
	for _, state := range []string{"queued", "running", "succeeded", "failed", "cancelled"} {
		t.Run(state, func(t *testing.T) {
			body := `{"task":{"id":"task-1","status":"` + state + `","content":{"url":"https://files.metaso.cn/video.mp4"},"error":{"message":"blocked"}}}`
			u := &grokMediaContentUpstreamStub{response: grokMediaContentStatusResponse(body)}
			svc := &OpenAIGatewayService{httpUpstream: u}
			c, w := grokMediaContentTestContext(http.MethodGet, "/v2/query/video_generation/task-1", nil)
			result, err := svc.ForwardMiniMaxVideo(context.Background(), c, miniMaxV2TestAccount(), MiniMaxVideoEndpointStatus, "minimax:task-1", nil)
			require.NoError(t, err)
			require.JSONEq(t, body, w.Body.String())
			require.Equal(t, "/api/minimax/v2/query/video_generation/task-1", u.request.URL.Path)
			require.Empty(t, u.request.URL.RawQuery, "v2 query parameters would invoke the task list")
			require.Len(t, u.requests, 1, "v2 must not call the v1 files API")
			if state == "succeeded" {
				require.Equal(t, "completed", result.AsyncTaskStatus)
				require.Equal(t, 1, result.VideoCount)
				require.Equal(t, "https://files.metaso.cn/video.mp4", result.AsyncTaskURL)
			} else {
				require.Zero(t, result.VideoCount)
				if state == "failed" || state == "cancelled" {
					require.Equal(t, state, result.AsyncTaskStatus)
					require.Equal(t, "blocked", result.AsyncTaskError)
				}
			}
		})
	}
	for _, body := range []string{`{"items":[{"id":"task-1","status":"succeeded"}]}`, `{"task":{"id":"other","status":"running"}}`, `{"task":{"id":"task-1","status":"succeeded"}}`, `{"task":{"id":"task-1","status":"unknown"}}`} {
		u := &grokMediaContentUpstreamStub{response: grokMediaContentStatusResponse(body)}
		svc := &OpenAIGatewayService{httpUpstream: u}
		c, _ := grokMediaContentTestContext(http.MethodGet, "/v2/query/video_generation/task-1", nil)
		_, err := svc.ForwardMiniMaxVideo(context.Background(), c, miniMaxV2TestAccount(), MiniMaxVideoEndpointStatus, "minimax:task-1", nil)
		require.Error(t, err, body)
	}
}

func TestMiniMaxV2ValidationAndErrors(t *testing.T) {
	for _, body := range []string{
		`{"model":"MiniMax-H3","content":[],"ratio":"16:9"}`,
		`{"model":"MiniMax-H3","content":[{"type":"text","text":"waves"}]}`,
		`{"model":"MiniMax-H3","content":[{"type":"text","text":"waves"}],"ratio":"adaptive"}`,
		`{"model":"MiniMax-H3","prompt":"waves","duration":3}`,
		`{"model":"MiniMax-H3-Max","prompt":"waves","duration":4}`,
		`{"model":"MiniMax-H3","prompt":"waves","duration":7.5}`,
		`{"model":"MiniMax-H3","prompt":"waves","resolution":"480P"}`,
	} {
		_, err := ParseMiniMaxVideoRequest([]byte(body))
		require.Error(t, err, body)
	}
	for _, duration := range []string{"4", "7", "15"} {
		_, err := ParseMiniMaxVideoRequest([]byte(`{"model":"MiniMax-H3","prompt":"waves","duration":` + duration + `}`))
		require.NoError(t, err)
	}
	// Check model-specific limits after mapping a public alias.
	a := miniMaxV2TestAccount()
	a.Credentials["model_mapping"] = map[string]any{"public-video": "MiniMax-H3-Max"}
	u := &grokMediaContentUpstreamStub{}
	svc := &OpenAIGatewayService{httpUpstream: u}
	c, _ := grokMediaContentTestContext(http.MethodPost, "/v2/video_generation", nil)
	_, err := svc.ForwardMiniMaxVideo(context.Background(), c, a, MiniMaxVideoEndpointCreate, "", []byte(`{"model":"public-video","content":[{"type":"text","text":"waves"}],"ratio":"16:9","duration":4}`))
	require.Error(t, err)
	require.Empty(t, u.requests)
	for _, code := range []int{200, 400} {
		response := grokMediaContentStatusResponse(`{"type":"error","error":{"type":"bad_request_error","message":"content 不能为空 (2013)"}}`)
		response.StatusCode = code
		u := &grokMediaContentUpstreamStub{response: response}
		svc := &OpenAIGatewayService{httpUpstream: u}
		c, w := grokMediaContentTestContext(http.MethodPost, "/v2/video_generation", nil)
		_, err := svc.ForwardMiniMaxVideo(context.Background(), c, miniMaxV2TestAccount(), MiniMaxVideoEndpointCreate, "", []byte(`{"model":"public-video","prompt":"waves"}`))
		require.Error(t, err)
		require.Contains(t, w.Body.String(), "content 不能为空 (2013)")
		require.GreaterOrEqual(t, w.Code, 400)
		require.Len(t, u.requests, 1)
	}
}

func TestMiniMaxV2ModelSpecificResolutionsAfterMapping(t *testing.T) {
	for _, tc := range []struct {
		model      string
		resolution string
		wantError  bool
	}{
		{"MiniMax-H3", "2K", false},
		{"MiniMax-H3", "768P", false},
		{"MiniMax-H3", "480P", true},
		{"MiniMax-H3-Max", "2K", true},
		{"minimax-h3-max", "2k", true},
		{"MiniMax-H3-Max", "768P", false},
		{"MiniMax-H3-Max", "480P", false},
	} {
		t.Run(tc.model+"/"+tc.resolution, func(t *testing.T) {
			body := `{"model":"` + tc.model + `","prompt":"waves","resolution":"` + tc.resolution + `"}`
			_, err := ParseMiniMaxVideoRequest([]byte(body))
			if tc.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			// Public aliases must receive the same validation as native model IDs.
			a := miniMaxV2TestAccount()
			a.Credentials["model_mapping"] = map[string]any{"public-video": tc.model}
			u := &grokMediaContentUpstreamStub{response: grokMediaContentStatusResponse(`{"task_id":"task-1"}`)}
			svc := &OpenAIGatewayService{httpUpstream: u}
			c, _ := grokMediaContentTestContext(http.MethodPost, "/v2/video_generation", nil)
			result, err := svc.ForwardMiniMaxVideo(context.Background(), c, a, MiniMaxVideoEndpointCreate, "", []byte(`{"model":"public-video","content":[{"type":"text","text":"waves"}],"ratio":"16:9","resolution":"`+tc.resolution+`"}`))
			if tc.wantError {
				require.Error(t, err)
				require.Nil(t, result)
				require.Empty(t, u.requests, "invalid model/resolution pairs must not reach upstream")
			} else {
				require.NoError(t, err)
				require.Len(t, u.requests, 1)
				payload, err := io.ReadAll(u.request.Body)
				require.NoError(t, err)
				require.Equal(t, tc.model, gjson.GetBytes(payload, "model").String())
				require.Equal(t, strings.ToUpper(tc.resolution), gjson.GetBytes(payload, "resolution").String())
			}
		})
	}
}

func TestMiniMaxV2DownloadRefreshesURLAndNeverForwardsKey(t *testing.T) {
	a := miniMaxV2TestAccount()
	u := &grokMediaContentUpstreamStub{responses: []*http.Response{
		grokMediaContentStatusResponse(`{"task":{"id":"task-1","status":"succeeded","content":{"url":"https://files.metaso.cn/fresh.mp4"}}}`),
		{StatusCode: 200, Header: http.Header{"Content-Type": {"video/mp4"}}, Body: io.NopCloser(strings.NewReader("video-data")), ContentLength: 10},
	}}
	svc := &OpenAIGatewayService{cache: &schedulerTestGatewayCache{}, accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{*a}}, httpUpstream: u}
	groupID := int64(12)
	task := &AsyncTaskRecord{AsyncTask: AsyncTask{GroupID: &groupID, APIKeyID: 2, ResultURL: "https://old.example/expired"}, UserID: 1, RequestID: "minimax:task-1"}
	require.NoError(t, svc.BindGrokMediaVideoRequestAccount(context.Background(), &groupID, task.RequestID, task.UserID, task.APIKeyID, a.ID))
	c, w := grokMediaContentTestContext(http.MethodGet, "/user/async-tasks/one/content", nil)
	require.NoError(t, svc.ForwardMiniMaxVideoContent(context.Background(), c, task))
	require.Equal(t, "video-data", w.Body.String())
	require.Len(t, u.requests, 2)
	require.Equal(t, "/api/minimax/v2/query/video_generation/task-1", u.requests[0].URL.Path)
	require.Equal(t, "https://files.metaso.cn/fresh.mp4", u.requests[1].URL.String())
	require.Empty(t, u.requests[1].Header.Get("Authorization"))
	require.True(t, HTTPUpstreamRedirectsDisabled(u.requests[1].Context()))
	require.True(t, HTTPUpstreamPublicHostsOnly(u.requests[1].Context()))
}

func TestAccountTestServiceMiniMaxV2GeneratesAndPreviews(t *testing.T) {
	for _, withImage := range []bool{false, true} {
		a := miniMaxV2TestAccount()
		svc, u := adaptiveCNAccountTestService(a,
			grokMediaContentStatusResponse(`{"task_id":"task-1"}`),
			grokMediaContentStatusResponse(`{"task":{"id":"task-1","status":"succeeded","content":{"url":"https://files.metaso.cn/video.mp4"}}}`),
			&http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"video/mp4"}}, Body: io.NopCloser(strings.NewReader("video-bytes"))},
		)
		c, w := newTestContext()
		opts := AccountTestOptions{}
		if withImage {
			opts.ImageDataURL = minimalAccountTestPNGDataURL(256, 256)
		}
		require.NoError(t, svc.TestAccountConnection(c, a.ID, "public-video", "A red ball", "video", opts))
		require.Len(t, u.requests, 3)
		require.Contains(t, w.Body.String(), `"type":"test_complete","success":true`)
		require.Contains(t, w.Body.String(), `"video_url":"data:video/mp4;base64,`)
		require.Empty(t, u.requests[2].Header.Get("Authorization"))
		require.Empty(t, u.requests[2].Header.Get("x-goog-api-key"))
		require.True(t, HTTPUpstreamPublicHostsOnly(u.requests[2].Context()))
		require.Equal(t, "A red ball", gjson.GetBytes(u.bodies[0], "content.0.text").String())
		require.Equal(t, "/api/minimax/v2/query/video_generation/task-1", u.requests[1].URL.Path)
		if withImage {
			require.Equal(t, "adaptive", gjson.GetBytes(u.bodies[0], "ratio").String())
			require.Equal(t, opts.ImageDataURL, gjson.GetBytes(u.bodies[0], "content.1.image_url.url").String())
		} else {
			require.Equal(t, "16:9", gjson.GetBytes(u.bodies[0], "ratio").String())
		}
	}
	a := miniMaxV2TestAccount()
	a.Credentials["model_mapping"] = map[string]any{}
	require.Equal(t, []string{"MiniMax-H3", "MiniMax-H3-Max"}, a.VideoTestModelIDs())
	_, err := normalizeMiniMaxV2TestImage(minimalAccountTestPNGDataURL(8, 8))
	require.ErrorContains(t, err, "256")
}

func TestMiniMaxTestPreviewRejectsUnsafeOrInvalidDownloads(t *testing.T) {
	for _, link := range []string{"http://cdn.example/a", "https://127.0.0.1/a", "https://user@cdn.example/a", "https://cdn.example/a#fragment"} {
		svc, u := adaptiveCNAccountTestService(miniMaxV2TestAccount())
		c, _ := newTestContext()
		require.Error(t, svc.emitNativeVideoTestResult(c, c.Request.Context(), miniMaxV2TestAccount(), link))
		require.Empty(t, u.requests)
	}
	for _, code := range []int{http.StatusFound, http.StatusUnauthorized} {
		svc, u := adaptiveCNAccountTestService(miniMaxV2TestAccount(), &http.Response{StatusCode: code, Header: http.Header{"Location": {"https://evil.example"}}, Body: io.NopCloser(strings.NewReader("error"))})
		c, w := newTestContext()
		require.Error(t, svc.emitNativeVideoTestResult(c, c.Request.Context(), miniMaxV2TestAccount(), "https://files.metaso.cn/video.mp4"))
		require.Len(t, u.requests, 1)
		require.True(t, HTTPUpstreamRedirectsDisabled(u.requests[0].Context()))
		require.NotContains(t, w.Body.String(), `"type":"video"`)
	}
}
