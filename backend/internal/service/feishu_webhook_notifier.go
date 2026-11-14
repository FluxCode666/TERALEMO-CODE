package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/pkg/outbound"
)

const scheduledQuestionFeishuPayloadLimit = 20 * 1024

type scheduledQuestionFeishuNotifier interface {
	Send(context.Context, string, string, scheduledQuestionNotification) (bool, string)
}

type scheduledQuestionNotification struct {
	Title       string
	Color       string
	Lines       []string
	DetailStart int
	Rows        []scheduledQuestionNotificationRow
}

type feishuWebhookNotifier struct {
	client *http.Client
	now    func() time.Time
}

func newFeishuWebhookNotifier() *feishuWebhookNotifier {
	return &feishuWebhookNotifier{
		client: &http.Client{
			Timeout:       10 * time.Second,
			Transport:     outbound.WrapTransport(outbound.NewTransport()),
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		now: time.Now,
	}
}

func validateScheduledQuestionFeishuWebhook(raw string) error {
	if raw == "" {
		return nil
	}
	invalid := func() error {
		return scheduledQuestionValidation("feishu_webhook_url", "invalid feishu webhook URL")
	}
	if utf8.RuneCountInString(raw) > 500 {
		return invalid()
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || strings.Contains(raw, "#") {
		return invalid()
	}
	if parsed.Host != "open.feishu.cn" && parsed.Host != "open.larksuite.com" {
		return invalid()
	}
	const prefix = "/open-apis/bot/v2/hook/"
	if !strings.HasPrefix(parsed.Path, prefix) || strings.TrimSpace(strings.TrimPrefix(parsed.Path, prefix)) == "" {
		return invalid()
	}
	return nil
}

func scheduledQuestionFeishuSign(timestamp, secret string) string {
	mac := hmac.New(sha256.New, []byte(timestamp+"\n"+secret))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

type scheduledQuestionFeishuText struct {
	Tag     string `json:"tag"`
	Content string `json:"content"`
}

type scheduledQuestionFeishuElement struct {
	Tag               string                              `json:"tag"`
	Text              *scheduledQuestionFeishuText        `json:"text,omitempty"`
	PageSize          int                                 `json:"page_size,omitempty"`
	RowHeight         string                              `json:"row_height,omitempty"`
	FreezeFirstColumn bool                                `json:"freeze_first_column,omitempty"`
	HeaderStyle       *scheduledQuestionFeishuHeaderStyle `json:"header_style,omitempty"`
	Columns           []scheduledQuestionFeishuColumn     `json:"columns,omitempty"`
	Rows              []scheduledQuestionFeishuRow        `json:"rows,omitempty"`
}

type scheduledQuestionFeishuHeaderStyle struct {
	TextAlign       string `json:"text_align"`
	TextSize        string `json:"text_size"`
	BackgroundStyle string `json:"background_style"`
	TextColor       string `json:"text_color"`
	Bold            bool   `json:"bold"`
	Lines           int    `json:"lines"`
}

type scheduledQuestionFeishuColumn struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	DataType    string `json:"data_type"`
	Width       string `json:"width"`
}

type scheduledQuestionFeishuOption struct {
	Text  string `json:"text"`
	Color string `json:"color"`
}

type scheduledQuestionFeishuRow struct {
	Account string                          `json:"account"`
	Result  []scheduledQuestionFeishuOption `json:"result"`
	Ratio   string                          `json:"ratio"`
	Before  string                          `json:"before"`
	After   string                          `json:"after"`
	Action  []scheduledQuestionFeishuOption `json:"action"`
	Note    string                          `json:"note"`
}

type scheduledQuestionFeishuMessage struct {
	Timestamp string `json:"timestamp,omitempty"`
	Sign      string `json:"sign,omitempty"`
	MsgType   string `json:"msg_type"`
	Card      struct {
		Header struct {
			Title    scheduledQuestionFeishuText `json:"title"`
			Template string                      `json:"template"`
		} `json:"header"`
		Elements []scheduledQuestionFeishuElement `json:"elements"`
	} `json:"card"`
}

func scheduledQuestionFeishuPayload(notification scheduledQuestionNotification, secret string, now time.Time) ([]byte, error) {
	payload := scheduledQuestionFeishuMessage{MsgType: "interactive"}
	if secret != "" {
		payload.Timestamp = strconv.FormatInt(now.Unix(), 10)
		payload.Sign = scheduledQuestionFeishuSign(payload.Timestamp, secret)
	}
	payload.Card.Header.Title = scheduledQuestionFeishuText{Tag: "plain_text", Content: notification.Title}
	payload.Card.Header.Template = notification.Color
	lines := append([]string{}, notification.Lines...)
	details := min(50, len(notification.Rows))
	omitted := len(notification.Rows) - details
	for {
		payload.Card.Elements = make([]scheduledQuestionFeishuElement, 0, len(lines)+2)
		for _, line := range lines {
			payload.Card.Elements = append(payload.Card.Elements, scheduledQuestionFeishuElement{
				Tag: "div", Text: &scheduledQuestionFeishuText{Tag: "plain_text", Content: line},
			})
		}
		if details > 0 {
			payload.Card.Elements = append(payload.Card.Elements, scheduledQuestionFeishuTable(notification.Rows[:details]))
		}
		if omitted > 0 {
			payload.Card.Elements = append(payload.Card.Elements, scheduledQuestionFeishuElement{
				Tag: "div", Text: &scheduledQuestionFeishuText{Tag: "plain_text", Content: fmt.Sprintf("另有 %d 个账号未列出", omitted)},
			})
		}
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshal feishu card: %w", err)
		}
		if len(data) <= scheduledQuestionFeishuPayloadLimit {
			return data, nil
		}
		if details > 0 {
			details--
			omitted++
			continue
		}
		// Site names and round-level errors can also be large; keep the card bounded.
		longest, length := -1, utf8.RuneCountInString(payload.Card.Header.Title.Content)
		for i, line := range lines {
			if count := utf8.RuneCountInString(line); count > length {
				longest, length = i, count
			}
		}
		if length == 0 {
			return nil, errors.New("feishu card exceeds payload limit")
		}
		if longest == -1 {
			payload.Card.Header.Title.Content = truncateScheduledQuestionText(payload.Card.Header.Title.Content, length/2)
		} else {
			lines[longest] = truncateScheduledQuestionText(lines[longest], length/2)
		}
	}
}

func scheduledQuestionFeishuTable(rows []scheduledQuestionNotificationRow) scheduledQuestionFeishuElement {
	// JSON 1.0 table schema: https://open.feishu.cn/document/feishu-cards/card-components/content-components/table
	table := scheduledQuestionFeishuElement{
		Tag: "table", PageSize: 10, RowHeight: "middle", FreezeFirstColumn: true,
		HeaderStyle: &scheduledQuestionFeishuHeaderStyle{TextAlign: "left", TextSize: "normal", BackgroundStyle: "grey", TextColor: "default", Bold: true, Lines: 1},
		Columns: []scheduledQuestionFeishuColumn{
			{Name: "account", DisplayName: "账号", DataType: "text", Width: "160px"},
			{Name: "result", DisplayName: "结果", DataType: "options", Width: "100px"},
			{Name: "ratio", DisplayName: "通过/判定", DataType: "text", Width: "120px"},
			{Name: "before", DisplayName: "测试前调度", DataType: "text", Width: "100px"},
			{Name: "after", DisplayName: "测试后调度", DataType: "text", Width: "100px"},
			{Name: "action", DisplayName: "本轮动作", DataType: "options", Width: "220px"},
			{Name: "note", DisplayName: "说明", DataType: "text", Width: "280px"},
		},
		Rows: make([]scheduledQuestionFeishuRow, 0, len(rows)),
	}
	for _, row := range rows {
		table.Rows = append(table.Rows, scheduledQuestionFeishuRow{
			Account: row.Account, Result: []scheduledQuestionFeishuOption{{Text: row.Result, Color: row.ResultColor}},
			Ratio: row.Ratio, Before: row.Before, After: row.After,
			Action: []scheduledQuestionFeishuOption{{Text: row.Action, Color: row.ActionColor}}, Note: row.Note,
		})
	}
	return table
}

func (n *feishuWebhookNotifier) Send(ctx context.Context, webhook, secret string, notification scheduledQuestionNotification) (bool, string) {
	webhook, secret = strings.TrimSpace(webhook), strings.TrimSpace(secret)
	if err := validateScheduledQuestionFeishuWebhook(webhook); err != nil || webhook == "" {
		return false, "invalid feishu webhook URL"
	}
	if n == nil || n.client == nil {
		return false, "feishu service unavailable"
	}
	now := time.Now()
	if n.now != nil {
		now = n.now()
	}
	payload, err := scheduledQuestionFeishuPayload(notification, secret, now)
	if err != nil {
		return false, "feishu card exceeds payload limit"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhook, bytes.NewReader(payload))
	if err != nil {
		return false, "invalid feishu request"
	}
	req.Header.Set("Content-Type", "application/json")
	// URL validation restricts the destination to the two exact official robot hosts.
	resp, err := n.client.Do(req) //nolint:gosec // Destination is validated above; redirects are disabled.
	if err != nil {
		return false, scheduledQuestionFeishuTransportDetail(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return false, fmt.Sprintf("http %d", resp.StatusCode)
	}
	var result struct {
		Code       *int `json:"code"`
		StatusCode *int `json:"StatusCode"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&result); err != nil {
		return false, "invalid feishu response"
	}
	code := result.Code
	if code == nil {
		code = result.StatusCode
	}
	if code == nil {
		return false, "invalid feishu response"
	}
	if *code == 0 {
		return true, "sent"
	}
	message := "request rejected"
	switch *code {
	case 19021:
		message = "signature verification failed"
	case 19022:
		message = "IP not allowed"
	case 19024:
		message = "required keyword missing"
	case 9499:
		message = "invalid request"
	}
	return false, fmt.Sprintf("feishu code=%d msg=%s", *code, message)
}

func scheduledQuestionFeishuTransportDetail(err error) string {
	var urlErr *url.Error
	for errors.As(err, &urlErr) {
		err = urlErr.Err
	}
	if errors.Is(err, context.Canceled) {
		return "request canceled"
	}
	var networkError net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &networkError) && networkError.Timeout()) {
		return "timeout"
	}
	if strings.Contains(strings.ToLower(err.Error()), "connection refused") {
		return "connection refused"
	}
	return "connection failed"
}

var scheduledQuestionSensitiveURL = regexp.MustCompile(`(?i)(?:https?://|open\.feishu\.cn/|open\.larksuite\.com/)[^\s<>"']+`)
var scheduledQuestionSafeNotificationDetail = regexp.MustCompile(`^(?:sent(?: [0-9]+/[0-9]+ recipients(?: \((?:timeout|aborted|connection failed|auth failed|rejected)\))?)?|http [0-9]{3}|feishu code=-?[0-9]+ msg=(?:request rejected|signature verification failed|IP not allowed|required keyword missing|invalid request)|no verified recipients|email (?:service|recipients) unavailable|feishu service unavailable|invalid feishu (?:webhook URL|request|response)|feishu card exceeds payload limit|request canceled|timeout|aborted|connection refused|connection failed|notification failed)$`)

func sanitizeScheduledQuestionNotificationDetail(detail, webhook, secret string) string {
	for _, sensitive := range []string{webhook, secret} {
		if sensitive != "" {
			detail = strings.ReplaceAll(detail, sensitive, "[redacted]")
		}
	}
	if parsed, err := url.Parse(webhook); err == nil {
		token := strings.TrimPrefix(parsed.Path, "/open-apis/bot/v2/hook/")
		if token != "" && token != parsed.Path {
			detail = strings.ReplaceAll(detail, token, "[redacted]")
			if encoded := url.PathEscape(token); encoded != token {
				detail = strings.ReplaceAll(detail, encoded, "[redacted]")
			}
		}
	}
	detail = scheduledQuestionSensitiveURL.ReplaceAllString(detail, "[redacted]")
	detail = strings.Join(strings.Fields(detail), " ")
	// Custom implementations and test doubles must not echo upstream response bodies.
	if !scheduledQuestionSafeNotificationDetail.MatchString(detail) {
		return "notification failed"
	}
	return truncateScheduledQuestionText(detail, 200)
}

func truncateScheduledQuestionText(text string, limit int) string {
	text = strings.ToValidUTF8(text, "�")
	if limit <= 0 {
		return ""
	}
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	return string([]rune(text)[:limit])
}
