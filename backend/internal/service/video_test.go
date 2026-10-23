//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestVideoAccountProtocolDefaultsAndIsolation(t *testing.T) {
	a := &Account{Platform: PlatformVideo, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "secret"}}
	require.NoError(t, ValidateVideoAccount(a))
	require.True(t, a.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilitySeedance))
	require.False(t, a.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityGeminiVideo))
	require.False(t, a.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityChatCompletions))
	a.Credentials["video_protocol"] = "gemini"
	require.True(t, a.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityGeminiVideo))
	require.False(t, a.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilitySeedance))
	a.Credentials["video_protocol"] = "unknown"
	require.Error(t, ValidateVideoAccount(a))
	a.Credentials["video_protocol"] = "seedance"
	a.Type = AccountTypeOAuth
	require.Error(t, ValidateVideoAccount(a))
}

func TestValidateVideoAccountRejectsInvalidInputAsBadRequest(t *testing.T) {
	valid := func() *Account {
		return &Account{Platform: PlatformVideo, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "secret"}}
	}
	for name, mutate := range map[string]func(*Account){
		"oauth type":       func(a *Account) { a.Type = AccountTypeOAuth },
		"unknown protocol": func(a *Account) { a.Credentials["video_protocol"] = "unknown" },
		"missing api key":  func(a *Account) { delete(a.Credentials, "api_key") },
		"userinfo":         func(a *Account) { a.Credentials["base_url"] = "https://user:pass@ark.example/api/v3" },
		"non-http scheme":  func(a *Account) { a.Credentials["base_url"] = "ftp://ark.example" },
		"privacy not bool": func(a *Account) { a.Credentials["hide_video_upstream_url"] = "true" },
	} {
		t.Run(name, func(t *testing.T) {
			a := valid()
			mutate(a)
			err := ValidateVideoAccount(a)
			require.Error(t, err)
			require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
			require.Equal(t, "INVALID_VIDEO_ACCOUNT", infraerrors.Reason(err))
			require.NotContains(t, err.Error(), "user:pass")
		})
	}
	require.NoError(t, ValidateVideoAccount(valid()))
	require.NoError(t, ValidateVideoAccount(&Account{Platform: PlatformOpenAI}))
}

func TestSeedanceVideoPlatformAndBaseURLs(t *testing.T) {
	for _, base := range []string{"https://ark.example", "https://ark.example/v1", "https://ark.example/api/v3/", "https://ark.example/api/v3/contents/generations/tasks"} {
		got, err := buildSeedanceURL(base, SeedanceEndpointCreate, "")
		require.NoError(t, err)
		require.Equal(t, "https://ark.example/api/v3/contents/generations/tasks", got)
	}
	a := &Account{ID: 1, Platform: PlatformVideo, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "secret"}}
	u := &grokMediaContentUpstreamStub{response: grokMediaContentStatusResponse(`{"id":"ark-1"}`)}
	svc := &OpenAIGatewayService{httpUpstream: u}
	c, _ := grokMediaContentTestContext(http.MethodPost, "/api/v3/contents/generations/tasks", nil)
	_, err := svc.ForwardSeedance(context.Background(), c, a, SeedanceEndpointCreate, "", []byte(`{"model":"ep-1","content":[{"type":"text","text":"waves"}]}`))
	require.NoError(t, err)
	require.Equal(t, "ark.cn-beijing.volces.com", u.request.URL.Host)
}

func TestGeminiVideoNativeForwardingAndCompletion(t *testing.T) {
	a := &Account{ID: 1, Platform: PlatformVideo, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "google-secret", "video_protocol": "gemini", "base_url": "https://google.example/v1beta", "model_mapping": map[string]any{"public-video": "veo-3.0-generate-001"}}}
	u := &grokMediaContentUpstreamStub{response: grokMediaContentStatusResponse(`{"name":"models/veo-3.0-generate-001/operations/one"}`)}
	svc := &OpenAIGatewayService{httpUpstream: u}
	c, _ := grokMediaContentTestContext(http.MethodPost, "/v1beta/models/public-video:predictLongRunning", nil)
	result, err := svc.ForwardGeminiVideo(context.Background(), c, a, GeminiVideoEndpointCreate, "", []byte(`{"model":"public-video","instances":[{"prompt":"waves"}],"parameters":{"durationSeconds":8,"resolution":"1080p","sampleCount":1},"future":{"keep":true}}`))
	require.NoError(t, err)
	require.Equal(t, "/v1beta/models/veo-3.0-generate-001:predictLongRunning", u.request.URL.Path)
	require.Equal(t, "google-secret", u.request.Header.Get("x-goog-api-key"))
	require.Empty(t, u.request.Header.Get("Authorization"))
	body, err := io.ReadAll(u.request.Body)
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(body, "model").Exists())
	require.True(t, gjson.GetBytes(body, "future.keep").Bool())
	require.Equal(t, "gemini:models/veo-3.0-generate-001/operations/one", result.ResponseID)
	require.Equal(t, "1080p", result.VideoResolution)
	require.Equal(t, 8, result.VideoDurationSeconds)
	require.Zero(t, result.VideoCount, "submitting a task must not bill")

	u.response = grokMediaContentStatusResponse(`{"name":"models/veo-3.0-generate-001/operations/one","done":true,"response":{"generateVideoResponse":{"generatedSamples":[{"video":{"uri":"https://google.example/v1beta/files/a:download"}},{"video":{"uri":"https://google.example/v1beta/files/b:download"}}]}}}`)
	c, _ = grokMediaContentTestContext(http.MethodGet, "/v1beta/models/veo-3.0-generate-001/operations/one", nil)
	result, err = svc.ForwardGeminiVideo(context.Background(), c, a, GeminiVideoEndpointStatus, result.ResponseID, nil)
	require.NoError(t, err)
	require.Equal(t, "completed", result.AsyncTaskStatus)
	require.Equal(t, 2, result.VideoCount)
	require.Equal(t, "/v1beta/models/veo-3.0-generate-001/operations/one", u.request.URL.Path)
	for _, name := range []string{"", "models/a/operations/..", "models/a/operations/a?key=secret", "models/a/operations/%2e%2e", "https://evil.example/a", "models/a/operations/a/extra"} {
		require.Error(t, ValidateGeminiVideoOperation(name), name)
	}
}

