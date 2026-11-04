//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestScheduledQuestionErrorTextSanitizesEveryURL(t *testing.T) {
	text := `request https://user:password@provider.example/api/test?api_key=private-query#private-fragment failed; second http://other:pass@another.example/hooks/unknown-hook-token?private=1#hidden`
	detail := sanitizeScheduledQuestionErrorText(text, nil, 1000)
	require.Contains(t, detail, "https://provider.example/api/test")
	require.Contains(t, detail, "http://another.example/hooks/****")
	for _, sensitive := range []string{"user", "password", "private-query", "private-fragment", "other:pass", "unknown-hook-token", "private=1", "hidden"} {
		require.NotContains(t, detail, sensitive)
	}
	require.Contains(t, detail, "failed; second")
}

func TestScheduledQuestionErrorTextSanitizesKnownCredentialsBeforeTruncating(t *testing.T) {
	plan := &ScheduledQuestionTestPlan{FeishuWebhookURL: scheduledQuestionTestWebhook, FeishuSecret: "private-secret-very-sensitive"}
	for _, sensitive := range []string{plan.FeishuSecret, "token-sensitive-1234", plan.FeishuWebhookURL} {
		for _, limit := range []int{300, 500} {
			text := strings.Repeat("x", limit-10) + sensitive + " trailing reason"
			detail := sanitizeScheduledQuestionErrorText(text, plan, limit)
			require.LessOrEqual(t, utf8.RuneCountInString(detail), limit)
			require.NotContains(t, detail, "private-se", "must remove the full secret before the limit splits it")
			require.NotContains(t, detail, "token-sens", "must remove the full token before the limit splits it")
			require.NotContains(t, detail, sensitive)
		}
	}
	require.Equal(t, "[redacted] and [redacted]", sanitizeScheduledQuestionErrorText(plan.FeishuSecret+" and token-sensitive-1234", plan, 500))
}

func TestScheduledQuestionErrorTextMasksEncodedHookTokens(t *testing.T) {
	plan := &ScheduledQuestionTestPlan{FeishuWebhookURL: "https://open.feishu.cn/open-apis/bot/v2/hook/token%2Fsecret", FeishuSecret: "secret with spaces"}
	text := `URL https://name:password@provider.example/open-apis/bot/v2/hook/token%2Fsecret?secret=hidden#private token%2Fsecret secret+with+spaces`
	detail := sanitizeScheduledQuestionErrorText(text, plan, 500)
	for _, sensitive := range []string{"password", "token", "hidden", "private", "secret+with+spaces"} {
		require.NotContains(t, detail, sensitive)
	}
	require.Contains(t, detail, "/hook/****")
}

func TestScheduledQuestionErrorTextPreservesReasonAndUnicodeBounds(t *testing.T) {
	require.Equal(t, "upstream returned 503: service unavailable", sanitizeScheduledQuestionErrorText("upstream returned 503: service unavailable", nil, 500))
	require.Equal(t, "测试错误测", sanitizeScheduledQuestionErrorText(strings.Repeat("测试错误", 10), nil, 5))
	require.Empty(t, sanitizeScheduledQuestionErrorText("reason", nil, 0))
	require.Empty(t, sanitizeScheduledQuestionErrorText("reason", nil, -1))
	require.Equal(t, "request [redacted URL] failed", sanitizeScheduledQuestionErrorText("request https://%zz?private=value failed", nil, 500))
}

func TestScheduledQuestionReportErrorUsesSharedSanitizerBeforeLimit(t *testing.T) {
	plan := &ScheduledQuestionTestPlan{Name: "safe report", FeishuWebhookURL: scheduledQuestionTestWebhook, FeishuSecret: "private-secret-very-sensitive"}
	for _, secret := range []string{plan.FeishuSecret, "token-sensitive-1234", "https://user:password@provider.example/hook/unknown-token?query-secret=1#fragment-secret"} {
		run := &ScheduledQuestionTestRun{Status: "failed", ErrorMessage: strings.Repeat("x", 290) + secret + " tail"}
		notification := buildScheduledQuestionNotification("Site", plan, run, "report", nil, time.Now())
		body := strings.Join(notification.Lines, "\n")
		for _, sensitive := range []string{"private-se", "token-sens", "password", "unknown-token", "query-secret", "fragment-secret"} {
			require.NotContains(t, body, sensitive)
		}
		require.Contains(t, body, "Round error: "+sanitizeScheduledQuestionErrorText(run.ErrorMessage, plan, 300))
		require.Contains(t, run.ErrorMessage, secret, "run-level administrator error stays unchanged")
	}
}

