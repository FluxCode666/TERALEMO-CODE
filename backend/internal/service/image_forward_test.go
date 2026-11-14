//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/imagecompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type imagePlatformTestHTTP struct {
	HTTPUpstream
	client *http.Client
}

type imagePlatformHTTPFunc func(*http.Request) (*http.Response, error)

func (f imagePlatformHTTPFunc) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return f(req)
}

func (f imagePlatformHTTPFunc) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return f.Do(req, proxy, id, concurrency)
}

func (u imagePlatformTestHTTP) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return u.client.Do(req)
}

func TestImagePlatformForwardProtocols(t *testing.T) {
	for _, tc := range []struct {
		name, protocol, model, path, result string
		n, calls, images                    int
	}{
		{"openai", "openai", "gpt-image-2", "/v1/images/generations", `{"data":[{"b64_json":"aW1hZ2U="}]}`, 3, 1, 1},
		{"gemini", "gemini", "gemini-2.5-flash-image", "/v1beta/models/gemini-2.5-flash-image:generateContent", `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"aW1hZ2U="}}]},"finishReason":"STOP"}]}`, 2, 2, 2},
		{"imagen", "gemini", "imagen-4.0-generate-001", "/v1beta/models/imagen-4.0-generate-001:predict", `{"predictions":[{"bytesBase64Encoded":"aW1hZ2U=","mimeType":"image/png"}]}`, 2, 1, 1},
		{"grok", "grok", "grok-imagine-image", "/v1/images/generations", `{"data":[{"b64_json":"aW1hZ2U="}]}`, 3, 1, 1},
		{"gemini_partial", "gemini", "gemini-2.5-flash-image", "/v1beta/models/gemini-2.5-flash-image:generateContent", `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"aW1hZ2U="}}]}}]}`, 2, 2, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				call := calls.Add(1)
				require.Equal(t, tc.path, r.URL.Path)
				if tc.protocol == "gemini" {
					require.Equal(t, "secret", r.Header.Get("x-goog-api-key"))
					require.Empty(t, r.Header.Get("Authorization"))
				} else {
					require.Equal(t, "Bearer secret", r.Header.Get("Authorization"))
				}
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				if tc.protocol != "gemini" {
					require.Equal(t, tc.model, gjson.GetBytes(body, "model").String())
				}
				w.Header().Set("Content-Type", "application/json")
				if tc.name == "gemini_partial" && call == 2 {
					w.WriteHeader(http.StatusServiceUnavailable)
					_, _ = io.WriteString(w, `{"error":{"message":"overloaded"}}`)
					return
				}
				_, _ = io.WriteString(w, tc.result)
			}))
			defer upstream.Close()
			cfg := &config.Config{}
			cfg.Security.URLAllowlist.AllowInsecureHTTP = true
			svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: imagePlatformTestHTTP{client: upstream.Client()}}
			body := []byte(fmt.Sprintf(`{"model":"public-image","prompt":"draw","n":%d,"size":"1024x1024"}`, tc.n))
			c, rec := newOpenAIImagesTestContext(t, body)
			c.Request = c.Request.WithContext(WithResolvedTargetPlatform(c.Request.Context(), PlatformImage))
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			a := &Account{ID: 42, Platform: PlatformImage, Type: AccountTypeAPIKey, Credentials: map[string]any{
				"api_key": "secret", "image_protocol": tc.protocol, "base_url": upstream.URL,
				"model_mapping": map[string]any{"public-image": tc.model},
			}}
			result, err := svc.ForwardImages(c.Request.Context(), c, a, body, parsed, "")
			require.NoError(t, err, rec.Body.String())
			require.EqualValues(t, tc.calls, calls.Load())
			require.Equal(t, tc.images, result.ImageCount)
			require.Equal(t, tc.model, result.UpstreamModel)
			require.Equal(t, 200, rec.Code)
			require.Len(t, gjson.GetBytes(rec.Body.Bytes(), "data").Array(), tc.images)
		})
	}
}

