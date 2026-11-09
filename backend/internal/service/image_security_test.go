//go:build unit

package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/imagecompat"
	"github.com/stretchr/testify/require"
)

type imageStateRepo struct {
	rateLimitAccountRepoStub
	until time.Time
}

func (r *imageStateRepo) SetTempUnschedulable(ctx context.Context, id int64, until time.Time, reason string) error {
	r.until = until
	return r.rateLimitAccountRepoStub.SetTempUnschedulable(ctx, id, until, reason)
}

func TestImagePlatformErrorStatePolicy(t *testing.T) {
	for _, tc := range []struct {
		status              int
		body                string
		permanent, cooldown bool
	}{
		{401, `{"error":{"message":"relay-secret-42"}}`, true, false},
		{400, `{"error":{"message":"API_KEY_INVALID"}}`, true, false},
		{403, `<html>relay-secret-42 at /srv/render</html>`, false, true},
		{403, `{"error":{"message":"no credits relay-secret-42"}}`, false, true},
		{403, `{"error":{"status":"PERMISSION_DENIED"}}`, false, true},
		{402, `{"error":{"message":"credits"}}`, false, true},
		{400, `{"error":{"message":"bad prompt relay-secret-42"}}`, false, false},
		{400, `{"error":{"code":"content_policy_violation"}}`, false, false},
	} {
		t.Run(fmt.Sprintf("%d/%s", tc.status, tc.body), func(t *testing.T) {
			repo := &imageStateRepo{}
			rate := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
			account := &Account{ID: 7, Platform: PlatformImage, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "relay-secret-42", "image_protocol": "gemini", "base_url": "https://relay.example/v1"}}
			rate.HandleUpstreamError(context.Background(), account, tc.status, nil, []byte(tc.body))
			require.Equal(t, tc.permanent, repo.setErrorCalls == 1)
			require.Equal(t, tc.cooldown, repo.tempCalls == 1)
			require.NotContains(t, repo.lastErrorMsg+repo.lastTempReason, "relay-secret-42")
			require.NotContains(t, repo.lastErrorMsg+repo.lastTempReason, "/srv/render")
			if tc.cooldown {
				require.WithinDuration(t, time.Now().Add(5*time.Minute), repo.until, time.Second)
				account.TempUnschedulableUntil = &repo.until
				require.False(t, account.IsSchedulable())
				expired := time.Now().Add(-time.Second)
				account.TempUnschedulableUntil = &expired
				require.True(t, account.IsSchedulable(), "finite cooldown expires without a manual status reset")
			}
		})
	}
}

func TestImagePlatformErrorsRedactActualCredentials(t *testing.T) {
	for _, status := range []int{400, 422} {
		for _, raw := range []bool{false, true} {
			message := "RenderError relay-secret-42 https://relay.example/v1 relay.example\n at render (/srv/image/renderer.js:87)"
			body := message
			if !raw {
				body = fmt.Sprintf(`{"error":{"message":%q}}`, message)
			}
			upstream := imagePlatformHTTPFunc(func(*http.Request) (*http.Response, error) {
				return b64BackfillImageResponse(status, "application/json", []byte(body)), nil
			})
			svc := newOpenAIImagesTestService(upstream)
			request := []byte(`{"model":"gemini-2.5-flash-image","prompt":"draw"}`)
			c, rec := newOpenAIImagesTestContext(t, request)
			parsed, err := svc.ParseOpenAIImagesRequest(c, request)
			require.NoError(t, err)
			_, err = svc.ForwardImages(c.Request.Context(), c, &Account{Platform: PlatformImage, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "relay-secret-42", "base_url": "https://relay.example/v1", "image_protocol": "gemini"}}, request, parsed, "")
			require.Error(t, err)
			for _, secret := range []string{"relay-secret-42", "relay.example", "/srv/image", "at render"} {
				require.NotContains(t, rec.Body.String()+err.Error(), secret)
			}
		}
	}
}

func TestImagePlatformShortSecretKeepsNormalizedMetadata(t *testing.T) {
	for _, secret := range []string{"a", "ra", "r42", "r42x", "r42xy"} {
		account := &Account{Platform: PlatformImage, Credentials: map[string]any{"api_key": secret}}
		require.Equal(t, "[redacted]", redactImageAccountText(account, secret))
		typed := imagecompat.ConvertUpstreamError(imagecompat.ProtocolGemini, 400, []byte(fmt.Sprintf(`{"error":{"message":%q}}`, "RenderError "+secret)))
		sanitized := sanitizeImageAccountError(account, typed)
		require.Equal(t, "Upstream rejected the request", sanitized.Message)
		require.Equal(t, typed.Type, sanitized.Type)
		require.Equal(t, typed.Code, sanitized.Code)
		require.Equal(t, typed.Param, sanitized.Param)
	}
}