func TestScheduledQuestionSummaryErrorUsesSharedSanitizerBeforeLimit(t *testing.T) {
	plan := &ScheduledQuestionTestPlan{AccountIDs: []int64{1}, QuestionIDs: []int64{1}, MinPassRatio: 80, FeishuWebhookURL: scheduledQuestionTestWebhook, FeishuSecret: "private-secret-very-sensitive"}
	for _, tc := range []struct {
		name, errorText string
		items           []AccountBatchTestItemResult
	}{
		{name: "item error", errorText: strings.Repeat("x", 490) + plan.FeishuSecret, items: []AccountBatchTestItemResult{{AccountID: 1, QuestionID: 1, Status: "error", Error: strings.Repeat("x", 490) + plan.FeishuSecret}}},
		{name: "round error", errorText: strings.Repeat("x", 490) + "token-sensitive-1234"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			summaries := summarizeScheduledQuestionTests(plan, tc.items, tc.errorText)
			require.Len(t, summaries, 1)
			require.LessOrEqual(t, utf8.RuneCountInString(summaries[0].Error), 500)
			require.NotContains(t, summaries[0].Error, "private-se")
			require.NotContains(t, summaries[0].Error, "token-sens")
		})
	}
}

func TestScheduledQuestionErrorURLBoundaries(t *testing.T) {
	const first = "https://first-user:first-password@one.example/path?first-query=1#first-fragment"
	const second = "https://second-user:second-password@two.example/api?second-query=1#second-fragment"
	for _, separator := range []string{"", ",", "，", "、", ";", "；", "\"", "'", "<", ">", "(", ")", "[", "]", "{", "}", " ", "\t", "\n", "\u3000", "\u00a0", "\u2003"} {
		t.Run(fmt.Sprintf("separator=%q", separator), func(t *testing.T) {
			text := "failure " + first + separator + second + " ended"
			require.Equal(t, "failure https://one.example/path"+separator+"https://two.example/api ended", sanitizeScheduledQuestionErrorText(text, nil, 1000))
		})
	}
	require.Equal(t, "https://one.example/pathhttps://two.example/api", sanitizeScheduledQuestionErrorText("https://one.example/pathhttps://second-user:second-password@two.example/api?key=secret", nil, 1000))
	noScheme := "provider.example/hook/UNKNOWN_TOKEN?key=private ordinary hook/path and user:password@example.com"
	require.Equal(t, noScheme, sanitizeScheduledQuestionErrorText(noScheme, nil, 1000))
}

func TestScheduledQuestionErrorEncodedHookPaths(t *testing.T) {
	for _, tc := range []struct{ path, expected string }{
		{path: "/%68ook/UNKNOWN_TOKEN", expected: "/hook/****"},
		{path: "/hook%2FUNKNOWN_TOKEN", expected: "/hook/****"},
		{path: "/%68%6f%6f%6b/UNKNOWN_TOKEN", expected: "/hook/****"},
		{path: "/%48oOk%2FUNKNOWN_TOKEN/extra/credentials", expected: "/HoOk/****"},
		{path: "/HOOKS/UNKNOWN_TOKEN/extra/credentials", expected: "/HOOKS/****"},
		{path: "/hooks/%E6%9C%AA%E7%9F%A5%E7%A7%98%E9%92%A5/extra", expected: "/hooks/****"},
		{path: "/%2568ook%252FUNKNOWN_TOKEN", expected: "/hook/****"},
		{path: "/%252568ook%25252FUNKNOWN_TOKEN", expected: "/hook/****"},
		{path: "/safe/%E6%B5%8B%E8%AF%95", expected: "/safe/测试"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			text := "request HTTPS://user:password@provider.example" + tc.path + "?private-query=1#private-fragment failed"
			detail := sanitizeScheduledQuestionErrorText(text, nil, 1000)
			require.Equal(t, "request https://provider.example"+tc.expected+" failed", detail)
			for _, sensitive := range []string{"UNKNOWN_TOKEN", "credentials", "未知秘钥", "private-query", "private-fragment", "password"} {
				require.NotContains(t, detail, sensitive)
			}
		})
	}
}

