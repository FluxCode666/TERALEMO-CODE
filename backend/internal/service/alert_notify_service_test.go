//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type alertNotifySettingsFixture struct {
	SettingRepository
	stored string
	reads  int
}

func (r *alertNotifySettingsFixture) GetValue(_ context.Context, key string) (string, error) {
	if key == SettingKeyAlertNotifyChannels {
		r.reads++
		return r.stored, nil
	}
	return "", ErrSettingNotFound
}

func (r *alertNotifySettingsFixture) Set(_ context.Context, key, value string) error {
	if key != SettingKeyAlertNotifyChannels {
		return errors.New("unexpected setting")
	}
	r.stored = value
	return nil
}

type alertNotifyCipherFixture struct{}

func (alertNotifyCipherFixture) Encrypt(value string) (string, error) { return "cipher:" + value, nil }
func (alertNotifyCipherFixture) Decrypt(value string) (string, error) {
	if plaintext, ok := strings.CutPrefix(value, "cipher:"); ok {
		return plaintext, nil
	}
	return "", errors.New("not ciphertext")
}

type alertNotifyRoundTrip func(*http.Request) (*http.Response, error)

func (fn alertNotifyRoundTrip) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func alertNotifyInput() AlertNotifyChannelInput {
	return AlertNotifyChannelInput{Name: " 主渠道 ", Type: "webhook", Enabled: true,
		URL: "https://example.com/hook/topsecret1234?token=private#fragment", Secret: "fixture-secret",
		Events: []string{AlertNotifyEventOps, AlertNotifyEventOps, AlertNotifyEventQuota}}
}

func TestAlertNotifyReplaceAndCache(t *testing.T) {
	ctx := context.Background()
	repo := &alertNotifySettingsFixture{}
	svc := NewAlertNotifyService(repo, alertNotifyCipherFixture{}, nil)
	now := time.Unix(1700000000, 0)
	svc.now = func() time.Time { return now }
	view, err := svc.GetChannels(ctx)
	require.NoError(t, err)
	require.NotNil(t, view.Channels)
	require.Empty(t, view.Channels)
	_, err = svc.GetChannels(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, repo.reads)

	input := alertNotifyInput()
	view, err = svc.ReplaceChannels(ctx, []AlertNotifyChannelInput{input})
	require.NoError(t, err)
	first := view.Channels[0]
	require.Regexp(t, `^[0-9a-f]{16}$`, first.ID)
	require.Equal(t, "主渠道", first.Name)
	require.Equal(t, []string{AlertNotifyEventOps, AlertNotifyEventQuota}, first.Events)
	require.Equal(t, "https://example.com/hook/****1234", first.URLMasked)
	require.True(t, first.SecretConfigured)
	require.Equal(t, now.UTC(), first.UpdatedAt)
	view, err = svc.GetChannels(ctx)
	require.NoError(t, err)
	require.Equal(t, first, view.Channels[0])
	require.Equal(t, 2, repo.reads)
	now = now.Add(alertNotifyCacheTTL)
	_, err = svc.GetChannels(ctx)
	require.NoError(t, err)
	require.Equal(t, 3, repo.reads)

	old := repo.stored
	update := input
	update.ID, update.URL, update.Secret = first.ID, "", ""
	view, err = svc.ReplaceChannels(ctx, []AlertNotifyChannelInput{input, update})
	require.NoError(t, err)
	require.NotEqual(t, first.ID, view.Channels[0].ID)
	require.Equal(t, first.ID, view.Channels[1].ID)
	require.Equal(t, first.URLMasked, view.Channels[1].URLMasked)
	require.True(t, view.Channels[1].SecretConfigured)
	var original, saved []alertNotifyStoredChannel
	require.NoError(t, json.Unmarshal([]byte(old), &original))
	require.NoError(t, json.Unmarshal([]byte(repo.stored), &saved))
	require.Equal(t, original[0].URLEnc, saved[1].URLEnc)
	require.Equal(t, original[0].SecretEnc, saved[1].SecretEnc)

	update.URL = "https://example.com/new-destination"
	update.Secret = "replacement-secret"
	view, err = svc.ReplaceChannels(ctx, []AlertNotifyChannelInput{update})
	require.NoError(t, err)
	require.Equal(t, "https://example.com/****tion", view.Channels[0].URLMasked)
	require.Contains(t, repo.stored, "cipher:replacement-secret")
	update.ClearSecret = true
	view, err = svc.ReplaceChannels(ctx, []AlertNotifyChannelInput{update})
	require.NoError(t, err)
	require.False(t, view.Channels[0].SecretConfigured)
	require.NotContains(t, repo.stored, "secret_enc")
	view, err = svc.ReplaceChannels(ctx, nil)
	require.NoError(t, err)
	require.Empty(t, view.Channels)
	require.Equal(t, "[]", repo.stored)
}

