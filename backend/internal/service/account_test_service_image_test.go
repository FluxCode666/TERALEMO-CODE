//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type imageConnectionHTTP struct {
	HTTPUpstream
	client *http.Client
	calls  atomic.Int32
	proxy  string
	id     int64
	limit  int
}

func (u *imageConnectionHTTP) Do(req *http.Request, proxy string, id int64, limit int) (*http.Response, error) {
	u.calls.Add(1)
	u.proxy, u.id, u.limit = proxy, id, limit
	return u.client.Do(req)
}

func (u *imageConnectionHTTP) DoWithTLS(req *http.Request, proxy string, id int64, limit int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, id, limit)
}

type imageConnectionUsage struct {
	UsageLogRepository
	writes int
}

func (u *imageConnectionUsage) Create(context.Context, *UsageLog) (bool, error) {
	u.writes++
	return true, nil
}

func imageConnectionFixture(t *testing.T, protocol, modelID string, serve http.HandlerFunc) (*AccountTestService, *Account, *imageConnectionHTTP, *rateLimitClearRepoStub, *imageConnectionUsage) {
	t.Helper()
	server := httptest.NewServer(serve)
	t.Cleanup(server.Close)
	account := &Account{ID: 91, Platform: PlatformImage, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 3,
		Credentials: map[string]any{"api_key": "image-connection-secret", "base_url": server.URL, "image_protocol": protocol}}
	if modelID != "" {
		account.Credentials["model_mapping"] = map[string]any{"public-image": modelID}
	}
	repo := &rateLimitClearRepoStub{getByIDAccount: account}
	upstream := &imageConnectionHTTP{client: server.Client()}
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	usage := &imageConnectionUsage{}
	svc := NewAccountTestService(repo, nil, nil, nil, nil, upstream, cfg, nil)
	// Account tests must not use the application's billing/state-enabled gateway.
	svc.SetOpenAIGatewayService(&OpenAIGatewayService{cfg: cfg, httpUpstream: upstream, usageLogRepo: usage,
		rateLimitService: NewRateLimitService(repo, nil, cfg, nil, nil)})
	return svc, account, upstream, repo, usage
}

func imageConnectionEvents(t *testing.T, body string) []TestEvent {
	t.Helper()
	events := make([]TestEvent, 0)
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "data:") {
			var event TestEvent
			require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &event))
			events = append(events, event)
		}
	}
	return events
}

func TestImageAccountConnectionProtocols(t *testing.T) {
	for _, tc := range []struct{ protocol, model, path, response string }{
		{"openai", "gpt-image-2", "/v1/images/generations", `{"data":[{"b64_json":"aW1hZ2U=","revised_prompt":"a cat"}]}`},
		{"gemini", "gemini-2.5-flash-image", "/v1beta/models/gemini-2.5-flash-image:generateContent", `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"aW1hZ2U="}}]}}]}`},
		{"gemini", "imagen-4.0-generate-001", "/v1beta/models/imagen-4.0-generate-001:predict", `{"predictions":[{"bytesBase64Encoded":"aW1hZ2U=","mimeType":"image/png"}]}`},
		{"grok", "grok-imagine-image", "/v1/images/generations", `{"data":[{"b64_json":"aW1hZ2U="}]}`},
	} {
		t.Run(tc.model, func(t *testing.T) {
			var path, auth, googleKey string
			var body []byte
			svc, account, upstream, repo, usage := imageConnectionFixture(t, tc.protocol, tc.model, func(w http.ResponseWriter, r *http.Request) {
				path, auth, googleKey = r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("x-goog-api-key")
				body, _ = io.ReadAll(r.Body)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.response)
			})
			c, recorder := newTestContext()
			require.NoError(t, svc.TestAccountConnection(c, account.ID, "public-image", "draw a cat", ""))
			require.Equal(t, tc.path, path)
			if tc.protocol == "gemini" {
				require.Equal(t, "image-connection-secret", googleKey)
				require.Empty(t, auth)
				if strings.HasPrefix(tc.model, "imagen-") {
					require.EqualValues(t, 1, gjson.GetBytes(body, "parameters.sampleCount").Int())
					require.Equal(t, "1K", gjson.GetBytes(body, "parameters.sampleImageSize").String())
					require.Equal(t, "draw a cat", gjson.GetBytes(body, "instances.0.prompt").String())
				} else {
					require.Equal(t, "1:1", gjson.GetBytes(body, "generationConfig.imageConfig.aspectRatio").String())
					require.Equal(t, "draw a cat", gjson.GetBytes(body, "contents.0.parts.0.text").String())
				}
			} else {
				require.Equal(t, "Bearer image-connection-secret", auth)
				require.Empty(t, googleKey)
				require.Equal(t, tc.model, gjson.GetBytes(body, "model").String())
				require.EqualValues(t, 1, gjson.GetBytes(body, "n").Int())
				require.Equal(t, "b64_json", gjson.GetBytes(body, "response_format").String())
				if tc.protocol == "openai" {
					require.Equal(t, "1024x1024", gjson.GetBytes(body, "size").String())
				} else {
					require.Equal(t, "1k", gjson.GetBytes(body, "resolution").String())
				}
			}
			events := imageConnectionEvents(t, recorder.Body.String())
			var types []string
			for _, event := range events {
				types = append(types, event.Type)
				if event.Type == "image" {
					require.Equal(t, "data:image/png;base64,aW1hZ2U=", event.ImageURL)
					require.Equal(t, "image/png", event.MimeType)
				}
			}
			require.Equal(t, []string{"test_start", "status", "content"}, types[:3])
			require.Equal(t, []string{"image", "test_complete"}, types[len(types)-2:])
			require.True(t, events[len(events)-1].Success)
			require.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
			require.EqualValues(t, 1, upstream.calls.Load())
			require.Equal(t, account.ID, upstream.id)
			require.Equal(t, account.Concurrency, upstream.limit)
			require.Zero(t, usage.writes)
			require.Zero(t, repo.clearErrorCalls)
			require.Equal(t, StatusActive, account.Status)
			require.True(t, account.Schedulable)
		})
	}
}