func TestImagePlatformFailures(t *testing.T) {
	for _, tc := range []struct {
		name, protocol, body string
		status               int
		retry                bool
	}{
		{"gemini_safety", "gemini", `{"promptFeedback":{"blockReason":"SAFETY"}}`, 200, false},
		{"imagen_safety", "gemini", `{"predictions":[{"raiFilteredReason":"safety"}]}`, 200, false},
		{"gemini_429", "gemini", `{"error":{"message":"limited"}}`, 429, true},
		{"grok_429", "grok", `{"error":{"message":"limited"}}`, 429, true},
		{"grok_auth", "grok", `{"error":{"message":"secret https://private.example/raw-stack"}}`, 401, true},
		{"openai_auth", "openai", `{"error":{"message":"secret https://private.example/raw-stack"}}`, 401, true},
		{"openai_zero", "openai", `{"data":[],"usage":{"input_tokens":9999}}`, 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := "gemini-2.5-flash-image"
			if strings.HasPrefix(tc.name, "imagen") {
				model = "imagen-4.0-generate-001"
			}
			upstream := imagePlatformHTTPFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})
			svc := newOpenAIImagesTestService(upstream)
			body := []byte(fmt.Sprintf(`{"model":%q,"prompt":"draw","n":3}`, model))
			c, rec := newOpenAIImagesTestContext(t, body)
			c.Request = c.Request.WithContext(WithResolvedTargetPlatform(c.Request.Context(), PlatformImage))
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			a := &Account{ID: 42, Platform: PlatformImage, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "secret", "image_protocol": tc.protocol}}
			result, err := svc.ForwardImages(c.Request.Context(), c, a, body, parsed, "")
			require.Error(t, err)
			require.Nil(t, result)
			var failover *UpstreamFailoverError
			require.Equal(t, tc.retry, errors.As(err, &failover))
			output := rec.Body.String()
			if failover != nil {
				output += string(failover.ResponseBody)
			}
			require.NotContains(t, output, "secret")
			require.NotContains(t, output, "private.example")
			require.NotContains(t, output, "raw-stack")
		})
	}
}

func TestImagePlatformConcurrentErrorHeadersMatchCallIndex(t *testing.T) {
	for _, statuses := range [][2]int{{429, 503}, {503, 429}} {
		t.Run(fmt.Sprint(statuses), func(t *testing.T) {
			svc := newOpenAIImagesTestService(imagePlatformHTTPFunc(func(req *http.Request) (*http.Response, error) {
				index := 0
				if req.URL.Path == "/1" {
					index = 1
				}
				response := b64BackfillImageResponse(statuses[index], "application/json", []byte(`{"error":{"message":"fixture"}}`))
				if statuses[index] == 429 {
					response.Header.Set("Retry-After", "60")
				}
				return response, nil
			}))
			// Distinct paths bind each fake response to a planned slot, regardless
			// of worker start/completion order.
			plan := &imagecompat.Plan{Protocol: imagecompat.ProtocolGemini, Model: "gemini-2.5-flash-image", RequestedN: 2,
				Calls: []imagecompat.UpstreamCall{{Method: "POST", Path: "/0", ExpectImages: 1}, {Method: "POST", Path: "/1", ExpectImages: 1}}}
			results, responses := svc.executeImageCompatPlan(context.Background(), &Account{}, plan, "https://example.com", "")
			_, err := imagecompat.BuildResponse(plan, results, time.Now())
			var typed *imagecompat.Error
			require.ErrorAs(t, err, &typed)
			require.Equal(t, 0, typed.CallIndex)
			failover := imagePlatformResponseFailoverError(typed, responses)
			require.Equal(t, statuses[0], failover.StatusCode)
			require.Equal(t, statuses[0], failover.ImagePlatformError.HTTPStatus)
			if statuses[0] == 429 {
				require.Equal(t, "60", failover.ResponseHeaders.Get("Retry-After"))
			} else {
				require.Empty(t, failover.ResponseHeaders.Get("Retry-After"))
			}
			for _, index := range []int{-1, 2} {
				typed.CallIndex = index
				require.Empty(t, imagePlatformResponseFailoverError(typed, responses).ResponseHeaders)
			}
			typed.CallIndex = 0
			responses[0] = nil
			require.Empty(t, imagePlatformResponseFailoverError(typed, responses).ResponseHeaders)
		})
	}
}