func TestAlertNotifyUpdatedAtChangesOnlyForChangedChannels(t *testing.T) {
	ctx := t.Context()
	svc := NewAlertNotifyService(&alertNotifySettingsFixture{}, alertNotifyCipherFixture{}, nil)
	createdAt := time.Date(2026, 11, 22, 14, 0, 0, 0, time.UTC)
	now := createdAt
	svc.now = func() time.Time { return now }
	first, second := alertNotifyInput(), alertNotifyInput()
	second.Secret = ""
	created, err := svc.ReplaceChannels(ctx, []AlertNotifyChannelInput{first, second})
	require.NoError(t, err)
	first.ID, second.ID = created.Channels[0].ID, created.Channels[1].ID
	first.URL, first.Secret, second.URL = "", "", ""
	second.ClearSecret = true
	now = createdAt.Add(time.Hour)

	unchanged, err := svc.ReplaceChannels(ctx, []AlertNotifyChannelInput{second, first})
	require.NoError(t, err)
	require.Equal(t, created.Channels[1], unchanged.Channels[0])
	require.Equal(t, created.Channels[0], unchanged.Channels[1])

	first.Enabled = false
	changed, err := svc.ReplaceChannels(ctx, []AlertNotifyChannelInput{first, second})
	require.NoError(t, err)
	require.Equal(t, now, changed.Channels[0].UpdatedAt)
	require.Equal(t, createdAt, changed.Channels[1].UpdatedAt)
	changedAt := now
	now = now.Add(time.Hour)
	first.URL, first.Secret = alertNotifyInput().URL, alertNotifyInput().Secret
	resubmitted, err := svc.ReplaceChannels(ctx, []AlertNotifyChannelInput{first, second})
	require.NoError(t, err)
	require.Equal(t, changedAt, resubmitted.Channels[0].UpdatedAt)
	first.URL += "-changed"
	changed, err = svc.ReplaceChannels(ctx, []AlertNotifyChannelInput{first, second})
	require.NoError(t, err)
	require.Equal(t, now, changed.Channels[0].UpdatedAt)
	now = now.Add(time.Hour)
	first.URL, first.Secret = "", "replacement-key"
	changed, err = svc.ReplaceChannels(ctx, []AlertNotifyChannelInput{first, second})
	require.NoError(t, err)
	require.Equal(t, now, changed.Channels[0].UpdatedAt)
	now = now.Add(time.Hour)
	first.Secret, first.ClearSecret = "", true
	cleared, err := svc.ReplaceChannels(ctx, []AlertNotifyChannelInput{first, second})
	require.NoError(t, err)
	require.Equal(t, now, cleared.Channels[0].UpdatedAt)
	require.False(t, cleared.Channels[0].SecretConfigured)
	require.Equal(t, createdAt, cleared.Channels[1].UpdatedAt)
}