func TestImageAccountConnectionDefaultsAndEdits(t *testing.T) {
	for _, protocol := range []string{"openai", "gemini", "grok"} {
		for _, edit := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/edit=%t", protocol, edit), func(t *testing.T) {
				var path string
				var body []byte
				svc, account, upstream, _, _ := imageConnectionFixture(t, protocol, "", func(w http.ResponseWriter, r *http.Request) {
					path = r.URL.Path
					body, _ = io.ReadAll(r.Body)
					if protocol == "gemini" {
						_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"aW1hZ2U="}}]}}]}`)
					} else {
						_, _ = io.WriteString(w, `{"data":[{"b64_json":"aW1hZ2U="}]}`)
					}
				})
				proxyID := int64(5)
				account.ProxyID, account.Proxy = &proxyID, &Proxy{Protocol: "http", Host: "proxy.example", Port: 8080}
				opts := AccountTestOptions{}
				if edit {
					opts.ImageDataURL = minimalAccountTestPNGDataURL(8, 8)
				}
				c, rec := newTestContext()
				require.NoError(t, svc.TestAccountConnection(c, account.ID, "", "", "", opts))
				events := imageConnectionEvents(t, rec.Body.String())
				require.Equal(t, DefaultImageModelIDs(protocol)[0], events[0].Model)
				require.Equal(t, account.Proxy.URL(), upstream.proxy)
				if protocol == "gemini" {
					require.Contains(t, path, ":generateContent")
					require.Equal(t, defaultOpenAIImageTestPrompt, gjson.GetBytes(body, "contents.0.parts.0.text").String())
					require.Equal(t, edit, gjson.GetBytes(body, "contents.0.parts.1.inlineData").Exists())
				} else {
					require.Equal(t, defaultOpenAIImageTestPrompt, gjson.GetBytes(body, "prompt").String())
					if edit {
						require.Equal(t, "/v1/images/edits", path)
						field := "images.0.image_url"
						if protocol == "grok" {
							field = "image.url"
						}
						require.Equal(t, opts.ImageDataURL, gjson.GetBytes(body, field).String())
					} else {
						require.Equal(t, "/v1/images/generations", path)
					}
				}
			})
		}
	}
}

func TestImageAccountConnectionLocalRejections(t *testing.T) {
	for _, tc := range []struct{ name, protocol, model, requested, image, want string }{
		{"unavailable", "openai", "gpt-image-2", "other-image", "", "not available"},
		{"imagen edit", "gemini", "imagen-4.0-generate-001", "public-image", minimalAccountTestPNGDataURL(8, 8), "Imagen models do not support"},
		{"invalid image", "grok", "grok-imagine-image", "public-image", "http://private.example/image", "valid image data URL"},
		{"invalid protocol", "bad", "custom", "public-image", "", "image_protocol"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, account, upstream, _, usage := imageConnectionFixture(t, tc.protocol, tc.model, func(http.ResponseWriter, *http.Request) {})
			c, rec := newTestContext()
			err := svc.TestAccountConnection(c, account.ID, tc.requested, "", "", AccountTestOptions{ImageDataURL: tc.image})
			require.ErrorContains(t, err, tc.want)
			require.Contains(t, rec.Body.String(), `"type":"error"`)
			require.NotContains(t, rec.Body.String(), `"success":true`)
			require.Zero(t, upstream.calls.Load())
			require.Zero(t, usage.writes)
		})
	}
}

func TestImageAccountConnectionErrorsAreSafeAndDoNotChangeState(t *testing.T) {
	for _, protocol := range []string{"openai", "gemini", "grok"} {
		for _, key := range []string{"image-connection-secret", "r42"} {
			t.Run(protocol+"/"+key, func(t *testing.T) {
				var message string
				svc, account, upstream, repo, usage := imageConnectionFixture(t, protocol, "", func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(401)
					_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": message}})
				})
				account.Credentials["api_key"] = key
				message = key + " " + account.ImageBaseURL() + " /srv/private/stack"
				core, logs := observer.New(zap.WarnLevel)
				c, rec := newTestContext()
				c.Request = c.Request.WithContext(logger.IntoContext(c.Request.Context(), zap.New(core)))
				err := svc.TestAccountConnection(c, account.ID, "", "", "")
				require.Error(t, err)
				for _, text := range []string{err.Error(), rec.Body.String(), fmt.Sprint(logs.All())} {
					for _, secret := range []string{key, account.ImageBaseURL(), "/srv/private/stack"} {
						require.NotContains(t, text, secret)
					}
				}
				require.Len(t, logs.FilterMessage("account_test.failed").All(), 1)
				require.EqualValues(t, 1, upstream.calls.Load())
				require.Zero(t, usage.writes)
				require.Zero(t, repo.clearErrorCalls)
				require.Equal(t, StatusActive, account.Status)
				require.True(t, account.Schedulable)
			})
		}
	}
}

func TestImageAccountConnectionRejectsEmptyResultAndCancellation(t *testing.T) {
	svc, account, upstream, _, _ := imageConnectionFixture(t, "openai", "", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":[]}`)
	})
	c, rec := newTestContext()
	require.Error(t, svc.TestAccountConnection(c, account.ID, "", "", ""))
	require.NotContains(t, rec.Body.String(), `"success":true`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c, _ = newTestContext()
	c.Request = c.Request.WithContext(ctx)
	require.ErrorContains(t, svc.TestAccountConnection(c, account.ID, "", "", ""), "canceled")
	require.EqualValues(t, 1, upstream.calls.Load(), "canceled tests must not submit another billable request")
}

