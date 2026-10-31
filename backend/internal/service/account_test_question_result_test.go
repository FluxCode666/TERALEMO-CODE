//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func decodeQuestionTestEvents(t *testing.T, body string) []gjson.Result {
	t.Helper()
	events := []gjson.Result{}
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "data: ") {
			text := strings.TrimPrefix(line, "data: ")
			require.True(t, gjson.Valid(text))
			events = append(events, gjson.Parse(text))
		}
	}
	return events
}

func TestAccountTestQuestionVerdict(t *testing.T) {
	for _, tc := range []struct {
		name     string
		patterns []string
		judged   bool
		success  bool
		matched  []string
	}{
		{name: "manual", success: true, matched: []string{}},
		{name: "match across chunks with flags", patterns: []string{"(?i)ANTHROPIC", "(?i)clau(de)", "absent"}, judged: true, success: true, matched: []string{"(?i)ANTHROPIC", "(?i)clau(de)"}},
		{name: "case sensitive by default", patterns: []string{"ANTHROPIC", "claude"}, judged: true, matched: []string{}},
		{name: "no matches", patterns: []string{"not-present"}, judged: true, matched: []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, recorder := newTestContext()
			question := &model.AccountTestQuestion{ID: 7, Name: "identity", Content: "identify yourself", AnswerPatterns: tc.patterns}
			c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), accountTestQuestionContextKey{}, &accountTestQuestionState{question: question}))
			svc := &AccountTestService{}
			svc.sendEvent(c, TestEvent{Type: "content", Text: "I am ClAu"})
			svc.sendEvent(c, TestEvent{Type: "content", Text: "de from Anthropic."})
			svc.sendEvent(c, TestEvent{Type: "test_complete", Success: true})
			events := decodeQuestionTestEvents(t, recorder.Body.String())
			require.Len(t, events, 4)
			require.Equal(t, "question_result", events[2].Get("type").String())
			require.Equal(t, tc.success, events[2].Get("success").Bool())
			require.True(t, events[2].Get("success").Exists(), "false must not be omitted")
			require.Equal(t, tc.judged, events[2].Get("data.judged").Bool())
			require.Equal(t, int64(7), events[2].Get("data.question_id").Int())
			require.Equal(t, "identity", events[2].Get("data.question_name").String())
			expected, err := json.Marshal(append([]string{}, tc.patterns...))
			require.NoError(t, err)
			require.JSONEq(t, string(expected), events[2].Get("data.answer_patterns").Raw)
			matched, err := json.Marshal(tc.matched)
			require.NoError(t, err)
			require.JSONEq(t, string(matched), events[2].Get("data.matched_patterns").Raw)
			require.Equal(t, "test_complete", events[3].Get("type").String())
			require.True(t, events[3].Get("success").Exists())
			require.Equal(t, tc.success, events[3].Get("success").Bool())
			if !tc.success {
				require.Equal(t, "Answer did not match any expected pattern", events[3].Get("error").String())
			}
		})
	}
}

func TestEvaluateAccountTestAnswerPatterns(t *testing.T) {
	for _, tc := range []struct {
		name     string
		answer   string
		patterns []string
		judged   bool
		matched  []string
	}{
		{"manual", "Claude", nil, false, []string{}},
		{"case sensitive", "Claude", []string{"claude"}, true, []string{}},
		{"case flag", "Claude", []string{"(?i)^claude$"}, true, []string{"(?i)^claude$"}},
		{"alternation and anchors", "MODEL: Claude-4", []string{`^MODEL: (Claude|GPT)-\d+$`, "absent"}, true, []string{`^MODEL: (Claude|GPT)-\d+$`}},
		{"dot excludes newline", "one\ntwo", []string{"one.two"}, true, []string{}},
		{"dotall flag", "one\ntwo", []string{"(?s)one.two"}, true, []string{"(?s)one.two"}},
		{"unicode and comma quantifier", "猫猫猫", []string{"^猫{2,3}$"}, true, []string{"^猫{2,3}$"}},
		{"all matches preserve order", "ABC", []string{"A", "(?i)b", "C"}, true, []string{"A", "(?i)b", "C"}},
		{"invalid persisted pattern", "anything", []string{"["}, true, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			judged, matched := EvaluateAccountTestAnswer(tc.answer, tc.patterns)
			require.Equal(t, tc.judged, judged)
			require.Equal(t, tc.matched, matched)
		})
	}
}

