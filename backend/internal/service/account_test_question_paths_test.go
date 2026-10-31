//go:build unit

package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const accountTestQuestionPathPrompt = "Which model are you?\nReturn \"MODEL\" and your version."

type accountTestQuestionPathRequest struct {
	path   string
	header http.Header
	body   []byte
	err    error
}

// Preserve provider request construction while routing even fixed official URLs
// to a real local HTTP server, so assertions cover serialized upstream bodies.
type accountTestQuestionPathUpstream struct {
	client *http.Client
	target *url.URL
}

func (u *accountTestQuestionPathUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	forwarded := req.Clone(req.Context())
	forwarded.URL.Scheme = u.target.Scheme
	forwarded.URL.Host = u.target.Host
	forwarded.Host = ""
	return u.client.Do(forwarded)
}

func (u *accountTestQuestionPathUpstream) DoWithTLS(req *http.Request, proxy string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, accountID, concurrency)
}

func newAccountTestQuestionPathUpstream(t *testing.T) (*accountTestQuestionPathUpstream, <-chan accountTestQuestionPathRequest) {
	t.Helper()
	requests := make(chan accountTestQuestionPathRequest, 16)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		requests <- accountTestQuestionPathRequest{path: r.URL.Path, header: r.Header.Clone(), body: body, err: err}
		if err != nil {
			http.Error(w, "failed to read request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		var response string
		switch {
		case strings.HasSuffix(r.URL.Path, "/invoke"):
			w.Header().Set("Content-Type", "application/json")
			response = `{"content":[{"type":"text","text":"bedrock answer"}]}`
		case strings.Contains(r.URL.Path, "/chat/completions"):
			response = "data: {\"choices\":[{\"delta\":{\"content\":\"chat answer\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
		case strings.HasSuffix(r.URL.Path, "/responses"):
			response = "data: {\"type\":\"response.output_text.delta\",\"delta\":\"responses answer\"}\n\ndata: {\"type\":\"response.completed\"}\n\n"
		case strings.Contains(r.URL.Path, "streamGenerateContent"):
			response = "data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"gemini answer\"}]},\"finishReason\":\"STOP\"}]}}\n\n"
		default:
			response = "data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"claude answer\"}}\n\ndata: {\"type\":\"message_stop\"}\n\n"
		}
		_, _ = io.WriteString(w, response)
	}))
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
	require.NoError(t, err)
	return &accountTestQuestionPathUpstream{client: server.Client(), target: target}, requests
}

func newAccountTestQuestionPathAccount(platform, accountType string, credentials map[string]any) *Account {
	merged := map[string]any{
		"api_key":       "question-api-key",
		"access_token":  "question-access-token",
		"refresh_token": "question-refresh-token",
		"expires_at":    time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}
	for key, value := range credentials {
		merged[key] = value
	}
	return &Account{
		ID: 731, Name: "question-path", Platform: platform, Type: accountType,
		Status: StatusActive, Schedulable: true, Concurrency: 1, Credentials: merged,
	}
}

func newAccountTestQuestionPathService(account *Account, upstream HTTPUpstream) *AccountTestService {
	repo := &openAIAccountTestRepo{mockAccountRepoForGemini: mockAccountRepoForGemini{
		accountsByID: map[int64]*Account{account.ID: account},
	}}
	cache := &vertexModelsTokenCacheStub{token: "question-service-account-token"}
	return &AccountTestService{
		accountRepo:         repo,
		httpUpstream:        upstream,
		cfg:                 rawChatCompletionsTestConfig(),
		claudeTokenProvider: NewClaudeTokenProvider(repo, cache, nil),
		geminiTokenProvider: NewGeminiTokenProvider(repo, cache, nil),
		grokTokenProvider:   NewGrokTokenProvider(repo, nil),
		antigravityGatewayService: &AntigravityGatewayService{
			accountRepo: repo, httpUpstream: upstream,
			tokenProvider: NewAntigravityTokenProvider(repo, nil, nil),
		},
	}
}

type accountTestQuestionPathExpectation struct {
	path   string
	format string
}