func TestAlertNotifyValidation(t *testing.T) {
	ctx := context.Background()
	svc := NewAlertNotifyService(&alertNotifySettingsFixture{}, alertNotifyCipherFixture{}, nil)
	for _, entry := range []struct {
		name   string
		modify func(*AlertNotifyChannelInput)
	}{
		{"unknown id", func(input *AlertNotifyChannelInput) { input.ID = "unknown" }},
		{"empty name", func(input *AlertNotifyChannelInput) { input.Name = " \t " }},
		{"long name", func(input *AlertNotifyChannelInput) { input.Name = strings.Repeat("测", 65) }},
		{"missing URL", func(input *AlertNotifyChannelInput) { input.URL = "" }},
		{"relative URL", func(input *AlertNotifyChannelInput) { input.URL = "//example.com/hook" }},
		{"userinfo", func(input *AlertNotifyChannelInput) { input.URL = "https://user:password@example.com" }},
		{"non-HTTP URL", func(input *AlertNotifyChannelInput) { input.URL = "ftp://example.com" }},
		{"long URL", func(input *AlertNotifyChannelInput) { input.URL += strings.Repeat("x", 2049) }},
		{"non-HTTPS feishu", func(input *AlertNotifyChannelInput) { input.Type, input.URL = "feishu", "http://example.com/hook" }},
		{"unknown type", func(input *AlertNotifyChannelInput) { input.Type = "email" }},
		{"long secret", func(input *AlertNotifyChannelInput) { input.Secret = strings.Repeat("s", 257) }},
		{"missing events", func(input *AlertNotifyChannelInput) { input.Events = nil }},
		{"unknown event", func(input *AlertNotifyChannelInput) { input.Events = []string{"custom"} }},
		{"unknown level", func(input *AlertNotifyChannelInput) { input.MinSeverity = "fatal" }},
	} {
		t.Run(entry.name, func(t *testing.T) {
			input := alertNotifyInput()
			entry.modify(&input)
			_, err := svc.ReplaceChannels(ctx, []AlertNotifyChannelInput{input})
			require.Error(t, err)
			if input.URL != "" {
				require.NotContains(t, err.Error(), input.URL)
			}
			require.NotContains(t, err.Error(), input.Secret)
		})
	}
	input := alertNotifyInput()
	input.URL = "http://example.com/hook"
	view, err := svc.ReplaceChannels(ctx, []AlertNotifyChannelInput{input})
	require.NoError(t, err)
	input.ID, input.URL = view.Channels[0].ID, ""
	_, err = svc.ReplaceChannels(ctx, []AlertNotifyChannelInput{input, input})
	require.ErrorContains(t, err, "duplicate id")
	input.Type = "feishu"
	_, err = svc.ReplaceChannels(ctx, []AlertNotifyChannelInput{input})
	require.ErrorContains(t, err, "requires an HTTPS URL")
	_, err = svc.ReplaceChannels(ctx, make([]AlertNotifyChannelInput, 21))
	require.ErrorContains(t, err, "at most 20")
}

func TestAlertNotifyURLMask(t *testing.T) {
	for raw, masked := range map[string]string{
		"https://example.com/hook/abcdefghijkl?q=secret#secret": "https://example.com/hook/****ijkl",
		"https://example.com/hook/abc/":                         "https://example.com/hook/****/",
		"https://example.com:8443?token=secret":                 "https://example.com:8443/****",
		"https://example.com/hook/a%2Fb%2Fc?secret":             "https://example.com/hook/****%2Fb%2Fc",
	} {
		require.Equal(t, masked, maskAlertNotifyURL(raw))
	}
}

func TestAlertNotifyFilteringAndRateLimit(t *testing.T) {
	channel := alertNotifyStoredChannel{alertNotifyChannelMetadata: alertNotifyChannelMetadata{
		Enabled: true, Events: []string{AlertNotifyEventOps}, MinSeverity: "warning",
	}}
	alert := AlertNotification{Event: AlertNotifyEventOps, Status: OpsAlertStatusFiring, Severity: "critical"}
	require.True(t, matchesAlertNotifyChannel(channel, alert))
	alert.Severity = "info"
	require.False(t, matchesAlertNotifyChannel(channel, alert))
	alert.Severity, alert.Status = "warning", OpsAlertStatusResolved
	require.False(t, matchesAlertNotifyChannel(channel, alert))
	channel.SendResolved = true
	require.True(t, matchesAlertNotifyChannel(channel, alert))
	alert.Event = AlertNotifyEventQuota
	require.False(t, matchesAlertNotifyChannel(channel, alert))
	alert.Event, channel.Enabled = AlertNotifyEventOps, false
	require.False(t, matchesAlertNotifyChannel(channel, alert))

	svc := NewAlertNotifyService(nil, nil, nil)
	now := time.Unix(1700000000, 0)
	svc.now = func() time.Time { return now }
	for range 30 {
		require.True(t, svc.allow("a"))
	}
	require.False(t, svc.allow("a"))
	require.True(t, svc.allow("b"))
	now = now.Add(time.Minute)
	require.True(t, svc.allow("a"))
}