func TestAccountTestQuestionCompletionSuppressionAndFailure(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(fmt.Sprintf("failure=%t", failure), func(t *testing.T) {
			c, recorder := newTestContext()
			question := &model.AccountTestQuestion{ID: 7, AnswerPatterns: []string{"cross-chunk"}}
			c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), accountTestQuestionContextKey{}, &accountTestQuestionState{question: question}))
			svc := &AccountTestService{}
			c.Set(accountTestSuppressCompletionContextKey, true)
			svc.sendEvent(c, TestEvent{Type: "content", Text: "cross-"})
			svc.sendEvent(c, TestEvent{Type: "test_complete", Success: true})
			require.NotContains(t, recorder.Body.String(), "question_result")
			svc.sendEvent(c, TestEvent{Type: "content", Text: "chunk"})
			c.Set(accountTestSuppressCompletionContextKey, false)
			if failure {
				svc.sendEvent(c, TestEvent{Type: "error", Error: "connection failed"})
				svc.sendEvent(c, TestEvent{Type: "test_complete", Success: false})
				require.NotContains(t, recorder.Body.String(), "question_result")
			} else {
				svc.sendEvent(c, TestEvent{Type: "test_complete", Success: true})
				require.Equal(t, 1, strings.Count(recorder.Body.String(), `"type":"question_result"`))
				require.Contains(t, recorder.Body.String(), `"matched_patterns":["cross-chunk"]`)
			}
		})
	}
}

func TestAccountTestQuestionAdaptiveFinalVerdict(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(fmt.Sprintf("last endpoint fails=%t", failure), func(t *testing.T) {
			account := adaptiveCNAccountTestAccount(701, PlatformDeepseek)
			last := adaptiveCNResponsesTestResponse()
			if failure {
				last = newJSONResponse(503, `{"error":"upstream unavailable"}`)
			}
			svc, _ := adaptiveCNAccountTestService(account, adaptiveCNChatTestResponse(), adaptiveCNAnthropicTestResponse(), last)
			c, recorder := newTestContext()
			err := svc.TestAccountConnection(c, account.ID, "deepseek-chat", "ignored", "", AccountTestOptions{Question: &model.AccountTestQuestion{
				ID: 7, Name: "identity", Content: "identify yourself", AnswerPatterns: []string{"chat ok", "anthropic ok", "responses ok"},
			}})
			if failure {
				require.Error(t, err)
				require.NotContains(t, recorder.Body.String(), "question_result")
				require.NotContains(t, recorder.Body.String(), `"type":"test_complete"`)
			} else {
				require.NoError(t, err)
				events := decodeQuestionTestEvents(t, recorder.Body.String())
				require.Equal(t, 1, strings.Count(recorder.Body.String(), `"type":"question_result"`))
				require.Equal(t, 1, strings.Count(recorder.Body.String(), `"type":"test_complete"`))
				require.Len(t, events[len(events)-2].Get("data.matched_patterns").Array(), 3)
			}
		})
	}
}