func TestImagePlatformConcurrentCallsPreserveOrder(t *testing.T) {
	entered := make(chan string, 3)
	release := map[string]chan struct{}{"0": make(chan struct{}), "1": make(chan struct{}), "2": make(chan struct{})}
	completed := make(chan string, 3)
	svc := &OpenAIGatewayService{httpUpstream: imagePlatformHTTPFunc(func(req *http.Request) (*http.Response, error) {
		id := strings.TrimPrefix(req.URL.Path, "/")
		entered <- id
		<-release[id]
		completed <- id
		return b64BackfillImageResponse(200, "application/json", []byte(id)), nil
	})}
	plan := &imagecompat.Plan{Protocol: imagecompat.ProtocolGemini}
	for i := range 3 {
		plan.Calls = append(plan.Calls, imagecompat.UpstreamCall{Method: "POST", Path: fmt.Sprintf("/%d", i)})
	}
	done := make(chan []imagecompat.UpstreamResult, 1)
	go func() {
		results, _ := svc.executeImageCompatPlan(context.Background(), &Account{}, plan, "https://example.com", "")
		done <- results
	}()
	for range 3 {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("all three calls must start before any call finishes")
		}
	}
	for _, id := range []string{"2", "0", "1"} {
		close(release[id])
		require.Equal(t, id, <-completed)
	}
	for i, result := range <-done {
		require.Equal(t, fmt.Sprint(i), string(result.Body))
	}
}

func TestImagePlatformDowngradeForwardAndBilling(t *testing.T) {
	for _, size := range []string{"1536x1024", "4K"} {
		t.Run(size, func(t *testing.T) {
			var calls atomic.Int32
			upstream := imagePlatformHTTPFunc(func(req *http.Request) (*http.Response, error) {
				calls.Add(1)
				payload, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				require.Equal(t, "3:2", gjson.GetBytes(payload, "generationConfig.imageConfig.aspectRatio").String())
				return b64BackfillImageResponse(200, "application/json", []byte(`{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"aW1hZ2U="}}]}}]}`)), nil
			})
			svc := newOpenAIImagesTestService(upstream)
			body := []byte(fmt.Sprintf(`{"model":"gemini-2.5-flash-image","prompt":"draw","size":%q}`, size))
			c, rec := newOpenAIImagesTestContext(t, body)
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			account := &Account{ID: 3, Platform: PlatformImage, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "secret", "image_protocol": "gemini"}}
			result, err := svc.ForwardImages(c.Request.Context(), c, account, body, parsed, "")
			if size == "4K" {
				require.Error(t, err)
				require.Nil(t, result)
				require.Equal(t, 400, rec.Code)
				require.Zero(t, calls.Load())
				return
			}
			require.NoError(t, err)
			require.Equal(t, "1K", result.ImageSize)
			require.Equal(t, size, result.ImageInputSize)
			logs, users := &openAIRecordUsageLogRepoStub{inserted: true}, &openAIRecordUsageUserRepoStub{}
			billing := newOpenAIRecordUsageServiceForTest(logs, users, &openAIRecordUsageSubRepoStub{}, nil)
			p1, p2, gid := .1, .8, int64(7)
			err = billing.RecordUsage(context.Background(), &OpenAIRecordUsageInput{Result: result, Account: account,
				APIKey: &APIKey{ID: 1, GroupID: &gid, Group: &Group{ID: gid, Platform: PlatformImage, RateMultiplier: 1, ImagePrice1K: &p1, ImagePrice2K: &p2}}, User: &User{ID: 2}})
			require.NoError(t, err)
			require.InDelta(t, .1, logs.lastLog.ActualCost, 1e-9)
			require.Equal(t, "1K", *logs.lastLog.ImageSize)
			require.Equal(t, size, *logs.lastLog.ImageInputSize)
		})
	}
}

