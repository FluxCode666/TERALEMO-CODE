//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/stretchr/testify/require"
)

const scheduledQuestionTestWebhook = "https://open.feishu.cn/open-apis/bot/v2/hook/token-sensitive-1234"

type scheduledQuestionFeishuRoundTripper func(*http.Request) (*http.Response, error)

func (f scheduledQuestionFeishuRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestScheduledQuestionFeishuWebhookValidation(t *testing.T) {
	for _, tc := range []struct {
		name, url string
		valid     bool
	}{
		{name: "empty allowed", valid: true},
		{name: "feishu", url: scheduledQuestionTestWebhook, valid: true},
		{name: "lark", url: "https://open.larksuite.com/open-apis/bot/v2/hook/token", valid: true},
		{name: "http", url: "http://open.feishu.cn/open-apis/bot/v2/hook/token"},
		{name: "host case", url: "https://OPEN.FEISHU.CN/open-apis/bot/v2/hook/token"},
		{name: "port", url: "https://open.feishu.cn:443/open-apis/bot/v2/hook/token"},
		{name: "userinfo", url: "https://user@open.feishu.cn/open-apis/bot/v2/hook/token"},
		{name: "host suffix", url: "https://open.feishu.cn.attacker.test/open-apis/bot/v2/hook/token"},
		{name: "query", url: scheduledQuestionTestWebhook + "?a=b"},
		{name: "empty query", url: scheduledQuestionTestWebhook + "?"},
		{name: "fragment", url: scheduledQuestionTestWebhook + "#secret"},
		{name: "empty fragment", url: scheduledQuestionTestWebhook + "#"},
		{name: "missing token", url: "https://open.feishu.cn/open-apis/bot/v2/hook/"},
		{name: "blank token", url: "https://open.feishu.cn/open-apis/bot/v2/hook/%20"},
		{name: "wrong path", url: "https://open.feishu.cn/other/token"},
		{name: "parse error", url: "https://%zz"},
		{name: "too long", url: scheduledQuestionTestWebhook + strings.Repeat("a", 500)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateScheduledQuestionFeishuWebhook(tc.url)
			if tc.valid {
				require.NoError(t, err)
				return
			}
			var validation *model.ValidationError
			require.ErrorAs(t, err, &validation)
			require.Equal(t, "feishu_webhook_url", validation.Field)
			require.NotContains(t, err.Error(), "token-sensitive")
		})
	}
}

func TestScheduledQuestionFeishuSignatureOfficialExample(t *testing.T) {
	// Official example: https://open.feishu.cn/document/client-docs/bot-v3/add-custom-bot
	// Java example uses secret="demo", timestamp=1599360473; HMAC signs empty bytes.
	require.Equal(t, "l1N0gAcBjdwBvGm1xMjOF0XSyaLRpR7tuO5dHfhAYc8=", scheduledQuestionFeishuSign("1599360473", "demo"))
}

func TestScheduledQuestionFeishuClientDefaults(t *testing.T) {
	notifier := newFeishuWebhookNotifier()
	require.Equal(t, 10*time.Second, notifier.client.Timeout)
	require.ErrorIs(t, notifier.client.CheckRedirect(nil, nil), http.ErrUseLastResponse)
	require.NotNil(t, notifier.client.Transport)
}