func assertAccountTestQuestionPathBody(t *testing.T, request accountTestQuestionPathRequest, expected accountTestQuestionPathExpectation) {
	t.Helper()
	require.NoError(t, request.err)
	require.Contains(t, request.path, expected.path)
	require.True(t, gjson.ValidBytes(request.body), "upstream request must be JSON: %s", request.body)
	require.Equal(t, "application/json", request.header.Get("Content-Type"))
	require.NotContains(t, string(request.body), "ignored-request-prompt")
	require.NotContains(t, string(request.body), "ignored-image")
	require.NotContains(t, string(request.body), "ignored-audio")

	messagePath, textPath := "messages", "content.0.text"
	switch expected.format {
	case "responses":
		messagePath = "input"
	case "chat":
		textPath = "content"
	case "gemini":
		messagePath, textPath = "contents", "parts.0.text"
	case "wrapped-gemini", "antigravity":
		messagePath, textPath = "request.contents", "parts.0.text"
	case "grok":
		require.Equal(t, accountTestQuestionPathPrompt, gjson.GetBytes(request.body, "input").String())
		require.True(t, gjson.GetBytes(request.body, "stream").Bool())
		return
	}
	messages := gjson.GetBytes(request.body, messagePath).Array()
	require.Len(t, messages, 1, "question must be the only user message: %s", request.body)
	require.Equal(t, "user", messages[0].Get("role").String())
	require.Equal(t, accountTestQuestionPathPrompt, messages[0].Get(textPath).String())
	if expected.format != "chat" {
		blocksPath := "content"
		if expected.format == "gemini" || expected.format == "wrapped-gemini" || expected.format == "antigravity" {
			blocksPath = "parts"
		}
		require.Len(t, messages[0].Get(blocksPath).Array(), 1)
	}

	switch expected.format {
	case "claude", "vertex-claude":
		require.True(t, gjson.GetBytes(request.body, "stream").Bool())
		require.Equal(t, int64(1024), gjson.GetBytes(request.body, "max_tokens").Int())
		require.NotEmpty(t, gjson.GetBytes(request.body, "metadata.user_id").String())
		require.NotEmpty(t, gjson.GetBytes(request.body, "system.0.text").String())
		require.Equal(t, "ephemeral", messages[0].Get("content.0.cache_control.type").String())
		if expected.format == "vertex-claude" {
			require.Equal(t, "vertex-2023-10-16", gjson.GetBytes(request.body, "anthropic_version").String())
		}
	case "bedrock":
		require.Equal(t, "bedrock-2023-05-31", gjson.GetBytes(request.body, "anthropic_version").String())
		require.Equal(t, int64(1024), gjson.GetBytes(request.body, "max_tokens").Int())
		require.False(t, gjson.GetBytes(request.body, "stream").Exists())
	case "antigravity":
		require.Equal(t, int64(4096), gjson.GetBytes(request.body, "request.generationConfig.maxOutputTokens").Int())
	case "responses", "chat":
		require.True(t, gjson.GetBytes(request.body, "stream").Bool())
		require.False(t, gjson.GetBytes(request.body, "input.0.type").String() == "compaction_trigger")
	case "gemini":
		require.NotEmpty(t, gjson.GetBytes(request.body, "systemInstruction.parts.0.text").String())
	}
}