func TestImageAccountConnectionCancelsUpstreamBody(t *testing.T) {
	svc, account, upstream, _, _ := imageConnectionFixture(t, "openai", "", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-r.Context().Done()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	c, rec := newTestContext()
	c.Request = c.Request.WithContext(ctx)
	started := time.Now()
	require.Error(t, svc.TestAccountConnection(c, account.ID, "", "", ""))
	require.Less(t, time.Since(started), 2*time.Second)
	require.NotContains(t, rec.Body.String(), `"success":true`)
	require.EqualValues(t, 1, upstream.calls.Load())
}

func TestImageAccountConnectionWildcardDefaultAndURL(t *testing.T) {
	svc, account, upstream, _, _ := imageConnectionFixture(t, "openai", "", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"url":"https://cdn.example/generated.png"}]}`)
	})
	account.Credentials["model_mapping"] = map[string]any{"gpt-image-*": "gpt-image-2"}
	c, rec := newTestContext()
	require.NoError(t, svc.TestAccountConnection(c, account.ID, "", "", ""))
	events := imageConnectionEvents(t, rec.Body.String())
	require.Equal(t, "gpt-image-2", events[0].Model)
	require.Equal(t, "https://cdn.example/generated.png", events[len(events)-2].ImageURL)
	require.EqualValues(t, 1, upstream.calls.Load())
}

type imageConnectionPlanRepo struct {
	ScheduledTestPlanRepository
	updated int
}

func (r *imageConnectionPlanRepo) UpdateAfterRun(context.Context, int64, time.Time, time.Time) error {
	r.updated++
	return nil
}

type imageConnectionResultRepo struct {
	ScheduledTestResultRepository
	results []*ScheduledTestResult
}

func (r *imageConnectionResultRepo) Create(_ context.Context, result *ScheduledTestResult) (*ScheduledTestResult, error) {
	r.results = append(r.results, result)
	return result, nil
}

func (r *imageConnectionResultRepo) PruneOldResults(context.Context, int64, int) error { return nil }

func TestImageAccountConnectionScheduledRunner(t *testing.T) {
	for _, status := range []int{200, 401} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			svc, account, upstream, accounts, usage := imageConnectionFixture(t, "openai", "", func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"data":[{"b64_json":"aW1hZ2U="}]}`)
			})
			account.Status = StatusError
			reset := time.Now().Add(time.Hour)
			account.RateLimitResetAt = &reset
			plans, results := &imageConnectionPlanRepo{}, &imageConnectionResultRepo{}
			runner := NewScheduledTestRunnerService(plans, NewScheduledTestService(plans, results), svc, NewRateLimitService(accounts, nil, svc.cfg, nil, nil), svc.cfg)
			runner.runOnePlan(context.Background(), &ScheduledTestPlan{ID: 8, AccountID: account.ID, CronExpression: "*/5 * * * *", AutoRecover: true, MaxResults: 5})
			require.Len(t, results.results, 1)
			require.Equal(t, 1, plans.updated)
			require.EqualValues(t, 1, upstream.calls.Load())
			require.Zero(t, usage.writes)
			if status == 200 {
				require.Equal(t, "success", results.results[0].Status)
				require.Equal(t, 1, accounts.clearErrorCalls)
				require.Equal(t, 1, accounts.clearRateLimitCalls)
			} else {
				require.Equal(t, "failed", results.results[0].Status)
				require.Zero(t, accounts.clearErrorCalls)
				require.Zero(t, accounts.clearRateLimitCalls)
			}
			require.NotContains(t, results.results[0].ResponseText, "base64", "scheduled results must not persist generated image bytes")
		})
	}
}