func TestImagePlatformUsageActualCount(t *testing.T) {
	for _, count := range []int{0, 2} {
		logs := &openAIRecordUsageLogRepoStub{inserted: true}
		users := &openAIRecordUsageUserRepoStub{}
		svc := newOpenAIRecordUsageServiceForTest(logs, users, &openAIRecordUsageSubRepoStub{}, nil)
		price, gid := 0.17, int64(7)
		err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
			Result: &OpenAIForwardResult{RequestID: fmt.Sprintf("image-%d", count), Model: "custom-image", ImageCount: count, ImageSize: "1K", Usage: OpenAIUsage{InputTokens: 999999, OutputTokens: 99999}, Duration: time.Second},
			APIKey: &APIKey{ID: 1, GroupID: &gid, Group: &Group{ID: gid, Platform: PlatformImage, RateMultiplier: 1, ImagePrice1K: &price}},
			User:   &User{ID: 2}, Account: &Account{ID: 3, Platform: PlatformImage, Type: AccountTypeAPIKey},
		})
		require.NoError(t, err)
		require.NotNil(t, logs.lastLog)
		require.InDelta(t, float64(count)*price, logs.lastLog.ActualCost, 1e-9)
		require.Equal(t, string(BillingModeImage), *logs.lastLog.BillingMode)
		if count == 0 {
			require.Zero(t, users.deductCalls)
		}
	}
}

func TestImagePlatformBulkValidatesMergedCredentials(t *testing.T) {
	account := &Account{ID: 1, Platform: PlatformImage, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "secret", "image_protocol": "gemini"}}
	for _, delta := range []map[string]any{{"image_protocol": "invalid"}, {"api_key": ""}, {"base_url": "file:///secret"}} {
		repo := &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{account}}
		svc := &adminServiceImpl{accountRepo: repo}
		_, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{1}, Credentials: delta})
		require.Equal(t, 400, infraerrors.Code(err))
		if _, provided := delta["base_url"]; provided {
			require.Equal(t, "INVALID_UPSTREAM_URL", infraerrors.Reason(err))
		} else {
			require.Equal(t, "INVALID_IMAGE_ACCOUNT", infraerrors.Reason(err))
		}
		require.Zero(t, repo.bulkUpdateCalls)
	}
}

func TestImagePlatformEditsReferenceInputs(t *testing.T) {
	for _, source := range []string{"multipart", "data", "https", "loopback", "private"} {
		t.Run(source, func(t *testing.T) {
			var downloads, generations atomic.Int32
			upstream := imagePlatformHTTPFunc(func(req *http.Request) (*http.Response, error) {
				if req.Method == http.MethodGet {
					downloads.Add(1)
					require.Equal(t, "https://public.example/image.png", req.URL.String())
					require.True(t, HTTPUpstreamPublicHostsOnly(req.Context()))
					return b64BackfillImageResponse(200, "image/png", b64BackfillPNGBytes), nil
				}
				generations.Add(1)
				payload, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				require.Equal(t, base64.StdEncoding.EncodeToString(b64BackfillPNGBytes), gjson.GetBytes(payload, "contents.0.parts.1.inlineData.data").String())
				return b64BackfillImageResponse(200, "application/json", []byte(`{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"aW1hZ2U="}}]}}]}`)), nil
			})
			svc := newOpenAIImagesTestService(upstream)
			url := "data:image/png;base64," + base64.StdEncoding.EncodeToString(b64BackfillPNGBytes)
			switch source {
			case "https":
				url = "https://public.example/image.png"
			case "loopback":
				url = "https://127.0.0.1/private.png"
			case "private":
				url = "https://10.0.0.1/private.png"
			}
			body := []byte(fmt.Sprintf(`{"model":"gemini-2.5-flash-image","prompt":"edit","images":[{"image_url":%q}]}`, url))
			contentType := "application/json"
			if source == "multipart" {
				var buf bytes.Buffer
				writer := multipart.NewWriter(&buf)
				require.NoError(t, writer.WriteField("model", "gemini-2.5-flash-image"))
				require.NoError(t, writer.WriteField("prompt", "edit"))
				part, err := writer.CreateFormFile("image", "image.png")
				require.NoError(t, err)
				_, err = part.Write(b64BackfillPNGBytes)
				require.NoError(t, err)
				require.NoError(t, writer.Close())
				body, contentType = buf.Bytes(), writer.FormDataContentType()
			}
			c, rec := newOpenAIImagesTestContext(t, body)
			c.Request.URL.Path = "/v1/images/edits"
			c.Request.Header.Set("Content-Type", contentType)
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			result, err := svc.ForwardImages(c.Request.Context(), c, &Account{ID: 3, Platform: PlatformImage, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "secret", "image_protocol": "gemini"}}, body, parsed, "")
			if source == "loopback" || source == "private" {
				require.Error(t, err)
				require.Nil(t, result)
				require.Equal(t, 400, rec.Code)
				require.Zero(t, generations.Load())
				require.Zero(t, downloads.Load())
			} else {
				require.NoError(t, err)
				require.Equal(t, 1, result.ImageCount)
				require.EqualValues(t, 1, generations.Load())
				if source == "https" {
					require.EqualValues(t, 1, downloads.Load())
				} else {
					require.Zero(t, downloads.Load())
				}
			}
		})
	}
}