func TestImagePlatformRejectsIgnoredParameters(t *testing.T) {
	for _, body := range []string{
		`{"model":"gemini-2.5-flash-image","prompt":"draw","images":[{"image_url":"https://public.example/a"}]}`,
		`{"model":"gemini-2.5-flash-image","prompt":"draw","image":"abc"}`,
		`{"model":"gemini-2.5-flash-image","prompt":"draw","n":1.5}`,
		`{"model":"gemini-2.5-flash-image","prompt":"draw","partial_images":0.5}`,
	} {
		c, _ := newOpenAIImagesTestContext(t, []byte(body))
		c.Request = c.Request.WithContext(WithResolvedTargetPlatform(c.Request.Context(), PlatformImage))
		_, err := (&OpenAIGatewayService{}).ParseOpenAIImagesRequest(c, []byte(body))
		require.Error(t, err)
	}
	for _, file := range []bool{false, true} {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		require.NoError(t, writer.WriteField("model", "gemini-2.5-flash-image"))
		require.NoError(t, writer.WriteField("prompt", "draw"))
		image, err := writer.CreateFormFile("image", "image.png")
		require.NoError(t, err)
		_, err = image.Write(b64BackfillPNGBytes)
		require.NoError(t, err)
		if file {
			part, err := writer.CreateFormFile("quality", "quality.txt")
			require.NoError(t, err)
			_, err = io.WriteString(part, "auto")
			require.NoError(t, err)
		} else {
			require.NoError(t, writer.WriteField("mask", "ignored"))
		}
		require.NoError(t, writer.Close())
		c, _ := newOpenAIImagesTestContext(t, body.Bytes())
		c.Request.URL.Path = "/v1/images/edits"
		c.Request.Header.Set("Content-Type", writer.FormDataContentType())
		parsed, err := (&OpenAIGatewayService{}).ParseOpenAIImagesRequest(c, body.Bytes())
		require.NoError(t, err)
		require.Error(t, validateImagePlatformInput(parsed, true))
	}
	for _, platform := range []string{PlatformOpenAI, PlatformImage} {
		body := []byte(`{"model":"gpt-image-2","prompt":"draw","n":1.5}`)
		c, _ := newOpenAIImagesTestContext(t, body)
		c.Request = c.Request.WithContext(WithResolvedTargetPlatform(c.Request.Context(), platform))
		_, err := (&OpenAIGatewayService{}).ParseOpenAIImagesRequest(c, body)
		require.Equal(t, platform == PlatformImage, err != nil, "legacy parser remains unchanged")
	}
	body := []byte(`{"model":"gemini-2.5-flash-image","prompt":"edit","images":[{"image_url":"https://public.example/a"},{"unknown":"silently ignored before validation"}]}`)
	c, _ := newOpenAIImagesTestContext(t, body)
	c.Request.URL.Path = "/v1/images/edits"
	c.Request = c.Request.WithContext(WithResolvedTargetPlatform(c.Request.Context(), PlatformImage))
	_, err := (&OpenAIGatewayService{}).ParseOpenAIImagesRequest(c, body)
	require.Error(t, err)
}

func TestImagePlatformLocalErrorsDoNotChangeSchedulingStats(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
	openAIAdvancedSchedulerSettingCache.Store(&cachedOpenAIAdvancedSchedulerSetting{enabled: true, expiresAt: time.Now().Add(time.Minute).UnixNano()})
	stats := newOpenAIAccountRuntimeStats()
	svc := &OpenAIGatewayService{openaiAccountStats: stats, openaiScheduler: &defaultOpenAIAccountScheduler{stats: stats}}
	for _, platform := range []string{PlatformImage, PlatformOpenAI} {
		id := int64(1)
		if platform == PlatformOpenAI {
			id = 2
		}
		svc.ReportOpenAIAccountScheduleResult(&Account{ID: id, Platform: platform}, "gpt-image-2", false, nil, imageRequestError("quality", "Unsupported image option"))
		rate, _, _ := stats.snapshot(id)
		if platform == PlatformImage {
			require.Zero(t, rate)
		} else {
			require.Positive(t, rate)
		}
	}
}

func TestImagePlatformMixedFailurePriority(t *testing.T) {
	for _, statuses := range [][]int{{400, 429, 401}, {400, 429, 402}, {400, 500, 429}} {
		best, status := 0, 0
		for _, code := range statuses {
			e := imagecompat.ConvertUpstreamError(imagecompat.ProtocolGemini, code, []byte(`{"error":{"message":"fixture"}}`))
			if rank := imageFailurePriority(e); rank > best {
				best, status = rank, code
			}
		}
		require.Equal(t, statuses[2], status)
	}
}

func TestImagePlatformGrokBackfillsRequestedBase64(t *testing.T) {
	svc := newOpenAIImagesTestService(imagePlatformHTTPFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method == "GET" {
			require.Equal(t, "https://public.example/result.png", req.URL.String())
			return b64BackfillImageResponse(200, "image/png", b64BackfillPNGBytes), nil
		}
		return b64BackfillImageResponse(200, "application/json", []byte(`{"data":[{"url":"https://public.example/result.png"}]}`)), nil
	}))
	body := []byte(`{"model":"grok-imagine-image","prompt":"draw","response_format":"b64_json"}`)
	c, rec := newOpenAIImagesTestContext(t, body)
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	_, err = svc.ForwardImages(c.Request.Context(), c, &Account{Platform: PlatformImage, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "secret", "image_protocol": "grok"}}, body, parsed, "")
	require.NoError(t, err)
	require.Contains(t, rec.Body.String(), "b64_json")
	require.NotEqual(t, "", strings.TrimSpace(rec.Body.String()))
}