func TestScheduledQuestionFeishuSignedPlainTextCard(t *testing.T) {
	notifier := newFeishuWebhookNotifier()
	notifier.now = func() time.Time { return time.Unix(1599360473, 0) }
	notifier.client.Transport = scheduledQuestionFeishuRoundTripper(func(req *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodPost, req.Method)
		require.Equal(t, scheduledQuestionTestWebhook, req.URL.String())
		require.Equal(t, "application/json", req.Header.Get("Content-Type"))
		var message scheduledQuestionFeishuMessage
		require.NoError(t, json.NewDecoder(req.Body).Decode(&message))
		require.Equal(t, "interactive", message.MsgType)
		require.Equal(t, "1599360473", message.Timestamp)
		require.Equal(t, "l1N0gAcBjdwBvGm1xMjOF0XSyaLRpR7tuO5dHfhAYc8=", message.Sign)
		require.Equal(t, "plain_text", message.Card.Header.Title.Tag)
		require.Equal(t, `<at id=all> **plan**`, message.Card.Header.Title.Content)
		require.Equal(t, "orange", message.Card.Header.Template)
		require.Len(t, message.Card.Elements, 1)
		require.Equal(t, "plain_text", message.Card.Elements[0].Text.Tag)
		require.Equal(t, `<a href="evil">**account**</a>`, message.Card.Elements[0].Text.Content)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"code":0}`))}, nil
	})
	sent, detail := notifier.Send(context.Background(), scheduledQuestionTestWebhook, "demo", scheduledQuestionNotification{
		Title: `<at id=all> **plan**`, Color: "orange", Lines: []string{`<a href="evil">**account**</a>`},
	})
	require.True(t, sent)
	require.Equal(t, "sent", detail)
}

func TestScheduledQuestionFeishuPayloadBounds(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		count      int
	}{
		{name: "fifty account cap", text: "account", count: 75},
		{name: "byte cap", text: strings.Repeat("测试账号", 1000), count: 20},
		{name: "large metadata", text: strings.Repeat("&<>", 10000), count: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			notification := scheduledQuestionNotification{Title: "report", Color: "green", DetailStart: 1, Lines: []string{"metadata"}}
			if tc.count == 0 {
				notification.Title, notification.Lines[0] = tc.text, tc.text
			}
			for i := 0; i < tc.count; i++ {
				notification.Rows = append(notification.Rows, scheduledQuestionNotificationRow{
					Account: tc.text, Result: "通过", ResultColor: "green", Ratio: "1/1 (100%)", Before: "开", After: "开", Action: "—", ActionColor: "neutral",
				})
			}
			data, err := scheduledQuestionFeishuPayload(notification, "secret", time.Unix(1, 0))
			require.NoError(t, err)
			require.LessOrEqual(t, len(data), scheduledQuestionFeishuPayloadLimit)
			require.True(t, utf8.Valid(data))
			var message scheduledQuestionFeishuMessage
			require.NoError(t, json.Unmarshal(data, &message))
			if tc.count == 75 {
				require.Len(t, message.Card.Elements, 3)
				require.Equal(t, "table", message.Card.Elements[1].Tag)
				require.Len(t, message.Card.Elements[1].Rows, 50)
				require.Equal(t, "另有 25 个账号未列出", message.Card.Elements[2].Text.Content)
			}
			if tc.count == 20 {
				require.Contains(t, message.Card.Elements[len(message.Card.Elements)-1].Text.Content, "个账号未列出")
			}
		})
	}
}

func TestScheduledQuestionFeishuTableSchemaAndOptions(t *testing.T) {
	unsafe := `<at id=all> **[account](https://evil.test)**`
	rows := []scheduledQuestionNotificationRow{
		{Account: unsafe, Result: "通过", ResultColor: "green", Ratio: "1/1 (100%)", Before: "关", After: "开", Action: "已恢复", ActionColor: "green", Note: unsafe},
		{Result: "未通过", ResultColor: "red", Action: "已暂停", ActionColor: "red"},
		{Result: "需人工", ResultColor: "orange", Action: "其它计划未通过，未恢复", ActionColor: "orange"},
		{Result: "已跳过", ResultColor: "neutral", Action: "—", ActionColor: "neutral"},
	}
	data, err := scheduledQuestionFeishuPayload(scheduledQuestionNotification{Title: "report", Color: "green", Rows: rows}, "secret", time.Unix(1, 0))
	require.NoError(t, err)
	var message scheduledQuestionFeishuMessage
	require.NoError(t, json.Unmarshal(data, &message))
	require.Len(t, message.Card.Elements, 1)
	table := message.Card.Elements[0]
	require.Equal(t, "table", table.Tag)
	require.Nil(t, table.Text, "table must not contain a div text field")
	require.Equal(t, 10, table.PageSize)
	require.Equal(t, "middle", table.RowHeight)
	require.True(t, table.FreezeFirstColumn)
	require.Equal(t, "grey", table.HeaderStyle.BackgroundStyle)
	require.True(t, table.HeaderStyle.Bold)
	require.Equal(t, 1, table.HeaderStyle.Lines)
	require.Len(t, table.Columns, 7)
	for i, column := range table.Columns {
		if i == 1 || i == 5 {
			require.Equal(t, "options", column.DataType)
		} else {
			require.Equal(t, "text", column.DataType)
		}
	}
	require.NotContains(t, string(data), "lark_md")
	require.NotContains(t, string(data), `"data_type":"markdown"`)
	require.Equal(t, unsafe, table.Rows[0].Account)
	require.Equal(t, unsafe, table.Rows[0].Note)
	for i, color := range []string{"green", "red", "orange", "neutral"} {
		require.Equal(t, color, table.Rows[i].Result[0].Color)
		require.Equal(t, rows[i].Result, table.Rows[i].Result[0].Text)
		require.Equal(t, color, table.Rows[i].Action[0].Color)
	}
}

func TestScheduledQuestionFeishuResponsesAreSafe(t *testing.T) {
	for _, tc := range []struct {
		name, body, detail string
		status             int
		success            bool
	}{
		{name: "new response", status: 200, body: `{"code":0}`, success: true, detail: "sent"},
		{name: "legacy response", status: 200, body: `{"StatusCode":0}`, success: true, detail: "sent"},
		{name: "new code wins", status: 200, body: `{"code":19021,"StatusCode":0,"msg":"token-sensitive-1234 demo"}`, detail: "feishu code=19021 msg=signature verification failed"},
		{name: "unrecognized failure", status: 200, body: `{"code":42,"msg":"Authorization Bearer secret body"}`, detail: "feishu code=42 msg=request rejected"},
		{name: "HTTP error", status: 502, body: `secret upstream response`, detail: "http 502"},
		{name: "redirect", status: 302, body: `secret redirect`, detail: "http 302"},
		{name: "invalid JSON", status: 200, body: `secret upstream response`, detail: "invalid feishu response"},
		{name: "missing code", status: 200, body: `{}`, detail: "invalid feishu response"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			notifier := newFeishuWebhookNotifier()
			notifier.client.Transport = scheduledQuestionFeishuRoundTripper(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})
			sent, detail := notifier.Send(context.Background(), scheduledQuestionTestWebhook, "demo", scheduledQuestionNotification{Title: "test", Color: "green"})
			require.Equal(t, tc.success, sent)
			require.Equal(t, tc.detail, detail)
			require.NotContains(t, detail, "token-sensitive")
			require.NotContains(t, detail, "secret")
		})
	}
}