func TestGeminiVideoFailureAndNoRetry(t *testing.T) {
	a := &Account{ID: 1, Platform: PlatformVideo, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "secret", "video_protocol": "gemini"}}
	u := &grokMediaContentUpstreamStub{response: grokMediaContentStatusResponse(`{"error":{"code":429,"message":"quota exceeded"}}`)}
	u.response.StatusCode = 429
	svc := &OpenAIGatewayService{httpUpstream: u}
	c, w := grokMediaContentTestContext(http.MethodPost, "/v1beta/models/veo:predictLongRunning", nil)
	_, err := svc.ForwardGeminiVideo(context.Background(), c, a, GeminiVideoEndpointCreate, "", []byte(`{"model":"veo","instances":[{"prompt":"waves"}]}`))
	require.Error(t, err)
	require.Equal(t, 429, w.Code)
	require.Len(t, u.requests, 1)
	status, _, message := videoTaskObservation("gemini", []byte(`{"done":true,"error":{"message":"blocked"}}`))
	require.Equal(t, "failed", status)
	require.Equal(t, "blocked", message)
	status, _, message = videoTaskObservation("gemini", []byte(`{"done":true,"response":{}}`))
	require.Equal(t, "failed", status)
	require.NotEmpty(t, message)
	info, err := ParseGeminiVideoRequest([]byte(`{"model":"veo","instances":[{"prompt":"waves"}]}`))
	require.NoError(t, err)
	require.Equal(t, "720p", info.Resolution)
	require.Equal(t, 8, info.DurationSeconds)
}

func TestGeminiVideoContentUsesBoundAccountAndRejectsUnsafeURLs(t *testing.T) {
	account := Account{ID: 91, Platform: PlatformVideo, Type: AccountTypeAPIKey, Credentials: map[string]any{
		"api_key": "upstream-secret", "video_protocol": VideoProtocolGemini, "base_url": "https://google.example/v1beta",
	}}
	upstream := &grokMediaContentUpstreamStub{response: &http.Response{
		StatusCode: 200, Header: http.Header{"Content-Type": []string{"video/mp4"}},
		Body: io.NopCloser(strings.NewReader("video-data")), ContentLength: 10,
	}}
	svc := &OpenAIGatewayService{cache: &schedulerTestGatewayCache{}, accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{account}}, httpUpstream: upstream}
	groupID := int64(12)
	task := &AsyncTaskRecord{AsyncTask: AsyncTask{GroupID: &groupID, APIKeyID: 2, ResultURL: "https://google.example/v1beta/files/a:download?alt=media&key=old&api_key=old"}, UserID: 1, RequestID: "gemini:models/veo/operations/one"}
	require.NoError(t, svc.BindGrokMediaVideoRequestAccount(context.Background(), &groupID, task.RequestID, task.UserID, task.APIKeyID, account.ID))
	c, recorder := grokMediaContentTestContext(http.MethodGet, "/user/async-tasks/one/content", nil)
	require.NoError(t, svc.ForwardGeminiVideoContent(context.Background(), c, task))
	require.Equal(t, "video-data", recorder.Body.String())
	require.Equal(t, "upstream-secret", upstream.request.Header.Get("x-goog-api-key"))
	require.Empty(t, upstream.request.Header.Get("Authorization"))
	require.Equal(t, "alt=media", upstream.request.URL.RawQuery)
	require.True(t, HTTPUpstreamRedirectsDisabled(upstream.request.Context()))
	for _, target := range []string{"https://evil.example/a", "http://google.example/a", "https://google.example:8443/a", "https://user@google.example/a", "https://google.example/a#fragment", "/relative"} {
		task.ResultURL = target
		before := len(upstream.requests)
		require.Error(t, svc.ForwardGeminiVideoContent(context.Background(), c, task), target)
		require.Len(t, upstream.requests, before)
	}
	task.ResultURL = "https://google.example/v1beta/files/a:download"
	upstream.response = &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://evil.example/a"}}, Body: io.NopCloser(strings.NewReader(""))}
	require.Error(t, svc.ForwardGeminiVideoContent(context.Background(), c, task))
	require.True(t, HTTPUpstreamRedirectsDisabled(upstream.request.Context()))
	task.UserID++
	before := len(upstream.requests)
	require.Error(t, svc.ForwardGeminiVideoContent(context.Background(), c, task))
	require.Len(t, upstream.requests, before)
}
