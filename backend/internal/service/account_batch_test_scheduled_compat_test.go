//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

func scheduledCompatResponsesRequest(t *testing.T) string {
	t.Helper()
	instructions, err := json.Marshal(openai.DefaultInstructions)
	require.NoError(t, err)
	return `{"input":[{"content":[{"text":"question 1","type":"input_text"}],"role":"user"}],"instructions":` + string(instructions) + `,"model":"manual-model","stream":true}`
}

func scheduledCompatOpenAIAccount() *Account {
	return &Account{ID: 1, Name: "manual-account", Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: false,
		Credentials: map[string]any{"api_key": "test-key", "base_url": "https://batch-test.example"}}
}

func scheduledCompatNormalizeLatency(event AccountBatchTestEvent) AccountBatchTestEvent {
	if item, ok := event.Data.(AccountBatchTestItemResult); ok {
		item.LatencyMs = 0
		event.Data = item
	}
	return event
}

func TestAccountBatchManualQuotaAndLimitsPreserveRequestAndEventJSON(t *testing.T) {
	for _, limitation := range []string{"none", "total quota", "daily quota", "weekly quota", "account rate limit", "model rate limit"} {
		t.Run(limitation, func(t *testing.T) {
			account := scheduledCompatOpenAIAccount()
			now := time.Now().UTC()
			switch limitation {
			case "total quota":
				account.Extra = map[string]any{"quota_limit": 1, "quota_used": 1}
			case "daily quota":
				account.Extra = map[string]any{"quota_daily_limit": 1, "quota_daily_used": 1, "quota_daily_start": now.Format(time.RFC3339)}
			case "weekly quota":
				account.Extra = map[string]any{"quota_weekly_limit": 1, "quota_weekly_used": 1, "quota_weekly_start": now.Format(time.RFC3339)}
			case "account rate limit":
				account.RateLimitResetAt = scheduledQuestionSkipTimePointer(now.Add(time.Hour))
			case "model rate limit":
				setAccountModelRateLimitSnapshot(account, "manual-model", now.Add(time.Hour), "", now)
			}
			accounts := &batchTestAccountRepo{accounts: map[int64]*Account{1: account}}
			questions := &batchTestQuestionRepo{questions: map[int64]*model.AccountTestQuestion{1: batchEnabledQuestion(1, "^ok$")}}
			requestCalls := 0
			upstream := &batchTestHTTPUpstream{call: func(request *http.Request, accountID int64) (*http.Response, error) {
				requestCalls++
				body, err := io.ReadAll(request.Body)
				if err != nil {
					return nil, err
				}
				require.Equal(t, int64(1), accountID)
				require.Equal(t, http.MethodPost, request.Method)
				require.Equal(t, "https://batch-test.example/v1/responses", request.URL.String())
				require.Equal(t, "Bearer test-key", request.Header.Get("Authorization"))
				require.Equal(t, scheduledCompatResponsesRequest(t), string(body), "manual upstream request stays byte-for-byte identical")
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(
					"data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\ndata: {\"type\":\"response.completed\"}\n\n"))}, nil
			}}
			svc := newBatchTestService(accounts, questions, upstream, false)
			plan, err := svc.Prepare(context.Background(), AccountBatchTestInput{AccountIDs: []int64{1}, QuestionIDs: []int64{1}, ModelID: "manual-model", Concurrency: batchInt(1)})
			require.NoError(t, err)
			var events []string
			err = svc.Run(context.Background(), plan, func(event AccountBatchTestEvent) error {
				if item, ok := event.Data.(AccountBatchTestItemResult); ok {
					require.Zero(t, item.StatusCode, "manual input must not opt into scheduled status capture")
				}
				encoded, err := json.Marshal(scheduledCompatNormalizeLatency(event))
				if err == nil {
					events = append(events, string(encoded))
				}
				return err
			})
			require.NoError(t, err)
			require.Equal(t, 1, requestCalls, "manual tests still exercise exhausted and manually paused accounts")
			require.Equal(t, []string{
				`{"type":"batch_start","data":{"total":1,"account_count":1,"question_count":1,"concurrency":1}}`,
				`{"type":"item_result","data":{"index":0,"account_id":1,"account_name":"manual-account","platform":"openai","question_id":1,"question_name":"question-1","model":"manual-model","status":"passed","judged":true,"answer_patterns":["^ok$"],"matched_patterns":["^ok$"],"answer":"ok","error":"","latency_ms":0}}`,
				`{"type":"batch_complete","data":{"total":1,"passed":1,"failed":0,"unjudged":0,"error":0,"cancelled":false}}`,
			}, events, "all event JSON bytes remain fixed; only elapsed latency is normalized")
		})
	}
}

