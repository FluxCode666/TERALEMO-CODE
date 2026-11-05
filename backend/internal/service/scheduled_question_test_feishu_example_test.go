//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestScheduledQuestionFeishuTestNotificationUsesReportTableSchema(t *testing.T) {
	now := time.Date(2026, time.November, 5, 12, 0, 0, 0, time.Local)
	notification := scheduledQuestionFeishuTestNotification("Example site", now)
	require.Contains(t, notification.Title, "Test notification")
	require.Equal(t, "blue", notification.Color)
	require.Contains(t, notification.Lines, "Example site")
	require.Contains(t, notification.Lines, now.Format("2006-01-02 15:04:05 -07:00"))
	require.Contains(t, strings.Join(notification.Lines, "\n"), "示例数据")
	require.Contains(t, strings.Join(notification.Lines, "\n"), "不会执行实际测试或修改账号调度")
	require.Len(t, notification.Rows, 2)
	data, err := scheduledQuestionFeishuPayload(notification, "", now)
	require.NoError(t, err)
	require.LessOrEqual(t, len(data), scheduledQuestionFeishuPayloadLimit)
	var message scheduledQuestionFeishuMessage
	require.NoError(t, json.Unmarshal(data, &message))
	require.Equal(t, "interactive", message.MsgType)
	require.Len(t, message.Card.Elements, len(notification.Lines)+1)
	table := message.Card.Elements[len(notification.Lines)]
	require.Equal(t, "table", table.Tag)
	require.Len(t, table.Rows, 2)
	require.Equal(t, scheduledQuestionFeishuTable(nil).Columns, table.Columns, "test card must use the report's exact seven columns")
	require.Equal(t, []scheduledQuestionFeishuOption{{Text: "通过", Color: "green"}}, table.Rows[0].Result)
	require.Equal(t, []scheduledQuestionFeishuOption{{Text: "已恢复", Color: "green"}}, table.Rows[0].Action)
	require.Equal(t, "3/3 (100%)", table.Rows[0].Ratio)
	require.Equal(t, "关", table.Rows[0].Before)
	require.Equal(t, "开", table.Rows[0].After)
	require.Equal(t, []scheduledQuestionFeishuOption{{Text: "未通过", Color: "red"}}, table.Rows[1].Result)
	require.Equal(t, []scheduledQuestionFeishuOption{{Text: "已暂停", Color: "red"}}, table.Rows[1].Action)
	require.Equal(t, "1/3 (33%)", table.Rows[1].Ratio)
	for _, row := range table.Rows {
		require.Contains(t, row.Account, "示例账号")
		require.Equal(t, "示例数据", row.Note)
	}
}

func TestScheduledQuestionFeishuExamplePayloadKeepsNamesPlainAndCredentialsPrivate(t *testing.T) {
	now := time.Unix(1599360473, 0)
	const secret = "test-signing-secret-sensitive"
	unsafe := `<at id=all> **site**`
	notifier := newFeishuWebhookNotifier()
	notifier.now = func() time.Time { return now }
	notifier.client.Transport = scheduledQuestionFeishuRoundTripper(func(req *http.Request) (*http.Response, error) {
		data, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		for _, sensitive := range []string{scheduledQuestionTestWebhook, "token-sensitive-1234", secret} {
			require.NotContains(t, string(data), sensitive)
		}
		require.NotContains(t, string(data), "lark_md")
		require.NotContains(t, string(data), `"data_type":"markdown"`)
		var message scheduledQuestionFeishuMessage
		require.NoError(t, json.Unmarshal(data, &message))
		require.Equal(t, scheduledQuestionFeishuSign("1599360473", secret), message.Sign)
		for _, element := range message.Card.Elements {
			if element.Tag == "div" {
				require.Equal(t, "plain_text", element.Text.Tag)
			}
		}
		require.Contains(t, message.Card.Elements[0].Text.Content, unsafe)
		require.Len(t, message.Card.Elements[len(message.Card.Elements)-1].Rows, 2)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"code":0}`))}, nil
	})
	notification := scheduledQuestionFeishuTestNotification(unsafe+" "+scheduledQuestionTestWebhook, now)
	sent, detail := notifier.Send(context.Background(), scheduledQuestionTestWebhook, secret, notification)
	require.True(t, sent)
	require.Equal(t, "sent", detail)
}

func TestScheduledQuestionFeishuExampleLargeSiteNamePreservesBothRows(t *testing.T) {
	now := time.Now()
	notification := scheduledQuestionFeishuTestNotification(strings.Repeat("站点<&>", 10000), now)
	data, err := scheduledQuestionFeishuPayload(notification, "secret", now)
	require.NoError(t, err)
	require.LessOrEqual(t, len(data), scheduledQuestionFeishuPayloadLimit)
	var message scheduledQuestionFeishuMessage
	require.NoError(t, json.Unmarshal(data, &message))
	require.Len(t, message.Card.Elements[len(message.Card.Elements)-1].Rows, 2)
	require.NotContains(t, string(data), "个账号未列出")
}