func TestScheduledQuestionErrorURLFixesReachSummaryHistoryAndReport(t *testing.T) {
	for _, text := range []string{
		"https://one.example/path,https://second-user:second-password@two.example/api?key=second-query#second-fragment",
		"https://one.example/path\u3000https://second-user:second-password@two.example/api?key=second-query",
		"https://one.example/pathhttps://second-user:second-password@two.example/api?key=second-query",
		"https://provider.example/%68ook/UNKNOWN_TOKEN?key=second-query",
		"https://provider.example/hook%2FUNKNOWN_TOKEN?key=second-query",
		"https://provider.example/%68%6f%6f%6b/UNKNOWN_TOKEN/credential-tail",
		"https://provider.example/%252568ook%25252FUNKNOWN_TOKEN/credential-tail",
	} {
		t.Run(text, func(t *testing.T) {
			svc, repo, _, _ := newScheduledQuestionFixture()
			defer svc.Stop()
			plan := scheduledQuestionValidPlan(1)
			plan.AccountIDs, plan.QuestionIDs = []int64{1}, []int64{1}
			repo.plans[1] = plan
			summary := summarizeScheduledQuestionTests(plan, []AccountBatchTestItemResult{{AccountID: 1, QuestionID: 1, Status: "error", Error: text}}, "")
			require.Len(t, summary, 1)
			repo.runs = []*ScheduledQuestionTestRun{{Status: "failed", ErrorMessage: text, Summary: []ScheduledQuestionTestAccountSummary{{AccountID: 1, Error: text}}}}
			history, err := svc.ListRuns(context.Background(), 1, 1)
			require.NoError(t, err)
			require.Len(t, history, 1)
			notification := buildScheduledQuestionNotification("Site", plan, repo.runs[0], "report", nil, time.Now())
			for _, result := range []string{summary[0].Error, history[0].Summary[0].Error, strings.Join(notification.Lines, "\n")} {
				for _, sensitive := range []string{"second-user", "second-password", "second-query", "second-fragment", "UNKNOWN_TOKEN", "credential-tail"} {
					require.NotContains(t, result, sensitive)
				}
			}
			payload, err := json.Marshal(struct {
				Summary []ScheduledQuestionTestAccountSummary `json:"summary"`
			}{Summary: history[0].Summary})
			require.NoError(t, err)
			require.NotContains(t, string(payload), "second-password")
			require.NotContains(t, string(payload), "UNKNOWN_TOKEN")
			require.Equal(t, text, history[0].ErrorMessage, "administrator round error keeps its existing contract")
		})
	}
}

func TestScheduledQuestionErrorTruncationPreservesMultibyteCharacters(t *testing.T) {
	plan := &ScheduledQuestionTestPlan{FeishuSecret: "未知私密签名密钥", FeishuWebhookURL: scheduledQuestionTestWebhook}
	for _, limit := range []int{300, 500} {
		text := strings.Repeat("测", limit-4) + plan.FeishuSecret + "https://user:password@provider.example/hook/未知令牌?private=secret"
		detail := sanitizeScheduledQuestionErrorText(text, plan, limit)
		require.True(t, utf8.ValidString(detail))
		require.Equal(t, limit, utf8.RuneCountInString(detail))
		require.Equal(t, strings.Repeat("测", limit-4)+"[red", detail)
		require.NotContains(t, detail, "未知")
		require.NotContains(t, detail, "password")
	}
}