func TestAccountBatchScheduledHTTPStatusCaptureIsPrivateAndAuthoritative(t *testing.T) {
	for _, code := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			runBatch := func(capture bool) ([]string, AccountBatchTestItemResult, string) {
				t.Helper()
				account := scheduledCompatOpenAIAccount()
				accounts := &batchTestAccountRepo{accounts: map[int64]*Account{1: account}}
				questions := &batchTestQuestionRepo{questions: map[int64]*model.AccountTestQuestion{1: batchEnabledQuestion(1, "ok")}}
				var requestBody string
				upstream := &batchTestHTTPUpstream{call: func(request *http.Request, _ int64) (*http.Response, error) {
					body, err := io.ReadAll(request.Body)
					if err != nil {
						return nil, err
					}
					requestBody = string(body)
					return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":{"message":"body includes API returned 429: not a status"}}`))}, nil
				}}
				svc := newBatchTestService(accounts, questions, upstream, false)
				plan, err := svc.Prepare(context.Background(), AccountBatchTestInput{
					AccountIDs: []int64{1}, QuestionIDs: []int64{1}, ModelID: "manual-model", Concurrency: batchInt(1), captureStatus: capture,
				})
				require.NoError(t, err)
				var events []string
				var result AccountBatchTestItemResult
				err = svc.Run(context.Background(), plan, func(event AccountBatchTestEvent) error {
					if item, ok := event.Data.(AccountBatchTestItemResult); ok {
						result = item
					}
					encoded, err := json.Marshal(scheduledCompatNormalizeLatency(event))
					if err == nil {
						events = append(events, string(encoded))
					}
					return err
				})
				require.NoError(t, err)
				require.Equal(t, "error", result.Status)
				return events, result, requestBody
			}
			manualJSON, manual, manualRequest := runBatch(false)
			scheduledJSON, scheduled, scheduledRequest := runBatch(true)
			require.Zero(t, manual.StatusCode)
			require.Equal(t, code, scheduled.StatusCode)
			require.Equal(t, manualRequest, scheduledRequest, "status capture must not change any upstream request bytes")
			require.Equal(t, scheduledCompatResponsesRequest(t), manualRequest)
			require.Equal(t, manualJSON, scheduledJSON, "even status capture must not add fields to SSE event JSON")
			require.NotContains(t, strings.Join(scheduledJSON, ""), "status_code")
			require.Equal(t, code == http.StatusTooManyRequests, scheduledQuestionAllErrorsRateLimited(1, 1, []AccountBatchTestItemResult{scheduled}))
		})
	}
}