func TestAlertNotifyDispatchRetriesOnlyTransientFailures(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		response int
		fail     bool
		attempts int
	}{
		{"network", 0, true, 2},
		{"server error", http.StatusInternalServerError, false, 2},
		{"client error", http.StatusTooManyRequests, false, 1},
		{"success", http.StatusOK, false, 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			requests := make(chan struct{}, 3)
			client := &http.Client{Transport: alertNotifyRoundTrip(func(*http.Request) (*http.Response, error) {
				requests <- struct{}{}
				if scenario.fail {
					return nil, errors.New("transport unavailable")
				}
				return &http.Response{StatusCode: scenario.response, Body: http.NoBody}, nil
			})}
			svc := NewAlertNotifyService(&alertNotifySettingsFixture{}, alertNotifyCipherFixture{}, client)
			_, err := svc.ReplaceChannels(context.Background(), []AlertNotifyChannelInput{alertNotifyInput()})
			require.NoError(t, err)
			svc.Dispatch(AlertNotification{Event: AlertNotifyEventOps, Severity: "info", Status: OpsAlertStatusFiring})
			require.Eventually(t, func() bool { return len(svc.batches) == 0 }, time.Second, time.Millisecond)
			require.Len(t, requests, scenario.attempts)
		})
	}
}

func TestAlertNotifyDispatchDropsWhenFullWithoutBlocking(t *testing.T) {
	svc := NewAlertNotifyService(nil, nil, nil)
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	for range cap(svc.batches) {
		svc.batches <- struct{}{}
	}
	returned := make(chan struct{})
	go func() {
		defer close(returned)
		svc.Dispatch(AlertNotification{
			Event: AlertNotifyEventOps, Source: AlertNotifySource{Type: "ops_alert", ID: "42"},
			Summary: "private-summary-canary",
		})
	}()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("dispatch blocked with all pending slots occupied")
	}
	require.Len(t, svc.batches, 128)
	require.Equal(t, 8, cap(svc.sends))
	var warning map[string]string
	require.NoError(t, json.Unmarshal(output.Bytes(), &warning))
	require.Equal(t, "WARN", warning["level"])
	require.Equal(t, AlertNotifyEventOps, warning["event"])
	require.Equal(t, "ops_alert", warning["source_type"])
	require.Equal(t, "42", warning["source_id"])
	require.NotContains(t, output.String(), "private-summary-canary")
}