func TestImageAccountConnectionQuestionAndBatchRemainUnsupported(t *testing.T) {
	svc, account, upstream, _, _ := imageConnectionFixture(t, "gemini", "", func(http.ResponseWriter, *http.Request) {})
	question := &model.AccountTestQuestion{ID: 2, Enabled: true, Content: "text question"}
	c, rec := newTestContext()
	require.ErrorContains(t, svc.TestAccountConnection(c, account.ID, "", "", "", AccountTestOptions{Question: question}), "Test questions are not supported")
	require.Contains(t, rec.Body.String(), `"type":"error"`)
	batch := NewAccountBatchTestService(svc.accountRepo, svc, NewAccountTestQuestionService(&batchTestQuestionRepo{questions: map[int64]*model.AccountTestQuestion{2: question}}), nil)
	plan, err := batch.Prepare(context.Background(), AccountBatchTestInput{AccountIDs: []int64{account.ID}, QuestionIDs: []int64{2}})
	require.NoError(t, err)
	var item AccountBatchTestItemResult
	err = batch.Run(context.Background(), plan, func(event AccountBatchTestEvent) error {
		if result, ok := event.Data.(AccountBatchTestItemResult); ok {
			item = result
		}
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, "error", item.Status)
	require.Contains(t, item.Error, "Test questions are not supported")
	scheduled, plans, accounts, _ := newScheduledQuestionFixture()
	defer scheduled.Stop()
	account.ID = 2
	accounts.accounts[2] = account
	scheduled.batch = batch
	questionPlan := scheduledQuestionValidPlan(1)
	questionPlan.AccountIDs, questionPlan.QuestionIDs = []int64{2}, []int64{2}
	plans.plans[1] = questionPlan
	run, err := scheduled.runPlan(context.Background(), questionPlan)
	require.NoError(t, err)
	require.Len(t, run.Summary, 1)
	require.Contains(t, run.Summary[0].Error, "Test questions are not supported")
	require.Zero(t, upstream.calls.Load())
}