func TestAccountTestQuestionTextPathsSendConfiguredPrompt(t *testing.T) {
	vertexClaude := vertexModelSyncTestAccount(nil)
	vertexClaude.Platform = PlatformAnthropic
	openAIChat := newAccountTestQuestionPathAccount(PlatformOpenAI, AccountTypeAPIKey, nil)
	openAIChat.Extra = map[string]any{openai_compat.ExtraKeyResponsesSupported: false}

	tests := []struct {
		name          string
		account       *Account
		model         string
		mode          string
		upstreamModel string
		expected      []accountTestQuestionPathExpectation
	}{
		{
			name: "claude oauth", account: newAccountTestQuestionPathAccount(PlatformAnthropic, AccountTypeOAuth, nil), model: "claude-sonnet-4-6",
			expected: []accountTestQuestionPathExpectation{{"/v1/messages", "claude"}},
		},
		{
			name: "claude setup token", account: newAccountTestQuestionPathAccount(PlatformAnthropic, AccountTypeSetupToken, nil), model: "claude-sonnet-4-6",
			expected: []accountTestQuestionPathExpectation{{"/v1/messages", "claude"}},
		},
		{
			name: "claude oauth ignores unused media mapping", account: newAccountTestQuestionPathAccount(PlatformAnthropic, AccountTypeOAuth, map[string]any{
				"model_mapping": map[string]any{"claude-sonnet-4-6": "gpt-image-2"},
			}), model: "claude-sonnet-4-6", upstreamModel: "claude-sonnet-4-6",
			expected: []accountTestQuestionPathExpectation{{"/v1/messages", "claude"}},
		},
		{
			name: "claude setup token ignores unused media mapping", account: newAccountTestQuestionPathAccount(PlatformAnthropic, AccountTypeSetupToken, map[string]any{
				"model_mapping": map[string]any{"claude-sonnet-4-6": "gpt-image-2"},
			}), model: "claude-sonnet-4-6", upstreamModel: "claude-sonnet-4-6",
			expected: []accountTestQuestionPathExpectation{{"/v1/messages", "claude"}},
		},
		{
			name: "claude api key", account: newAccountTestQuestionPathAccount(PlatformAnthropic, AccountTypeAPIKey, nil), model: "claude-sonnet-4-6",
			expected: []accountTestQuestionPathExpectation{{"/v1/messages", "claude"}},
		},
		{
			name: "claude vertex service account", account: vertexClaude, model: "claude-sonnet-4-6",
			expected: []accountTestQuestionPathExpectation{{"publishers/anthropic/models/claude-sonnet-4-6:streamRawPredict", "vertex-claude"}},
		},
		{
			name: "bedrock api key", account: newAccountTestQuestionPathAccount(PlatformAnthropic, AccountTypeBedrock, map[string]any{"auth_mode": "apikey"}), model: "claude-sonnet-4-6",
			expected: []accountTestQuestionPathExpectation{{"/invoke", "bedrock"}},
		},
		{
			name: "bedrock sigv4", account: newAccountTestQuestionPathAccount(PlatformAnthropic, AccountTypeBedrock, map[string]any{
				"aws_access_key_id": "question-access-key", "aws_secret_access_key": "question-secret-key", "aws_region": "us-east-1",
			}), model: "claude-sonnet-4-6",
			expected: []accountTestQuestionPathExpectation{{"/invoke", "bedrock"}},
		},
		{
			name: "openai oauth overrides compact", account: newAccountTestQuestionPathAccount(PlatformOpenAI, AccountTypeOAuth, nil), model: "gpt-5.4", mode: AccountTestModeCompact,
			expected: []accountTestQuestionPathExpectation{{"/codex/responses", "responses"}},
		},
		{
			name: "openai api key responses", account: newAccountTestQuestionPathAccount(PlatformOpenAI, AccountTypeAPIKey, nil), model: "gpt-5.4",
			expected: []accountTestQuestionPathExpectation{{"/v1/responses", "responses"}},
		},
		{
			name: "openai question model mapping is not recursive", account: newAccountTestQuestionPathAccount(PlatformOpenAI, AccountTypeAPIKey, map[string]any{
				"model_mapping": map[string]any{"alias": "gpt-5.4", "gpt-5.4": "gpt-image-2"},
			}), model: "alias", upstreamModel: "gpt-5.4",
			expected: []accountTestQuestionPathExpectation{{"/v1/responses", "responses"}},
		},
		{
			name: "openai api key chat completions", account: openAIChat, model: "gpt-5.4",
			expected: []accountTestQuestionPathExpectation{{"/v1/chat/completions", "chat"}},
		},
		{
			name: "gemini api key", account: newAccountTestQuestionPathAccount(PlatformGemini, AccountTypeAPIKey, nil), model: "gemini-2.5-flash",
			expected: []accountTestQuestionPathExpectation{{"/models/gemini-2.5-flash:streamGenerateContent", "gemini"}},
		},
		{
			name: "gemini api key ignores unused wildcard media mapping", account: newAccountTestQuestionPathAccount(PlatformGemini, AccountTypeAPIKey, map[string]any{
				"model_mapping": map[string]any{"gemini-*": "gemini-2.5-flash-image"},
			}), model: "gemini-2.5-flash",
			expected: []accountTestQuestionPathExpectation{{"/models/gemini-2.5-flash:streamGenerateContent", "gemini"}},
		},
		{
			name: "gemini oauth ai studio", account: newAccountTestQuestionPathAccount(PlatformGemini, AccountTypeOAuth, nil), model: "gemini-2.5-flash",
			expected: []accountTestQuestionPathExpectation{{"/models/gemini-2.5-flash:streamGenerateContent", "gemini"}},
		},
		{
			name: "gemini oauth ai studio ignores unused media mapping", account: newAccountTestQuestionPathAccount(PlatformGemini, AccountTypeOAuth, map[string]any{
				"model_mapping": map[string]any{"gemini-2.5-flash": "gemini-2.5-flash-image"},
			}), model: "gemini-2.5-flash",
			expected: []accountTestQuestionPathExpectation{{"/models/gemini-2.5-flash:streamGenerateContent", "gemini"}},
		},
		{
			name: "gemini oauth code assist", account: newAccountTestQuestionPathAccount(PlatformGemini, AccountTypeOAuth, map[string]any{"project_id": "question-project"}), model: "gemini-2.5-flash",
			expected: []accountTestQuestionPathExpectation{{"/v1internal:streamGenerateContent", "wrapped-gemini"}},
		},
		{
			name: "gemini oauth code assist ignores unused media mapping", account: newAccountTestQuestionPathAccount(PlatformGemini, AccountTypeOAuth, map[string]any{
				"project_id": "question-project", "model_mapping": map[string]any{"gemini-2.5-flash": "gemini-2.5-flash-image"},
			}), model: "gemini-2.5-flash", upstreamModel: "gemini-2.5-flash",
			expected: []accountTestQuestionPathExpectation{{"/v1internal:streamGenerateContent", "wrapped-gemini"}},
		},
		{
			name: "gemini vertex service account", account: vertexModelSyncTestAccount(nil), model: "gemini-2.5-flash",
			expected: []accountTestQuestionPathExpectation{{"publishers/google/models/gemini-2.5-flash:streamGenerateContent", "gemini"}},
		},
		{
			name: "gemini vertex api key express", account: newAccountTestQuestionPathAccount(PlatformGemini, AccountTypeServiceAccount, map[string]any{"auth_mode": "apikey"}), model: "gemini-2.5-flash",
			expected: []accountTestQuestionPathExpectation{{"/v1/publishers/google/models/gemini-2.5-flash:streamGenerateContent", "gemini"}},
		},
		{
			name: "gemini vertex api key project", account: newAccountTestQuestionPathAccount(PlatformGemini, AccountTypeServiceAccount, map[string]any{"auth_mode": "apikey", "project_id": "question-project"}), model: "gemini-2.5-flash",
			expected: []accountTestQuestionPathExpectation{{"/projects/question-project/locations/us-central1/publishers/google/models/gemini-2.5-flash:streamGenerateContent", "gemini"}},
		},
		{
			name: "antigravity api key claude", account: newAccountTestQuestionPathAccount(PlatformAntigravity, AccountTypeAPIKey, nil), model: "claude-sonnet-4-6",
			expected: []accountTestQuestionPathExpectation{{"/v1/messages", "claude"}},
		},
		{
			name: "antigravity api key gemini", account: newAccountTestQuestionPathAccount(PlatformAntigravity, AccountTypeAPIKey, nil), model: "gemini-2.5-flash",
			expected: []accountTestQuestionPathExpectation{{"/models/gemini-2.5-flash:streamGenerateContent", "gemini"}},
		},
		{
			name: "antigravity oauth claude builder", account: newAccountTestQuestionPathAccount(PlatformAntigravity, AccountTypeOAuth, map[string]any{
				"project_id": "question-project", "model_mapping": map[string]any{"claude-sonnet-4-6": "claude-sonnet-4-6"},
			}), model: "claude-sonnet-4-6",
			expected: []accountTestQuestionPathExpectation{{"/v1internal:streamGenerateContent", "antigravity"}},
		},
		{
			name: "antigravity oauth gemini builder", account: newAccountTestQuestionPathAccount(PlatformAntigravity, AccountTypeOAuth, map[string]any{
				"project_id": "question-project", "model_mapping": map[string]any{"gemini-3.1-pro-high": "gemini-3.1-pro-high"},
			}), model: "gemini-3.1-pro-high",
			expected: []accountTestQuestionPathExpectation{{"/v1internal:streamGenerateContent", "antigravity"}},
		},
		{
			name: "antigravity upstream claude builder", account: newAccountTestQuestionPathAccount(PlatformAntigravity, AccountTypeUpstream, map[string]any{
				"project_id": "question-project", "model_mapping": map[string]any{"claude-sonnet-4-6": "claude-sonnet-4-6"},
			}), model: "claude-sonnet-4-6",
			expected: []accountTestQuestionPathExpectation{{"/v1internal:streamGenerateContent", "antigravity"}},
		},
		{
			name: "antigravity upstream gemini builder", account: newAccountTestQuestionPathAccount(PlatformAntigravity, AccountTypeUpstream, map[string]any{
				"project_id": "question-project", "model_mapping": map[string]any{"gemini-3.1-pro-high": "gemini-3.1-pro-high"},
			}), model: "gemini-3.1-pro-high",
			expected: []accountTestQuestionPathExpectation{{"/v1internal:streamGenerateContent", "antigravity"}},
		},
		{
			name: "deepseek adaptive all protocols", account: adaptiveCNAccountTestAccount(732, PlatformDeepseek), model: "deepseek-chat",
			expected: []accountTestQuestionPathExpectation{{"/v1/chat/completions", "chat"}, {"/v1/messages", "claude"}, {"/responses", "responses"}},
		},
		{
			name: "kimi adaptive all protocols", account: adaptiveCNAccountTestAccount(733, PlatformKimi), model: "k3-256k",
			expected: []accountTestQuestionPathExpectation{{"/v1/chat/completions", "chat"}, {"/v1/messages", "claude"}, {"/v1/responses", "responses"}},
		},
		{
			name: "zhipu adaptive both protocols", account: adaptiveCNAccountTestAccount(734, PlatformZhipu), model: "glm-4.7",
			expected: []accountTestQuestionPathExpectation{{"/v1/chat/completions", "chat"}, {"/v1/messages", "claude"}},
		},
		{
			name: "cn provider fixed anthropic", account: anthropicProtocolCNAccount(735, PlatformZhipu, nil), model: "glm-4.7",
			expected: []accountTestQuestionPathExpectation{{"/v1/messages", "claude"}},
		},
		{
			name: "opencode model routed responses", account: openCodeGoTestAccount(736), model: "grok-4.6",
			expected: []accountTestQuestionPathExpectation{{"/v1/responses", "responses"}},
		},
		{
			name: "opencode model routed chat", account: openCodeGoTestAccount(737), model: "deepseek-v4-flash",
			expected: []accountTestQuestionPathExpectation{{"/v1/chat/completions", "chat"}},
		},
		{
			name: "opencode model routed anthropic", account: openCodeGoTestAccount(738), model: "minimax-m3",
			expected: []accountTestQuestionPathExpectation{{"/v1/messages", "claude"}},
		},
		{
			name: "command code model routed responses", account: commandCodeTestAccount(739), model: "gpt-5.4",
			expected: []accountTestQuestionPathExpectation{{"/provider/v1/responses", "responses"}},
		},
		{
			name: "command code model routed chat", account: commandCodeTestAccount(740), model: "deepseek/deepseek-v4-flash",
			expected: []accountTestQuestionPathExpectation{{"/provider/v1/chat/completions", "chat"}},
		},
		{
			name: "command code model routed anthropic", account: commandCodeTestAccount(741), model: "claude-sonnet-4-6",
			expected: []accountTestQuestionPathExpectation{{"/provider/v1/messages", "claude"}},
		},
		{
			name: "grok oauth overrides realtime", account: newAccountTestQuestionPathAccount(PlatformGrok, AccountTypeOAuth, nil), model: "grok-4.3", mode: AccountTestModeGrokRealtime,
			expected: []accountTestQuestionPathExpectation{{"/v1/responses", "grok"}},
		},
		{
			name: "grok api key text", account: newAccountTestQuestionPathAccount(PlatformGrok, AccountTypeAPIKey, nil), model: "grok-4.3",
			expected: []accountTestQuestionPathExpectation{{"/v1/responses", "grok"}},
		},
		{
			name: "grok question model mapping is not recursive", account: newAccountTestQuestionPathAccount(PlatformGrok, AccountTypeAPIKey, map[string]any{
				"model_mapping": map[string]any{"alias": "grok-4.3", "grok-4.3": "grok-imagine-image"},
			}), model: "alias", upstreamModel: "grok-4.3",
			expected: []accountTestQuestionPathExpectation{{"/v1/responses", "grok"}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			upstream, requests := newAccountTestQuestionPathUpstream(t)
			svc := newAccountTestQuestionPathService(tc.account, upstream)
			c, recorder := newTestContext()
			question := &model.AccountTestQuestion{
				ID: 39, Name: "Model identity", Content: accountTestQuestionPathPrompt,
				AnswerPatterns: []string{`(?i)^(claude|bedrock|responses|chat|gemini) answer`}, Enabled: true,
			}
			err := svc.TestAccountConnection(c, tc.account.ID, tc.model, "ignored-request-prompt", tc.mode, AccountTestOptions{
				Question: question, ImageDataURL: "ignored-image", AudioDataURL: "ignored-audio",
			})
			require.NoError(t, err, "SSE output: %s", recorder.Body.String())
			require.Len(t, requests, len(tc.expected))
			for _, expected := range tc.expected {
				request := <-requests
				assertAccountTestQuestionPathBody(t, request, expected)
				if tc.upstreamModel != "" {
					require.Equal(t, tc.upstreamModel, gjson.GetBytes(request.body, "model").String(), "model mapping must stop after the first resolution")
				}
			}
			output := recorder.Body.String()
			require.Contains(t, output, `"type":"content"`)
			require.Contains(t, output, "answer")
			require.Equal(t, 1, strings.Count(output, `"type":"question_result"`))
			require.Equal(t, 1, strings.Count(output, `"type":"test_complete"`))
			events := decodeQuestionTestEvents(t, output)
			resultIndex := -1
			for i, event := range events {
				if event.Get("type").String() == "question_result" {
					resultIndex = i
					break
				}
			}
			require.GreaterOrEqual(t, resultIndex, 0)
			require.Less(t, resultIndex+1, len(events))
			result := events[resultIndex]
			require.True(t, result.Get("success").Bool())
			require.True(t, result.Get("data.judged").Bool())
			require.Len(t, result.Get("data.answer_patterns").Array(), 1)
			require.Len(t, result.Get("data.matched_patterns").Array(), 1)
			require.Equal(t, question.AnswerPatterns[0], result.Get("data.matched_patterns.0").String())
			require.Equal(t, "test_complete", events[resultIndex+1].Get("type").String())
			require.True(t, events[resultIndex+1].Get("success").Bool())
			require.NotContains(t, output, "expected_keywords")
			require.NotContains(t, output, "matched_keywords")
			require.Contains(t, output, `"success":true`)
		})
	}
}