func TestAlertNotifyFeishuSignatureAndResponse(t *testing.T) {
	require.Equal(t, "eSSBpPPdWfBl7avPl9BWSBfQOnLGZ91Xn8G5oZis0sc=", alertNotifyFeishuSignature("1700000000", "fixture-secret"))
	require.NoError(t, checkAlertNotifyFeishuResponse([]byte(`{"code":0}`)))
	require.NoError(t, checkAlertNotifyFeishuResponse([]byte(`{"StatusCode":0}`)))
	require.ErrorContains(t, checkAlertNotifyFeishuResponse([]byte(`{"code":19021,"StatusCode":0,"msg":"fixture-secret"}`)), "feishu returned code 19021")
	require.Error(t, checkAlertNotifyFeishuResponse([]byte(`{}`)))

	svc := NewAlertNotifyService(nil, nil, &http.Client{Transport: alertNotifyRoundTrip(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		require.Contains(t, string(body), `"template":"green"`)
		require.Contains(t, string(body), `【已恢复】内存告警`)
		require.Contains(t, string(body), `"msg_type":"interactive"`)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"code":19021,"msg":"fixture-secret"}`))}, nil
	})})
	retry, err := svc.send(context.Background(), alertNotifyEndpoint{typ: "feishu", url: "https://fixture.invalid/"}, AlertNotification{
		Status: OpsAlertStatusResolved, Severity: "critical", Title: "内存告警",
	})
	require.False(t, retry)
	require.ErrorContains(t, err, "feishu returned code 19021")
	require.NotContains(t, err.Error(), "fixture-secret")
}

func TestAlertNotifyFeishuBotErrorDetails(t *testing.T) {
	for response, message := range map[string]string{
		`{"code":19021}`:       "feishu returned code 19021: signature verification failed",
		`{"code":19022}`:       "feishu returned code 19022: request IP not in the bot allowlist",
		`{"StatusCode":19024}`: "feishu returned code 19024: message does not contain the bot's custom keyword",
	} {
		require.EqualError(t, checkAlertNotifyFeishuResponse([]byte(response)), message)
	}
}

func alertNotifyTestFeishuCard(t *testing.T, alert AlertNotification) (string, string) {
	t.Helper()
	raw, err := json.Marshal(buildAlertNotifyFeishuPayload(alert, "1700000000", ""))
	require.NoError(t, err)
	var payload struct {
		Card struct {
			Elements []struct {
				Text     struct{ Content string }
				Elements []struct{ Content string }
			}
		}
	}
	require.NoError(t, json.Unmarshal(raw, &payload))
	require.Len(t, payload.Card.Elements, 2)
	require.Len(t, payload.Card.Elements[1].Elements, 1)
	return payload.Card.Elements[0].Text.Content, payload.Card.Elements[1].Elements[0].Content
}

func TestAlertNotifyFeishuEscapesMarkdownValues(t *testing.T) {
	value := `<at user_id="all"></at> & detail`
	markdown, _ := alertNotifyTestFeishuCard(t, AlertNotification{
		Summary: value, Fields: []AlertNotifyField{{Label: "描述", Value: value}},
	})
	require.NotContains(t, markdown, "<at")
	require.NotContains(t, markdown, "</at>")
	require.Equal(t, 2, strings.Count(markdown, "&lt;at"))
	require.Equal(t, 2, strings.Count(markdown, "&amp; detail"))
	require.Contains(t, markdown, "**描述**：")
}

func TestAlertNotifyFeishuLocalTimeAndWebhookUTC(t *testing.T) {
	const layout = "2006-01-02 15:04:05 MST"
	firedAt := time.Date(2026, 11, 22, 14, 0, 0, 0, time.FixedZone("upstream", 5*60*60))
	resolvedAt := firedAt.Add(time.Minute)
	svc := &OpsAlertEvaluatorService{}
	capture := &alertNotifyCapture{}
	svc.SetAlertNotifier(capture)
	svc.dispatchOpsAlert(nil, &OpsAlertRule{Name: "test rule"}, &OpsAlertEvent{FiredAt: firedAt}, OpsAlertStatusResolved, resolvedAt, 0, "", nil)
	require.Len(t, capture.alerts, 1)
	alert := capture.alerts[0]
	alert.SiteName = "Test Site"
	markdown, note := alertNotifyTestFeishuCard(t, alert)
	require.Contains(t, markdown, "**触发时间**："+firedAt.In(time.Local).Format(layout))
	require.Equal(t, "Test Site · "+resolvedAt.In(time.Local).Format(layout), note)
	require.Equal(t, "2026-11-22T09:01:00Z", buildAlertNotifyWebhookPayload(alert).OccurredAt)
}

type alertNotifyCapture struct{ alerts []AlertNotification }

func (capture *alertNotifyCapture) Dispatch(alert AlertNotification) {
	capture.alerts = append(capture.alerts, alert)
}

func TestAlertNotifyOpsSilencing(t *testing.T) {
	svc := &OpsAlertEvaluatorService{}
	capture := &alertNotifyCapture{}
	svc.SetAlertNotifier(capture)
	now := time.Now().UTC()
	rule := &OpsAlertRule{ID: 1, Name: "测试规则", Severity: "P0", NotifyEmail: false}
	event := &OpsAlertEvent{ID: 2, RuleID: 1, Severity: "P0", FiredAt: now}
	config := &OpsAlertRuntimeSettings{Silencing: OpsAlertSilencingSettings{
		Enabled: true, GlobalUntilRFC3339: now.Add(time.Hour).Format(time.RFC3339),
	}}
	svc.dispatchOpsAlert(config, rule, event, OpsAlertStatusFiring, now, 42, "openai", nil)
	require.Empty(t, capture.alerts)
	svc.dispatchOpsAlert(nil, rule, event, OpsAlertStatusFiring, now, 42, "openai", nil)
	svc.dispatchOpsAlert(nil, rule, event, OpsAlertStatusResolved, now.Add(time.Second), 1, "openai", nil)
	require.Len(t, capture.alerts, 2)
	require.Equal(t, "critical", capture.alerts[0].Severity)
	require.Equal(t, "resolved", capture.alerts[1].Status)
	require.Equal(t, "2", capture.alerts[1].Source.ID)
}
