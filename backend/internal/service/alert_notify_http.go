package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"
)

const alertNotifyResponseLimit = 64 << 10

func formatAlertNotifyLocalTime(at time.Time) string {
	return at.In(time.Local).Format("2006-01-02 15:04:05 MST")
}

type alertNotifyHTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

func rejectAlertNotifyRedirect(_ *http.Request, _ []*http.Request) error {
	return http.ErrUseLastResponse
}

func (s *AlertNotifyService) send(ctx context.Context, endpoint alertNotifyEndpoint, alert AlertNotification) (retry bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, alertNotifyTimeout)
	defer cancel()
	timestamp := strconv.FormatInt(s.now().Unix(), 10)
	var body []byte
	if endpoint.typ == alertNotifyFeishu {
		body, err = json.Marshal(buildAlertNotifyFeishuPayload(alert, timestamp, endpoint.secret))
	} else {
		body, err = json.Marshal(buildAlertNotifyWebhookPayload(alert))
	}
	if err != nil {
		return false, errors.New("could not encode notification")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.url, bytes.NewReader(body))
	if err != nil {
		return false, errors.New("request failed: invalid channel URL")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "AlertNotify/1.0")
	if endpoint.typ == alertNotifyWebhook {
		req.Header.Set("X-Webhook-Event", alert.Event)
		req.Header.Set("X-Webhook-Timestamp", timestamp)
		if endpoint.secret != "" {
			req.Header.Set("X-Webhook-Signature", alertNotifyWebhookSignature(timestamp, endpoint.secret, body))
		}
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return alertNotifyRequestError(err)
	}
	defer func() { _ = resp.Body.Close() }()
	limited := io.LimitReader(resp.Body, alertNotifyResponseLimit)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, limited)
		return resp.StatusCode >= 500 && resp.StatusCode < 600, fmt.Errorf("%s returned HTTP %d", endpoint.typ, resp.StatusCode)
	}
	if endpoint.typ == alertNotifyWebhook {
		_, _ = io.Copy(io.Discard, limited)
		return false, nil
	}
	response, err := io.ReadAll(limited)
	if err != nil {
		return alertNotifyRequestError(err)
	}
	return false, checkAlertNotifyFeishuResponse(response)
}

func alertNotifyRequestError(err error) (bool, error) {
	var dnsErr *net.DNSError
	var networkErr *net.OpError
	switch {
	case errors.Is(err, context.Canceled):
		return false, errors.New("request failed: canceled")
	case errors.Is(err, context.DeadlineExceeded):
		return true, errors.New("request failed: timed out")
	case errors.As(err, &dnsErr):
		return true, errors.New("request failed: could not resolve the target")
	case errors.As(err, &networkErr):
		return true, errors.New("request failed: network connection error")
	default:
		return true, errors.New("request failed: target unreachable or blocked by outbound policy")
	}
}

type alertNotifyWebhookPayload struct {
	Version    string             `json:"version"`
	Event      string             `json:"event"`
	Status     string             `json:"status"`
	Severity   string             `json:"severity"`
	Title      string             `json:"title"`
	Summary    string             `json:"summary"`
	Fields     []AlertNotifyField `json:"fields"`
	OccurredAt string             `json:"occurred_at"`
	SiteName   string             `json:"site_name"`
	Source     AlertNotifySource  `json:"source"`
	Test       bool               `json:"test"`
}

func buildAlertNotifyWebhookPayload(alert AlertNotification) alertNotifyWebhookPayload {
	fields := alert.Fields
	if fields == nil {
		fields = []AlertNotifyField{}
	}
	return alertNotifyWebhookPayload{
		Version: "1", Event: alert.Event, Status: alert.Status, Severity: alert.Severity, Title: alert.Title,
		Summary: alert.Summary, Fields: fields, OccurredAt: alert.OccurredAt.UTC().Format(time.RFC3339),
		SiteName: alert.SiteName, Source: alert.Source, Test: alert.Test,
	}
}

func alertNotifyWebhookSignature(timestamp, secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = io.WriteString(mac, timestamp+".")
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func alertNotifyFeishuSignature(timestamp, secret string) string {
	mac := hmac.New(sha256.New, []byte(timestamp+"\n"+secret))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func buildAlertNotifyFeishuPayload(alert AlertNotification, timestamp, secret string) map[string]any {
	color, title := "blue", "【告警】"+alert.Title
	switch alert.Severity {
	case "critical":
		color = "red"
	case "warning":
		color = "orange"
	}
	if alert.Status == OpsAlertStatusResolved {
		color, title = "green", "【已恢复】"+alert.Title
	}
	if alert.Test {
		color, title = "turquoise", "【测试】告警通知渠道连通性测试"
	}
	markdown := html.EscapeString(alert.Summary)
	for _, field := range alert.Fields {
		markdown += "\n**" + field.Label + "**：" + html.EscapeString(field.Value)
	}
	card := map[string]any{
		"header": map[string]any{
			"template": color,
			"title":    map[string]string{"tag": "plain_text", "content": title},
		},
		"elements": []map[string]any{
			{"tag": "div", "text": map[string]string{"tag": "lark_md", "content": markdown}},
			{"tag": "note", "elements": []map[string]string{{"tag": "plain_text", "content": alert.SiteName + " · " + formatAlertNotifyLocalTime(alert.OccurredAt)}}},
		},
	}
	payload := map[string]any{"msg_type": "interactive", "card": card}
	if secret != "" {
		payload["timestamp"] = timestamp
		payload["sign"] = alertNotifyFeishuSignature(timestamp, secret)
	}
	return payload
}

func checkAlertNotifyFeishuResponse(body []byte) error {
	var result struct {
		Code       *int `json:"code"`
		StatusCode *int `json:"StatusCode"`
	}
	if json.Unmarshal(body, &result) != nil || (result.Code == nil && result.StatusCode == nil) {
		return errors.New("feishu returned an invalid response")
	}
	code := result.Code
	if code == nil {
		code = result.StatusCode
	}
	switch *code {
	case 0:
		return nil
	case 19021:
		return errors.New("feishu returned code 19021: signature verification failed")
	case 19022:
		return errors.New("feishu returned code 19022: request IP not in the bot allowlist")
	case 19024:
		return errors.New("feishu returned code 19024: message does not contain the bot's custom keyword")
	default:
		return fmt.Errorf("feishu returned code %d", *code)
	}
}