func TestAccountTestQuestionTextPathWithoutPatternsRemainsManual(t *testing.T) {
	account := newAccountTestQuestionPathAccount(PlatformGemini, AccountTypeAPIKey, nil)
	upstream, requests := newAccountTestQuestionPathUpstream(t)
	svc := newAccountTestQuestionPathService(account, upstream)
	c, recorder := newTestContext()
	err := svc.TestAccountConnection(c, account.ID, "gemini-2.5-flash", "ignored-request-prompt", "", AccountTestOptions{
		Question: &model.AccountTestQuestion{
			ID: 39, Name: "Manual identity check", Content: accountTestQuestionPathPrompt, AnswerPatterns: []string{}, Enabled: true,
		},
	})
	require.NoError(t, err)
	require.Len(t, requests, 1)
	assertAccountTestQuestionPathBody(t, <-requests, accountTestQuestionPathExpectation{
		path: "/models/gemini-2.5-flash:streamGenerateContent", format: "gemini",
	})
	output := recorder.Body.String()
	require.Contains(t, output, "gemini answer")
	require.Equal(t, 1, strings.Count(output, `"type":"question_result"`))
	require.Equal(t, 1, strings.Count(output, `"type":"test_complete"`))
	events := decodeQuestionTestEvents(t, output)
	result := events[len(events)-2]
	require.Equal(t, "question_result", result.Get("type").String())
	require.True(t, result.Get("success").Bool())
	require.False(t, result.Get("data.judged").Bool())
	require.Equal(t, "[]", result.Get("data.answer_patterns").Raw)
	require.Equal(t, "[]", result.Get("data.matched_patterns").Raw)
	require.Equal(t, "test_complete", events[len(events)-1].Get("type").String())
	require.True(t, events[len(events)-1].Get("success").Bool())
}