func TestImagePlatformConcurrentFailureSideEffectsOnce(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(fmt.Sprint(partial), func(t *testing.T) {
			repo := &rateLimitAccountRepoStub{}
			var calls atomic.Int32
			upstream := imagePlatformHTTPFunc(func(*http.Request) (*http.Response, error) {
				call := calls.Add(1)
				if partial && call != 2 {
					return b64BackfillImageResponse(200, "application/json", []byte(`{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"aW1hZ2U="}}]}}]}`)), nil
				}
				return b64BackfillImageResponse(429, "application/json", []byte(`{"error":{"message":"rate limited"}}`)), nil
			})
			svc := newOpenAIImagesTestService(upstream)
			svc.rateLimitService = &RateLimitService{accountRepo: repo, cfg: &config.Config{}}
			body := []byte(`{"model":"gemini-2.5-flash-image","prompt":"draw","n":3}`)
			c, _ := newOpenAIImagesTestContext(t, body)
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			result, err := svc.ForwardImages(c.Request.Context(), c, &Account{ID: 3, Platform: PlatformImage, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "secret", "image_protocol": "gemini"}}, body, parsed, "")
			require.EqualValues(t, 3, calls.Load())
			require.Equal(t, 1, repo.rateLimitedCalls)
			if partial {
				require.NoError(t, err)
				require.Equal(t, 2, result.ImageCount)
			} else {
				var retry *UpstreamFailoverError
				require.ErrorAs(t, err, &retry)
			}
		})
	}
}

func TestImagePlatformSSEErrorsAreSanitized(t *testing.T) {
	upstream := imagePlatformHTTPFunc(func(*http.Request) (*http.Response, error) {
		return b64BackfillImageResponse(200, "text/event-stream", []byte("event: error\ndata: {\"type\":\"error\",\"error\":{\"message\":\"secret https://private.example/stack\",\"type\":\"server_error\"}}\n\n")), nil
	})
	svc := newOpenAIImagesTestService(upstream)
	body := []byte(`{"model":"gpt-image-2","prompt":"draw","stream":true}`)
	c, rec := newOpenAIImagesTestContext(t, body)
	c.Request = c.Request.WithContext(WithResolvedTargetPlatform(c.Request.Context(), PlatformImage))
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	result, err := svc.ForwardImages(c.Request.Context(), c, &Account{ID: 3, Platform: PlatformImage, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "secret"}}, body, parsed, "")
	require.Error(t, err)
	require.Nil(t, result)
	output := rec.Body.String()
	var failover *UpstreamFailoverError
	if errors.As(err, &failover) {
		output += string(failover.ResponseBody)
	}
	require.NotContains(t, output, "private.example")
	require.NotContains(t, output, "secret")
	require.Contains(t, output, "upstream_error")
}