func TestScheduledQuestionFeishuTransportErrorOmitsWebhook(t *testing.T) {
	for _, tc := range []struct {
		err    error
		detail string
	}{
		{err: context.DeadlineExceeded, detail: "timeout"},
		{err: context.Canceled, detail: "request canceled"},
		{err: errors.New("dial tcp: connection refused " + scheduledQuestionTestWebhook), detail: "connection refused"},
		{err: errors.New("demo token-sensitive-1234 provider body"), detail: "connection failed"},
	} {
		notifier := newFeishuWebhookNotifier()
		notifier.client.Transport = scheduledQuestionFeishuRoundTripper(func(*http.Request) (*http.Response, error) {
			return nil, &url.Error{Op: "Post", URL: scheduledQuestionTestWebhook, Err: tc.err}
		})
		sent, detail := notifier.Send(context.Background(), scheduledQuestionTestWebhook, "demo", scheduledQuestionNotification{Title: "test"})
		require.False(t, sent)
		require.Equal(t, tc.detail, detail)
		require.NotContains(t, detail, "token-sensitive-1234")
		require.NotContains(t, detail, "demo")
	}
}

func TestScheduledQuestionFeishuInvalidStoredURLIsNotSent(t *testing.T) {
	notifier := newFeishuWebhookNotifier()
	notifier.client.Transport = scheduledQuestionFeishuRoundTripper(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid destination reached transport")
		return nil, errors.New("unexpected transport")
	})
	sent, detail := notifier.Send(context.Background(), "https://attacker.test/hook/token", "secret", scheduledQuestionNotification{})
	require.False(t, sent)
	require.Equal(t, "invalid feishu webhook URL", detail)
}

func TestScheduledQuestionNotificationDetailRejectsSensitiveOrUnrecognizedText(t *testing.T) {
	for _, text := range []string{
		"Post " + scheduledQuestionTestWebhook + ": secret-value token-sensitive-1234 upstream body",
		strings.Repeat("敏感", 200),
		"upstream message with no recognized credentials",
	} {
		detail := sanitizeScheduledQuestionNotificationDetail(text, scheduledQuestionTestWebhook, "secret-value")
		require.Equal(t, "notification failed", detail)
		require.LessOrEqual(t, utf8.RuneCountInString(detail), 200)
	}
	for _, text := range []string{"sent", "sent 2/3 recipients", "sent 0/1 recipients (timeout)", "sent 1/2 recipients (aborted)", "sent 0/1 recipients (connection failed)", "sent 0/1 recipients (auth failed)", "sent 0/1 recipients (rejected)", "timeout", "http 502", "feishu code=19021 msg=signature verification failed"} {
		require.Equal(t, text, sanitizeScheduledQuestionNotificationDetail(text, scheduledQuestionTestWebhook, "secret-value"))
	}
}