func TestAccountBatchScheduledStatusCaptureCoversCNAndGrokTextPaths(t *testing.T) {
	for _, path := range []struct {
		name     string
		platform string
		protocol string
		model    string
		urls     []string
	}{
		{name: "native CN Anthropic", platform: PlatformZhipu, protocol: APIProtocolAnthropic, model: "glm-4.7", urls: []string{"http://anthropic.example/v1/messages"}},
		{name: "adaptive Anthropic", platform: PlatformDeepseek, protocol: APIProtocolAdaptive, model: "deepseek-chat", urls: []string{"http://chat.example/v1/chat/completions", "http://anthropic.example/v1/messages"}},
		{name: "adaptive Responses", platform: PlatformDeepseek, protocol: APIProtocolAdaptive, model: "deepseek-chat", urls: []string{"http://chat.example/v1/chat/completions", "http://anthropic.example/v1/messages", "http://responses.example/responses"}},
		{name: "Grok text", platform: PlatformGrok, model: "grok-4.5", urls: []string{"https://api.x.ai/v1/responses"}},
	} {
		for _, code := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
			t.Run(path.name+"/"+fmt.Sprint(code), func(t *testing.T) {
				runBatch := func(capture bool) ([]string, AccountBatchTestItemResult) {
					t.Helper()
					account := adaptiveCNAccountTestAccount(1, path.platform)
					account.Credentials["api_protocol"] = path.protocol
					account.Credentials["base_url"] = "http://anthropic.example"
					if path.platform == PlatformGrok {
						account.Credentials["base_url"] = "https://api.x.ai"
					}
					repo := &openAIAccountTestRepo{mockAccountRepoForGemini: mockAccountRepoForGemini{accountsByID: map[int64]*Account{1: account}}}
					responses := make([]*http.Response, 0, len(path.urls))
					if len(path.urls) >= 2 {
						responses = append(responses, adaptiveCNChatTestResponse())
					}
					if len(path.urls) >= 3 {
						responses = append(responses, adaptiveCNAnthropicTestResponse())
					}
					responses = append(responses, newJSONResponse(code, `{"error":{"message":"body mentions API returned 429: not a status"}}`))
					upstream := &httpUpstreamRecorder{responses: responses}
					questions := &batchTestQuestionRepo{questions: map[int64]*model.AccountTestQuestion{1: batchEnabledQuestion(1, "ok")}}
					accountTest := NewAccountTestService(repo, nil, nil, nil, nil, upstream, rawChatCompletionsTestConfig(), nil)
					svc := NewAccountBatchTestService(repo, accountTest, NewAccountTestQuestionService(questions), nil)
					plan, err := svc.Prepare(context.Background(), AccountBatchTestInput{
						AccountIDs: []int64{1}, QuestionIDs: []int64{1}, ModelID: path.model, Concurrency: batchInt(1), captureStatus: capture,
					})
					require.NoError(t, err)
					var events []string
					var result AccountBatchTestItemResult
					err = svc.Run(context.Background(), plan, func(event AccountBatchTestEvent) error {
						if item, ok := event.Data.(AccountBatchTestItemResult); ok {
							result = item
						}
						encoded, err := json.Marshal(scheduledCompatNormalizeLatency(event))
						if err == nil {
							events = append(events, string(encoded))
						}
						return err
					})
					require.NoError(t, err)
					require.Len(t, upstream.requests, len(path.urls), "the failing endpoint must actually be reached")
					for index, request := range upstream.requests {
						require.Equal(t, path.urls[index], request.URL.String())
					}
					require.Equal(t, "error", result.Status)
					require.Contains(t, result.Error, fmt.Sprintf("returned %d", code))
					require.False(t, strings.HasPrefix(result.Error, "API returned 429"), "these paths need structured capture rather than the legacy fallback")
					return events, result
				}
				manualJSON, manual := runBatch(false)
				scheduledJSON, scheduled := runBatch(true)
				require.Zero(t, manual.StatusCode)
				require.Equal(t, code, scheduled.StatusCode)
				require.Equal(t, manualJSON, scheduledJSON, "private status capture preserves every serialized event byte")
				require.NotContains(t, strings.Join(scheduledJSON, ""), "status_code")
				require.Equal(t, code == http.StatusTooManyRequests, scheduledQuestionAllErrorsRateLimited(1, 1, []AccountBatchTestItemResult{scheduled}))
			})
		}
	}
}

func TestAntigravityTestConnectionErrorCaptureKeepsCallbackBehavior(t *testing.T) {
	for _, capture := range []bool{false, true} {
		for _, code := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
			t.Run(fmt.Sprintf("capture=%t/status=%d", capture, code), func(t *testing.T) {
				ctx := context.Background()
				status := 0
				if capture {
					ctx = context.WithValue(ctx, accountTestStatusContextKey{}, &status)
				}
				account := &Account{ID: 1, Platform: PlatformAntigravity, Type: AccountTypeOAuth, Schedulable: false}
				before := *account
				result := testConnectionHandleError(ctx, "[scheduled-status-test]", account, code, make(http.Header), []byte(`{"error":"upstream unavailable"}`), "gemini-2.5-flash", 0, "", false)
				require.Nil(t, result, "status capture must not change retry callback decisions")
				require.Equal(t, before, *account)
				if capture {
					require.Equal(t, code, status)
				} else {
					require.Zero(t, status)
				}
			})
		}
	}
}