func TestAccountTestQuestionRejectsUnsupportedAccountsAndMediaModels(t *testing.T) {
	tests := []struct {
		name     string
		platform string
		model    string
		mapped   string
		message  string
	}{
		{"video", PlatformVideo, "video-model", "", "Test questions are not supported for this account"},
		{"typesafe", PlatformTypeSafe, "claude-sonnet-4-6", "", "Test questions are not supported for this account"},
		{"openai image", PlatformOpenAI, "gpt-image-2", "", "Test questions require a text model"},
		{"openai image alias", PlatformOpenAI, "custom-image", "gpt-image-2", "Test questions require a text model"},
		{"grok image", PlatformGrok, "grok-imagine-image", "", "Test questions require a text model"},
		{"grok video", PlatformGrok, "grok-imagine-video", "", "Test questions require a text model"},
		{"grok video alias", PlatformGrok, "custom-video", "grok-imagine-video", "Test questions require a text model"},
		{"gemini image", PlatformGemini, "gemini-2.5-flash-image", "", "Test questions require a text model"},
		{"gemini image alias", PlatformGemini, "custom-image", "gemini-2.5-flash-image", "Test questions require a text model"},
		{"antigravity image", PlatformAntigravity, "gemini-3.1-flash-image", "", "Test questions require a text model"},
		{"antigravity image alias", PlatformAntigravity, "custom-image", "gemini-3.1-flash-image", "Test questions require a text model"},
		{"opencode image alias", PlatformOpenCodeGo, "custom-image", "gemini-2.5-flash-image", "Test questions require a text model"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			account := newAccountTestQuestionPathAccount(tc.platform, AccountTypeAPIKey, nil)
			if tc.mapped != "" {
				account.Credentials["model_mapping"] = map[string]any{tc.model: tc.mapped}
			}
			upstream, requests := newAccountTestQuestionPathUpstream(t)
			svc := newAccountTestQuestionPathService(account, upstream)
			c, recorder := newTestContext()
			err := svc.TestAccountConnection(c, account.ID, tc.model, "", "", AccountTestOptions{
				Question: &model.AccountTestQuestion{ID: 39, Content: accountTestQuestionPathPrompt, Enabled: true},
			})
			require.Error(t, err)
			require.Contains(t, recorder.Body.String(), tc.message)
			require.Contains(t, recorder.Body.String(), `"type":"error"`)
			require.NotContains(t, recorder.Body.String(), `"type":"question_result"`)
			require.Empty(t, requests, "unsupported questions must not reach any upstream")
		})
	}
}