// Golden JSON locks the pre-question payload shape and encoding, not merely its meaning.
func TestAccountTestDefaultPayloadBytes(t *testing.T) {
	c, _ := newTestContext()
	claude, err := createTestPayload("claude-test")
	require.NoError(t, err)
	metadata, ok := claude["metadata"].(map[string]string)
	require.True(t, ok)
	require.NotEmpty(t, metadata["user_id"])
	metadata["user_id"] = "<random-session>"
	applyAccountTestQuestion(c, claude, "messages")
	claudeBytes, err := json.Marshal(claude)
	require.NoError(t, err)
	require.Equal(t, `{"max_tokens":1024,"messages":[{"content":[{"cache_control":{"type":"ephemeral"},"text":"hi","type":"text"}],"role":"user"}],"metadata":{"user_id":"\u003crandom-session\u003e"},"model":"claude-test","stream":true,"system":[{"cache_control":{"type":"ephemeral"},"text":"You are Claude Code, Anthropic's official CLI for Claude.","type":"text"}],"temperature":1}`, string(claudeBytes))
	for _, oauth := range []bool{false, true} {
		payload := createOpenAITestPayload("gpt-test", oauth)
		applyAccountTestQuestion(c, payload, "input")
		actual, marshalErr := json.Marshal(payload)
		require.NoError(t, marshalErr)
		instructions, marshalErr := json.Marshal(openai.DefaultInstructions)
		require.NoError(t, marshalErr)
		store := ""
		if oauth {
			store = `,"store":false`
		}
		require.Equal(t, `{"input":[{"content":[{"text":"hi","type":"input_text"}],"role":"user"}],"instructions":`+string(instructions)+`,"model":"gpt-test"`+store+`,"stream":true}`, string(actual))
	}
	bedrock := createBedrockTestPayload()
	applyAccountTestQuestion(c, bedrock, "messages")
	bedrockBytes, err := json.Marshal(bedrock)
	require.NoError(t, err)
	require.Equal(t, `{"anthropic_version":"bedrock-2023-05-31","max_tokens":256,"messages":[{"content":[{"text":"hi","type":"text"}],"role":"user"}],"temperature":1}`, string(bedrockBytes))
	chat, err := json.Marshal(createOpenAIChatCompletionsTestPayload("gpt-test", ""))
	require.NoError(t, err)
	require.Equal(t, `{"messages":[{"content":"hi","role":"user"}],"model":"gpt-test","stream":true}`, string(chat))
	require.Equal(t, `{"contents":[{"parts":[{"text":"hi"}],"role":"user"}],"systemInstruction":{"parts":[{"text":"You are a helpful AI assistant."}]}}`, string(createGeminiTestPayload("gemini-2.5-flash", "")))
	grok, err := buildGrokQuotaProbeBody("grok-test")
	require.NoError(t, err)
	require.Equal(t, `{"input":"hi","model":"grok-test","stream":true}`, string(grok))
	for _, gemini := range []bool{false, true} {
		var body []byte
		svc := &AntigravityGatewayService{}
		if gemini {
			body, err = svc.buildGeminiTestRequestWithPrompt("project", "gemini-2.5-flash", "")
		} else {
			body, err = svc.buildClaudeTestRequest("project", "claude-sonnet-4-6")
		}
		require.NoError(t, err)
		unchanged, applyErr := applyAntigravityTestQuestion(c.Request.Context(), body)
		require.NoError(t, applyErr)
		require.Equal(t, body, unchanged, "without a question even random request IDs must remain byte-identical")
		require.Equal(t, ".", gjson.GetBytes(body, "request.contents.0.parts.0.text").String())
		require.Equal(t, int64(1), gjson.GetBytes(body, "request.generationConfig.maxOutputTokens").Int())
	}
}

func TestAntigravityTestQuestionOutputBudget(t *testing.T) {
	for _, tc := range []struct {
		model  string
		gemini bool
	}{
		{model: "claude-sonnet-4-6"},
		{model: "claude-opus-4-6"},
		{model: "gemini-2.5-flash", gemini: true},
		{model: "gemini-3.1-pro-high", gemini: true},
	} {
		t.Run(tc.model, func(t *testing.T) {
			svc := &AntigravityGatewayService{}
			var body []byte
			var err error
			if tc.gemini {
				body, err = svc.buildGeminiTestRequestWithPrompt("project", tc.model, "")
			} else {
				body, err = svc.buildClaudeTestRequest("project", tc.model)
			}
			require.NoError(t, err)
			require.Equal(t, int64(1), gjson.GetBytes(body, "request.generationConfig.maxOutputTokens").Int())
			question := &model.AccountTestQuestion{Content: "Identify yourself"}
			ctx := context.WithValue(context.Background(), accountTestQuestionContextKey{}, &accountTestQuestionState{question: question})
			patchedBody, err := applyAntigravityTestQuestion(ctx, body)
			require.NoError(t, err)
			require.Equal(t, int64(4096), gjson.GetBytes(patchedBody, "request.generationConfig.maxOutputTokens").Int())
			require.Equal(t, question.Content, gjson.GetBytes(patchedBody, "request.contents.0.parts.0.text").String())
			require.False(t, gjson.GetBytes(patchedBody, "request.max_tokens").Exists(), "Claude max_tokens is converted, not retained as a second limit")

			var original, patched map[string]any
			require.NoError(t, json.Unmarshal(body, &original))
			require.NoError(t, json.Unmarshal(patchedBody, &patched))
			originalRequest, ok := original["request"].(map[string]any)
			require.True(t, ok)
			patchedRequest, ok := patched["request"].(map[string]any)
			require.True(t, ok)
			originalConfig, ok := originalRequest["generationConfig"].(map[string]any)
			require.True(t, ok)
			patchedConfig, ok := patchedRequest["generationConfig"].(map[string]any)
			require.True(t, ok)
			patchedRequest["contents"] = originalRequest["contents"]
			patchedConfig["maxOutputTokens"] = originalConfig["maxOutputTokens"]
			require.Equal(t, original, patched, "only user content and the question output budget may change")
		})
	}
}
